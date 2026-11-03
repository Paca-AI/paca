package gitsvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// maxBodyBytes caps a comment, review or description an agent posts.
const maxBodyBytes = 60 << 10

// OpsAPI is the part of a provider the day-to-day operations use: reading
// change requests, issues and CI, and writing comments, reviews and metadata.
// gitprovider.Client implements it.
type OpsAPI interface {
	GetChangeRequest(ctx context.Context, req gitprovider.CRRequest) (gitprovider.ChangeRequest, error)
	ListChangedFiles(ctx context.Context, req gitprovider.PagedCRRequest) (gitprovider.ChangedFilePage, error)
	GetDiff(ctx context.Context, req gitprovider.DiffRequest) (gitprovider.Diff, error)
	ListThreads(ctx context.Context, req gitprovider.PagedCRRequest) (gitprovider.ThreadPage, error)
	ListComments(ctx context.Context, req gitprovider.ListCommentsRequest) (gitprovider.CommentPage, error)
	GetIssue(ctx context.Context, req gitprovider.IssueRequest) (gitprovider.Issue, error)
	GetChecks(ctx context.Context, req gitprovider.ChecksRequest) ([]gitprovider.Check, error)
	GetCheckLogs(ctx context.Context, req gitprovider.CheckLogsRequest) (gitprovider.Logs, error)
	GetFile(ctx context.Context, req gitprovider.FileRequest) (gitprovider.File, error)
	SearchCode(ctx context.Context, req gitprovider.CodeSearchRequest) (gitprovider.CodeHits, error)

	OpenChangeRequest(ctx context.Context, req gitprovider.OpenCRRequest) (gitprovider.ChangeRequest, error)
	UpdateChangeRequest(ctx context.Context, req gitprovider.UpdateCRRequest) (gitprovider.ChangeRequest, error)
	Comment(ctx context.Context, req gitprovider.CommentRequest) (gitprovider.CommentResult, error)
	ReplyThread(ctx context.Context, req gitprovider.ReplyThreadRequest) (gitprovider.CommentResult, error)
	SubmitReview(ctx context.Context, req gitprovider.SubmitReviewRequest) (gitprovider.ReviewResult, error)
	SetStatus(ctx context.Context, req gitprovider.SetStatusRequest) (gitprovider.StatusResult, error)
	ResolveThread(ctx context.Context, req gitprovider.ResolveThreadRequest) error
	AddLabels(ctx context.Context, req gitprovider.LabelsRequest) error
	RequestReviewers(ctx context.Context, req gitprovider.PeopleRequest) error
	SetAssignees(ctx context.Context, req gitprovider.PeopleRequest) error
	MergeChangeRequest(ctx context.Context, req gitprovider.MergeRequest) (gitprovider.MergeResult, error)
	RerunChecks(ctx context.Context, req gitprovider.RerunRequest) error
	React(ctx context.Context, req gitprovider.ReactRequest) error
	GetActorPermission(ctx context.Context, req gitprovider.ActorPermissionRequest) (gitprovider.Permission, error)
	GetBranch(ctx context.Context, req gitprovider.GetBranchRequest) (gitprovider.BranchInfo, error)
	CreateBranch(ctx context.Context, req gitprovider.CreateBranchRequest) (gitprovider.BranchInfo, error)
}

// Scope says whose repository an operation targets and who performs it. The
// acting agent selects the identity (its own, else the repository's) and is
// recorded in the audit log and the comment index.
type Scope struct {
	ProjectID      uuid.UUID
	RepositoryID   uuid.UUID
	AgentID        uuid.UUID
	ConversationID *uuid.UUID
}

// AgentNamer names an agent for comment footers and vouches for the
// conversation a tool call claims to belong to.
type AgentNamer interface {
	AgentName(ctx context.Context, agentID uuid.UUID) string
	// OwnsConversation reports whether the conversation is the agent's, in the project.
	OwnsConversation(ctx context.Context, projectID, agentID, conversationID uuid.UUID) bool
}

