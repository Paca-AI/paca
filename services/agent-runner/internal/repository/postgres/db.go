// Package postgres provides sqlx-backed readers/writers for the
// agents/agent_conversations tables services/api owns. Column and table
// names here must stay in sync with services/api's migrations — there is
// no shared schema package across the module boundary (see
// internal/agent's doc comment for why), so a schema change on that side
// has to be mirrored here by hand.
package postgres

import (
	"fmt"

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
