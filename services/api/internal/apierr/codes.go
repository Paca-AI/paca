// Package apierr defines machine-readable error codes used in API error
// responses. Clients should switch on Code values rather than HTTP status
// codes or human-readable messages, because messages are subject to change.
package apierr

// Code is a stable, machine-readable string that identifies a specific error
// condition. Codes are grouped by domain prefix (e.g. AUTH_, USER_).
type Code string

const (
	// CodeInvalidCredentials represents invalid authentication credentials.
	CodeInvalidCredentials Code = "AUTH_INVALID_CREDENTIALS"
	// CodeMissingToken represents a missing authentication token.
	CodeMissingToken Code = "AUTH_MISSING_TOKEN"
	// CodeTokenInvalid represents an invalid authentication token.
	CodeTokenInvalid Code = "AUTH_TOKEN_INVALID"
	// CodeUnauthenticated represents an unauthenticated request.
	CodeUnauthenticated Code = "AUTH_UNAUTHENTICATED"

	// CodeUserNotFound represents a user that was not found.
	CodeUserNotFound Code = "USER_NOT_FOUND"
	// CodeUsernameTaken represents a username that is already taken.
	CodeUsernameTaken Code = "USER_USERNAME_TAKEN"
	// CodeForbidden represents a forbidden action.
	CodeForbidden Code = "FORBIDDEN"

	// CodeBadRequest represents a bad request.
	CodeBadRequest Code = "BAD_REQUEST"
	// CodeInternalError represents an internal server error.
	CodeInternalError Code = "INTERNAL_ERROR"
)

// Error carries a machine-readable Code alongside a human-readable Message.
// It implements the error interface so it can propagate through service layers
// and be detected by the transport presenter.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return e.Message }

// New returns a new *Error with the given code and message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}