// NewScope builds a Scope for a tool call. A conversation id the caller
// claimed is kept only if the agent really owns that conversation: it ends up
// in a link on the platform, so it must not point at someone else's.
func (s *Service) NewScope(ctx context.Context, projectID, repositoryID, agentID uuid.UUID, claimedConversation string) Scope {
	sc := Scope{ProjectID: projectID, RepositoryID: repositoryID, AgentID: agentID}
	if id, err := uuid.Parse(strings.TrimSpace(claimedConversation)); err == nil && s.namer != nil && s.namer.OwnsConversation(ctx, projectID, agentID, id) {
		sc.ConversationID = &id
	}
	return sc
}

// WithAgentNamer sets how comment footers name the acting agent.
func (s *Service) WithAgentNamer(n AgentNamer) *Service {
	s.namer = n
	return s
}

type opTarget struct {
	repo   *gitdom.Repository
	target gitprovider.Target
	api    OpsAPI
}

// resolve finds the repository (it must belong to the project), picks the
// identity and gets the provider. A repository of another project is reported
// as not found so its existence is not revealed.
func (s *Service) resolve(ctx context.Context, sc Scope) (*opTarget, error) {
	repo, err := s.store.FindRepository(ctx, sc.RepositoryID)
	if err != nil {
		return nil, err
	}
	if repo.ProjectID != sc.ProjectID {
		return nil, gitdom.ErrNotFound
	}
	ident, err := s.pickIdentity(ctx, repo, &sc.AgentID)
	if err != nil {
		return nil, err
	}
	prov, err := s.providers.Provider(ctx, repo.ProviderID)
	if err != nil {
		return nil, err
	}
	api, ok := prov.(OpsAPI)
	if !ok {
		return nil, gitdom.ErrProviderUnavailable
	}
	return &opTarget{repo: repo, api: api, target: gitprovider.Target{Identity: identityRef(ident), Repo: repoRef(repo)}}, nil
}

// need fails with ErrUnsupported unless the provider declares the capability.
func (s *Service) need(ctx context.Context, providerID string, has func(gitprovider.Capabilities) bool) error {
	info, err := s.providers.Describe(ctx, providerID)
	if err != nil {
		return err
	}
	if !has(info.Capabilities) {
		return gitdom.ErrUnsupported
	}
	return nil
}

// run performs one provider call and writes its audit row.
func run[T any](s *Service, ctx context.Context, sc Scope, t *opTarget, tool, subject string, call func() (T, error)) (T, error) {
	out, err := call()
	s.auditOp(ctx, sc, t.repo, tool, subject, err)
	return out, err
}

func (s *Service) auditOp(ctx context.Context, sc Scope, repo *gitdom.Repository, tool, subject string, err error) {
	e := &gitdom.ActionLogEntry{ProjectID: &repo.ProjectID, RepositoryID: &repo.ID, ConversationID: sc.ConversationID,
		ActorAccountID: &sc.AgentID, Tool: tool, Subject: subject, OK: err == nil}
	if err != nil {
		var pe *gitprovider.Error
		if errors.As(err, &pe) {
			e.Detail = string(pe.Code)
		} else {
			e.Detail = "error"
		}
	}
	_ = s.store.AppendActionLog(ctx, e)
}

func subjectLabel(kind gitprovider.SubjectKind, number string) string {
	return string(kind) + "#" + number
}

func checkSubject(kind gitprovider.SubjectKind, number string) (gitprovider.SubjectRef, error) {
	if kind != gitprovider.SubjectIssue && kind != gitprovider.SubjectChangeRequest {
		return gitprovider.SubjectRef{}, fmt.Errorf("%w: subject kind must be issue or change_request", gitdom.ErrInvalidInput)
	}
	if strings.TrimSpace(number) == "" {
		return gitprovider.SubjectRef{}, fmt.Errorf("%w: number is required", gitdom.ErrInvalidInput)
	}
	return gitprovider.SubjectRef{Kind: kind, Number: number}, nil
}

func checkBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%w: body is required", gitdom.ErrInvalidInput)
	}
	if len(body) > maxBodyBytes {
		return fmt.Errorf("%w: body is too long", gitdom.ErrInvalidInput)
	}
	return nil
}

