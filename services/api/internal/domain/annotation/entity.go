// Package annotationdom defines the PageAnnotation aggregate — a comment a
// user pins to a specific element on a page running inside an
// environment's forwarded port, created via the Paca browser extension
// (apps/extension). See docs/ai-agent/environment-management.md's "Port
// Forwarding" section for the underlying port-forward feature this
// attaches to, and the extension's own README for the end-to-end flow.
package annotationdom

import (
	"time"

	"github.com/google/uuid"
)

// Status values a PageAnnotation can be in.
const (
	StatusOpen     = "open"
	StatusResolved = "resolved"
)

// BoundingBox is the element's on-page position captured at comment time,
// as both a percentage of the document (used to re-place a pin
// approximately when every selector below fails to resolve on a later
// visit) and the raw viewport size at capture time (context for how narrow
// or wide the page was).
type BoundingBox struct {
	XPct           float64 `json:"x_pct"`
	YPct           float64 `json:"y_pct"`
	WidthPct       float64 `json:"width_pct"`
	HeightPct      float64 `json:"height_pct"`
	ViewportWidth  int     `json:"viewport_width"`
	ViewportHeight int     `json:"viewport_height"`
}

// ElementSnapshot describes the commented-on element as it existed at
// comment time, independent of whether it can still be found later — this
// is what lets a human or agent understand exactly what was being pointed
// at without reopening the page. OuterHTMLExcerpt is attacker-controllable
// (it's arbitrary page HTML) and capped/sanitized by the extension before
// it's ever sent here; treat it as untrusted display data, not markup to
// execute.
type ElementSnapshot struct {
	TagName          string `json:"tag_name"`
	TextExcerpt      string `json:"text_excerpt"`
	OuterHTMLExcerpt string `json:"outer_html_excerpt"`
	AccessibleName   string `json:"accessible_name"`
	Role             string `json:"role"`
}

// ConsoleEntry is one console.error/console.warn call, window.onerror, or
// unhandledrejection captured by the extension between page load and
// comment submission.
type ConsoleEntry struct {
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// FailedRequest is one failed (status >= 400, or a network-level error —
// StatusCode 0) request observed by the extension's background worker for
// this tab between page load and comment submission.
type FailedRequest struct {
	Method     string `json:"method"`
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Error      string `json:"error,omitempty"`
}

// Author is the denormalized creator info attached to a PageAnnotation or
// AnnotationComment at read time (see AnnotationRepository's users JOIN) —
// the same convention task.Activity/doc.Activity already use for
// ActorName/ActorUsername/ActorAvatarKey rather than making a caller
// resolve CreatedBy separately. Unlike those two, an annotation's
// CreatedBy always references a real user (never an agent — there is no
// agent-facing path to this feature), so this needs only a plain JOIN,
// not their COALESCE-across-users-or-agents pattern. AvatarKey/
// AvatarThumbKey are nil when the user has no avatar set; resolving a
// non-nil key into a displayable URL happens in the HTTP handler (see
// AnnotationHandler.toAnnotationResponse), not here — this stays a plain
// read-model value with no I/O of its own.
type Author struct {
	Name           string
	Username       string
	AvatarKey      *string
	AvatarThumbKey *string
}

// PageAnnotation is a single pinned comment on one element of one page.
// Identity for "does this belong to the same page as a later visit" is
// (PortForwardID, PagePath) — deliberately not host/port, since a Docker
// environment's forwarded host_port can change across a "restart to apply
// port changes" cycle without the page itself changing (see
// environmentdom.Environment.PortsPendingRestart).
type PageAnnotation struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	// EnvironmentID is derived, denormalized display context — always the
	// owning PortForwardID's own environment, copied in at creation and
	// never independently settable. PortForwardID is the actual owner: a
	// comment belongs to one specific port forward's running app, not the
	// environment as a whole, since an environment can have several.
	EnvironmentID     uuid.UUID
	PortForwardID     uuid.UUID
	PagePath          string
	ElementSelector   string
	SelectorFallbacks []string
	BoundingBox       BoundingBox
	ElementSnapshot   ElementSnapshot
	ConsoleErrors     []ConsoleEntry
	FailedRequests    []FailedRequest
	// ScreenshotFileID references a files row uploaded via
	// Service.InitiateScreenshotUpload/CompleteScreenshotUpload — nil until
	// the upload completes, and permanently nil if the user skipped
	// attaching one.
	ScreenshotFileID *uuid.UUID
	Body             string
	Status           string
	// TaskID is set once CreateTaskFromAnnotation succeeds — an annotation
	// can exist and be resolved independently of ever becoming a task.
	TaskID    *uuid.UUID
	CreatedBy uuid.UUID
	// CreatedByAuthor is CreatedBy's denormalized display info — see
	// Author's own doc comment.
	CreatedByAuthor Author
	ResolvedBy      *uuid.UUID
	ResolvedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time

	// Comments is populated by ListForPage/ListForEnvironment/FindByID —
	// the reply thread on this pin.
	Comments []*AnnotationComment
}

// AnnotationComment is one reply in a PageAnnotation's thread. Unlike task
// comments (task.Activity with ActivityTypeComment), an annotation isn't a
// Task and has no other activity types to share a table with, so this gets
// its own small table rather than reusing task.Activity's shape.
type AnnotationComment struct {
	ID           uuid.UUID
	AnnotationID uuid.UUID
	Body         string
	CreatedBy    uuid.UUID
	// CreatedByAuthor is CreatedBy's denormalized display info — see
	// Author's own doc comment.
	CreatedByAuthor Author
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PortForwardMatch is the result of resolving a raw host port back to the
// project/environment/port-forward it currently belongs to — see
// Repository.ResolvePortForward. This is how the extension turns "I'm on
// host:31842" into "this is environment X in project Y" before it knows
// which project-scoped endpoints to call next.
type PortForwardMatch struct {
	ProjectID     uuid.UUID
	EnvironmentID uuid.UUID
	PortForwardID uuid.UUID
	Label         string
}
