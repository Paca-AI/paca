package postgres

import (
	"context"
	"fmt"
	"time"
)

// CurrentDatabaseTime returns PostgreSQL's transaction timestamp. Callers
// comparing a cutoff with database-generated timestamps (for example,
// agent_conversations.updated_at) must use the same clock domain rather than
// an application host's wall clock, which may be skewed from PostgreSQL.
func (r *ConversationRepository) CurrentDatabaseTime(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := r.db.GetContext(ctx, &now, `SELECT now()`); err != nil {
		return time.Time{}, fmt.Errorf("postgres: query current database time: %w", err)
	}
	return now, nil
}
