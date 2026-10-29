package gitdom

import "errors"

var (
	// ErrNotFound means the repository, identity, endpoint or change request does not exist.
	ErrNotFound = errors.New("git: not found")
	// ErrAlreadyLinked means the project already linked this repository.
	ErrAlreadyLinked = errors.New("git: repository is already linked to this project")
	// ErrNoIdentity means no identity can act on the repository (the platform is not set up for it).
	ErrNoIdentity = errors.New("git: no identity is available for this repository")
	// ErrProviderUnavailable means the provider plugin is not installed, enabled or answering.
	ErrProviderUnavailable = errors.New("git: provider is unavailable")
	// ErrInvalidSignature means a webhook delivery failed verification.
	ErrInvalidSignature = errors.New("git: webhook signature is invalid")
	// ErrUnsupported means the provider does not offer this operation (its capability flag is off).
	ErrUnsupported = errors.New("git: the provider does not support this operation")
	// ErrRateLimited means core refused the call to protect the platform or the repository.
	ErrRateLimited = errors.New("git: rate limited")
	// ErrInvalidInput means the request is malformed in a way only core can tell.
	ErrInvalidInput = errors.New("git: invalid request")
	// ErrRunActive means the agent already has an unfinished run on the subject.
	ErrRunActive = errors.New("git: the agent already has an unfinished run on this subject")
	// ErrIdentityExists means the agent already has its own identity for the provider.
	ErrIdentityExists = errors.New("git: an identity already exists")
	// ErrDescriptionTooLong means a repository description exceeds the allowed length.
	ErrDescriptionTooLong = errors.New("git: repository description is too long")
	// ErrIdentityRequired means several connections could serve the request and the caller must pick one.
	ErrIdentityRequired = errors.New("git: several connections are available; choose one")
)

var (
	// ErrSetupExpired means the setup state is unknown, expired, already used or belongs to another user.
	// One error for all of them: a caller learns nothing about which.
	ErrSetupExpired = errors.New("git: setup session is invalid or expired")
	// ErrPublicURLRequired means webhooks and redirects need the instance's public URL.
	ErrPublicURLRequired = errors.New("git: PUBLIC_URL is not configured")
	// ErrInvalidReturnURL means the return URL is not on this instance.
	ErrInvalidReturnURL = errors.New("git: return_url must be on this instance")
	// ErrNotVisible means none of the connections offered can see the repository
	// (on a platform where access needs no grant: its tokens do not reach it).
	ErrNotVisible = errors.New("git: none of the connections can see this repository")
	// ErrAccessNotGranted means the identity cannot see the repository, and the
	// provider lets a user grant it access to the owner's account.
	ErrAccessNotGranted = errors.New("git: the identity has no access to this repository's owner")
)

// DetailedError is a sentinel above plus the provider's own explanation of why
// (already redacted and capped). errors.Is and errors.As still see the
// sentinel, so a status/code mapping is unchanged; Error() is the sentinel's
// text. The explanation is untrusted text a client may show, never interpret.
type DetailedError struct {
	Err    error
	Detail string
}

func (e *DetailedError) Error() string { return e.Err.Error() }
func (e *DetailedError) Unwrap() error { return e.Err }

// WithDetail returns err carrying detail, or err itself when there is none.
func WithDetail(err error, detail string) error {
	if detail == "" {
		return err
	}
	return &DetailedError{Err: err, Detail: detail}
}
