package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
	gitsvc "github.com/Paca-AI/api/internal/service/git"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// GitOps is what the day-to-day Git endpoints need from the Git service.
type GitOps interface {
	NewScope(ctx context.Context, projectID, repositoryID, agentID uuid.UUID, claimedConversation string) gitsvc.Scope

	GetChangeRequestContext(ctx context.Context, sc gitsvc.Scope, number string) (*gitsvc.ChangeRequestContext, error)
	ListChangedFiles(ctx context.Context, sc gitsvc.Scope, number, cursor string) (gitprovider.ChangedFilePage, error)
	GetDiff(ctx context.Context, sc gitsvc.Scope, number, path string, maxBytes int) (gitprovider.Diff, error)
	ListThreads(ctx context.Context, sc gitsvc.Scope, number, cursor string) (gitprovider.ThreadPage, error)
	ListComments(ctx context.Context, sc gitsvc.Scope, kind gitprovider.SubjectKind, number, cursor string) (gitprovider.CommentPage, error)
	GetIssue(ctx context.Context, sc gitsvc.Scope, number string) (gitprovider.Issue, error)
	ListIssues(ctx context.Context, sc gitsvc.Scope, state, cursor string) (gitprovider.IssuePage, error)
	GetChecks(ctx context.Context, sc gitsvc.Scope, ref, changeRequest string) ([]gitprovider.Check, error)
	GetCheckLogs(ctx context.Context, sc gitsvc.Scope, checkID string, tailBytes int) (gitprovider.Logs, error)
	GetFile(ctx context.Context, sc gitsvc.Scope, ref, path string, maxBytes int) (gitprovider.File, error)
	SearchCode(ctx context.Context, sc gitsvc.Scope, query string) (gitprovider.CodeHits, error)

	OpenChangeRequest(ctx context.Context, sc gitsvc.Scope, in gitsvc.OpenInput) (gitprovider.ChangeRequest, error)
	UpdateChangeRequest(ctx context.Context, sc gitsvc.Scope, number string, in gitsvc.UpdateInput) (gitprovider.ChangeRequest, error)
	PostComment(ctx context.Context, sc gitsvc.Scope, kind gitprovider.SubjectKind, number, body string) (gitprovider.CommentResult, error)
	ReplyThread(ctx context.Context, sc gitsvc.Scope, number, threadID, body string) (gitprovider.CommentResult, error)
	ResolveThread(ctx context.Context, sc gitsvc.Scope, number, threadID string, resolved bool) error
	SubmitReview(ctx context.Context, sc gitsvc.Scope, number string, in gitsvc.ReviewInput) (gitprovider.ReviewResult, error)
	AddLabels(ctx context.Context, sc gitsvc.Scope, kind gitprovider.SubjectKind, number string, labels []string) error
	RequestReviewers(ctx context.Context, sc gitsvc.Scope, number string, logins []string) error
	SetAssignees(ctx context.Context, sc gitsvc.Scope, kind gitprovider.SubjectKind, number string, logins []string) error
	RerunChecks(ctx context.Context, sc gitsvc.Scope, ref, checkID string) error
	MergeChangeRequest(ctx context.Context, sc gitsvc.Scope, number, method, expectedHeadSHA string) (gitprovider.MergeResult, error)

	GetProjectSettings(ctx context.Context, projectID uuid.UUID) (*gitdom.ProjectSettings, error)
	SetProjectSettings(ctx context.Context, st *gitdom.ProjectSettings) (*gitdom.ProjectSettings, error)
	ListAgentTriggers(ctx context.Context, agentID uuid.UUID) ([]gitsvc.TriggerView, error)
	SetAgentTrigger(ctx context.Context, agentID uuid.UUID, kind gitdom.TriggerKind, enabled bool, config json.RawMessage) error
	ListProjectRuns(ctx context.Context, projectID uuid.UUID, limit int) ([]*gitdom.ProjectRun, error)
	ListTaskGit(ctx context.Context, projectID, taskID uuid.UUID) (*gitdom.TaskGit, error)
	LinkTaskChangeRequest(ctx context.Context, ref gitsvc.TaskRef, repositoryID uuid.UUID, number string) error
	UnlinkTaskChangeRequest(ctx context.Context, ref gitsvc.TaskRef, repositoryID uuid.UUID, number string) error
	LinkTaskBranch(ctx context.Context, ref gitsvc.TaskRef, repositoryID uuid.UUID, branch string) error
	CreateTaskBranch(ctx context.Context, ref gitsvc.TaskRef, repositoryID uuid.UUID, branch, from string) (gitprovider.BranchInfo, error)
	UnlinkTaskBranch(ctx context.Context, ref gitsvc.TaskRef, repositoryID uuid.UUID, branch string) error
}

