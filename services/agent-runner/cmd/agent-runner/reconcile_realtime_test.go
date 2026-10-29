package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

type emptySandbox struct{ sandbox.Backend }

func (emptySandbox) LiveConversationIDs(context.Context) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

// Run against an isolated, disposable Postgres database via PACA_RECONCILE_TEST_DSN.
func TestReconcileOrphanRealtimeRouting(t *testing.T) {
	dsn := os.Getenv("PACA_RECONCILE_TEST_DSN")
	if dsn == "" {
		t.Skip("set PACA_RECONCILE_TEST_DSN to an empty disposable Postgres database")
	}
	ctx := context.Background()
	db, err := postgres.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TEMP TABLE agents (id uuid, agent_type text);
        CREATE TEMP TABLE agent_conversations (id uuid, agent_id uuid, project_id uuid, environment_id uuid, updated_at timestamptz, status text, deleted_at timestamptz, error_message text, actor_account_id uuid, chat_session_id uuid);
        CREATE TEMP TABLE agent_chat_sessions (id uuid, member_id uuid);
        CREATE TEMP TABLE project_members (id uuid, account_id uuid)`)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	sub := client.Subscribe(ctx, messaging.ChannelRealtime)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewConversationRepository(db)
	agentID, project, owner, member, session := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err = db.Exec(`INSERT INTO agents VALUES ($1, 'llm')`, agentID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO project_members VALUES ($1, $2)`, member, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO agent_chat_sessions VALUES ($1, $2)`, session, member); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                    string
		project, actor, session any
		private                 bool
	}{
		{"global", nil, owner, nil, true},
		{"private project", project, nil, session, true},
		{"shared project", project, nil, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			_, err := db.Exec(`INSERT INTO agent_conversations (id, agent_id, project_id, updated_at, status, actor_account_id, chat_session_id) VALUES ($1, $2, $3, $4, 'running', $5, $6)`, id, agentID, tc.project, time.Now().Add(-2*reconcileConversationGrace), tc.actor, tc.session)
			if err != nil {
				t.Fatal(err)
			}
			reconcileRunningConversationsOnStartup(ctx, repo, emptySandbox{}, messaging.NewPublisher(client), slog.New(slog.NewTextHandler(io.Discard, nil)))
			wait, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			msg, err := sub.ReceiveMessage(wait)
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Type    string         `json:"type"`
				Payload map[string]any `json:"payload"`
				Authz   map[string]any `json:"authz"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				t.Fatal(err)
			}
			if event.Type != "agent.conversation.failed" || event.Payload["conversation_id"] != id.String() {
				t.Fatalf("unexpected event: %s", msg.Payload)
			}
			if tc.private {
				if event.Payload["actor_account_id"] != owner.String() || event.Authz != nil {
					t.Fatalf("private event not owner-routed: %s", msg.Payload)
				}
			} else if event.Payload["actor_account_id"] != nil || event.Authz["resource"] != fmt.Sprintf("project/%s/conversation/%s", project, id) {
				t.Fatalf("shared event not project-routed: %s", msg.Payload)
			}
			if tc.project == nil && event.Payload["project_id"] != nil {
				t.Fatalf("global event has project: %s", msg.Payload)
			}
			var status string
			if err := db.Get(&status, `SELECT status FROM agent_conversations WHERE id = $1`, id); err != nil || status != "failed" {
				t.Fatalf("status = %q, err = %v", status, err)
			}
			entries, err := client.XRange(ctx, messaging.StreamAgentConversationStatus, "-", "+").Result()
			if err != nil || len(entries) == 0 || entries[len(entries)-1].Values["conversation_id"] != id.String() || entries[len(entries)-1].Values["status"] != "failed" {
				t.Fatalf("status stream = %v, err = %v", entries, err)
			}
		})
	}
	t.Run("lookup failure still publishes terminal status", func(t *testing.T) {
		id := uuid.New()
		if _, err := db.Exec(`INSERT INTO agent_conversations (id, agent_id, updated_at, status) VALUES ($1, $2, $3, 'running')`, id, agentID, time.Now().Add(-2*reconcileConversationGrace)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`DROP TABLE project_members`); err != nil {
			t.Fatal(err)
		}
		reconcileRunningConversationsOnStartup(ctx, repo, emptySandbox{}, messaging.NewPublisher(client), slog.New(slog.NewTextHandler(io.Discard, nil)))
		wait, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		if msg, err := sub.ReceiveMessage(wait); err == nil {
			t.Fatalf("unexpected unscoped realtime event: %s", msg.Payload)
		}
		entries, err := client.XRange(ctx, messaging.StreamAgentConversationStatus, "-", "+").Result()
		if err != nil || len(entries) == 0 || entries[len(entries)-1].Values["conversation_id"] != id.String() || entries[len(entries)-1].Values["status"] != "failed" {
			t.Fatalf("status stream = %v, err = %v", entries, err)
		}
	})
}
