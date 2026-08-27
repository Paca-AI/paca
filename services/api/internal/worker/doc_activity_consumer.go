package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/events"
)

const (
	docActivityConsumerGroup = "api.doc_activity_writer"
	docActivityReadBlock     = 5 * time.Second
	docActivityReadCount     = 50
)

// DocActivityConsumer reads doc-activity events from the StreamDocActivities
// Valkey stream and persists each entry to the database via ActivityRepository.
//
// The actor_id stored in the stream is the authenticated user's UUID.  The
// consumer resolves it to the corresponding project_members.id via memberRepo
// before writing to the DB (since doc_activities.actor_id references
// project_members, not users).
//
// Comment operations (AddComment / UpdateComment / DeleteComment) write to the
// database directly, so they are NOT handled here.
type DocActivityConsumer struct {
	client       *redis.Client
	repo         docdom.ActivityRepository
	memberRepo   projectdom.MemberRepository
	log          *slog.Logger
	consumerName string
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewDocActivityConsumer creates a consumer that is ready to be started.
func NewDocActivityConsumer(client *redis.Client, repo docdom.ActivityRepository, memberRepo projectdom.MemberRepository, log *slog.Logger) *DocActivityConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &DocActivityConsumer{
		client:       client,
		repo:         repo,
		memberRepo:   memberRepo,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", docActivityConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed, then begins reading from the
// stream in a background goroutine.  Call Stop to drain and exit cleanly.
func (c *DocActivityConsumer) Start(ctx context.Context) {
	// "0": see ActivityConsumer.Start's identical comment.
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Warn("doc activity consumer: could not create consumer group, will retry on first read", "err", err)
	}

	go c.run()
}

// ensureGroup creates the consumer group at startID if it doesn't already
// exist — see ActivityConsumer.ensureGroup's doc comment for why startID
// differs between Start's first-ever creation ("0") and the NOGROUP
// recovery path in run() below ("$").
func (c *DocActivityConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamDocActivities, docActivityConsumerGroup, startID).Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Stop signals the consumer to stop and waits for the goroutine to exit.
func (c *DocActivityConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

// run is the main loop executed in a goroutine by Start.
func (c *DocActivityConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("doc activity consumer: started", "stream", events.StreamDocActivities)

	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("doc activity consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), docActivityReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    docActivityConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamDocActivities, ">"},
			Count:    docActivityReadCount,
			Block:    docActivityReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			c.log.Error("doc activity consumer: xreadgroup error", "err", err)
			if strings.Contains(err.Error(), "NOGROUP") {
				// "$", not "0": see ActivityConsumer's identical recovery
				// comment — this handler has no event-level dedup either.
				recoverCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				geErr := c.ensureGroup(recoverCtx, "$")
				cancel()
				if geErr != nil {
					c.log.Warn("doc activity consumer: failed to recreate consumer group", "err", geErr)
				}
			}
			time.Sleep(2 * time.Second)
			continue
		}

		for _, stream := range msgs {
			for _, msg := range stream.Messages {
				c.handle(msg)
			}
		}
	}
}

// processPending re-delivers and acknowledges any messages in the PEL that
// were not acked during a previous run.
func (c *DocActivityConsumer) processPending(ctx context.Context) {
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    docActivityConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{events.StreamDocActivities, "0"},
		Count:    docActivityReadCount,
	}).Result()
	if err != nil && err != redis.Nil {
		c.log.Warn("doc activity consumer: could not read pending messages", "err", err)
		return
	}
	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			c.handle(msg)
		}
	}
}

