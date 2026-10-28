// Code generated from plugin-sdk-go/gitprovider/types.go by scripts/sync-git-spi.sh. DO NOT EDIT.
// Change it in the SDK, then re-run the script. Contract: wire-shapes.golden.json.

package gitprovider

import (
	"encoding/json"
	"time"
)

// SPIVersion identifies this revision of the contract. A provider reports the
// version it was written against in [Info.SPIVersion]; core refuses a provider
// whose major version it does not speak.
const SPIVersion = "gitprovider.v1"

// ── Enumerations ─────────────────────────────────────────────────────────────

// Permission is a neutral, ordered access level. Providers map their own roles
// onto it (e.g. GitLab Guest→read, Reporter→triage, Developer→write,
// Maintainer→maintain, Owner→admin).
type Permission string

const (
	PermissionNone     Permission = "none"
	PermissionRead     Permission = "read"
	PermissionTriage   Permission = "triage"
	PermissionWrite    Permission = "write"
	PermissionMaintain Permission = "maintain"
	PermissionAdmin    Permission = "admin"
)

var permissionRank = map[Permission]int{
	PermissionNone: 0, PermissionRead: 1, PermissionTriage: 2,
	PermissionWrite: 3, PermissionMaintain: 4, PermissionAdmin: 5,
}

// Valid reports whether p is one of the defined levels.
func (p Permission) Valid() bool { _, ok := permissionRank[p]; return ok }

// Rank orders permissions; unknown values rank below [PermissionNone].
func (p Permission) Rank() int {
	if r, ok := permissionRank[p]; ok {
		return r
	}
	return -1
}

// AtLeast reports whether p grants at least the level of min.
func (p Permission) AtLeast(min Permission) bool { return p.Rank() >= min.Rank() && p.Valid() }

// IdentityKind is how a bot identity exists on the platform.
type IdentityKind string

const (
	IdentityApp     IdentityKind = "app"      // GitHub App style
	IdentityBotUser IdentityKind = "bot_user" // a user/bot account with an access token
	IdentityToken   IdentityKind = "token"    // a bare personal/service token
)

// CredentialModel says whether credentials can be minted per use.
type CredentialModel string

const (
	// CredentialEphemeral platforms mint short-lived tokens down-scoped to a
	// repository and access level (GitHub installation tokens).
	CredentialEphemeral CredentialModel = "ephemeral"
	// CredentialStatic platforms only have long-lived scoped tokens; the
	// provider keeps the secret and core never hands it to an agent.
	CredentialStatic CredentialModel = "static"
)

// WebhookScope is where the platform registers webhooks.
type WebhookScope string

const (
	WebhookApp   WebhookScope = "app"   // one webhook for everything the identity can see
	WebhookGroup WebhookScope = "group" // per group/workspace/organization
	WebhookRepo  WebhookScope = "repo"  // per repository
)

// KindProfile is what one identity kind implies on a provider that supports
// several (GitHub: an App is ephemeral with an app-level webhook and needs an
// installation per account; a personal access token is static with a webhook per
// repository). The provider-wide fields of [Capabilities] describe the default
// kind; Kinds refines them per kind.
type KindProfile struct {
	Kind            IdentityKind    `json:"kind"`
	CredentialModel CredentialModel `json:"credential_model"`
	WebhookScope    WebhookScope    `json:"webhook_scope"`
	// AccessGrants is true when identities of this kind must be granted access
	// to each account or organization (see [Capabilities.AccessGrants]).
	AccessGrants bool `json:"access_grants"`
}

// StatusKind is how a run's state is shown on the platform.
type StatusKind string

const (
	StatusCheckRun     StatusKind = "check_run"
	StatusCommitStatus StatusKind = "commit_status"
	StatusComment      StatusKind = "comment"
)

// Access is the credential access level requested for an operation.
type Access string

const (
	AccessRead  Access = "read"
	AccessWrite Access = "write"
)

// TriggerKind names a way the platform can address a bot or report work.
type TriggerKind string

