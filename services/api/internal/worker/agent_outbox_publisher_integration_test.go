package worker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
	"github.com/Paca-AI/api/internal/repository/postgres"
)

func TestAgentOutboxPublisherPostgresToValkey(t *testing.T) {
	dsn, valkeyURL := os.Getenv("PACA_TEST_DATABASE_URL"), os.Getenv("PACA_TEST_VALKEY_URL")
	if dsn == "" || valkeyURL == "" {
		t.Skip("PACA_TEST_DATABASE_URL and PACA_TEST_VALKEY_URL are required")
	}
	ctx := context.Background()
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	options, err := redis.ParseURL(valkeyURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	eventID, turnID := uuid.New(), uuid.New()
	payload, _ := json.Marshal(map[string]string{"turn_id": turnID.String()})
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_outbox_events
		(id,aggregate_type,aggregate_id,event_type,payload,idempotency_key,available_at,created_at)
		VALUES ($1,'agent_turn',$2,'agent.turn.requested',$3,$4,NOW(),NOW())`,
		eventID, turnID, payload, "integration:"+eventID.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM agent_outbox_events WHERE id=$1`, eventID)
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	publisher := NewAgentOutboxPublisher(
		postgres.NewAgentTurnRepository(db), messaging.NewPublisher(client, log), log,
	)
	for attempt := 0; attempt < 20; attempt++ {
		if err := publisher.publishBatch(ctx); err != nil {
			t.Fatal(err)
		}
		var current string
		if err := db.GetContext(ctx, &current, `SELECT status FROM agent_outbox_events WHERE id=$1`, eventID); err != nil {
			t.Fatal(err)
		}
		if current == "published" {
			break
		}
	}
	messages, err := client.XRangeN(ctx, events.StreamAgentTurnRequests, "-", "+", 10_000).Result()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	var publishedMessageIDs []string
	for _, message := range messages {
		if message.Values["outbox_event_id"] == eventID.String() && message.Values["turn_id"] == turnID.String() {
			found = true
			publishedMessageIDs = append(publishedMessageIDs, message.ID)
		}
	}
	t.Cleanup(func() {
		if len(publishedMessageIDs) > 0 {
			_ = client.XDel(context.Background(), events.StreamAgentTurnRequests, publishedMessageIDs...).Err()
		}
	})
	if !found {
		t.Fatalf("turn request %s was not appended to %s", eventID, events.StreamAgentTurnRequests)
	}
	var status string
	if err := db.GetContext(ctx, &status, `SELECT status FROM agent_outbox_events WHERE id=$1`, eventID); err != nil || status != "published" {
		t.Fatalf("outbox status=%q err=%v", status, err)
	}
}