// conversationHeader carries the conversation a tool call runs in; the service
// keeps it only if the calling agent owns it.
const conversationHeader = "X-Paca-Conversation-Id"

// WithOps enables the change-request, review, comment and CI endpoints.
func (h *GitHandler) WithOps(o GitOps) *GitHandler {
	h.ops = o
	return h
}

// scope resolves the project, repository and acting account of a request.
func (h *GitHandler) scope(w http.ResponseWriter, r *http.Request) (gitsvc.Scope, bool) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return gitsvc.Scope{}, false
	}
	repoID, ok := gitRepositoryID(w, r)
	if !ok {
		return gitsvc.Scope{}, false
	}
	actor, ok := requireAccountID(w, r)
	if !ok {
		return gitsvc.Scope{}, false
	}
	return h.ops.NewScope(r.Context(), projectID, repoID, actor, r.Header.Get(conversationHeader)), true
}

func subjectKind(w http.ResponseWriter, r *http.Request) (gitprovider.SubjectKind, bool) {
	switch k := gitprovider.SubjectKind(chi.URLParam(r, "kind")); k {
	case gitprovider.SubjectIssue, gitprovider.SubjectChangeRequest:
		return k, true
	}
	presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "subject kind must be issue or change_request"))
	return "", false
}

func intQuery(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

// ── reads ────────────────────────────────────────────────────────────────────

// GetChangeRequest handles GET …/change-requests/:number: the change request
// with its files, threads and checks.
func (h *GitHandler) GetChangeRequest(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	out, err := h.ops.GetChangeRequestContext(r.Context(), sc, chi.URLParam(r, "number"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, out)
}

// ListChangedFiles handles GET …/change-requests/:number/files.
func (h *GitHandler) ListChangedFiles(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	out, err := h.ops.ListChangedFiles(r.Context(), sc, chi.URLParam(r, "number"), r.URL.Query().Get("cursor"))
	h.respond(w, r, out, err)
}

// GetDiff handles GET …/change-requests/:number/diff.
func (h *GitHandler) GetDiff(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	out, err := h.ops.GetDiff(r.Context(), sc, chi.URLParam(r, "number"), r.URL.Query().Get("path"), intQuery(r, "max_bytes"))
	h.respond(w, r, out, err)
}

// ListThreads handles GET …/change-requests/:number/threads.
func (h *GitHandler) ListThreads(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	out, err := h.ops.ListThreads(r.Context(), sc, chi.URLParam(r, "number"), r.URL.Query().Get("cursor"))
	h.respond(w, r, out, err)
}

// ListComments handles GET …/subjects/:kind/:number/comments.
func (h *GitHandler) ListComments(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	kind, ok := subjectKind(w, r)
	if !ok {
		return
	}
	out, err := h.ops.ListComments(r.Context(), sc, kind, chi.URLParam(r, "number"), r.URL.Query().Get("cursor"))
	h.respond(w, r, out, err)
}

// ListIssues handles GET …/issues?state=…&cursor=….
func (h *GitHandler) ListIssues(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	out, err := h.ops.ListIssues(r.Context(), sc, q.Get("state"), q.Get("cursor"))
	h.respond(w, r, out, err)
}

// GetIssue handles GET …/issues/:number.
func (h *GitHandler) GetIssue(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	out, err := h.ops.GetIssue(r.Context(), sc, chi.URLParam(r, "number"))
	h.respond(w, r, out, err)
}

// GetChecks handles GET …/checks?ref=…|change_request=….
func (h *GitHandler) GetChecks(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	out, err := h.ops.GetChecks(r.Context(), sc, q.Get("ref"), q.Get("change_request"))
	h.respond(w, r, out, err)
}

// GetCheckLogs handles GET …/checks/:checkId/logs.
func (h *GitHandler) GetCheckLogs(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	out, err := h.ops.GetCheckLogs(r.Context(), sc, chi.URLParam(r, "checkId"), intQuery(r, "tail_bytes"))
	h.respond(w, r, out, err)
}

// GetFile handles GET …/files?ref=…&path=….
func (h *GitHandler) GetFile(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	out, err := h.ops.GetFile(r.Context(), sc, q.Get("ref"), q.Get("path"), intQuery(r, "max_bytes"))
	h.respond(w, r, out, err)
}

// SearchCode handles GET …/code-search?q=….
func (h *GitHandler) SearchCode(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	out, err := h.ops.SearchCode(r.Context(), sc, r.URL.Query().Get("q"))
	h.respond(w, r, out, err)
}

// ── writes ───────────────────────────────────────────────────────────────────

// OpenChangeRequest handles POST …/change-requests.
func (h *GitHandler) OpenChangeRequest(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req dto.OpenGitChangeRequestRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	out, err := h.ops.OpenChangeRequest(r.Context(), sc, gitsvc.OpenInput{Title: req.Title, Body: req.Body, Head: req.Head,
		Base: req.Base, Draft: req.Draft, Labels: req.Labels, Reviewers: req.Reviewers})
	h.respond(w, r, out, err)
}

// UpdateChangeRequest handles PATCH …/change-requests/:number.
func (h *GitHandler) UpdateChangeRequest(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req dto.UpdateGitChangeRequestRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	in := gitsvc.UpdateInput{Title: req.Title, Body: req.Body, Draft: req.Draft}
	if req.State != nil {
		st := gitprovider.CRState(*req.State)
		in.State = &st
	}
	out, err := h.ops.UpdateChangeRequest(r.Context(), sc, chi.URLParam(r, "number"), in)
	h.respond(w, r, out, err)
}

// PostComment handles POST …/subjects/:kind/:number/comments.
func (h *GitHandler) PostComment(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	kind, ok := subjectKind(w, r)
	if !ok {
		return
	}
	var req dto.GitCommentRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	out, err := h.ops.PostComment(r.Context(), sc, kind, chi.URLParam(r, "number"), req.Body)
	h.respond(w, r, out, err)
}

// ReplyThread handles POST …/change-requests/:number/threads/:threadId/replies.
func (h *GitHandler) ReplyThread(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req dto.GitCommentRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	out, err := h.ops.ReplyThread(r.Context(), sc, chi.URLParam(r, "number"), chi.URLParam(r, "threadId"), req.Body)
	h.respond(w, r, out, err)
}

// ResolveThread handles POST …/change-requests/:number/threads/:threadId/resolution.
func (h *GitHandler) ResolveThread(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req dto.GitResolveThreadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.ops.ResolveThread(r.Context(), sc, chi.URLParam(r, "number"), chi.URLParam(r, "threadId"), req.Resolved); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

func reviewInput(req dto.GitReviewRequest, verdict gitprovider.Verdict) gitsvc.ReviewInput {
	in := gitsvc.ReviewInput{Verdict: verdict, Summary: req.Summary, HeadSHA: req.HeadSHA}
	for _, c := range req.Comments {
		in.Comments = append(in.Comments, gitprovider.InlineComment{Path: c.Path, Line: c.Line, Side: c.Side, Body: c.Body})
	}
	return in
}

// SubmitReview handles POST …/change-requests/:number/reviews: a review that
// comments or requests changes. Approving has its own route and gate.
func (h *GitHandler) SubmitReview(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req dto.GitReviewRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	verdict := gitprovider.Verdict(strings.TrimSpace(req.Verdict))
	if verdict == "" {
		verdict = gitprovider.VerdictComment
	}
	if verdict != gitprovider.VerdictComment && verdict != gitprovider.VerdictRequestChanges {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "verdict must be comment or request_changes; approving uses …/approvals"))
		return
	}
	out, err := h.ops.SubmitReview(r.Context(), sc, chi.URLParam(r, "number"), reviewInput(req, verdict))
	h.respond(w, r, out, err)
}

// ApproveChangeRequest handles POST …/change-requests/:number/approvals.
func (h *GitHandler) ApproveChangeRequest(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req dto.GitReviewRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	out, err := h.ops.SubmitReview(r.Context(), sc, chi.URLParam(r, "number"), reviewInput(req, gitprovider.VerdictApprove))
	h.respond(w, r, out, err)
}

// AddLabels handles POST …/subjects/:kind/:number/labels.
func (h *GitHandler) AddLabels(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	kind, ok := subjectKind(w, r)
	if !ok {
		return
	}
	var req dto.GitLabelsRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.ops.AddLabels(r.Context(), sc, kind, chi.URLParam(r, "number"), req.Labels); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// RequestReviewers handles POST …/change-requests/:number/reviewers.
func (h *GitHandler) RequestReviewers(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req dto.GitPeopleRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.ops.RequestReviewers(r.Context(), sc, chi.URLParam(r, "number"), req.Logins); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// SetAssignees handles PUT …/subjects/:kind/:number/assignees.
func (h *GitHandler) SetAssignees(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	kind, ok := subjectKind(w, r)
	if !ok {
		return
	}
	var req dto.GitPeopleRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.ops.SetAssignees(r.Context(), sc, kind, chi.URLParam(r, "number"), req.Logins); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// RerunChecks handles POST …/checks/rerun.
func (h *GitHandler) RerunChecks(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req dto.GitRerunRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.ops.RerunChecks(r.Context(), sc, req.Ref, req.CheckID); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// MergeChangeRequest handles POST …/change-requests/:number/merge.
func (h *GitHandler) MergeChangeRequest(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req dto.GitMergeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	out, err := h.ops.MergeChangeRequest(r.Context(), sc, chi.URLParam(r, "number"), req.Method, req.ExpectedHeadSHA)
	h.respond(w, r, out, err)
}

func (h *GitHandler) respond(w http.ResponseWriter, r *http.Request, out any, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, out)
}

// ── workflow settings ────────────────────────────────────────────────────────

func settingsResponse(st *gitdom.ProjectSettings) dto.GitProjectSettingsResponse {
	return dto.GitProjectSettingsResponse{DefaultResponderAgentID: st.DefaultResponderAgentID, Paused: st.Paused, MinActorPermission: st.MinActorPermission}
}

// GetSettings handles GET /projects/:projectId/git/settings.
func (h *GitHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	st, err := h.ops.GetProjectSettings(r.Context(), projectID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, settingsResponse(st))
}

// PutSettings handles PUT /projects/:projectId/git/settings.
func (h *GitHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	var req dto.UpdateGitProjectSettingsRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	st, err := h.ops.SetProjectSettings(r.Context(), &gitdom.ProjectSettings{ProjectID: projectID,
		DefaultResponderAgentID: req.DefaultResponderAgentID, Paused: req.Paused, MinActorPermission: strings.TrimSpace(req.MinActorPermission)})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, settingsResponse(st))
}

// ListProjectRuns handles GET /projects/:projectId/git/runs.
func (h *GitHandler) ListProjectRuns(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	runs, err := h.ops.ListProjectRuns(r.Context(), projectID, intQuery(r, "limit"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]dto.GitRunResponse, 0, len(runs))
	for _, run := range runs {
		out = append(out, dto.GitRunFromEntity(run))
	}
	presenter.OK(w, r, out)
}

// ListAgentTriggers handles GET /projects/:projectId/agents/:agentId/git/triggers.
func (h *GitHandler) ListAgentTriggers(w http.ResponseWriter, r *http.Request) {
	agentID, _, ok := h.agentOf(w, r)
	if !ok {
		return
	}
	out, err := h.ops.ListAgentTriggers(r.Context(), agentID)
	h.respond(w, r, out, err)
}

// SetAgentTrigger handles PUT /projects/:projectId/agents/:agentId/git/triggers/:kind.
func (h *GitHandler) SetAgentTrigger(w http.ResponseWriter, r *http.Request) {
	agentID, _, ok := h.agentOf(w, r)
	if !ok {
		return
	}
	var req dto.SetGitTriggerRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.ops.SetAgentTrigger(r.Context(), agentID, gitdom.TriggerKind(chi.URLParam(r, "kind")), req.Enabled, req.Config); err != nil {
		h.fail(w, r, err)
		return
	}
	out, err := h.ops.ListAgentTriggers(r.Context(), agentID)
	h.respond(w, r, out, err)
}

// GetTaskGit handles GET /projects/:projectId/tasks/:taskId/git: the pull/merge
// requests and branches linked to a task. It reads Paca's own records, not the
// platform, so it needs only access to the task.
func (h *GitHandler) GetTaskGit(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid task ID"))
		return
	}
	g, err := h.ops.ListTaskGit(r.Context(), projectID, taskID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GitTaskFromEntity(g))
}

func (h *GitHandler) taskRef(w http.ResponseWriter, r *http.Request) (gitsvc.TaskRef, bool) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return gitsvc.TaskRef{}, false
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid task ID"))
		return gitsvc.TaskRef{}, false
	}
	actor, ok := requireAccountID(w, r)
	if !ok {
		return gitsvc.TaskRef{}, false
	}
	return gitsvc.TaskRef{ProjectID: projectID, TaskID: taskID, ActorID: actor}, true
}

