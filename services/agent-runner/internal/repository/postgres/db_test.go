package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestIsUniqueViolation_TrueForCode23505 is a regression test for
// AssignSSHPort/AssignHostPort's own retry decision: a genuine Postgres
// unique-constraint violation (the exact error two concurrent callers'
// single-UPDATE-pick-lowest-free-port queries collide with) must be
// recognized so a retry actually happens instead of giving up immediately.
func TestIsUniqueViolation_TrueForCode23505(t *testing.T) {
	err := &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
	if !isUniqueViolation(err) {
		t.Error("isUniqueViolation(23505) = false, want true")
	}
}

// TestIsUniqueViolation_TrueWhenWrapped confirms detection survives the
// %w-wrapping every real call site here does (fmt.Errorf(...: %w, err)),
// not just a bare *pgconn.PgError.
func TestIsUniqueViolation_TrueWhenWrapped(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505"}
	wrapped := fmt.Errorf("postgres: assign ssh port for environment %s: %w", "env-1", pgErr)
	if !isUniqueViolation(wrapped) {
		t.Error("isUniqueViolation(wrapped 23505) = false, want true")
	}
}

// TestIsUniqueViolation_FalseForOtherPgErrorCodes guards against retrying
// a genuine, non-collision failure (e.g. the environment row itself was
// deleted mid-call) as if it were a transient port collision.
func TestIsUniqueViolation_FalseForOtherPgErrorCodes(t *testing.T) {
	err := &pgconn.PgError{Code: "23503", Message: "foreign key violation"}
	if isUniqueViolation(err) {
		t.Error("isUniqueViolation(23503) = true, want false")
	}
}

// TestIsUniqueViolation_FalseForNonPgError guards against a plain Go error
// (a context cancellation, a connection drop) being mistaken for a
// retryable collision.
func TestIsUniqueViolation_FalseForNonPgError(t *testing.T) {
	if isUniqueViolation(errors.New("connection reset")) {
		t.Error("isUniqueViolation(plain error) = true, want false")
	}
	if isUniqueViolation(nil) {
		t.Error("isUniqueViolation(nil) = true, want false")
	}
}