const (
	TriggerMention       TriggerKind = "mention"
	TriggerLabel         TriggerKind = "label"
	TriggerReviewRequest TriggerKind = "review_request"
	TriggerAssignment    TriggerKind = "assignment"
	TriggerCIFailure     TriggerKind = "ci_failure"
	TriggerOwnCRFeedback TriggerKind = "own_cr_feedback"
	TriggerAutoReview    TriggerKind = "auto_review"
)

// AllTriggerKinds lists every defined trigger kind.
var AllTriggerKinds = []TriggerKind{
	TriggerMention, TriggerLabel, TriggerReviewRequest, TriggerAssignment,
	TriggerCIFailure, TriggerOwnCRFeedback, TriggerAutoReview,
}

// ── References ───────────────────────────────────────────────────────────────

// IdentityRef names a bot identity. ProviderRef is opaque to core: the provider
// puts whatever it needs (an installation id, a token id) there and gets it back
// on every call.
type IdentityRef struct {
	ID          string          `json:"id"`
	Handle      string          `json:"handle,omitempty"`
	ProviderRef json.RawMessage `json:"provider_ref,omitempty"`
}

// RepoRef addresses a repository in a platform-neutral way.
type RepoRef struct {
	Host          string          `json:"host"`
	FullPath      string          `json:"full_path"` // "owner/name" or "group/subgroup/project"
	ExternalID    string          `json:"external_id,omitempty"`
	DefaultBranch string          `json:"default_branch,omitempty"`
	CloneURL      string          `json:"clone_url,omitempty"` // HTTPS, token-free
	ProviderRef   json.RawMessage `json:"provider_ref,omitempty"`
}

// Target is the identity to act as plus the repository to act on. Core resolves
// the identity; a provider never chooses one.
type Target struct {
	Identity IdentityRef `json:"identity"`
	Repo     RepoRef     `json:"repo"`
}

// SubjectKind is what a comment or status attaches to.
type SubjectKind string

const (
	SubjectIssue         SubjectKind = "issue"
	SubjectChangeRequest SubjectKind = "change_request"
)

// SubjectRef names an issue or change request.
type SubjectRef struct {
	Kind   SubjectKind `json:"kind"`
	Number string      `json:"number"`
}

// ── Describe ─────────────────────────────────────────────────────────────────

// Terms is the vocabulary the UI uses for this platform.
type Terms struct {
	ChangeRequest       string `json:"change_request"`        // "Pull request" / "Merge request"
	ChangeRequestPlural string `json:"change_request_plural"` // "Pull requests"
	Repository          string `json:"repository"`            // "Repository" / "Project"
}

// Info describes a provider. It is static.
type Info struct {
	SPIVersion      string       `json:"spi_version"`
	ID              string       `json:"id"` // the plugin id, e.g. "com.paca.github"
	DisplayName     string       `json:"display_name"`
	Icon            string       `json:"icon,omitempty"`
	Hosts           []string     `json:"hosts"` // default hosts, e.g. ["github.com"]
	SelfHostedHosts bool         `json:"self_hosted_hosts"`
	Terms           Terms        `json:"terms"`
	Capabilities    Capabilities `json:"capabilities"`
}