// idempotencyKey makes a retried tool call post once: the same agent run
// repeating the same operation on the same subject with the same text.
func idempotencyKey(sc Scope, op, subject, text string) string {
	conv := ""
	if sc.ConversationID != nil {
		conv = sc.ConversationID.String()
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{sc.RepositoryID.String(), sc.AgentID.String(), conv, op, subject, text}, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// Footer is what core appends to everything an agent posts, so readers can tell
// which agent wrote it when several share one bot.
func (s *Service) footer(ctx context.Context, sc Scope) string {
	name := "an agent"
	if s.namer != nil {
		if n := strings.TrimSpace(s.namer.AgentName(ctx, sc.AgentID)); n != "" {
			name = "**" + n + "**"
		}
	}
	if s.publicURL != "" && sc.ConversationID != nil {
		return fmt.Sprintf("\n\n---\n<sub>Posted by %s via [Paca](%s/projects/%s/conversations/%s)</sub>", name, s.publicURL, sc.ProjectID, sc.ConversationID)
	}
	return fmt.Sprintf("\n\n---\n<sub>Posted by %s via Paca</sub>", name)
}

func (s *Service) index(ctx context.Context, sc Scope, repo *gitdom.Repository, kind gitprovider.SubjectKind, number, commentID, threadID string) {
	if commentID == "" {
		return
	}
	agent := sc.AgentID
	_ = s.store.IndexComment(ctx, &gitdom.IndexedComment{RepositoryID: repo.ID, CommentID: commentID, SubjectKind: string(kind),
		SubjectNumber: number, ThreadID: threadID, AgentID: &agent, ConversationID: sc.ConversationID})
}

// ── reads ────────────────────────────────────────────────────────────────────

// ChangeRequestContext is everything an agent usually needs before touching a
// change request, in one call. A part that failed to load is named in Warnings
// instead of failing the whole call.
type ChangeRequestContext struct {
	ChangeRequest   gitprovider.ChangeRequest `json:"change_request"`
	Files           []gitprovider.ChangedFile `json:"files"`
	FilesNextCursor string                    `json:"files_next_cursor,omitempty"`
	Threads         []gitprovider.Thread      `json:"threads"`
	ThreadsNext     string                    `json:"threads_next_cursor,omitempty"`
	UnresolvedCount int                       `json:"unresolved_threads"`
	Checks          []gitprovider.Check       `json:"checks"`
	OpenedByYou     bool                      `json:"opened_by_you"`
	Warnings        []string                  `json:"warnings,omitempty"`
}

// GetChangeRequestContext loads a change request with its first page of files
// and threads and its latest checks.
func (s *Service) GetChangeRequestContext(ctx context.Context, sc Scope, number string) (*ChangeRequestContext, error) {
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return nil, err
	}
	subject := subjectLabel(gitprovider.SubjectChangeRequest, number)
	cr, err := run(s, ctx, sc, t, "change_request.get", subject, func() (gitprovider.ChangeRequest, error) {
		return t.api.GetChangeRequest(ctx, gitprovider.CRRequest{Target: t.target, Number: number})
	})
	if err != nil {
		return nil, err
	}
	out := &ChangeRequestContext{ChangeRequest: cr, Files: []gitprovider.ChangedFile{}, Threads: []gitprovider.Thread{}, Checks: []gitprovider.Check{}}
	if files, err := t.api.ListChangedFiles(ctx, gitprovider.PagedCRRequest{Target: t.target, Number: number}); err != nil {
		out.Warnings = append(out.Warnings, "changed files could not be loaded")
	} else {
		out.Files, out.FilesNextCursor = nonNil(files.Items), files.NextCursor
	}
	if threads, err := t.api.ListThreads(ctx, gitprovider.PagedCRRequest{Target: t.target, Number: number}); err != nil {
		out.Warnings = append(out.Warnings, "review threads could not be loaded")
	} else {
		out.Threads, out.ThreadsNext = nonNil(threads.Items), threads.NextCursor
		for _, th := range threads.Items {
			if !th.Resolved && !th.Outdated {
				out.UnresolvedCount++
			}
		}
	}
	if cr.Head.SHA != "" {
		if checks, err := t.api.GetChecks(ctx, gitprovider.ChecksRequest{Target: t.target, Ref: cr.Head.SHA}); err != nil {
			out.Warnings = append(out.Warnings, "checks could not be loaded")
		} else {
			out.Checks = nonNil(checks)
		}
	}
	if cached, err := s.store.FindChangeRequest(ctx, t.repo.ID, number); err == nil && cached.OpenedByAgentID != nil {
		out.OpenedByYou = *cached.OpenedByAgentID == sc.AgentID
	}
	return out, nil
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// ListChangedFiles pages a change request's files.
func (s *Service) ListChangedFiles(ctx context.Context, sc Scope, number, cursor string) (gitprovider.ChangedFilePage, error) {
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.ChangedFilePage{}, err
	}
	return run(s, ctx, sc, t, "change_request.files", subjectLabel(gitprovider.SubjectChangeRequest, number), func() (gitprovider.ChangedFilePage, error) {
		return t.api.ListChangedFiles(ctx, gitprovider.PagedCRRequest{Target: t.target, Number: number, Cursor: cursor})
	})
}

// maxDiffBytes caps a diff an agent can pull in one call.
const maxDiffBytes = 400 << 10

// GetDiff returns a change request's diff, whole or for one file, size-capped.
func (s *Service) GetDiff(ctx context.Context, sc Scope, number, path string, maxBytes int) (gitprovider.Diff, error) {
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.Diff{}, err
	}
	if maxBytes <= 0 || maxBytes > maxDiffBytes {
		maxBytes = maxDiffBytes
	}
	return run(s, ctx, sc, t, "change_request.diff", subjectLabel(gitprovider.SubjectChangeRequest, number), func() (gitprovider.Diff, error) {
		return t.api.GetDiff(ctx, gitprovider.DiffRequest{Target: t.target, Number: number, Path: path, MaxBytes: maxBytes})
	})
}

