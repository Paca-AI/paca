package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestAgentTurnMigrationUpgradesEmptyDraft protects the development upgrade
// path from the early PACA-3 agent_turns shape. Fresh-database tests cannot
// catch CREATE TABLE IF NOT EXISTS silently preserving missing columns or a
// weaker private-tool constraint.
func TestAgentTurnMigrationUpgradesEmptyDraft(t *testing.T) {
	dsn := os.Getenv("PACA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PACA_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	publicTriggerStateBefore := loadPublicAgentMigrationTriggerState(t, ctx, db)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	schema := "paca3_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schema+", public"); err != nil {
		t.Fatal(err)
	}
	// 000040 alters these pre-existing tables as well as the four early draft
	// tables below. Shadow every altered relation so an opt-in migration test
	// can never attach test-schema functions or triggers to public tables in a
	// shared developer/CI database.
	if _, err := conn.ExecContext(ctx, `CREATE TABLE agent_chat_sessions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		agent_id UUID NOT NULL,
		project_id UUID,
		member_id UUID,
		actor_user_id UUID,
		title TEXT,
		last_message_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE TABLE agent_conversation_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		conversation_id UUID NOT NULL,
		event_index INTEGER NOT NULL,
		event_type TEXT NOT NULL,
		event_source TEXT NOT NULL,
		payload JSONB NOT NULL DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE TABLE agent_task_handoffs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		task_id UUID NOT NULL,
		conversation_id UUID NOT NULL,
		summary TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT uq_agent_task_handoffs_conversation UNIQUE (conversation_id)
	)`); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.ExecContext(ctx, `CREATE TABLE agent_turns (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		session_id UUID,
		conversation_id UUID NOT NULL,
		project_id UUID,
		agent_id UUID NOT NULL,
		requested_by_member_id UUID,
		requested_by_user_id UUID,
		turn_index INTEGER NOT NULL CHECK (turn_index > 0),
		input_text TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'queued',
		idempotency_key TEXT NOT NULL,
		tool_policy JSONB NOT NULL,
		state_version BIGINT NOT NULL DEFAULT 0,
		deadline_at TIMESTAMPTZ,
		started_at TIMESTAMPTZ,
		finished_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT ck_agent_turn_private_task_mutation_blocked CHECK (
			session_id IS NULL OR (
				jsonb_typeof(tool_policy) = 'object'
				AND tool_policy @> '{"tasks.write":false}'::jsonb
			)
		)
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TABLE agent_conclusion_preparations (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		project_id UUID NOT NULL,
		source_turn_id UUID NOT NULL,
		target_task_id UUID NOT NULL,
		prepared_by_user_id UUID NOT NULL,
		prepared_by_member_id UUID NOT NULL,
		generated_by_agent_id UUID NOT NULL,
		publication_kind TEXT NOT NULL,
		related_publication_id UUID,
		summary TEXT NOT NULL,
		summary_version INTEGER NOT NULL,
		summary_sha256 TEXT NOT NULL,
		is_frozen BOOLEAN NOT NULL DEFAULT TRUE,
		state TEXT NOT NULL DEFAULT 'prepared',
		idempotency_key TEXT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TABLE agent_conclusion_publications (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		project_id UUID NOT NULL,
		target_task_id UUID NOT NULL,
		source_turn_id UUID NOT NULL,
		preparation_id UUID NOT NULL,
		published_by_user_id UUID NOT NULL,
		published_by_member_id UUID NOT NULL,
		generated_by_agent_id UUID NOT NULL,
		kind TEXT NOT NULL,
		root_publication_id UUID,
		revises_publication_id UUID,
		withdraws_publication_id UUID,
		summary TEXT NOT NULL,
		summary_version INTEGER NOT NULL,
		summary_sha256 TEXT NOT NULL,
		idempotency_key TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE UNIQUE INDEX uq_agent_conclusion_revision_parent
		ON agent_conclusion_publications(revises_publication_id)
		WHERE revises_publication_id IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE UNIQUE INDEX uq_agent_conclusion_withdrawal_parent
		ON agent_conclusion_publications(withdraws_publication_id)
		WHERE withdraws_publication_id IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TABLE agent_outbox_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		aggregate_type TEXT NOT NULL,
		aggregate_id UUID NOT NULL,
		event_type TEXT NOT NULL,
		payload JSONB NOT NULL,
		idempotency_key TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'pending',
		attempts INTEGER NOT NULL DEFAULT 0,
		available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		locked_at TIMESTAMPTZ,
		locked_by TEXT,
		published_at TIMESTAMPTZ,
		last_error TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		t.Fatal(err)
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	migrationPath := filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations", "000043_add_agent_turns_and_conclusions.sql")
	migration, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(migration)); err != nil {
		t.Fatalf("upgrade empty PACA-3 draft: %v", err)
	}
	uniquenessMigrationPath := filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations", "000044_enforce_agent_conclusion_source_uniqueness.sql")
	uniquenessMigration, err := os.ReadFile(uniquenessMigrationPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(uniquenessMigration)); err != nil {
		t.Fatalf("apply conclusion source uniqueness migration: %v", err)
	}

	for _, column := range []string{"tool_policy_sha256", "command_sha256", "request_sha256"} {
		var nullable string
		if err := conn.QueryRowContext(ctx, `SELECT is_nullable
			FROM information_schema.columns
			WHERE table_schema=$1 AND table_name='agent_turns' AND column_name=$2`, schema, column).Scan(&nullable); err != nil {
			t.Fatalf("load %s: %v", column, err)
		}
		if nullable != "NO" {
			t.Fatalf("%s remained nullable", column)
		}
	}

	var policyConstraint string
	if err := conn.QueryRowContext(ctx, `SELECT pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_class t ON t.oid=c.conrelid
		JOIN pg_namespace n ON n.oid=t.relnamespace
		WHERE n.nspname=$1 AND t.relname='agent_turns'
		  AND c.conname='ck_agent_turn_private_task_mutation_blocked'`, schema).Scan(&policyConstraint); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(policyConstraint, "agent_private_tool_policy_is_safe") ||
		!strings.Contains(policyConstraint, "tool_policy_sha256") {
		t.Fatalf("draft private-tool constraint was not strengthened: %s", policyConstraint)
	}

	var preparationRequestNullable, supersedesGenerated string
	if err := conn.QueryRowContext(ctx, `SELECT is_nullable
		FROM information_schema.columns
		WHERE table_schema=$1 AND table_name='agent_conclusion_preparations'
		  AND column_name='request_sha256'`, schema).Scan(&preparationRequestNullable); err != nil {
		t.Fatal(err)
	}
	if preparationRequestNullable != "NO" {
		t.Fatal("preparation request hash remained nullable")
	}
	if err := conn.QueryRowContext(ctx, `SELECT is_generated
		FROM information_schema.columns
		WHERE table_schema=$1 AND table_name='agent_conclusion_publications'
		  AND column_name='supersedes_publication_id'`, schema).Scan(&supersedesGenerated); err != nil {
		t.Fatal(err)
	}
	if supersedesGenerated != "ALWAYS" {
		t.Fatalf("supersedes publication column is not generated: %s", supersedesGenerated)
	}
	var oldPublicationIndexes, unifiedPublicationIndexes int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname=$1 AND indexname IN (
			'uq_agent_conclusion_revision_parent',
			'uq_agent_conclusion_withdrawal_parent'
		)`, schema).Scan(&oldPublicationIndexes); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname=$1 AND indexname='uq_agent_conclusion_superseded_parent'`, schema).Scan(&unifiedPublicationIndexes); err != nil {
		t.Fatal(err)
	}
	if oldPublicationIndexes != 0 || unifiedPublicationIndexes != 1 {
		t.Fatalf("publication parent indexes old=%d unified=%d", oldPublicationIndexes, unifiedPublicationIndexes)
	}
	var sourceTaskUniquenessIndexes int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname=$1 AND indexname='uq_agent_conclusion_publication_source_task'`, schema).Scan(&sourceTaskUniquenessIndexes); err != nil {
		t.Fatal(err)
	}
	if sourceTaskUniquenessIndexes != 1 {
		t.Fatalf("conclusion source/task uniqueness indexes=%d", sourceTaskUniquenessIndexes)
	}
	var outboxConstraint string
	if err := conn.QueryRowContext(ctx, `SELECT pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_class t ON t.oid=c.conrelid
		JOIN pg_namespace n ON n.oid=t.relnamespace
		WHERE n.nspname=$1 AND t.relname='agent_outbox_events'
		  AND c.conname='ck_agent_outbox_lock'`, schema).Scan(&outboxConstraint); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outboxConstraint, "lock_token") ||
		!strings.Contains(outboxConstraint, "lock_expires_at") {
		t.Fatalf("outbox lock was not fenced: %s", outboxConstraint)
	}

	if _, err := conn.ExecContext(ctx, string(migration)); err != nil {
		t.Fatalf("repeat upgraded migration: %v", err)
	}
	if _, err := conn.ExecContext(ctx, string(uniquenessMigration)); err != nil {
		t.Fatalf("repeat conclusion source uniqueness migration: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	publicTriggerStateAfter := loadPublicAgentMigrationTriggerState(t, ctx, db)
	if publicTriggerStateAfter != publicTriggerStateBefore {
		t.Fatalf("migration fixture changed public agent triggers\nbefore:\n%s\nafter:\n%s", publicTriggerStateBefore, publicTriggerStateAfter)
	}
}

func loadPublicAgentMigrationTriggerState(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT event_object_table, trigger_name, action_statement
		FROM information_schema.triggers
		WHERE trigger_schema='public'
		  AND event_object_table IN ('agent_chat_sessions','agent_conversation_events','agent_task_handoffs')
		ORDER BY event_object_table, trigger_name, event_manipulation`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var state strings.Builder
	for rows.Next() {
		var table, trigger, action string
		if err := rows.Scan(&table, &trigger, &action); err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprintf(&state, "%s|%s|%s\n", table, trigger, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return state.String()
}

func TestAgentTurnMigrationRejectsNonEmptyDraftAuditRows(t *testing.T) {
	t.Run("turn", func(t *testing.T) {
		fixture := newAgentTurnDraftFixture(t)
		id := uuid.New()
		_, err := fixture.conn.ExecContext(fixture.ctx, `INSERT INTO agent_turns
			(id,conversation_id,agent_id,turn_index,input_text,idempotency_key,tool_policy)
			VALUES ($1,$2,$3,1,'draft input','draft-turn','{"tasks.write":false}')`,
			id, uuid.New(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.conn.ExecContext(fixture.ctx, fixture.migration); err == nil ||
			!strings.Contains(err.Error(), "incompatible PACA-3 draft audit hashes") {
			t.Fatalf("expected fail-closed turn audit migration, got %v", err)
		}
		_, _ = fixture.conn.ExecContext(fixture.ctx, "ROLLBACK")
		var rows int
		if err := fixture.conn.QueryRowContext(fixture.ctx, `SELECT COUNT(*) FROM agent_turns WHERE id=$1`, id).Scan(&rows); err != nil || rows != 1 {
			t.Fatalf("draft turn was not preserved after rollback: rows=%d err=%v", rows, err)
		}
		var added int
		if err := fixture.conn.QueryRowContext(fixture.ctx, `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema=$1 AND table_name='agent_turns' AND column_name='request_sha256'`, fixture.schema).Scan(&added); err != nil || added != 0 {
			t.Fatalf("failed migration did not roll back schema: columns=%d err=%v", added, err)
		}
	})

	t.Run("preparation", func(t *testing.T) {
		fixture := newAgentTurnDraftFixture(t)
		id := uuid.New()
		_, err := fixture.conn.ExecContext(fixture.ctx, `INSERT INTO agent_conclusion_preparations
			(id,project_id,source_turn_id,target_task_id,prepared_by_user_id,prepared_by_member_id,
			 generated_by_agent_id,publication_kind,summary,summary_version,summary_sha256,
			 idempotency_key,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'published','draft summary',1,$8,'draft-prep',NOW()+INTERVAL '1 hour')`,
			id, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.conn.ExecContext(fixture.ctx, fixture.migration); err == nil ||
			!strings.Contains(err.Error(), "incompatible PACA-3 draft requests") {
			t.Fatalf("expected fail-closed preparation migration, got %v", err)
		}
		_, _ = fixture.conn.ExecContext(fixture.ctx, "ROLLBACK")
		var rows int
		if err := fixture.conn.QueryRowContext(fixture.ctx, `SELECT COUNT(*) FROM agent_conclusion_preparations WHERE id=$1`, id).Scan(&rows); err != nil || rows != 1 {
			t.Fatalf("draft preparation was not preserved after rollback: rows=%d err=%v", rows, err)
		}
	})
}

func TestAgentTurnMigrationUpgradesConclusionConstraintsAndPublishingOutbox(t *testing.T) {
	fixture := newAgentTurnDraftFixture(t)
	outboxID := uuid.New()
	if _, err := fixture.conn.ExecContext(fixture.ctx, `INSERT INTO agent_outbox_events
		(id,aggregate_type,aggregate_id,event_type,payload,idempotency_key,status,locked_at,locked_by)
		VALUES ($1,'agent_turn',$2,'agent.turn.requested','{}',$3,'publishing',NOW(),'old-worker')`,
		outboxID, uuid.New(), "draft-outbox:"+outboxID.String()); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.conn.ExecContext(fixture.ctx, fixture.migration); err != nil {
		t.Fatalf("upgrade valid publication/outbox draft: %v", err)
	}
	var checks, foreignKeys, uniques int
	if err := fixture.conn.QueryRowContext(fixture.ctx, `SELECT
			COUNT(*) FILTER (WHERE contype='c'),
			COUNT(*) FILTER (WHERE contype='f'),
			COUNT(*) FILTER (WHERE contype='u')
		FROM pg_constraint
		WHERE conrelid IN ('agent_conclusion_preparations'::regclass,'agent_conclusion_publications'::regclass)
		`).Scan(&checks, &foreignKeys, &uniques); err != nil || checks != 17 || foreignKeys != 17 || uniques != 4 {
		t.Fatalf("draft conclusion constraints were not rebuilt: checks=%d fks=%d uniques=%d err=%v",
			checks, foreignKeys, uniques, err)
	}
	// Isolate the table constraints from the richer publication scope trigger:
	// internal FK triggers remain enabled, while the user trigger would reject
	// these deliberately incomplete rows before the CHECK/FK under test fires.
	if _, err := fixture.conn.ExecContext(fixture.ctx, `ALTER TABLE agent_conclusion_publications DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.conn.ExecContext(fixture.ctx, `INSERT INTO agent_conclusion_publications
		(id,project_id,target_task_id,source_turn_id,preparation_id,published_by_user_id,
		 published_by_member_id,generated_by_agent_id,kind,summary,summary_version,summary_sha256,idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'published',' ',1,$9,$10)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		strings.Repeat("b", 64), "invalid-summary:"+uuid.NewString()); err == nil ||
		!strings.Contains(err.Error(), "ck_agent_conclusion_publication_summary") {
		t.Fatalf("upgraded publication table accepted an invalid summary: %v", err)
	}
	if _, err := fixture.conn.ExecContext(fixture.ctx, `INSERT INTO agent_conclusion_publications
		(id,project_id,target_task_id,source_turn_id,preparation_id,published_by_user_id,
		 published_by_member_id,generated_by_agent_id,kind,summary,summary_version,summary_sha256,idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'published','summary',1,$9,$10)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		strings.Repeat("b", 64), "invalid-reference:"+uuid.NewString()); err == nil ||
		!strings.Contains(err.Error(), "fk_agent_conclusion_publication_project") {
		t.Fatalf("upgraded publication table accepted missing references: %v", err)
	}
	if _, err := fixture.conn.ExecContext(fixture.ctx, `ALTER TABLE agent_conclusion_publications ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	var status string
	var lockedAt, lockedBy, token, expires sql.NullString
	if err := fixture.conn.QueryRowContext(fixture.ctx, `SELECT status,locked_at::text,locked_by,lock_token::text,lock_expires_at::text
		FROM agent_outbox_events WHERE id=$1`, outboxID).Scan(&status, &lockedAt, &lockedBy, &token, &expires); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || lockedAt.Valid || lockedBy.Valid || token.Valid || expires.Valid {
		t.Fatalf("old publishing row was not safely requeued: status=%s locked_at=%v locked_by=%v token=%v expires=%v",
			status, lockedAt, lockedBy, token, expires)
	}
	if _, err := fixture.conn.ExecContext(fixture.ctx, fixture.migration); err != nil {
		t.Fatalf("repeat upgraded migration: %v", err)
	}
}

func TestAgentTurnMigrationRejectsCrossKindPublicationFork(t *testing.T) {
	fixture := newAgentTurnDraftFixture(t)
	rootID := uuid.New()
	insertDraftPublication(t, fixture, rootID, "published", uuid.Nil, uuid.Nil)
	insertDraftPublication(t, fixture, uuid.New(), "revised", rootID, uuid.Nil)
	insertDraftPublication(t, fixture, uuid.New(), "withdrawn", uuid.Nil, rootID)

	if _, err := fixture.conn.ExecContext(fixture.ctx, fixture.migration); err == nil {
		t.Fatal("expected unified publication parent constraint to reject cross-kind fork")
	}
	_, _ = fixture.conn.ExecContext(fixture.ctx, "ROLLBACK")
	var generated int
	if err := fixture.conn.QueryRowContext(fixture.ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema=$1 AND table_name='agent_conclusion_publications'
		  AND column_name='supersedes_publication_id'`, fixture.schema).Scan(&generated); err != nil || generated != 0 {
		t.Fatalf("failed fork migration did not roll back generated column: count=%d err=%v", generated, err)
	}
	var oldIndexes int
	if err := fixture.conn.QueryRowContext(fixture.ctx, `SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname=$1 AND indexname IN ('uq_agent_conclusion_revision_parent','uq_agent_conclusion_withdrawal_parent')`, fixture.schema).Scan(&oldIndexes); err != nil || oldIndexes != 2 {
		t.Fatalf("failed fork migration did not preserve old indexes: count=%d err=%v", oldIndexes, err)
	}
}

type agentTurnDraftFixture struct {
	ctx       context.Context
	conn      *sql.Conn
	schema    string
	migration string
}

func newAgentTurnDraftFixture(t *testing.T) agentTurnDraftFixture {
	t.Helper()
	dsn := os.Getenv("PACA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PACA_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	publicBefore := loadPublicAgentMigrationTriggerState(t, ctx, db)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	schema := "paca3_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schema+", public"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		publicAfter := loadPublicAgentMigrationTriggerState(t, context.Background(), db)
		if publicAfter != publicBefore {
			t.Errorf("migration fixture changed public agent triggers\nbefore:\n%s\nafter:\n%s", publicBefore, publicAfter)
		}
	})
	if _, err := conn.ExecContext(ctx, agentTurnDraftFixtureSQL); err != nil {
		t.Fatal(err)
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	migrationBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations", "000043_add_agent_turns_and_conclusions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return agentTurnDraftFixture{ctx: ctx, conn: conn, schema: schema, migration: string(migrationBytes)}
}

func insertDraftPublication(t *testing.T, fixture agentTurnDraftFixture, id uuid.UUID, kind string, revises, withdraws uuid.UUID) {
	t.Helper()
	var rootValue, revisesValue, withdrawsValue any
	if revises != uuid.Nil {
		rootValue = revises
		revisesValue = revises
	}
	if withdraws != uuid.Nil {
		rootValue = withdraws
		withdrawsValue = withdraws
	}
	_, err := fixture.conn.ExecContext(fixture.ctx, `INSERT INTO agent_conclusion_publications
		(id,project_id,target_task_id,source_turn_id,preparation_id,published_by_user_id,
		 published_by_member_id,generated_by_agent_id,kind,root_publication_id,
		 revises_publication_id,withdraws_publication_id,summary,summary_version,summary_sha256,idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'summary',1,$13,$14)`,
		id, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		kind, rootValue, revisesValue, withdrawsValue, strings.Repeat("b", 64), "publication:"+id.String())
	if err != nil {
		t.Fatal(err)
	}
}

const agentTurnDraftFixtureSQL = `
CREATE TABLE agent_chat_sessions (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), agent_id UUID NOT NULL, project_id UUID,
 member_id UUID, actor_user_id UUID, title TEXT, last_message_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE agent_conversation_events (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), conversation_id UUID NOT NULL,
 event_index INTEGER NOT NULL, event_type TEXT NOT NULL, event_source TEXT NOT NULL,
 payload JSONB NOT NULL DEFAULT '{}'::jsonb, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE agent_task_handoffs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), task_id UUID NOT NULL,
 conversation_id UUID NOT NULL, summary TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CONSTRAINT uq_agent_task_handoffs_conversation UNIQUE (conversation_id)
);
CREATE TABLE agent_turns (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), session_id UUID, conversation_id UUID NOT NULL,
 project_id UUID, agent_id UUID NOT NULL, requested_by_member_id UUID, requested_by_user_id UUID,
 turn_index INTEGER NOT NULL CHECK (turn_index > 0), input_text TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'queued', idempotency_key TEXT NOT NULL, tool_policy JSONB NOT NULL,
 state_version BIGINT NOT NULL DEFAULT 0, deadline_at TIMESTAMPTZ, started_at TIMESTAMPTZ,
 finished_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CONSTRAINT ck_agent_turn_private_task_mutation_blocked CHECK (
  session_id IS NULL OR (jsonb_typeof(tool_policy)='object' AND tool_policy @> '{"tasks.write":false}'::jsonb)
 )
);
CREATE TABLE agent_conclusion_preparations (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), project_id UUID NOT NULL, source_turn_id UUID NOT NULL,
 target_task_id UUID NOT NULL, prepared_by_user_id UUID NOT NULL, prepared_by_member_id UUID NOT NULL,
 generated_by_agent_id UUID NOT NULL, publication_kind TEXT NOT NULL, related_publication_id UUID,
 summary TEXT NOT NULL, summary_version INTEGER NOT NULL, summary_sha256 TEXT NOT NULL,
 is_frozen BOOLEAN NOT NULL DEFAULT TRUE, state TEXT NOT NULL DEFAULT 'prepared',
 idempotency_key TEXT NOT NULL, expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE agent_conclusion_publications (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), project_id UUID NOT NULL, target_task_id UUID NOT NULL,
 source_turn_id UUID NOT NULL, preparation_id UUID NOT NULL, published_by_user_id UUID NOT NULL,
 published_by_member_id UUID NOT NULL, generated_by_agent_id UUID NOT NULL, kind TEXT NOT NULL,
 root_publication_id UUID, revises_publication_id UUID, withdraws_publication_id UUID,
 summary TEXT NOT NULL, summary_version INTEGER NOT NULL, summary_sha256 TEXT NOT NULL,
 idempotency_key TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX uq_agent_conclusion_revision_parent
 ON agent_conclusion_publications(revises_publication_id) WHERE revises_publication_id IS NOT NULL;
CREATE UNIQUE INDEX uq_agent_conclusion_withdrawal_parent
 ON agent_conclusion_publications(withdraws_publication_id) WHERE withdraws_publication_id IS NOT NULL;
CREATE TABLE agent_outbox_events (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), aggregate_type TEXT NOT NULL, aggregate_id UUID NOT NULL,
 event_type TEXT NOT NULL, payload JSONB NOT NULL, idempotency_key TEXT NOT NULL UNIQUE,
 status TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
 available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), locked_at TIMESTAMPTZ, locked_by TEXT,
 published_at TIMESTAMPTZ, last_error TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`
