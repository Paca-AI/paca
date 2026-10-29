package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// LinkGitRepositoryRequest is the body for POST /projects/:projectId/git/repositories.
type LinkGitRepositoryRequest struct {
	ProviderID string `json:"provider_id"`
	Host       string `json:"host"`
	// PathOrURL is "owner/name", a group path, or a repository URL.
	PathOrURL string `json:"path_or_url"`
	// IdentityID is the connection to link through; empty lets Paca try every
	// connection the project can use.
	IdentityID *uuid.UUID `json:"identity_id"`
	// Description says what the repository is for (optional, at most 1000 characters).
	Description string `json:"description"`
}

// UpdateGitRepositoryRequest is the body for PATCH …/repositories/:id.
type UpdateGitRepositoryRequest struct {
	Description string `json:"description"`
}

// ReconnectGitRepositoryRequest is the body for POST …/repositories/:id/reconnect.
type ReconnectGitRepositoryRequest struct {
	// IdentityID is the connection to use; empty lets Paca try every connection the project can use.
	IdentityID *uuid.UUID `json:"identity_id"`
}

// GitRepositoryResponse is a repository linked to a project. It never carries
// credentials or the provider's opaque references.
type GitRepositoryResponse struct {
	ID            uuid.UUID  `json:"id"`
	ProviderID    string     `json:"provider_id"`
	Host          string     `json:"host"`
	FullPath      string     `json:"full_path"`
	DefaultBranch string     `json:"default_branch"`
	CloneURL      string     `json:"clone_url"`
	Description   string     `json:"description"`
	IdentityID    *uuid.UUID `json:"identity_id"`
	// IdentityHandle is what to @mention to reach the bot serving the repository.
	IdentityHandle string `json:"identity_handle"`
	// WebhookURL is set when the platform must be told by hand where to deliver
	// events for this repository (a token connection whose webhook could not be
	// registered automatically).
	WebhookURL string `json:"webhook_url,omitempty"`
	// WebhookAttempted is false when Paca never tried to register the webhook
	// (a repository carried over from an earlier plugin version).
	WebhookAttempted bool      `json:"webhook_attempted"`
	CreatedAt        time.Time `json:"created_at"`
}

// GitRepositoryFromEntity maps a linked repository to its response.
func GitRepositoryFromEntity(r *gitdom.Repository) GitRepositoryResponse {
	return GitRepositoryResponse{ID: r.ID, ProviderID: r.ProviderID, Host: r.Host, FullPath: r.FullPath,
		DefaultBranch: r.DefaultBranch, CloneURL: r.CloneURL, Description: r.Description, IdentityID: r.IdentityID, IdentityHandle: r.IdentityHandle, WebhookURL: r.WebhookURL, WebhookAttempted: r.WebhookAttempted, CreatedAt: r.CreatedAt}
}

