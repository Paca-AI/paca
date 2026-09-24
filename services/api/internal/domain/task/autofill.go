package taskdom

import (
	"context"

	"github.com/google/uuid"
)

// AutofillRepository persists the bookkeeping worker.TaskAutofillConsumer
// needs to fill in task fields a user left blank, via Jev (see
// internal/platform/jev), without ever overwriting a field a human
// explicitly set or re-processing the same task twice.
//
// This is deliberately a separate, narrow interface rather than part of the
// main Repository/Service — it's consumed directly by TaskHandler (to
// record which fields were user-set at creation) and by the worker package
// (to check/record autofill progress), neither of which needs the rest of
// Repository's surface for this. Implemented by repository/postgres.
// TaskRepository alongside its main Repository methods.
type AutofillRepository interface {
	// RecordUserSetFields records that fieldKeys were explicitly provided by
	// a human on taskID — scoped to the fields autofill actually considers
	// ("importance", "task_type_id", "custom:<fieldKey>"), not every field
	// on the task. A field's presence here permanently excludes it from
	// autofill for this task.
	RecordUserSetFields(ctx context.Context, taskID uuid.UUID, fieldKeys []string) error
	// ListUserSetFields returns the set of field keys previously recorded
	// via RecordUserSetFields for taskID.
	ListUserSetFields(ctx context.Context, taskID uuid.UUID) (map[string]bool, error)
	// IsTaskAutofilled reports whether the autofill pass has already run for
	// taskID — guards against reprocessing on at-least-once stream
	// redelivery.
	IsTaskAutofilled(ctx context.Context, taskID uuid.UUID) (bool, error)
	// MarkTaskAutofilled records that the autofill pass has run for taskID,
	// whether or not it actually changed any fields.
	MarkTaskAutofilled(ctx context.Context, taskID uuid.UUID) error
}
