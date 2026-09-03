// Package worker contains long-running background workers for the API service.
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

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/events"
)

const (
	activityConsumerGroup = "api.activity_writer"
	activityReadBlock     = 5 * time.Second
	activityReadCount     = 50
)

// ActivityConsumer reads task-activity events from the StreamTaskActivities
// Valkey stream (written by ActivitySvc.RecordActivity) and persists each
// entry to the database via ActivityRepository.
//
// The actor_id stored in the stream is the authenticated user's UUID.  The
// consumer resolves it to the corresponding project_members.id via memberRepo
// before writing to the DB (since task_activities.actor_id references
// project_members, not users).
//
// Comment operations (AddComment / UpdateComment / DeleteComment) write to the
// database directly, so they are NOT handled here.
type ActivityConsumer struct {
	client       *redis.Client
	repo         taskdom.ActivityRepository
	memberRepo   projectdom.MemberRepository
	log          *slog.Logger
	consumerName string // unique per instance, derived from hostname
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewActivityConsumer creates a consumer that is ready to be started.
// The consumer name is derived from the hostname so it is unique per pod/instance.
// If hostname retrieval fails, a random UUID suffix is used as fallback.
func NewActivityConsumer(client *redis.Client, repo taskdom.ActivityRepository, memberRepo projectdom.MemberRepository, log *slog.Logger) *ActivityConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &ActivityConsumer{
		client:       client,
		repo:         repo,
		memberRepo:   memberRepo,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", activityConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed, then begins reading from the
// stream in a background goroutine.  Call Stop to drain and exit cleanly.
func (c *ActivityConsumer) Start(ctx context.Context) {
	// "0" means start from the very beginning of the stream so this
	// first-ever creation processes any messages that arrived before the
	// group existed.
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Warn("activity consumer: could not create consumer group, will retry on first read", "err", err)
	}

	go c.run()
}

// ensureGroup creates the consumer group at startID if it doesn't already
// exist. MKSTREAM ensures the stream key is created if it doesn't exist
// yet. startID is "0" only for Start's own first-ever creation (see doc
// comment there) — the NOGROUP recovery path in run() below passes "$"
// instead, since recreating at "0" there would redeliver the stream's
// entire retained history (including entries already processed and acked
// before the group vanished) through handlers with no event-level
// deduplication.
func (c *ActivityConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamTaskActivities, activityConsumerGroup, startID).Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Stop signals the consumer to stop and waits for the goroutine to exit.
func (c *ActivityConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

// run is the main loop executed in a goroutine by Start.
func (c *ActivityConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("activity consumer: started", "stream", events.StreamTaskActivities)

	// On startup, replay any pending messages (PEL) that were delivered but
	// never acknowledged (e.g. after a crash).  "0" fetches the backlog.
	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("activity consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), activityReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    activityConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamTaskActivities, ">"},
			Count:    activityReadCount,
			Block:    activityReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				// Timeout with no new messages — loop and check stopCh.
				continue
			}
			c.log.Error("activity consumer: xreadgroup error", "err", err)
			if strings.Contains(err.Error(), "NOGROUP") {
				// The stream and/or consumer group vanished out from under
				// an already-running consumer (e.g. a Valkey restart
				// without persistence, or the group's initial creation at
				// Start failing transiently) — without this, every
				// subsequent XReadGroup call keeps failing the exact same
				// way forever, since nothing else recreates the group.
				// Recreate at "$" (from now), not "0": unlike Start's own
				// first-ever creation, the stream itself may still hold
				// this consumer's own already-processed history (e.g. a
				// manual XGROUP DESTROY that left the stream intact), and
				// this handler has no event-level dedup to make replaying
				// that safe.
				recoverCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				geErr := c.ensureGroup(recoverCtx, "$")
				cancel()
				if geErr != nil {
					c.log.Warn("activity consumer: failed to recreate consumer group", "err", geErr)
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
func (c *ActivityConsumer) processPending(ctx context.Context) {
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    activityConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{events.StreamTaskActivities, "0"},
		Count:    activityReadCount,
	}).Result()
	if err != nil && err != redis.Nil {
		c.log.Warn("activity consumer: could not read pending messages", "err", err)
		return
	}
	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			c.handle(msg)
		}
	}
}

// handle deserialises one stream message and writes the activity to the DB.
func (c *ActivityConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	// The Publisher.Append method stores the body in a "payload" field as a
	// JSON-encoded string.
	raw, ok := msg.Values["payload"].(string)
	if !ok {
		c.log.Warn("activity consumer: message has no payload field", "id", msg.ID)
		c.ack(ctx, msg.ID) // skip unrecognised messages
		return
	}

	var p activityStreamPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		c.log.Warn("activity consumer: failed to decode payload", "id", msg.ID, "err", err)
		c.ack(ctx, msg.ID)
		return
	}

	a, err := p.toActivity()
	if err != nil {
		c.log.Warn("activity consumer: invalid payload fields", "id", msg.ID, "err", err)
		c.ack(ctx, msg.ID)
		return
	}

	// Resolve actor → project_members.id so that task_activities.actor_id
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
						c.log.Warn("activity consumer: could not resolve member for actor", "actor_id", a.ActorID, "agent_id", p.ActorAgentID, "project_id", projectID, "err", mErr)
					}
					// Member may have been removed; store nil rather than a stale UUID.
					a.ActorID = nil
				}
			}
		}
	}

	if err := c.repo.CreateActivity(ctx, a); err != nil {
		// Log and do NOT ack — the message stays in the PEL and will be
		// retried on next startup via processPending.
		c.log.Error("activity consumer: failed to persist activity", "id", msg.ID, "err", err)
		return
	}

	c.ack(ctx, msg.ID)
}

func (c *ActivityConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamTaskActivities, activityConsumerGroup, id).Err(); err != nil {
		c.log.Warn("activity consumer: xack failed", "id", id, "err", err)
	}
}

// activityStreamPayload mirrors the JSON shape produced by activityPayload()
// in activity_service.go.
type activityStreamPayload struct {
	ID           string  `json:"id"`
	TaskID       string  `json:"task_id"`
	ProjectID    string  `json:"project_id"`
	ActorID      *string `json:"actor_id"`
	ActorAgentID *string `json:"actor_agent_id"`
	ActivityType string  `json:"activity_type"`
	Content      string  `json:"content"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func (p activityStreamPayload) toActivity() (*taskdom.Activity, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, fmt.Errorf("parse id: %w", err)
	}
	taskID, err := uuid.Parse(p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("parse task_id: %w", err)
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
		// Fallback: accept the Go default format used by time.Time.MarshalJSON.
		createdAt, err = time.Parse(`"2006-01-02T15:04:05.999999999Z07:00"`, p.CreatedAt)
		if err != nil {
			createdAt = time.Now()
		}
	}
	updatedAt := createdAt
	if p.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, p.UpdatedAt); err == nil {
			updatedAt = t
		}
	}
	return &taskdom.Activity{
		ID:           id,
		TaskID:       taskID,
		ActorID:      actorID,
		ActivityType: taskdom.ActivityType(p.ActivityType),
		Content:      content,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}, nil
}