// ListThreads pages a change request's review threads.
func (s *Service) ListThreads(ctx context.Context, sc Scope, number, cursor string) (gitprovider.ThreadPage, error) {
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.ThreadPage{}, err
	}
	return run(s, ctx, sc, t, "review.threads", subjectLabel(gitprovider.SubjectChangeRequest, number), func() (gitprovider.ThreadPage, error) {
		return t.api.ListThreads(ctx, gitprovider.PagedCRRequest{Target: t.target, Number: number, Cursor: cursor})
	})
}

// ListComments pages the comments of an issue or change request.
func (s *Service) ListComments(ctx context.Context, sc Scope, kind gitprovider.SubjectKind, number, cursor string) (gitprovider.CommentPage, error) {
	ref, err := checkSubject(kind, number)
	if err != nil {
		return gitprovider.CommentPage{}, err
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.CommentPage{}, err
	}
	return run(s, ctx, sc, t, "comment.list", subjectLabel(kind, number), func() (gitprovider.CommentPage, error) {
		return t.api.ListComments(ctx, gitprovider.ListCommentsRequest{Target: t.target, Subject: ref, Cursor: cursor})
	})
}

// ListIssues pages repository issues when the provider supports it.
func (s *Service) ListIssues(ctx context.Context, sc Scope, state, cursor string) (gitprovider.IssuePage, error) {
	switch state {
	case "", "open", "closed", "all":
	default:
		return gitprovider.IssuePage{}, fmt.Errorf("%w: state must be open, closed or all", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.IssuePage{}, err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.IssueList }); err != nil {
		return gitprovider.IssuePage{}, err
	}
	lister, ok := t.api.(interface {
		ListIssues(context.Context, gitprovider.ListIssuesRequest) (gitprovider.IssuePage, error)
	})
	if !ok {
		return gitprovider.IssuePage{}, gitdom.ErrUnsupported
	}
	page, err := run(s, ctx, sc, t, "issue.list", "", func() (gitprovider.IssuePage, error) {
		return lister.ListIssues(ctx, gitprovider.ListIssuesRequest{Target: t.target, State: state, Cursor: cursor})
	})
	page.Items = nonNil(page.Items)
	return page, err
}

