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

	"github.com/Paca-AI/api/internal/events"
	exportsvc "github.com/Paca-AI/api/internal/service/export"
)

const (
	projectExportConsumerGroup = "api.project_exports"
	projectExportReadBlock     = 5 * time.Second
	projectExportReadCount     = 5
	// projectExportRunTimeout bounds one export run. Building a CSV is
	// read-only and bounded by the project's task count; this only exists so
	// a hung database or storage call can't wedge the consumer forever.
	projectExportRunTimeout = 15 * time.Minute
	// projectExportCleanupInterval is how often expired files are swept.
	projectExportCleanupInterval = time.Hour
)

// projectExportExecutor is the exportsvc.Service surface this consumer needs.
type projectExportExecutor interface {
	Execute(ctx context.Context, exportID uuid.UUID) error
	CleanupExpired(ctx context.Context) (int, error)
}

// ProjectExportConsumer reads queued project exports from
// events.StreamProjectExports and runs them one at a time, and periodically
// deletes exports past their retention. Running them serially is deliberate:
// each export holds a whole project's CSV in memory, so concurrency would
// multiply that for no benefit.
type ProjectExportConsumer struct {
	client       *redis.Client
	svc          projectExportExecutor
	log          *slog.Logger
	consumerName string
	stopCh       chan struct{}
	doneCh       chan struct{}
	// baseCtx parents every export run; Stop cancels it so shutdown does not
	// wait out a long export.
	baseCtx    context.Context
	cancelBase context.CancelFunc
}

// NewProjectExportConsumer creates a consumer ready to be started. The
// consumer name is derived from the hostname so it is unique per instance.
func NewProjectExportConsumer(client *redis.Client, svc projectExportExecutor, log *slog.Logger) *ProjectExportConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	baseCtx, cancelBase := context.WithCancel(context.Background())
	return &ProjectExportConsumer{
		baseCtx:      baseCtx,
		cancelBase:   cancelBase,
		client:       client,
		svc:          svc,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", projectExportConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed and begins reading in a
// background goroutine. Call Stop to exit cleanly.
func (c *ProjectExportConsumer) Start(ctx context.Context) {
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Warn("project export consumer: could not create consumer group, will retry on first read", "err", err)
	}
	go c.run()
	go c.cleanupLoop()
}

// Stop signals the consumer to stop, cancels any export still running (it is
// recorded as failed, so the user can request a new one) and waits for the read
// loop to return.
func (c *ProjectExportConsumer) Stop() {
	close(c.stopCh)
	c.cancelBase()
	<-c.doneCh
}

func (c *ProjectExportConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamProjectExports, projectExportConsumerGroup, startID).Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

func (c *ProjectExportConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("project export consumer: started", "stream", events.StreamProjectExports)

	// Replay messages delivered before a crash but never acked.
	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("project export consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), projectExportReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    projectExportConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamProjectExports, ">"},
			Count:    projectExportReadCount,
			Block:    projectExportReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			c.log.Error("project export consumer: xreadgroup error", "err", err)
			if strings.Contains(err.Error(), "NOGROUP") {
				recoverCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				// "0": nothing here is unsafe to replay — Execute only acts on
				// rows still pending, so an already-run export is a no-op.
				if geErr := c.ensureGroup(recoverCtx, "0"); geErr != nil {
					c.log.Warn("project export consumer: failed to recreate consumer group", "err", geErr)
				}
				cancel()
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

func (c *ProjectExportConsumer) processPending(ctx context.Context) {
	for {
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    projectExportConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamProjectExports, "0"},
			Count:    projectExportReadCount,
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			c.log.Warn("project export consumer: could not read pending messages", "err", err)
			return
		}
		delivered := 0
		for _, stream := range msgs {
			for _, msg := range stream.Messages {
				c.handle(msg)
				delivered++
			}
		}
		if delivered < projectExportReadCount {
			return
		}
	}
}

// projectExportPayload mirrors what exportsvc.Service.RequestExport appends.
type projectExportPayload struct {
	ExportID string `json:"export_id"`
}

// handle runs one export. It acks in every case but one: a failed export is
// recorded on its row (the user sees "failed" and requests a new one), so
// redelivering the same message would only repeat the failure. The exception is
// a failed claim, where the row is still pending and the message stays in the
// pending list to be replayed on the next start.
func (c *ProjectExportConsumer) handle(msg redis.XMessage) {
	ctx := c.baseCtx
	acknowledge := true
	defer func() {
		if acknowledge {
			// Ack even when shutdown has cancelled ctx.
			c.ack(context.WithoutCancel(ctx), msg.ID)
		}
	}()

	eventType, _ := msg.Values["type"].(string)
	if eventType != events.TopicProjectExportRequested {
		c.log.Warn("project export consumer: unknown event type", "id", msg.ID, "type", eventType)
		return
	}
	raw, ok := msg.Values["payload"].(string)
	if !ok {
		c.log.Warn("project export consumer: message has no payload field", "id", msg.ID)
		return
	}
	var p projectExportPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		c.log.Warn("project export consumer: failed to decode payload", "id", msg.ID, "err", err)
		return
	}
	exportID, err := uuid.Parse(p.ExportID)
	if err != nil {
		c.log.Warn("project export consumer: invalid export_id", "id", msg.ID, "export_id", p.ExportID)
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, projectExportRunTimeout)
	defer cancel()
	if err := c.svc.Execute(runCtx, exportID); err != nil {
		if errors.Is(err, exportsvc.ErrClaim) {
			acknowledge = false
		}
		c.log.Error("project export consumer: export failed", "export_id", exportID, "err", err)
	}
}

func (c *ProjectExportConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamProjectExports, projectExportConsumerGroup, id).Err(); err != nil {
		c.log.Warn("project export consumer: xack failed", "id", id, "err", err)
	}
}

func (c *ProjectExportConsumer) cleanupLoop() {
	t := time.NewTicker(projectExportCleanupInterval)
	defer t.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			n, err := c.svc.CleanupExpired(ctx)
			cancel()
			if err != nil {
				c.log.Warn("project export consumer: cleanup failed", "err", err)
			} else if n > 0 {
				c.log.Info("project export consumer: removed expired exports", "count", n)
			}
		}
	}
}