// Capabilities declares what the provider can do. Core's UI, tools and router
// read these instead of hard-coding platform names; the conformance suite
// verifies they match the implemented interfaces.
type Capabilities struct {
	IdentityKinds   []IdentityKind  `json:"identity_kinds"`
	CredentialModel CredentialModel `json:"credential_model"`
	WebhookScope    WebhookScope    `json:"webhook_scope"`
	StatusKind      StatusKind      `json:"status_kind"`
	Triggers        []TriggerKind   `json:"triggers"`
	// MultipleIdentities is true when more than one identity per host can be
	// created (needed for per-agent identities, and for several connections of
	// a workspace or project).
	MultipleIdentities bool `json:"multiple_identities"`
	// Kinds refines CredentialModel, WebhookScope and AccessGrants per identity
	// kind, and lists the kinds a user can choose between when setting one up
	// (core then sends Params["kind"]). Empty: there is one way to connect, and
	// every kind in IdentityKinds uses the provider-wide values.
	Kinds []KindProfile `json:"kinds,omitempty"`

	// Optional operations (each backed by an optional interface).
	ResolveThreads    bool `json:"resolve_threads"`
	FileRead          bool `json:"file_read"`
	CodeSearch        bool `json:"code_search"`
	Labels            bool `json:"labels"`
	ReviewerRequests  bool `json:"reviewer_requests"`
	Assignees         bool `json:"assignees"`
	Merge             bool `json:"merge"`
	RerunChecks       bool `json:"rerun_checks"`
	Reactions         bool `json:"reactions"`
	WebhookManagement bool `json:"webhook_management"`
	IdentityDeletion  bool `json:"identity_deletion"`
	// Branches is true when branches can be looked up and created through the
	// provider (core uses it to link and create a task's branch).
	Branches bool `json:"branches"`
	// AccessGrants is true when an identity must be granted access to each
	// account or organization before it can see its repositories, and a user
	// does that on the platform (GitHub: installing the App on an account).
	// Core then offers the grant flow when a repository's owner is not covered.
	AccessGrants bool `json:"access_grants"`

	// Behaviours of required operations.
	BatchReview         bool `json:"batch_review"` // false: SubmitReview emulates
	IdempotentWrites    bool `json:"idempotent_writes"`
	HiddenMarkers       bool `json:"hidden_markers"` // HTML-comment markers survive
	ForkChangeRequests  bool `json:"fork_change_requests"`
	DraftChangeRequests bool `json:"draft_change_requests"`
	NestedPaths         bool `json:"nested_paths"`

	// CIConfigGlobs are repository paths agents must not modify (CI
	// configuration), e.g. ".github/workflows/**", ".gitlab-ci.yml".
	CIConfigGlobs []string `json:"ci_config_globs,omitempty"`
}

// Profile returns the profile of an identity kind: its entry in Kinds, or the
// provider-wide values when it has none.
func (c Capabilities) Profile(kind IdentityKind) KindProfile {
	for _, k := range c.Kinds {
		if k.Kind == kind {
			return k
		}
	}
	return KindProfile{Kind: kind, CredentialModel: c.CredentialModel, WebhookScope: c.WebhookScope, AccessGrants: c.AccessGrants}
}

// SupportsTrigger reports whether the provider declared trigger k.
func (c Capabilities) SupportsTrigger(k TriggerKind) bool {
	for _, t := range c.Triggers {
		if t == k {
			return true
		}
	}
	return false
}

// ── Identity setup ───────────────────────────────────────────────────────────

// SetupScope says whom an identity is for.
type SetupScope string

const (
	SetupShared SetupScope = "shared" // the host-wide shared identity
	SetupAgent  SetupScope = "agent"  // one agent's own identity
)

// BeginSetupRequest starts creating an identity.
type BeginSetupRequest struct {
	Scope       SetupScope        `json:"scope"`
	Host        string            `json:"host"`
	AgentID     string            `json:"agent_id,omitempty"`
	AgentName   string            `json:"agent_name,omitempty"` // default bot name
	ReturnURL   string            `json:"return_url"`           // where the browser returns when done
	CallbackURL string            `json:"callback_url"`         // the Paca page the platform redirects to
	WebhookURL  string            `json:"webhook_url"`          // core's endpoint for this identity
	Params      map[string]string `json:"params,omitempty"`
}

// SetupStepKind is the next thing core must do to continue setup.
type SetupStepKind string

const (
	StepRedirect  SetupStepKind = "redirect"   // send the browser to URL
	StepFormPost  SetupStepKind = "form_post"  // POST Fields to URL from the browser (manifest flow)
	StepTokenForm SetupStepKind = "token_form" // ask the user to paste a token (Prompt says which scopes)
	StepDone      SetupStepKind = "done"
)