// GetIssue reads one issue.
func (s *Service) GetIssue(ctx context.Context, sc Scope, number string) (gitprovider.Issue, error) {
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.Issue{}, err
	}
	return run(s, ctx, sc, t, "issue.get", subjectLabel(gitprovider.SubjectIssue, number), func() (gitprovider.Issue, error) {
		return t.api.GetIssue(ctx, gitprovider.IssueRequest{Target: t.target, Number: number})
	})
}

// GetChecks returns the CI results of a ref, or of a change request's head when
// only the number is given.
func (s *Service) GetChecks(ctx context.Context, sc Scope, ref, changeRequest string) ([]gitprovider.Check, error) {
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return nil, err
	}
	if ref == "" && changeRequest != "" {
		cr, err := t.api.GetChangeRequest(ctx, gitprovider.CRRequest{Target: t.target, Number: changeRequest})
		if err != nil {
			s.auditOp(ctx, sc, t.repo, "checks.get", subjectLabel(gitprovider.SubjectChangeRequest, changeRequest), err)
			return nil, err
		}
		ref = cr.Head.SHA
	}
	if ref == "" {
		return nil, fmt.Errorf("%w: ref or change_request is required", gitdom.ErrInvalidInput)
	}
	checks, err := run(s, ctx, sc, t, "checks.get", ref, func() ([]gitprovider.Check, error) {
		return t.api.GetChecks(ctx, gitprovider.ChecksRequest{Target: t.target, Ref: ref})
	})
	return nonNil(checks), err
}

// maxLogBytes caps the tail of a check log an agent can pull.
const maxLogBytes = 64 << 10