// handle deserialises one stream message and writes the activity to the DB.
func (c *DocActivityConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	raw, ok := msg.Values["payload"].(string)
	if !ok {
		c.log.Warn("doc activity consumer: message has no payload field", "id", msg.ID)
		c.ack(ctx, msg.ID)
		return
	}

	var p docActivityStreamPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		c.log.Warn("doc activity consumer: failed to decode payload", "id", msg.ID, "err", err)
		c.ack(ctx, msg.ID)
		return
	}

	a, err := p.toActivity()
	if err != nil {
		c.log.Warn("doc activity consumer: invalid payload fields", "id", msg.ID, "err", err)
		c.ack(ctx, msg.ID)
		return
	}

	// Skip entries that have no valid document_id (e.g. folder-level events
	// recorded before this pattern was established).  Inserting uuid.Nil would
	// violate the doc_activities.document_id FK constraint.
	if a.DocumentID == uuid.Nil {
		c.log.Warn("doc activity consumer: skipping entry with nil document_id", "id", msg.ID)
		c.ack(ctx, msg.ID)
		return
	}

	// Resolve actor → project_members.id so that doc_activities.actor_id
	// correctly references the project_members table.
	// FindMemberByActor picks agent lookup or user lookup based on agentID.
	if p.ProjectID != "" {
		projectID, pErr := uuid.Parse(p.ProjectID)
		if pErr == nil {
			var actorID uuid.UUID
			if a.ActorID != nil {
				actorID = *a.ActorID
			}
			var agentID *uuid.UUID
			if p.ActorAgentID != nil && *p.ActorAgentID != "" {
				if id, aErr := uuid.Parse(*p.ActorAgentID); aErr == nil {
					agentID = &id
				}
			}
			if agentID != nil || a.ActorID != nil {
				member, mErr := c.memberRepo.FindMemberByActor(ctx, projectID, actorID, agentID)
				if mErr == nil {
					a.ActorID = &member.ID
				} else {
					// actorID is userdom.SystemActorUserID for requests
					// authenticated with the shared agent API key but no
					// X-Agent-ID header — that identity is never itself a
					// project member by design, so ErrMemberNotFound is
					// expected there, not a bug; only warn when a genuine
					// actor can't be resolved to a member, or when the
					// lookup failed for some other (unexpected) reason.
					expected := userdom.IsUnidentifiedSystemActor(actorID, agentID) && errors.Is(mErr, projectdom.ErrMemberNotFound)
					if !expected {
						c.log.Warn("doc activity consumer: could not resolve member for actor", "actor_id", a.ActorID, "agent_id", p.ActorAgentID, "project_id", projectID, "err", mErr)
					}
					a.ActorID = nil
				}
			}
		}
	}

	if err := c.repo.CreateActivity(ctx, a); err != nil {
		c.log.Error("doc activity consumer: failed to persist activity", "id", msg.ID, "err", err)
		return
	}

	c.ack(ctx, msg.ID)
}

func (c *DocActivityConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamDocActivities, docActivityConsumerGroup, id).Err(); err != nil {
		c.log.Warn("doc activity consumer: xack failed", "id", id, "err", err)
	}
}

// docActivityStreamPayload mirrors the JSON shape produced by activityPayload()
// in activity_service.go.
type docActivityStreamPayload struct {
	ID           string  `json:"id"`
	DocumentID   string  `json:"document_id"`
	ProjectID    string  `json:"project_id"`
	ActorID      *string `json:"actor_id"`
	ActorAgentID *string `json:"actor_agent_id"`
	ActivityType string  `json:"activity_type"`
	Content      string  `json:"content"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func (p docActivityStreamPayload) toActivity() (*docdom.Activity, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, fmt.Errorf("parse id: %w", err)
	}
	documentID, err := uuid.Parse(p.DocumentID)
	if err != nil {
		return nil, fmt.Errorf("parse document_id: %w", err)
	}
	var actorID *uuid.UUID
	if p.ActorID != nil && *p.ActorID != "" {
		aid, err := uuid.Parse(*p.ActorID)
		if err != nil {
			return nil, fmt.Errorf("parse actor_id: %w", err)
		}
		actorID = &aid
	}
	content := json.RawMessage(p.Content)
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}
	updatedAt := createdAt
	if p.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, p.UpdatedAt); err == nil {
			updatedAt = t
		}
	}
	return &docdom.Activity{
		ID:           id,
		DocumentID:   documentID,
		ActorID:      actorID,
		ActivityType: docdom.ActivityType(p.ActivityType),
		Content:      content,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}, nil
}