// GitCredentialResponse is a short-lived credential for ONE git operation. It
// is for the workspace tooling's credential helper only: it is sent with
// Cache-Control: no-store and must never be shown to a model or stored.
type GitCredentialResponse struct {
	Username  string     `json:"username"`
	Secret    string     `json:"secret"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// GitWebhookResponse acknowledges a webhook delivery.
type GitWebhookResponse struct {
	Events   int `json:"events"`
	Stored   int `json:"stored"`
	Replayed int `json:"replayed"`
}

// GitIdentityResponse is a bot identity on a Git platform. Secrets and the
// provider's opaque reference stay out of it.
type GitIdentityResponse struct {
	ID         uuid.UUID `json:"id"`
	ProviderID string    `json:"provider_id"`
	Host       string    `json:"host"`
	Kind       string    `json:"kind"`
	// Scope is shared (every project may use it), project or agent.
	Scope string `json:"scope"`
	// ProjectID is the one project a project-scoped connection belongs to.
	ProjectID   *uuid.UUID `json:"project_id"`
	Handle      string     `json:"handle"`
	DisplayName string     `json:"display_name"`
	ExpiresAt   *time.Time `json:"expires_at"`
	// RepositoryCount is how many linked repositories, in every project, use it.
	RepositoryCount int       `json:"repository_count"`
	CreatedAt       time.Time `json:"created_at"`
}

// GitIdentityFromEntity maps an identity to its response.
func GitIdentityFromEntity(i *gitdom.Identity) GitIdentityResponse {
	return GitIdentityResponse{ID: i.ID, ProviderID: i.ProviderID, Host: i.Host, Kind: i.Kind, Scope: string(i.Scope), ProjectID: i.ProjectID,
		Handle: i.Handle, DisplayName: i.DisplayName, ExpiresAt: i.ExpiresAt, RepositoryCount: i.RepositoryCount, CreatedAt: i.CreatedAt}
}

// BeginGitSetupRequest is the body for starting identity setup. The provider
// defines which Params it reads (for example a name, or an organization).
type BeginGitSetupRequest struct {
	ProviderID string            `json:"provider_id"`
	Host       string            `json:"host"`
	ReturnURL  string            `json:"return_url"`
	Params     map[string]string `json:"params"`
}

// CompleteGitSetupRequest is the body for finishing one leg of identity setup:
// the state the flow began with plus whatever the platform's redirect (or the
// user's pasted token) supplied.
type CompleteGitSetupRequest struct {
	State  string            `json:"state"`
	Params map[string]string `json:"params"`
}

// GitSetupFieldResponse is one input of a token form.
type GitSetupFieldResponse struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Help     string `json:"help,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// GitSetupStepResponse tells the browser what to do next.
type GitSetupStepResponse struct {
	Kind   string                  `json:"kind"` // redirect | form_post | token_form | done
	URL    string                  `json:"url,omitempty"`
	Fields map[string]string       `json:"fields,omitempty"`
	Form   []GitSetupFieldResponse `json:"form,omitempty"`
	Prompt string                  `json:"prompt,omitempty"`
	State  string                  `json:"state,omitempty"`
}

// GitSetupStepFromProvider maps a provider step to its response.
func GitSetupStepFromProvider(s *gitprovider.SetupStep) *GitSetupStepResponse {
	if s == nil {
		return nil
	}
	out := &GitSetupStepResponse{Kind: string(s.Kind), URL: s.URL, Fields: s.Fields, Prompt: s.Prompt, State: s.State}
	for _, f := range s.Form {
		out.Form = append(out.Form, GitSetupFieldResponse{Name: f.Name, Label: f.Label, Help: f.Help, Secret: f.Secret, Required: f.Required})
	}
	return out
}

// GitSetupCompletionResponse is the outcome of one leg of identity setup.
type GitSetupCompletionResponse struct {
	Identity  GitIdentityResponse   `json:"identity"`
	Next      *GitSetupStepResponse `json:"next"`
	ReturnURL string                `json:"return_url"`
	Done      bool                  `json:"done"`
}

// BeginGitAccessRequest is the body for POST /projects/:projectId/git/access/begin:
// grant the host's shared identity access to one more account or organization.
type BeginGitAccessRequest struct {
	ProviderID string `json:"provider_id"`
	Host       string `json:"host"`
	// Owner is the account or organization of the repository being linked, when known.
	Owner     string `json:"owner"`
	ReturnURL string `json:"return_url"`
	// IdentityID is the connection to grant; empty when the project has only one for the provider.
	IdentityID *uuid.UUID `json:"identity_id"`
}

// CompleteGitAccessRequest is the body for POST /projects/:projectId/git/access/complete.
type CompleteGitAccessRequest struct {
	State  string            `json:"state"`
	Params map[string]string `json:"params"`
}

// GitAccessCompletionResponse is the outcome of a finished access grant.
type GitAccessCompletionResponse struct {
	Owner     string `json:"owner"`
	ReturnURL string `json:"return_url"`
}

// BeginAgentGitAccessRequest is the body for
// POST /projects/:projectId/agents/:agentId/git/identity/access/begin: grant the
// agent's own identity access to one more account or organization.
type BeginAgentGitAccessRequest struct {
	ProviderID string `json:"provider_id"`
	Owner      string `json:"owner"`
	ReturnURL  string `json:"return_url"`
}

// OpenGitChangeRequestRequest is the body for POST …/git/repositories/:id/change-requests.
type OpenGitChangeRequestRequest struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Head      string   `json:"head"`
	Base      string   `json:"base"`
	Draft     bool     `json:"draft"`
	Labels    []string `json:"labels"`
	Reviewers []string `json:"reviewers"`
}