func queryRepoID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.URL.Query().Get("repository_id"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid repository_id"))
		return uuid.Nil, false
	}
	return id, true
}

// LinkTaskChangeRequest handles POST /projects/:projectId/tasks/:taskId/git/change-requests.
func (h *GitHandler) LinkTaskChangeRequest(w http.ResponseWriter, r *http.Request) {
	ref, ok := h.taskRef(w, r)
	if !ok {
		return
	}
	var req dto.LinkGitTaskChangeRequestRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.ops.LinkTaskChangeRequest(r.Context(), ref, req.RepositoryID, req.Number); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// UnlinkTaskChangeRequest handles DELETE …/git/change-requests/:number?repository_id=….
func (h *GitHandler) UnlinkTaskChangeRequest(w http.ResponseWriter, r *http.Request) {
	ref, ok := h.taskRef(w, r)
	if !ok {
		return
	}
	repoID, ok := queryRepoID(w, r)
	if !ok {
		return
	}
	if err := h.ops.UnlinkTaskChangeRequest(r.Context(), ref, repoID, chi.URLParam(r, "number")); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// LinkTaskBranch handles POST …/git/branches.
func (h *GitHandler) LinkTaskBranch(w http.ResponseWriter, r *http.Request) {
	ref, ok := h.taskRef(w, r)
	if !ok {
		return
	}
	var req dto.LinkGitTaskBranchRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.ops.LinkTaskBranch(r.Context(), ref, req.RepositoryID, req.Branch); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// CreateTaskBranch handles POST …/git/branches/new: creates the branch on the
// platform and links it. Writing to the repository, it has its own gate.
func (h *GitHandler) CreateTaskBranch(w http.ResponseWriter, r *http.Request) {
	ref, ok := h.taskRef(w, r)
	if !ok {
		return
	}
	var req dto.CreateGitTaskBranchRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	info, err := h.ops.CreateTaskBranch(r.Context(), ref, req.RepositoryID, req.Branch, req.From)
	h.respond(w, r, info, err)
}

// UnlinkTaskBranch handles DELETE …/git/branches?repository_id=…&branch=….
func (h *GitHandler) UnlinkTaskBranch(w http.ResponseWriter, r *http.Request) {
	ref, ok := h.taskRef(w, r)
	if !ok {
		return
	}
	repoID, ok := queryRepoID(w, r)
	if !ok {
		return
	}
	if err := h.ops.UnlinkTaskBranch(r.Context(), ref, repoID, r.URL.Query().Get("branch")); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}
