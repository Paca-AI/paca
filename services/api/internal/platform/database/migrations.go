// Package database — migration runner.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// migrationsAdvisoryLockKey is an arbitrary, fixed Postgres advisory-lock
// key serializing concurrent migration runs — e.g. two app instances
// booting at the same time during a rolling deploy. The value itself is
// meaningless; what matters is that it never changes, since changing it
// would let an old and a new binary version run migrations unserialized
// against each other during a deploy that briefly overlaps both.
const migrationsAdvisoryLockKey int64 = 20240601001

// schemaMigrationsDDL creates the ledger table RunMigrations/RunMigrationsFS
// use to track which migration files have already been applied, so each
// file executes at most once ever instead of being re-run on every
// startup.
const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	filename   text PRIMARY KEY,
	applied_at timestamptz NOT NULL DEFAULT now()
)`

// RunMigrations executes every *.sql file in migrationsDir, in
// lexicographic order, that hasn't already been recorded as applied — see
// RunMigrationsFS for the full behavior. This os.DirFS-backed variant is
// used by tests that write migration files to a temp directory rather than
// embedding them into the binary.
func RunMigrations(db *sql.DB, migrationsDir string) error {
	return runMigrations(context.Background(), db, os.DirFS(migrationsDir))
}

// RunMigrationsFS executes every *.sql file in the root of fsys, in
// lexicographic order, that hasn't already been recorded as applied in the
// schema_migrations table (created automatically on first use — see
// schemaMigrationsDDL). Each file runs in its own transaction; once that
// commits, the filename is recorded as applied in a separate statement on
// the same connection before moving on to the next file. A Postgres
// advisory lock (migrationsAdvisoryLockKey) is held for the whole run, so
// that two instances booting concurrently — e.g. during a rolling deploy —
// can't both apply the same new file: the second one blocks until the
// first finishes, then finds nothing left to do.
//
// Because a file now executes at most once ever, new migrations no longer
// need to be written as defensively re-runnable (IF NOT EXISTS, ON
// CONFLICT, a WHERE guard re-checking whether the change already landed,
// etc.) the way every migration up to and including 000052 was under the
// old "safe to call on every startup" convention this replaces — plain,
// one-shot SQL is fine from here on. Existing files are left exactly as
// they are; their re-run guards are now simply inert (each one only ever
// executes the single time it's first seen), not a pattern to keep
// copying into new ones.
//
// The very first boot after this ledger was introduced finds the table
// empty and so replays every pre-existing migration file once more before
// recording it as applied — safe only because every one of those files was
// already written to tolerate a re-run under the old convention. No
// separate step is needed to seed the ledger with migration history.
func RunMigrationsFS(db *sql.DB, fsys fs.FS) error {
	return runMigrations(context.Background(), db, fsys)
}

func runMigrations(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	// Everything below — the lock, the ledger read, and every file's
	// exec-then-record — runs on this one checked-out connection.
	// pg_advisory_lock is session-scoped: taking it on one pooled
	// connection and later executing on another would leave the "lock"
	// unable to actually serialize anything.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrations: acquire connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationsAdvisoryLockKey); err != nil {
		return fmt.Errorf("migrations: acquire advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationsAdvisoryLockKey)
	}()

	if _, err := conn.ExecContext(ctx, schemaMigrationsDDL); err != nil {
		return fmt.Errorf("migrations: create schema_migrations: %w", err)
	}

	applied, err := alreadyApplied(ctx, conn)
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("migrations: read dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		if applied[name] {
			continue
		}

		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("migrations: read %q: %w", name, err)
		}

		if err := execAndRecord(ctx, conn, name, string(data)); err != nil {
			return fmt.Errorf("migrations: exec %q: %w", name, err)
		}
	}

	return nil
}

// alreadyApplied returns the set of filenames already recorded in
// schema_migrations.
func alreadyApplied(ctx context.Context, conn *sql.Conn) (map[string]bool, error) {
	rows, err := conn.QueryContext(ctx, "SELECT filename FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("migrations: list applied: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("migrations: scan applied: %w", err)
		}
		applied[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrations: list applied: %w", err)
	}
	return applied, nil
}

// execAndRecord runs sqlStr in its own transaction, then — only once that
// commits — records filename as applied in a separate statement on the
// same connection. The two aren't one atomic unit: sqlStr conventionally
// self-manages its own BEGIN/COMMIT (see execInTx), so it can't be nested
// inside a Go-level transaction that also covers the ledger insert. A
// crash in the narrow window between the two leaves sqlStr's effect
// applied but unrecorded, so the file gets retried on the next run — the
// same "dirty" edge case every migration tool has when a process dies
// mid-migration, not something new this introduces.
func execAndRecord(ctx context.Context, conn *sql.Conn, filename, sqlStr string) error {
	if err := execInTx(ctx, conn, sqlStr); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (filename) VALUES ($1)", filename)
	return err
}

// execInTx runs sqlStr inside a single transaction on conn, rolling back on
// any error.
func execInTx(ctx context.Context, conn *sql.Conn, sqlStr string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, sqlStr); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