// UpdateGitChangeRequestRequest is the body for PATCH …/change-requests/:number.
type UpdateGitChangeRequestRequest struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
	Draft *bool   `json:"draft"`
	State *string `json:"state"` // open | closed
}

// GitCommentRequest carries the text of a comment or thread reply.
type GitCommentRequest struct {
	Body string `json:"body"`
}

// GitResolveThreadRequest resolves (true) or reopens (false) a thread.
type GitResolveThreadRequest struct {
	Resolved bool `json:"resolved"`
}

// GitInlineComment is a review comment pinned to a line.
type GitInlineComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

// GitReviewRequest is the body for …/reviews and …/approvals. The verdict comes
// from the route (comment | request_changes, or approve), never the body, so a
// route gate decides who may approve.
type GitReviewRequest struct {
	Verdict  string             `json:"verdict"` // reviews only: comment | request_changes
	Summary  string             `json:"summary"`
	Comments []GitInlineComment `json:"comments"`
	HeadSHA  string             `json:"head_sha"`
}

// GitLabelsRequest adds labels.
type GitLabelsRequest struct {
	Labels []string `json:"labels"`
}

// GitPeopleRequest names reviewers or assignees.
type GitPeopleRequest struct {
	Logins []string `json:"logins"`
}

// GitRerunRequest re-runs failed checks of a ref.
type GitRerunRequest struct {
	Ref     string `json:"ref"`
	CheckID string `json:"check_id"`
}

// GitMergeRequest merges a change request.
type GitMergeRequest struct {
	Method          string `json:"method"`
	ExpectedHeadSHA string `json:"expected_head_sha"`
}

// GitProjectSettingsResponse is a project's Git workflow settings.
type GitProjectSettingsResponse struct {
	DefaultResponderAgentID *uuid.UUID `json:"default_responder_agent_id"`
	Paused                  bool       `json:"paused"`
	MinActorPermission      string     `json:"min_actor_permission"`
}

// UpdateGitProjectSettingsRequest replaces a project's Git workflow settings.
type UpdateGitProjectSettingsRequest struct {
	DefaultResponderAgentID *uuid.UUID `json:"default_responder_agent_id"`
	Paused                  bool       `json:"paused"`
	MinActorPermission      string     `json:"min_actor_permission"`
}

// SetGitTriggerRequest switches an agent's trigger on or off.
type SetGitTriggerRequest struct {
	Enabled bool            `json:"enabled"`
	Config  json.RawMessage `json:"config"`
}

// GitRunResponse is an agent run a Git event started.
type GitRunResponse struct {
	ID             uuid.UUID  `json:"id"`
	RepositoryID   uuid.UUID  `json:"repository_id"`
	Repository     string     `json:"repository"`
	AgentID        uuid.UUID  `json:"agent_id"`
	Trigger        string     `json:"trigger"`
	SubjectKind    string     `json:"subject_kind"`
	SubjectNumber  string     `json:"subject_number"`
	ConversationID *uuid.UUID `json:"conversation_id"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at"`
}

// GitRunFromEntity maps a run to its response.
func GitRunFromEntity(r *gitdom.ProjectRun) GitRunResponse {
	return GitRunResponse{ID: r.ID, RepositoryID: r.RepositoryID, Repository: r.RepositoryPath, AgentID: r.AgentID,
		Trigger: string(r.Trigger), SubjectKind: r.SubjectKind, SubjectNumber: r.SubjectNumber, ConversationID: r.ConversationID,
		Status: string(r.Status), CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt}
}

