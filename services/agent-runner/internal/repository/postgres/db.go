// Package postgres provides sqlx-backed readers/writers for the
// agents/agent_conversations tables services/api owns. Column and table
// names here must stay in sync with services/api's migrations — there is
// no shared schema package across the module boundary (see
// internal/agent's doc comment for why), so a schema change on that side
// has to be mirrored here by hand.
package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	// Registers the "pgx" database/sql driver name Open dials below — same
	// driver services/api uses. Side-effect import only.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

// Open connects to Postgres via the pgx stdlib driver and verifies the
// connection with a ping.
func Open(dsn string) (*sqlx.DB, error) {
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	return db, nil
}

// maxPortAssignRetries bounds how many times AssignSSHPort/AssignHostPort
// retry a unique_violation before giving up — see AssignSSHPort's own doc
// comment for why a retry (not an immediate give-up) is the correct
// response. Comfortably more than the number of agent-runner replicas any
// real deployment would run, so exhausting this genuinely means something
// else is wrong, not just an unlucky collision.
const maxPortAssignRetries = 5

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505) — used by EnvironmentRepository.AssignSSHPort
// and PortForwardRepository.AssignHostPort to tell "another concurrent
// caller's own single-UPDATE-pick-lowest-free-port query committed first
// and claimed the exact port this one just picked, retry" apart from a
// genuine failure worth giving up on immediately.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
