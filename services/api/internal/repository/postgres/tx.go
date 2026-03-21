package postgres

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// WithTx executes fn inside a database transaction.  The transaction is
// committed if fn returns nil, and rolled back on any error.
func WithTx(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := fn(tx); err != nil {
			return fmt.Errorf("tx: %w", err)
		}
		return nil
	})
}