// GetCheckLogs returns the tail of a check's log with likely secrets masked.
func (s *Service) GetCheckLogs(ctx context.Context, sc Scope, checkID string, tailBytes int) (gitprovider.Logs, error) {
	if strings.TrimSpace(checkID) == "" {
		return gitprovider.Logs{}, fmt.Errorf("%w: check_id is required", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.Logs{}, err
	}
	if tailBytes <= 0 || tailBytes > maxLogBytes {
		tailBytes = maxLogBytes
	}
	logs, err := run(s, ctx, sc, t, "checks.logs", checkID, func() (gitprovider.Logs, error) {
		return t.api.GetCheckLogs(ctx, gitprovider.CheckLogsRequest{Target: t.target, CheckID: checkID, TailBytes: tailBytes})
	})
	logs.Text = RedactSecrets(logs.Text)
	return logs, err
}

// GetFile reads a file at a ref without a checkout.
func (s *Service) GetFile(ctx context.Context, sc Scope, ref, path string, maxBytes int) (gitprovider.File, error) {
	if strings.TrimSpace(path) == "" {
		return gitprovider.File{}, fmt.Errorf("%w: path is required", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.File{}, err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.FileRead }); err != nil {
		return gitprovider.File{}, err
	}
	if maxBytes <= 0 || maxBytes > maxDiffBytes {
		maxBytes = maxDiffBytes
	}
	return run(s, ctx, sc, t, "repo.file", path, func() (gitprovider.File, error) {
		return t.api.GetFile(ctx, gitprovider.FileRequest{Target: t.target, Ref: ref, Path: path, MaxBytes: maxBytes})
	})
}

// SearchCode searches the repository's code.
func (s *Service) SearchCode(ctx context.Context, sc Scope, query string) (gitprovider.CodeHits, error) {
	if strings.TrimSpace(query) == "" {
		return gitprovider.CodeHits{}, fmt.Errorf("%w: query is required", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.CodeHits{}, err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.CodeSearch }); err != nil {
		return gitprovider.CodeHits{}, err
	}
	return run(s, ctx, sc, t, "repo.search", query, func() (gitprovider.CodeHits, error) {
		return t.api.SearchCode(ctx, gitprovider.CodeSearchRequest{Target: t.target, Query: query})
	})
}

// ── writes ───────────────────────────────────────────────────────────────────

// OpenInput describes a change request to open.
type OpenInput struct {
	Title     string
	Body      string
	Head      string
	Base      string // empty = the repository's default branch
	Draft     bool
	Labels    []string
	Reviewers []string
}

// OpenChangeRequest opens a change request from a pushed branch and records the
// acting agent as its owner, so feedback and CI failures return to it.
func (s *Service) OpenChangeRequest(ctx context.Context, sc Scope, in OpenInput) (gitprovider.ChangeRequest, error) {
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Head) == "" {
		return gitprovider.ChangeRequest{}, fmt.Errorf("%w: title and head are required", gitdom.ErrInvalidInput)
	}
	if len(in.Body) > maxBodyBytes {
		return gitprovider.ChangeRequest{}, fmt.Errorf("%w: body is too long", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.ChangeRequest{}, err
	}
	base := in.Base
	if base == "" {
		base = t.repo.DefaultBranch
	}
	cr, err := run(s, ctx, sc, t, "change_request.open", in.Head, func() (gitprovider.ChangeRequest, error) {
		return t.api.OpenChangeRequest(ctx, gitprovider.OpenCRRequest{Target: t.target, Title: in.Title, Body: in.Body + s.footer(ctx, sc),
			Head: in.Head, Base: base, Draft: in.Draft, Labels: in.Labels, Reviewers: in.Reviewers,
			IdempotencyKey: idempotencyKey(sc, "open", in.Head, in.Title)})
	})
	if err != nil {
		return cr, err
	}
	agent := sc.AgentID
	_, _ = s.saveChangeRequest(ctx, t.repo, &gitdom.ChangeRequest{RepositoryID: t.repo.ID, Number: cr.Number, Title: cr.Title, State: string(cr.State),
		Draft: cr.Draft, HeadRef: cr.Head.Ref, HeadSHA: cr.Head.SHA, BaseRef: cr.Base.Ref, Author: cr.Author, URL: cr.URL,
		OpenedByAgentID: &agent, OpenedConversationID: sc.ConversationID})
	return cr, nil
}

// UpdateInput edits a change request; nil fields stay as they are.
type UpdateInput struct {
	Title *string
	Body  *string
	Draft *bool
	State *gitprovider.CRState
}

// UpdateChangeRequest edits a change request.
func (s *Service) UpdateChangeRequest(ctx context.Context, sc Scope, number string, in UpdateInput) (gitprovider.ChangeRequest, error) {
	if in.State != nil && *in.State != gitprovider.CROpen && *in.State != gitprovider.CRClosed {
		return gitprovider.ChangeRequest{}, fmt.Errorf("%w: state must be open or closed; merging is a separate operation", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.ChangeRequest{}, err
	}
	return run(s, ctx, sc, t, "change_request.update", subjectLabel(gitprovider.SubjectChangeRequest, number), func() (gitprovider.ChangeRequest, error) {
		return t.api.UpdateChangeRequest(ctx, gitprovider.UpdateCRRequest{Target: t.target, Number: number, Title: in.Title, Body: in.Body, Draft: in.Draft, State: in.State})
	})
}

// PostComment posts a top-level comment on an issue or change request, with the
// attribution footer, and indexes it so replies come back to this agent.
func (s *Service) PostComment(ctx context.Context, sc Scope, kind gitprovider.SubjectKind, number, body string) (gitprovider.CommentResult, error) {
	ref, err := checkSubject(kind, number)
	if err != nil {
		return gitprovider.CommentResult{}, err
	}
	if err := checkBody(body); err != nil {
		return gitprovider.CommentResult{}, err
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.CommentResult{}, err
	}
	res, err := run(s, ctx, sc, t, "comment.post", subjectLabel(kind, number), func() (gitprovider.CommentResult, error) {
		return t.api.Comment(ctx, gitprovider.CommentRequest{Target: t.target, Subject: ref, Body: body + s.footer(ctx, sc),
			IdempotencyKey: idempotencyKey(sc, "comment", subjectLabel(kind, number), body)})
	})
	if err == nil {
		s.index(ctx, sc, t.repo, kind, number, res.ID, "")
	}
	return res, err
}

// ReplyThread replies inside a review thread.
func (s *Service) ReplyThread(ctx context.Context, sc Scope, number, threadID, body string) (gitprovider.CommentResult, error) {
	if strings.TrimSpace(threadID) == "" {
		return gitprovider.CommentResult{}, fmt.Errorf("%w: thread_id is required", gitdom.ErrInvalidInput)
	}
	if err := checkBody(body); err != nil {
		return gitprovider.CommentResult{}, err
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.CommentResult{}, err
	}
	res, err := run(s, ctx, sc, t, "review.reply", threadID, func() (gitprovider.CommentResult, error) {
		return t.api.ReplyThread(ctx, gitprovider.ReplyThreadRequest{Target: t.target, Number: number, ThreadID: threadID, Body: body + s.footer(ctx, sc),
			IdempotencyKey: idempotencyKey(sc, "reply", threadID, body)})
	})
	if err == nil {
		s.index(ctx, sc, t.repo, gitprovider.SubjectChangeRequest, number, res.ID, threadID)
	}
	return res, err
}

// ResolveThread resolves or reopens a review thread.
func (s *Service) ResolveThread(ctx context.Context, sc Scope, number, threadID string, resolved bool) error {
	if strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("%w: thread_id is required", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.ResolveThreads }); err != nil {
		return err
	}
	_, err = run(s, ctx, sc, t, "review.resolve", threadID, func() (struct{}, error) {
		return struct{}{}, t.api.ResolveThread(ctx, gitprovider.ResolveThreadRequest{Target: t.target, Number: number, ThreadID: threadID, Resolved: resolved})
	})
	return err
}

// ReviewInput is a review to submit.
type ReviewInput struct {
	Verdict  gitprovider.Verdict
	Summary  string
	Comments []gitprovider.InlineComment
	HeadSHA  string // the commit the review is about
}

// maxInlineComments caps one review's inline comments.
const maxInlineComments = 100

// SubmitReview submits a review. The router decides who may approve; this only
// checks the review is well-formed.
func (s *Service) SubmitReview(ctx context.Context, sc Scope, number string, in ReviewInput) (gitprovider.ReviewResult, error) {
	switch in.Verdict {
	case gitprovider.VerdictComment, gitprovider.VerdictApprove, gitprovider.VerdictRequestChanges:
	default:
		return gitprovider.ReviewResult{}, fmt.Errorf("%w: verdict must be comment, approve or request_changes", gitdom.ErrInvalidInput)
	}
	if len(in.Comments) > maxInlineComments {
		return gitprovider.ReviewResult{}, fmt.Errorf("%w: too many inline comments", gitdom.ErrInvalidInput)
	}
	if in.Verdict != gitprovider.VerdictApprove && strings.TrimSpace(in.Summary) == "" && len(in.Comments) == 0 {
		return gitprovider.ReviewResult{}, fmt.Errorf("%w: a review needs a summary or inline comments (only an approval may be empty)", gitdom.ErrInvalidInput)
	}
	if len(in.Summary) > maxBodyBytes {
		return gitprovider.ReviewResult{}, fmt.Errorf("%w: summary is too long", gitdom.ErrInvalidInput)
	}
	comments := make([]gitprovider.InlineComment, 0, len(in.Comments))
	footer := s.footer(ctx, sc)
	for _, c := range in.Comments {
		if strings.TrimSpace(c.Path) == "" || c.Line <= 0 || checkBody(c.Body) != nil {
			return gitprovider.ReviewResult{}, fmt.Errorf("%w: an inline comment needs a path, a line and a body", gitdom.ErrInvalidInput)
		}
		c.Body += footer
		comments = append(comments, c)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.ReviewResult{}, err
	}
	res, err := run(s, ctx, sc, t, "review.submit."+string(in.Verdict), subjectLabel(gitprovider.SubjectChangeRequest, number), func() (gitprovider.ReviewResult, error) {
		return t.api.SubmitReview(ctx, gitprovider.SubmitReviewRequest{Target: t.target, Number: number, Verdict: in.Verdict,
			Summary: in.Summary + footer, Comments: comments, HeadSHA: in.HeadSHA,
			IdempotencyKey: idempotencyKey(sc, "review", number+in.HeadSHA+string(in.Verdict), in.Summary)})
	})
	if err == nil {
		s.index(ctx, sc, t.repo, gitprovider.SubjectChangeRequest, number, res.ID, "")
	}
	return res, err
}

// AddLabels adds labels to an issue or change request.
func (s *Service) AddLabels(ctx context.Context, sc Scope, kind gitprovider.SubjectKind, number string, labels []string) error {
	ref, err := checkSubject(kind, number)
	if err != nil {
		return err
	}
	if len(labels) == 0 {
		return fmt.Errorf("%w: labels are required", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.Labels }); err != nil {
		return err
	}
	_, err = run(s, ctx, sc, t, "labels.add", subjectLabel(kind, number), func() (struct{}, error) {
		return struct{}{}, t.api.AddLabels(ctx, gitprovider.LabelsRequest{Target: t.target, Subject: ref, Labels: labels})
	})
	return err
}

// RequestReviewers asks people to review a change request.
func (s *Service) RequestReviewers(ctx context.Context, sc Scope, number string, logins []string) error {
	if len(logins) == 0 {
		return fmt.Errorf("%w: logins are required", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.ReviewerRequests }); err != nil {
		return err
	}
	_, err = run(s, ctx, sc, t, "reviewers.request", subjectLabel(gitprovider.SubjectChangeRequest, number), func() (struct{}, error) {
		return struct{}{}, t.api.RequestReviewers(ctx, gitprovider.PeopleRequest{Target: t.target,
			Subject: gitprovider.SubjectRef{Kind: gitprovider.SubjectChangeRequest, Number: number}, Logins: logins})
	})
	return err
}

// SetAssignees sets who an issue or change request is assigned to.
func (s *Service) SetAssignees(ctx context.Context, sc Scope, kind gitprovider.SubjectKind, number string, logins []string) error {
	ref, err := checkSubject(kind, number)
	if err != nil {
		return err
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.Assignees }); err != nil {
		return err
	}
	_, err = run(s, ctx, sc, t, "assignees.set", subjectLabel(kind, number), func() (struct{}, error) {
		return struct{}{}, t.api.SetAssignees(ctx, gitprovider.PeopleRequest{Target: t.target, Subject: ref, Logins: logins})
	})
	return err
}

