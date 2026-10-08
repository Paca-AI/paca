package postgres

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// isUniqueViolation reports whether err represents a unique-constraint
// violation. Safe to call with a nil err (returns false): a nil err.Error()
// call panics, so this guard is defense in depth against a caller that
// forgets the outer `err != nil` check.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique")
}

// uniqueViolationConstraint reports whether err is a Postgres unique-
// constraint violation (SQLSTATE 23505) and, if so, which constraint it
// violated, letting a repository disambiguate between two unique indexes on
// the same table (e.g. users has separate ones for username and email)
// instead of isUniqueViolation's single yes/no. It asserts the concrete
// *pgconn.PgError (via errors.As, which unwraps through database/sql's error
// wrapping) and reads the constraint name pgx reports directly.
func uniqueViolationConstraint(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName, true
	}
	return "", false
}