// FormField is one input of a [StepTokenForm] step. Platforms with static
// credentials often need more than one secret (a read-only and a write token),
// which is why the form is a list rather than a single token box.
type FormField struct {
	Name     string `json:"name"` // the key in CompleteSetupRequest.Params
	Label    string `json:"label"`
	Help     string `json:"help,omitempty"` // e.g. the scopes the token needs
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// SetupStep is the provider's instruction for the next setup step.
type SetupStep struct {
	Kind   SetupStepKind     `json:"kind"`
	URL    string            `json:"url,omitempty"`
	Fields map[string]string `json:"fields,omitempty"` // form_post: the fields to POST
	// Form lists the inputs to show for a token_form step.
	Form     []FormField `json:"form,omitempty"`
	Prompt   string      `json:"prompt,omitempty"`
	State    string      `json:"state,omitempty"` // opaque, echoed to CompleteIdentitySetup
	Identity *Identity   `json:"identity,omitempty"`
}

// CompleteSetupRequest finishes setup from a callback or a submitted token.
type CompleteSetupRequest struct {
	Scope  SetupScope        `json:"scope"`
	Host   string            `json:"host"`
	State  string            `json:"state"`
	Params map[string]string `json:"params"` // callback query parameters, or {"token": "..."}
}

// Identity is a created identity as core stores it. Secrets stay with the
// provider.
type Identity struct {
	Ref         IdentityRef  `json:"ref"`
	Host        string       `json:"host"`
	Kind        IdentityKind `json:"kind"`
	DisplayName string       `json:"display_name"`
	// ExpiresAt is when the identity's credentials stop working, for platforms
	// whose tokens expire (zero = does not expire). Core warns before it passes.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// Next, when set, is a further step to run before the identity is usable
	// (GitHub: install the app).
	Next *SetupStep `json:"next,omitempty"`
}

// BeginAccessRequest starts granting an identity access to one account or
// organization (capability AccessGrants). The step it returns is the same
// shape as identity setup: a redirect or form post to the platform, which
// returns to CallbackURL.
type BeginAccessRequest struct {
	Identity IdentityRef `json:"identity"`
	// Owner is the account or organization (its login or path) the user wants
	// to use, when known, so the platform can pre-select it.
	Owner       string `json:"owner,omitempty"`
	ReturnURL   string `json:"return_url"`
	CallbackURL string `json:"callback_url"`
}

// CompleteAccessRequest finishes a grant from the platform's redirect.
type CompleteAccessRequest struct {
	Identity IdentityRef       `json:"identity"`
	State    string            `json:"state"`
	Params   map[string]string `json:"params"` // callback query parameters
}

// AccessGrant is what became accessible to the identity.
type AccessGrant struct {
	// Owner is the account or organization now covered ("" if the platform did
	// not say).
	Owner string `json:"owner"`
}

// DeleteIdentityRequest asks the provider to forget an identity.
type DeleteIdentityRequest struct {
	Identity IdentityRef `json:"identity"`
}

// ── Repositories ─────────────────────────────────────────────────────────────

// Repository is a repository an identity can see.
type Repository struct {
	Ref         RepoRef `json:"ref"`
	Private     bool    `json:"private"`
	Description string  `json:"description,omitempty"`
}

// ListReposRequest pages the repositories an identity can see.
type ListReposRequest struct {
	Identity IdentityRef `json:"identity"`
	Query    string      `json:"query,omitempty"`
	Cursor   string      `json:"cursor,omitempty"`
	PageSize int         `json:"page_size,omitempty"`
}

// RepositoryPage is one page of repositories.
type RepositoryPage struct {
	Items      []Repository `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

// ResolveRepoRequest normalizes a user-supplied URL or path and finds which of
// the candidate identities can see it.
type ResolveRepoRequest struct {
	Host       string        `json:"host,omitempty"`
	PathOrURL  string        `json:"path_or_url"`
	Candidates []IdentityRef `json:"candidates"`
}

// ResolvedRepository is the answer to ResolveRepository.
type ResolvedRepository struct {
	Repository Repository  `json:"repository"`
	Identity   IdentityRef `json:"identity"`
}

// ── Webhooks ─────────────────────────────────────────────────────────────────

// WebhookRequest is a delivery as received by core's endpoint.
type WebhookRequest struct {
	Identity IdentityRef       `json:"identity"` // the identity the endpoint belongs to
	Method   string            `json:"method"`
	Headers  map[string]string `json:"headers"` // keys lower-cased
	Query    map[string]string `json:"query,omitempty"`
	Body     []byte            `json:"body"`
}

// EventKind classifies a normalized event.
type EventKind string

const (
	EventComment       EventKind = "comment"        // issue/CR comment, review comment, review body
	EventChangeRequest EventKind = "change_request" // opened, edited, ready, synchronized, closed, merged
	EventReview        EventKind = "review"         // a review was submitted
	EventCheck         EventKind = "check"          // a CI result completed
	EventPush          EventKind = "push"
	EventLabel         EventKind = "label"
	EventAssignment    EventKind = "assignment"
	EventReviewRequest EventKind = "review_request"
)

// AllEventKinds lists every defined event kind.
var AllEventKinds = []EventKind{
	EventComment, EventChangeRequest, EventReview, EventCheck, EventPush,
	EventLabel, EventAssignment, EventReviewRequest,
}

// ActorKind distinguishes people from bots.
type ActorKind string

const (
	ActorUser ActorKind = "user"
	ActorBot  ActorKind = "bot"
)

// Actor is who caused an event.
type Actor struct {
	Login string    `json:"login"`
	Kind  ActorKind `json:"kind"`
	// Permission is the actor's access to the repository when the provider
	// already knows it. Empty means "ask GetActorPermission".
	Permission Permission `json:"permission,omitempty"`
}

// Branch is one side of a change request.
type Branch struct {
	Ref      string `json:"ref"`
	SHA      string `json:"sha,omitempty"`
	Repo     string `json:"repo,omitempty"` // full path of the head repository
	FromFork bool   `json:"from_fork,omitempty"`
}

// EventSubject is what an event is about.
type EventSubject struct {
	Kind   SubjectKind `json:"kind"`
	Number string      `json:"number"`
	Head   *Branch     `json:"head,omitempty"`
	Base   string      `json:"base,omitempty"`
	Draft  bool        `json:"draft,omitempty"`
	Author string      `json:"author,omitempty"`
	// URL is the subject's web page, when the platform's event carries it; core
	// shows it as the link to the pull/merge request.
	URL string `json:"url,omitempty"`
}

// Location pins a review comment to code.
type Location struct {
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	CommentID string `json:"comment_id,omitempty"`
}

// CheckInfo describes a completed CI result.
type CheckInfo struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"` // success | failure | cancelled | skipped
	RunID      string `json:"run_id,omitempty"`
	SHA        string `json:"sha,omitempty"`
}

// AddressVia is how an identity was addressed.
type AddressVia string

const (
	ViaMention       AddressVia = "mention"
	ViaReviewRequest AddressVia = "review_request"
	ViaAssignment    AddressVia = "assignment"
	ViaLabel         AddressVia = "label"
)

// PushInfo describes a push or branch event.
type PushInfo struct {
	Ref     string `json:"ref"` // branch name without refs/heads/
	SHA     string `json:"sha,omitempty"`
	Created bool   `json:"created,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

// Addressed records that a specific identity was addressed. The provider
// decides this (mention syntax differs per platform); core only sees which
// identity and how.
type Addressed struct {
	IdentityID string     `json:"identity_id"`
	Via        AddressVia `json:"via"`
}

// Event is a normalized webhook event. Text is untrusted.
type Event struct {
	ID        string        `json:"id"` // unique per delivery (and per fan-out item)
	Kind      EventKind     `json:"kind"`
	Action    string        `json:"action"`
	Repo      RepoRef       `json:"repo"`
	Subject   *EventSubject `json:"subject,omitempty"`
	Actor     Actor         `json:"actor"`
	Text      string        `json:"text,omitempty"`
	Location  *Location     `json:"location,omitempty"`
	Check     *CheckInfo    `json:"check,omitempty"`
	Push      *PushInfo     `json:"push,omitempty"` // for EventPush
	Label     string        `json:"label,omitempty"`
	Addressed []Addressed   `json:"addressed,omitempty"`
	// RawRef is a provider-private id for debugging; never shown to agents.
	RawRef string `json:"raw_ref,omitempty"`
}

// ── Credentials, actor, protection ───────────────────────────────────────────

// MintRequest asks for a credential to use git or the API against a repository.
type MintRequest struct {
	Target     Target `json:"target"`
	Access     Access `json:"access"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

// Credential authenticates one git/API use. Core never persists it and never
// returns it to an agent; for [CredentialStatic] platforms it is the stored
// secret and core hands it only to the credential helper for one invocation.
type Credential struct {
	Username  string    `json:"username"`
	Secret    string    `json:"secret"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

// ActorPermissionRequest asks for a login's access to a repository.
type ActorPermissionRequest struct {
	Target Target `json:"target"`
	Login  string `json:"login"`
}

// BranchProtectionRequest asks about a branch's protection rules.
type BranchProtectionRequest struct {
	Target Target `json:"target"`
	Branch string `json:"branch"`
}

// BranchProtection summarizes protection relevant to agents.
type BranchProtection struct {
	Protected            bool `json:"protected"`
	RequiresReview       bool `json:"requires_review,omitempty"`
	RequiresStatusChecks bool `json:"requires_status_checks,omitempty"`
	AllowsForcePush      bool `json:"allows_force_push,omitempty"`
}

// ── Reads ────────────────────────────────────────────────────────────────────

// CRState is a change request's lifecycle state.
type CRState string

const (
	CROpen   CRState = "open"
	CRClosed CRState = "closed"
	CRMerged CRState = "merged"
)

// ChangeRequest is a pull/merge request.
type ChangeRequest struct {
	Number    string    `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	State     CRState   `json:"state"`
	Draft     bool      `json:"draft,omitempty"`
	Head      Branch    `json:"head"`
	Base      Branch    `json:"base"`
	Author    string    `json:"author"`
	URL       string    `json:"url"`
	Labels    []string  `json:"labels,omitempty"`
	Reviewers []string  `json:"reviewers,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}

// CRRequest addresses one change request.
type CRRequest struct {
	Target Target `json:"target"`
	Number string `json:"number"`
}

// PagedCRRequest addresses one change request with paging.
type PagedCRRequest struct {
	Target Target `json:"target"`
	Number string `json:"number"`
	Cursor string `json:"cursor,omitempty"`
}

// ChangedFile is one file of a change request.
type ChangedFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"` // added | modified | removed | renamed
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// ChangedFilePage is one page of changed files.
type ChangedFilePage struct {
	Items      []ChangedFile `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// Size limits (MaxBytes, TailBytes) count bytes. Text results (Diff, Logs) are
// cut on a UTF-8 boundary, so the result is always valid UTF-8 and may be a few
// bytes shorter than the limit; Truncated reports that anything was cut. File
// content is raw bytes and is cut exactly at the limit.

// DiffRequest asks for a change request's diff, whole or for one file.
type DiffRequest struct {
	Target   Target `json:"target"`
	Number   string `json:"number"`
	Path     string `json:"path,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

// Diff is unified diff text. Truncated is true when MaxBytes cut it short.
type Diff struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Comment is one comment.
type Comment struct {
	ID         string    `json:"id"`
	Author     string    `json:"author"`
	AuthorKind ActorKind `json:"author_kind"`
	Body       string    `json:"body"`
	URL        string    `json:"url,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

// Thread is a review discussion.
type Thread struct {
	ID       string    `json:"id"`
	Path     string    `json:"path,omitempty"`
	Line     int       `json:"line,omitempty"`
	Resolved bool      `json:"resolved"`
	Outdated bool      `json:"outdated,omitempty"`
	Comments []Comment `json:"comments"`
}

// ThreadPage is one page of threads.
type ThreadPage struct {
	Items      []Thread `json:"items"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// ListCommentsRequest pages the comments of an issue or change request.
type ListCommentsRequest struct {
	Target  Target     `json:"target"`
	Subject SubjectRef `json:"subject"`
	Cursor  string     `json:"cursor,omitempty"`
}

// CommentPage is one page of comments.
type CommentPage struct {
	Items      []Comment `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// IssueRequest addresses one issue.
type IssueRequest struct {
	Target Target `json:"target"`
	Number string `json:"number"`
}

// Issue is an issue.
type Issue struct {
	Number    string   `json:"number"`
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	State     string   `json:"state"` // open | closed
	Author    string   `json:"author"`
	URL       string   `json:"url"`
	Labels    []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
}

// CheckState is the normalized state of a CI result.
type CheckState string

const (
	CheckPending   CheckState = "pending"
	CheckSuccess   CheckState = "success"
	CheckFailure   CheckState = "failure"
	CheckCancelled CheckState = "cancelled"
	CheckSkipped   CheckState = "skipped"
)

// Valid reports whether s is a defined check state.
func (s CheckState) Valid() bool {
	switch s {
	case CheckPending, CheckSuccess, CheckFailure, CheckCancelled, CheckSkipped:
		return true
	}
	return false
}

// Check is a CI result mapped from check runs, pipelines or statuses.
type Check struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	State    CheckState `json:"state"`
	Required bool       `json:"required"`
	URL      string     `json:"url,omitempty"`
	Summary  string     `json:"summary,omitempty"`
}

// ChecksRequest asks for the CI results of a ref (usually a head SHA).
type ChecksRequest struct {
	Target Target `json:"target"`
	Ref    string `json:"ref"`
}

// CheckLogsRequest asks for a check's log, tail-limited.
type CheckLogsRequest struct {
	Target    Target `json:"target"`
	CheckID   string `json:"check_id"`
	TailBytes int    `json:"tail_bytes,omitempty"`
}

// Logs is log text. Truncated is true when the tail limit cut it.
type Logs struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// FileRequest reads a file at a ref without a checkout.
type FileRequest struct {
	Target   Target `json:"target"`
	Ref      string `json:"ref"`
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

// File is a file's content.
type File struct {
	Path      string `json:"path"`
	Content   []byte `json:"content"`
	SHA       string `json:"sha,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// CodeSearchRequest searches a repository's code.
type CodeSearchRequest struct {
	Target Target `json:"target"`
	Query  string `json:"query"`
}

// CodeHit is one search result.
type CodeHit struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// CodeHits is the result of a search.
type CodeHits struct {
	Items []CodeHit `json:"items"`
}

// ── Writes ───────────────────────────────────────────────────────────────────

// OpenCRRequest opens a change request from a pushed branch.
type OpenCRRequest struct {
	Target         Target   `json:"target"`
	Title          string   `json:"title"`
	Body           string   `json:"body,omitempty"`
	Head           string   `json:"head"`
	Base           string   `json:"base"`
	Draft          bool     `json:"draft,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	Reviewers      []string `json:"reviewers,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

// UpdateCRRequest edits a change request; nil fields are left alone.
type UpdateCRRequest struct {
	Target Target   `json:"target"`
	Number string   `json:"number"`
	Title  *string  `json:"title,omitempty"`
	Body   *string  `json:"body,omitempty"`
	Draft  *bool    `json:"draft,omitempty"`
	State  *CRState `json:"state,omitempty"` // open | closed
}

// CommentRequest posts a top-level comment. Core appends the footer.
type CommentRequest struct {
	Target         Target     `json:"target"`
	Subject        SubjectRef `json:"subject"`
	Body           string     `json:"body"`
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
}

// CommentResult identifies a posted comment so core can index it.
type CommentResult struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty"`
}

// ReplyThreadRequest replies inside a review thread.
type ReplyThreadRequest struct {
	Target         Target `json:"target"`
	Number         string `json:"number"`
	ThreadID       string `json:"thread_id"`
	Body           string `json:"body"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// Verdict is a review's overall outcome.
type Verdict string

const (
	VerdictComment        Verdict = "comment"
	VerdictApprove        Verdict = "approve"
	VerdictRequestChanges Verdict = "request_changes"
)

// InlineComment is a review comment pinned to a line of the diff.
type InlineComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side,omitempty"` // new | old
	Body string `json:"body"`
}

// SubmitReviewRequest submits a review. Providers without batch review emulate
// it (post the comments, then a summary) and say so in the result.
type SubmitReviewRequest struct {
	Target         Target          `json:"target"`
	Number         string          `json:"number"`
	Verdict        Verdict         `json:"verdict"`
	Summary        string          `json:"summary,omitempty"`
	Comments       []InlineComment `json:"comments,omitempty"`
	HeadSHA        string          `json:"head_sha,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

// ReviewResult reports what was posted.
type ReviewResult struct {
	ID       string `json:"id,omitempty"`
	URL      string `json:"url,omitempty"`
	Posted   int    `json:"posted"`   // inline comments accepted
	Emulated bool   `json:"emulated"` // true when batch review was emulated
}

// RunState is the state of an agent run shown on the platform.
type RunState string

const (
	RunQueued     RunState = "queued"
	RunInProgress RunState = "in_progress"
	RunSuccess    RunState = "success"
	RunFailure    RunState = "failure"
	RunCancelled  RunState = "cancelled"
)

// SetStatusRequest shows run state on the platform (check run, commit status or
// comment, per [Capabilities.StatusKind]). StatusRef is returned by the first
// call and passed back to update in place.
type SetStatusRequest struct {
	Target    Target     `json:"target"`
	Subject   SubjectRef `json:"subject"`
	SHA       string     `json:"sha,omitempty"`
	State     RunState   `json:"state"`
	Title     string     `json:"title"`
	Summary   string     `json:"summary,omitempty"`
	URL       string     `json:"url,omitempty"`
	StatusRef string     `json:"status_ref,omitempty"`
}

// StatusResult carries the reference to update the status later.
type StatusResult struct {
	StatusRef string `json:"status_ref"`
}

// ── Optional operations ──────────────────────────────────────────────────────

// ResolveThreadRequest resolves or reopens a thread.
type ResolveThreadRequest struct {
	Target   Target `json:"target"`
	Number   string `json:"number"`
	ThreadID string `json:"thread_id"`
	Resolved bool   `json:"resolved"`
}

// LabelsRequest adds labels to a subject.
type LabelsRequest struct {
	Target  Target     `json:"target"`
	Subject SubjectRef `json:"subject"`
	Labels  []string   `json:"labels"`
}

// PeopleRequest requests reviewers or sets assignees on a subject.
type PeopleRequest struct {
	Target  Target     `json:"target"`
	Subject SubjectRef `json:"subject"`
	Logins  []string   `json:"logins"`
}

// MergeRequest merges a change request. ExpectedHeadSHA guards against merging
// something other than what was reviewed.
type MergeRequest struct {
	Target          Target `json:"target"`
	Number          string `json:"number"`
	Method          string `json:"method,omitempty"` // merge | squash | rebase
	ExpectedHeadSHA string `json:"expected_head_sha"`
}

// MergeResult reports the merge commit.
type MergeResult struct {
	SHA string `json:"sha,omitempty"`
}

// RerunRequest re-runs failed checks of a ref (or one check).
type RerunRequest struct {
	Target  Target `json:"target"`
	Ref     string `json:"ref"`
	CheckID string `json:"check_id,omitempty"`
}

// ReactRequest adds an emoji reaction to a comment.
type ReactRequest struct {
	Target    Target `json:"target"`
	CommentID string `json:"comment_id"`
	Emoji     string `json:"emoji"` // eyes | rocket | confused | +1
}

// EnsureWebhookRequest registers a webhook on a repository (repo/group scoped
// platforms only). The signing secret is the provider's own business: it
// generates one per identity at setup, keeps it, and verifies deliveries with
// it — core never sees or stores webhook secrets.
type EnsureWebhookRequest struct {
	Target Target `json:"target"`
	URL    string `json:"url"`
}

// WebhookRef identifies a registered webhook for later removal.
type WebhookRef struct {
	ID string `json:"id"`
}

// RemoveWebhookRequest removes a webhook registered by EnsureWebhook.
type RemoveWebhookRequest struct {
	Target  Target     `json:"target"`
	Webhook WebhookRef `json:"webhook"`
}

// Empty is the result of operations that return nothing.
type Empty struct{}

// ── Branches ─────────────────────────────────────────────────────────────────

// GetBranchRequest asks whether a branch exists.
type GetBranchRequest struct {
	Target Target `json:"target"`
	Name   string `json:"name"`
}

// CreateBranchRequest creates a branch from another branch (or the default).
// With IdempotencyKey, and when the provider declares IdempotentWrites, a retry
// of a call that already succeeded returns the original result instead of
// conflict.
type CreateBranchRequest struct {
	Target         Target `json:"target"`
	Name           string `json:"name"`
	From           string `json:"from,omitempty"` // empty = the repository's default branch
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// BranchInfo is a branch and the commit it points at.
type BranchInfo struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
	URL  string `json:"url,omitempty"`
}