// maxRerunsPerHour bounds how often an agent may re-run CI on a repository.
const maxRerunsPerHour = 6

// RerunChecks re-runs the failed checks of a ref (or one check). A repository
// is capped per hour so a failing job cannot be retried in a tight loop.
func (s *Service) RerunChecks(ctx context.Context, sc Scope, ref, checkID string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("%w: ref is required", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.RerunChecks }); err != nil {
		return err
	}
	if !s.allowRerun(t.repo.ID) {
		return gitdom.ErrRateLimited
	}
	_, err = run(s, ctx, sc, t, "checks.rerun", ref, func() (struct{}, error) {
		return struct{}{}, t.api.RerunChecks(ctx, gitprovider.RerunRequest{Target: t.target, Ref: ref, CheckID: checkID})
	})
	return err
}

// MergeChangeRequest merges a change request that still has the head the agent
// reviewed. The platform's branch protection still applies.
func (s *Service) MergeChangeRequest(ctx context.Context, sc Scope, number, method, expectedHeadSHA string) (gitprovider.MergeResult, error) {
	if strings.TrimSpace(expectedHeadSHA) == "" {
		return gitprovider.MergeResult{}, fmt.Errorf("%w: expected_head_sha is required", gitdom.ErrInvalidInput)
	}
	switch method {
	case "", "merge", "squash", "rebase":
	default:
		return gitprovider.MergeResult{}, fmt.Errorf("%w: method must be merge, squash or rebase", gitdom.ErrInvalidInput)
	}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return gitprovider.MergeResult{}, err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.Merge }); err != nil {
		return gitprovider.MergeResult{}, err
	}
	return run(s, ctx, sc, t, "change_request.merge", subjectLabel(gitprovider.SubjectChangeRequest, number), func() (gitprovider.MergeResult, error) {
		return t.api.MergeChangeRequest(ctx, gitprovider.MergeRequest{Target: t.target, Number: number, Method: method, ExpectedHeadSHA: expectedHeadSHA})
	})
}
