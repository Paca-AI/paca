package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// MigrationRunner runs plugin-owned SQL migration files within the plugin's
// dedicated PostgreSQL schema namespace.
//
// Each plugin schema is named after the plugin (see schemaName).  A
// plugin_schema_migrations tracking table is created within the schema on
// first use to record which migration files have already been applied.
// Migrations are run in lexicographic filename order and are idempotent.
type MigrationRunner struct {
	db    *sql.DB
	store Store
	log   *slog.Logger
}

// NewMigrationRunner creates a MigrationRunner.
func NewMigrationRunner(db *sql.DB, store Store, log *slog.Logger) *MigrationRunner {
	return &MigrationRunner{db: db, store: store, log: log}
}

// quoteIdentifier safely quotes a PostgreSQL identifier to prevent SQL injection.
// It doubles any embedded quotes and wraps the result in double quotes.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Run applies any new migration files for the given plugin.
// It creates the plugin schema and migrations tracking table if they do not
// exist, then applies files that are not yet recorded there.
func (mr *MigrationRunner) Run(ctx context.Context, pluginName string) error {
	schema := schemaName(pluginName)
	quotedSchema := quoteIdentifier(schema)

	// 1. Ensure the plugin schema exists.
	if _, err := mr.db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+quotedSchema); err != nil {
		return fmt.Errorf("plugin migration %q: create schema: %w", pluginName, err)
	}

	// 2. Ensure the plugin KV store table exists (used by paca.storage_* host fns).
	if _, err := mr.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS `+quotedSchema+`.plugin_kv (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("plugin migration %q: create plugin_kv: %w", pluginName, err)
	}

	// 3. Ensure the migrations tracking table exists.
	if _, err := mr.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS `+quotedSchema+`.plugin_schema_migrations (
			filename   TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("plugin migration %q: create migrations table: %w", pluginName, err)
	}

	// 4. Load migration files from the store.
	files, err := mr.store.ListMigrations(ctx, pluginName)
	if err != nil {
		return fmt.Errorf("plugin migration %q: list files: %w", pluginName, err)
	}
	// Ensure lexicographic order even if the store returns them unordered.
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	// 5. Determine which migrations have already been applied.
	rows, err := mr.db.QueryContext(ctx,
		"SELECT filename FROM "+quotedSchema+".plugin_schema_migrations")
	if err != nil {
		return fmt.Errorf("plugin migration %q: query applied: %w", pluginName, err)
	}
	applied := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		applied[name] = struct{}{}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("plugin migration %q: scan applied: %w", pluginName, err)
	}

	// 6. Apply pending migrations in a single transaction per file.
	for _, f := range files {
		if _, ok := applied[f.Name]; ok {
			continue // already applied
		}

		mr.log.Info("plugin: applying migration", "plugin", pluginName, "file", f.Name)

		tx, err := mr.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("plugin migration %q %q: begin tx: %w", pluginName, f.Name, err)
		}

		// Set search path so the plugin SQL can reference tables without schema prefix.
		if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+quotedSchema+",public"); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("plugin migration %q %q: set search_path: %w", pluginName, f.Name, err)
		}

		if _, err := tx.ExecContext(ctx, f.SQL); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("plugin migration %q %q: exec: %w", pluginName, f.Name, err)
		}

		if _, err := tx.ExecContext(ctx,
			"INSERT INTO "+quotedSchema+".plugin_schema_migrations (filename) VALUES ($1)", f.Name); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("plugin migration %q %q: record: %w", pluginName, f.Name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("plugin migration %q %q: commit: %w", pluginName, f.Name, err)
		}
		mr.log.Info("plugin: migration applied", "plugin", pluginName, "file", f.Name)
	}

	return nil
}
