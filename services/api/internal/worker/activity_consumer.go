// Package worker contains long-running background workers for the API service.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/events"
)

const (
	activityConsumerGroup = "api.activity_writer"
	activityReadBlock     = 5 * time.Second
	activityReadCount     = 50
)

// activityIDNamespace seeds the row ID derived from a stream message ID for
// events whose payload carries no "id" of its own, so a message redelivered
// after a crash between insert and ack maps to the same row and the insert's
// ON CONFLICT drops it.
var activityIDNamespace = uuid.MustParse("5b0f5a9e-4c1e-4a57-9d2f-7f1c6b3e8a41")

// ActivityConsumer is the single writer of the activities table. It reads
// every entry on StreamActivities — task, doc, sprint, view, automation,
// environment and member events alike — and persists each one.
//
// It is one peer listener among several on that stream (see events.Fanout):
// it reads only the envelope fields Fanout writes as siblings of "payload"
// (project_id, entity_type, entity_id, actor_id, actor_agent_id, origin), so
// it never has to know any one domain's payload shape.
//
// The actor in the envelope is a user or agent UUID; the consumer resolves it
// to project_members.id, which is what activities.actor_id references.
//
// Comments are inserted directly by their service, because the request
// returns the row. Their stream copy carries the same "id", so the insert
// here conflicts and is dropped.
type ActivityConsumer struct {
	client       *redis.Client
	repo         activitydom.Repository
	memberRepo   projectdom.MemberRepository
	log          *slog.Logger
	consumerName string // unique per instance, derived from hostname
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewActivityConsumer creates a consumer that is ready to be started.
// The consumer name is derived from the hostname so it is unique per pod/instance.
// If hostname retrieval fails, a random UUID suffix is used as fallback.
func NewActivityConsumer(client *redis.Client, repo activitydom.Repository, memberRepo projectdom.MemberRepository, log *slog.Logger) *ActivityConsumer {
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
// yet. startID is "0" only for Start's own first-ever creation — the NOGROUP
// recovery path in run() passes "$" instead, since recreating at "0" there
// would redeliver the stream's entire retained history.
func (c *ActivityConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamActivities, activityConsumerGroup, startID).Err()
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
	c.log.Info("activity consumer: started", "stream", events.StreamActivities)

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
			Streams:  []string{events.StreamActivities, ">"},
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
				// The stream and/or group vanished under a running consumer
				// (e.g. a Valkey restart without persistence); nothing else
				// recreates it, so every later read would fail the same way.
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
		Streams:  []string{events.StreamActivities, "0"},
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

// handle decodes one stream message and writes the activity to the DB.
func (c *ActivityConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	a, actorUserID, actorAgentID, ok := activityFromMessage(msg)
	if !ok {
		c.log.Warn("activity consumer: skipping malformed entry", "id", msg.ID)
		c.ack(ctx, msg.ID)
		return
	}

	// Resolve the actor to project_members.id. An unresolvable actor (e.g. a
	// removed member) is stored as nil rather than dropping the entry.
	if actorUserID != nil || actorAgentID != nil {
		var userID uuid.UUID
		if actorUserID != nil {
			userID = *actorUserID
		}
		member, err := c.memberRepo.FindMemberByActor(ctx, a.ProjectID, userID, actorAgentID)
		if err == nil {
			a.ActorID = &member.ID
		} else if !userdom.IsUnidentifiedSystemActor(userID, actorAgentID) || !errors.Is(err, projectdom.ErrMemberNotFound) {
			// The shared-agent-API-key identity is never a member by design,
			// so ErrMemberNotFound is expected for it and not worth a warning.
			c.log.Warn("activity consumer: could not resolve member for actor", "actor_id", actorUserID, "agent_id", actorAgentID, "project_id", a.ProjectID, "err", err)
		}
	}

	if err := c.repo.Create(ctx, a); err != nil {
		// Not acked: left in the PEL for processPending to retry.
		c.log.Error("activity consumer: failed to persist activity", "id", msg.ID, "err", err)
		return
	}
	c.ack(ctx, msg.ID)
}

func (c *ActivityConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamActivities, activityConsumerGroup, id).Err(); err != nil {
		c.log.Warn("activity consumer: xack failed", "id", id, "err", err)
	}
}

// activityPayloadFields are the payload keys the task and doc activity
// services write. Other domains' payloads have their own shapes; every field
// here is optional.
type activityPayloadFields struct {
	ID        string          `json:"id"`
	Content   json.RawMessage `json:"content"`
	CreatedAt string          `json:"created_at"`
}

// activityFromMessage decodes one Fanout envelope. It returns the actor
// user/agent UUIDs separately because the row stores the resolved member ID.
func activityFromMessage(msg redis.XMessage) (a *activitydom.Activity, actorUserID, actorAgentID *uuid.UUID, ok bool) {
	str := func(k string) string { s, _ := msg.Values[k].(string); return s }
	optUUID := func(k string) *uuid.UUID {
		id, err := uuid.Parse(str(k))
		if err != nil {
			return nil
		}
		return &id
	}

	projectID, err := uuid.Parse(str("project_id"))
	if err != nil || projectID == uuid.Nil {
		return nil, nil, nil, false
	}
	topic := str("type")
	if topic == "" {
		return nil, nil, nil, false
	}
	origin := str("origin")
	if origin == "" {
		origin = string(events.OriginSystem)
	}

	raw := str("payload")
	var p activityPayloadFields
	_ = json.Unmarshal([]byte(raw), &p)

	// Prefer the producer's own ID: it is what a comment's direct insert
	// used, and what the per-entity feeds and comment edits refer to.
	id, err := uuid.Parse(p.ID)
	if err != nil {
		id = uuid.NewSHA1(activityIDNamespace, []byte(msg.ID))
	}
	createdAt, err := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if err != nil {
		createdAt = streamIDTime(msg.ID)
	}

	a = &activitydom.Activity{
		ID:           id,
		ProjectID:    projectID,
		EntityType:   str("entity_type"),
		EntityID:     optUUID("entity_id"),
		Origin:       origin,
		ActivityType: topic,
		Content:      activityContent(raw, p.Content),
		CreatedAt:    createdAt,
	}
	return a, optUUID("actor_id"), optUUID("actor_agent_id"), true
}

// activityContent picks what the row stores as content. Task and doc
// activity payloads carry their real body as a JSON string under "content"
// (see their services' activityPayload); that inner document is what the
// per-entity feeds have always rendered, so it is unwrapped. Any other
// payload is stored as-is.
func activityContent(raw string, inner json.RawMessage) json.RawMessage {
	var s string
	if len(inner) > 0 && json.Unmarshal(inner, &s) == nil && json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	if !json.Valid([]byte(raw)) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(raw)
}

// streamIDTime returns the millisecond timestamp Valkey encodes in a stream
// entry ID ("<ms>-<seq>") — when the event was published, which is when it
// happened. Falls back to now for a malformed ID.
func streamIDTime(id string) time.Time {
	msPart, _, _ := strings.Cut(id, "-")
	ms, err := strconv.ParseInt(msPart, 10, 64)
	if err != nil {
		return time.Now()
	}
	return time.UnixMilli(ms)
}

// isTaskEntry reports whether a StreamActivities entry concerns a task — the
// only entries the task listeners (autofill, auto-assign, automation) act
// on. An entry with no entity_type predates the unified stream's envelope.
func isTaskEntry(msg redis.XMessage) bool {
	et, _ := msg.Values["entity_type"].(string)
	return et == "" || et == string(events.EntityTask)
}