// GitTaskChangeRequestResponse is a pull/merge request linked to a task.
type GitTaskChangeRequestResponse struct {
	RepositoryID uuid.UUID  `json:"repository_id"`
	Repository   string     `json:"repository"`
	ProviderID   string     `json:"provider_id"`
	Number       string     `json:"number"`
	Title        string     `json:"title"`
	State        string     `json:"state"`
	Draft        bool       `json:"draft"`
	HeadRef      string     `json:"head_ref"`
	BaseRef      string     `json:"base_ref"`
	Author       string     `json:"author"`
	URL          string     `json:"url"`
	MergedAt     *time.Time `json:"merged_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// CI is the state of the head commit's checks; null when it has none.
	CI *GitCISummaryResponse `json:"ci"`
}

// GitCISummaryResponse summarizes a change request's checks.
type GitCISummaryResponse struct {
	State   string   `json:"state"` // passing | failing | pending
	Total   int      `json:"total"`
	Passed  int      `json:"passed"`
	Failed  int      `json:"failed"`
	Pending int      `json:"pending"`
	Failing []string `json:"failing"`
}

// LinkGitTaskChangeRequestRequest links a pull/merge request to a task.
type LinkGitTaskChangeRequestRequest struct {
	RepositoryID uuid.UUID `json:"repository_id"`
	Number       string    `json:"number"`
}

// LinkGitTaskBranchRequest links an existing branch to a task.
type LinkGitTaskBranchRequest struct {
	RepositoryID uuid.UUID `json:"repository_id"`
	Branch       string    `json:"branch"`
}

// CreateGitTaskBranchRequest creates a branch for a task.
type CreateGitTaskBranchRequest struct {
	RepositoryID uuid.UUID `json:"repository_id"`
	Branch       string    `json:"branch"`
	From         string    `json:"from"`
}

// GitTaskBranchResponse is a branch linked to a task.
type GitTaskBranchResponse struct {
	RepositoryID uuid.UUID `json:"repository_id"`
	Repository   string    `json:"repository"`
	ProviderID   string    `json:"provider_id"`
	Branch       string    `json:"branch"`
}

// GitTaskResponse is what Git knows about a task.
type GitTaskResponse struct {
	ChangeRequests []GitTaskChangeRequestResponse `json:"change_requests"`
	Branches       []GitTaskBranchResponse        `json:"branches"`
}

// GitTaskFromEntity maps a task's Git links to their response.
func GitTaskFromEntity(g *gitdom.TaskGit) GitTaskResponse {
	out := GitTaskResponse{ChangeRequests: []GitTaskChangeRequestResponse{}, Branches: []GitTaskBranchResponse{}}
	for _, c := range g.ChangeRequests {
		var ci *GitCISummaryResponse
		if c.CI != nil {
			ci = &GitCISummaryResponse{State: c.CI.State, Total: c.CI.Total, Passed: c.CI.Passed, Failed: c.CI.Failed, Pending: c.CI.Pending, Failing: c.CI.Failing}
		}
		out.ChangeRequests = append(out.ChangeRequests, GitTaskChangeRequestResponse{CI: ci, RepositoryID: c.RepositoryID, Repository: c.RepositoryPath,
			ProviderID: c.ProviderID, Number: c.Number, Title: c.Title, State: c.State, Draft: c.Draft, HeadRef: c.HeadRef, BaseRef: c.BaseRef,
			Author: c.Author, URL: c.URL, MergedAt: c.MergedAt, UpdatedAt: c.UpdatedAt})
	}
	for _, b := range g.Branches {
		out.Branches = append(out.Branches, GitTaskBranchResponse{RepositoryID: b.RepositoryID, Repository: b.RepositoryPath, ProviderID: b.ProviderID, Branch: b.Branch})
	}
	return out
}
