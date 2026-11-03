package gitsvc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// opsStore adds the comment index and change-request lookup to the shared fake.
type opsStore struct {
	*fakeStore
	indexed []*gitdom.IndexedComment
}

func (o *opsStore) IndexComment(_ context.Context, c *gitdom.IndexedComment) error {
	o.indexed = append(o.indexed, c)
	return nil
}
func (o *opsStore) FindChangeRequest(_ context.Context, repo uuid.UUID, number string) (*gitdom.ChangeRequest, error) {
	for i := len(o.crs) - 1; i >= 0; i-- {
		if o.crs[i].RepositoryID == repo && o.crs[i].Number == number {
			return o.crs[i], nil
		}
	}
	return nil, gitdom.ErrNotFound
}

// opsFake records the provider calls an operation makes and answers from fields.
type opsFake struct {
	*fakeProv
	calls []string
	last  any

	page     *gitprovider.IssuePage
	cr       gitprovider.ChangeRequest
	files    gitprovider.ChangedFilePage
	threads  gitprovider.ThreadPage
	checks   []gitprovider.Check
	checkErr error
	logs     gitprovider.Logs
	err      error

	permission    gitprovider.Permission
	branchMissing bool
}

func (o *opsFake) providerInfo() gitprovider.Info { return o.info }

func (o *opsFake) rec(name string, req any) { o.calls = append(o.calls, name); o.last = req }

func (o *opsFake) GetChangeRequest(_ context.Context, r gitprovider.CRRequest) (gitprovider.ChangeRequest, error) {
	o.rec("GetChangeRequest", r)
	return o.cr, o.err
}
func (o *opsFake) ListChangedFiles(_ context.Context, r gitprovider.PagedCRRequest) (gitprovider.ChangedFilePage, error) {
	o.rec("ListChangedFiles", r)
	return o.files, nil
}
func (o *opsFake) GetDiff(_ context.Context, r gitprovider.DiffRequest) (gitprovider.Diff, error) {
	o.rec("GetDiff", r)
	return gitprovider.Diff{Text: "diff"}, o.err
}
func (o *opsFake) ListThreads(_ context.Context, r gitprovider.PagedCRRequest) (gitprovider.ThreadPage, error) {
	o.rec("ListThreads", r)
	return o.threads, nil
}
func (o *opsFake) ListComments(_ context.Context, r gitprovider.ListCommentsRequest) (gitprovider.CommentPage, error) {
	o.rec("ListComments", r)
	return gitprovider.CommentPage{}, o.err
}
func (o *opsFake) GetIssue(_ context.Context, r gitprovider.IssueRequest) (gitprovider.Issue, error) {
	o.rec("GetIssue", r)
	return gitprovider.Issue{Number: r.Number}, o.err
}
func (o *opsFake) GetChecks(_ context.Context, r gitprovider.ChecksRequest) ([]gitprovider.Check, error) {
	o.rec("GetChecks", r)
	return o.checks, o.checkErr
}
func (o *opsFake) GetCheckLogs(_ context.Context, r gitprovider.CheckLogsRequest) (gitprovider.Logs, error) {
	o.rec("GetCheckLogs", r)
	return o.logs, o.err
}
func (o *opsFake) GetFile(_ context.Context, r gitprovider.FileRequest) (gitprovider.File, error) {
	o.rec("GetFile", r)
	return gitprovider.File{Path: r.Path}, o.err
}
func (o *opsFake) SearchCode(_ context.Context, r gitprovider.CodeSearchRequest) (gitprovider.CodeHits, error) {
	o.rec("SearchCode", r)
	return gitprovider.CodeHits{}, o.err
}
func (o *opsFake) OpenChangeRequest(_ context.Context, r gitprovider.OpenCRRequest) (gitprovider.ChangeRequest, error) {
	o.rec("OpenChangeRequest", r)
	return gitprovider.ChangeRequest{Number: "9", Title: r.Title, State: gitprovider.CROpen, Head: gitprovider.Branch{Ref: r.Head, SHA: "h1"},
		Base: gitprovider.Branch{Ref: r.Base}, Author: "paca-bot[bot]", URL: "https://x/pull/9"}, o.err
}
func (o *opsFake) UpdateChangeRequest(_ context.Context, r gitprovider.UpdateCRRequest) (gitprovider.ChangeRequest, error) {
	o.rec("UpdateChangeRequest", r)
	return gitprovider.ChangeRequest{Number: r.Number}, o.err
}
func (o *opsFake) Comment(_ context.Context, r gitprovider.CommentRequest) (gitprovider.CommentResult, error) {
	o.rec("Comment", r)
	return gitprovider.CommentResult{ID: "c-100"}, o.err
}
func (o *opsFake) ReplyThread(_ context.Context, r gitprovider.ReplyThreadRequest) (gitprovider.CommentResult, error) {
	o.rec("ReplyThread", r)
	return gitprovider.CommentResult{ID: "c-101"}, o.err
}
func (o *opsFake) SubmitReview(_ context.Context, r gitprovider.SubmitReviewRequest) (gitprovider.ReviewResult, error) {
	o.rec("SubmitReview", r)
	return gitprovider.ReviewResult{ID: "r-1", Posted: len(r.Comments)}, o.err
}
func (o *opsFake) SetStatus(_ context.Context, r gitprovider.SetStatusRequest) (gitprovider.StatusResult, error) {
	o.rec("SetStatus", r)
	return gitprovider.StatusResult{StatusRef: "s-1"}, o.err
}
func (o *opsFake) ResolveThread(_ context.Context, r gitprovider.ResolveThreadRequest) error {
	o.rec("ResolveThread", r)
	return o.err
}
func (o *opsFake) AddLabels(_ context.Context, r gitprovider.LabelsRequest) error {
	o.rec("AddLabels", r)
	return o.err
}
func (o *opsFake) RequestReviewers(_ context.Context, r gitprovider.PeopleRequest) error {
	o.rec("RequestReviewers", r)
	return o.err
}
func (o *opsFake) SetAssignees(_ context.Context, r gitprovider.PeopleRequest) error {
	o.rec("SetAssignees", r)
	return o.err
}
func (o *opsFake) MergeChangeRequest(_ context.Context, r gitprovider.MergeRequest) (gitprovider.MergeResult, error) {
	o.rec("MergeChangeRequest", r)
	return gitprovider.MergeResult{SHA: "m1"}, o.err
}
func (o *opsFake) RerunChecks(_ context.Context, r gitprovider.RerunRequest) error {
	o.rec("RerunChecks", r)
	return o.err
}
func (o *opsFake) React(_ context.Context, r gitprovider.ReactRequest) error {
	o.rec("React", r)
	return o.err
}
func (o *opsFake) GetBranch(_ context.Context, r gitprovider.GetBranchRequest) (gitprovider.BranchInfo, error) {
	o.rec("GetBranch", r)
	if o.branchMissing {
		return gitprovider.BranchInfo{}, &gitprovider.Error{Code: gitprovider.CodeNotFound}
	}
	return gitprovider.BranchInfo{Name: r.Name, SHA: "b1"}, o.err
}
func (o *opsFake) CreateBranch(_ context.Context, r gitprovider.CreateBranchRequest) (gitprovider.BranchInfo, error) {
	o.rec("CreateBranch", r)
	return gitprovider.BranchInfo{Name: r.Name, SHA: "b2"}, o.err
}
func (o *opsFake) GetActorPermission(_ context.Context, r gitprovider.ActorPermissionRequest) (gitprovider.Permission, error) {
	o.rec("GetActorPermission", r)
	if o.permission != "" {
		return o.permission, o.err
	}
	return gitprovider.PermissionWrite, o.err
}

type namer map[uuid.UUID]string

func (n namer) AgentName(_ context.Context, id uuid.UUID) string { return n[id] }
func (n namer) OwnsConversation(_ context.Context, _, agent, _ uuid.UUID) bool {
	_, ok := n[agent]
	return ok
}

type opsFx struct {
	svc   *Service
	st    *opsStore
	pr    *opsFake
	sc    Scope
	repo  *gitdom.Repository
	agent uuid.UUID
}

func newOpsFx(t *testing.T) *opsFx {
	t.Helper()
	st := &opsStore{fakeStore: newFakeStore()}
	pr := &opsFake{fakeProv: &fakeProv{}}
	pr.info.Capabilities = gitprovider.Capabilities{ResolveThreads: true, FileRead: true, CodeSearch: true, Labels: true,
		ReviewerRequests: true, Assignees: true, Merge: true, RerunChecks: true, Reactions: true}
	svc := New(st, fakeProviders{p: pr}).WithPublicURL("https://paca.example")
	ident := st.addIdentity("paca-bot", "app-1")
	project, agent := uuid.New(), uuid.New()
	repo := &gitdom.Repository{ProjectID: project, ProviderID: "p", Host: "git.example", FullPath: "acme/web", DefaultBranch: "main", IdentityID: &ident.ID}
	if err := st.CreateRepository(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	conv := uuid.New()
	svc.WithAgentNamer(namer{agent: "Reviewer"})
	return &opsFx{svc: svc, st: st, pr: pr, repo: repo, agent: agent,
		sc: Scope{ProjectID: project, RepositoryID: repo.ID, AgentID: agent, ConversationID: &conv}}
}

func (o *opsFake) ListIssues(_ context.Context, r gitprovider.ListIssuesRequest) (gitprovider.IssuePage, error) {
	o.rec("ListIssues", r)
	if o.page != nil {
		return *o.page, o.err
	}
	return gitprovider.IssuePage{Items: []gitprovider.Issue{{Number: "7"}}, NextCursor: "next"}, o.err
}

func TestOps_ListIssues(t *testing.T) {
	fx := newOpsFx(t)
	fx.pr.info.Capabilities.IssueList = true
	ctx := context.Background()
	page, err := fx.svc.ListIssues(ctx, fx.sc, "closed", "a+b")
	if err != nil || len(page.Items) != 1 || page.Items[0].Number != "7" || page.NextCursor != "next" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	req := fx.pr.last.(gitprovider.ListIssuesRequest)
	if req.State != "closed" || req.Cursor != "a+b" || req.Target.Identity.ID != "app-1" || req.Target.Repo.FullPath != "acme/web" {
		t.Fatalf("req=%+v", req)
	}
	if len(fx.st.log) != 1 || fx.st.log[0].Tool != "issue.list" || !fx.st.log[0].OK {
		t.Fatalf("log=%+v", fx.st.log)
	}
	fx.pr.err = &gitprovider.Error{Code: gitprovider.CodeForbidden}
	if _, err := fx.svc.ListIssues(ctx, fx.sc, "", ""); !gitprovider.IsCode(err, gitprovider.CodeForbidden) {
		t.Fatalf("err=%v", err)
	}
	if len(fx.st.log) != 2 || fx.st.log[1].OK {
		t.Fatalf("log=%+v", fx.st.log)
	}
}

func TestOps_ListIssuesPreservesPageAndEmptyDefault(t *testing.T) {
	fx := newOpsFx(t)
	fx.pr.info.Capabilities.IssueList = true
	fx.pr.page = &gitprovider.IssuePage{NextCursor: "later"}
	page, err := fx.svc.ListIssues(context.Background(), fx.sc, "", "opaque+cursor")
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != "later" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	req := fx.pr.last.(gitprovider.ListIssuesRequest)
	if req.State != "" || req.Cursor != "opaque+cursor" {
		t.Fatalf("req=%+v", req)
	}
}

type noIssueLister struct {
	*fakeProv
	OpsAPI
}

func (p noIssueLister) providerInfo() gitprovider.Info { return p.info }

func TestOps_ListIssuesRequiresImplementedLister(t *testing.T) {
	fx := newOpsFx(t)
	fx.pr.info.Capabilities.IssueList = true
	fx.svc.providers = fakeProviders{p: noIssueLister{fakeProv: fx.pr.fakeProv, OpsAPI: fx.pr}}
	_, err := fx.svc.ListIssues(context.Background(), fx.sc, "all", "")
	if !errors.Is(err, gitdom.ErrUnsupported) || len(fx.pr.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, fx.pr.calls)
	}
}

func TestOps_ListIssuesRefusesInvalidStateUnsupportedAndForeignRepo(t *testing.T) {
	fx := newOpsFx(t)
	ctx := context.Background()
	if _, err := fx.svc.ListIssues(ctx, fx.sc, "merged", ""); !errors.Is(err, gitdom.ErrInvalidInput) {
		t.Fatalf("state: %v", err)
	}
	if _, err := fx.svc.ListIssues(ctx, fx.sc, "", ""); !errors.Is(err, gitdom.ErrUnsupported) {
		t.Fatalf("unsupported: %v", err)
	}
	fx.pr.info.Capabilities.IssueList = true
	other := fx.sc
	other.ProjectID = uuid.New()
	if _, err := fx.svc.ListIssues(ctx, other, "all", ""); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("foreign: %v", err)
	}
	if len(fx.pr.calls) != 0 || len(fx.st.log) != 0 {
		t.Fatalf("called provider or audited: %v %v", fx.pr.calls, fx.st.log)
	}
}

func TestOps_RepositoryOfAnotherProjectIsNotFoundAndNothingIsCalled(t *testing.T) {
	fx := newOpsFx(t)
	other := fx.sc
	other.ProjectID = uuid.New()
	if _, err := fx.svc.PostComment(context.Background(), other, gitprovider.SubjectIssue, "1", "hi"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(fx.pr.calls) != 0 || len(fx.st.log) != 0 {
		t.Fatalf("a foreign repository must not reach the provider or the log: %v %v", fx.pr.calls, fx.st.log)
	}
}

func TestOps_PostCommentAddsFooterIndexesAndAudits(t *testing.T) {
	fx := newOpsFx(t)
	res, err := fx.svc.PostComment(context.Background(), fx.sc, gitprovider.SubjectChangeRequest, "7", "Looks good")
	if err != nil || res.ID != "c-100" {
		t.Fatalf("res = %+v %v", res, err)
	}
	req := fx.pr.last.(gitprovider.CommentRequest)
	if !strings.HasPrefix(req.Body, "Looks good") || !strings.Contains(req.Body, "Posted by **Reviewer** via [Paca](https://paca.example/projects/"+fx.sc.ProjectID.String()+"/conversations/"+fx.sc.ConversationID.String()+")") {
		t.Fatalf("body = %q", req.Body)
	}
	if req.IdempotencyKey == "" || req.Subject.Kind != gitprovider.SubjectChangeRequest || req.Subject.Number != "7" {
		t.Fatalf("req = %+v", req)
	}
	if req.Target.Identity.ID != "app-1" || req.Target.Repo.FullPath != "acme/web" {
		t.Fatalf("target = %+v", req.Target)
	}
	if len(fx.st.indexed) != 1 || fx.st.indexed[0].CommentID != "c-100" || fx.st.indexed[0].AgentID == nil || *fx.st.indexed[0].AgentID != fx.agent {
		t.Fatalf("index = %+v", fx.st.indexed)
	}
	if len(fx.st.log) != 1 || fx.st.log[0].Tool != "comment.post" || !fx.st.log[0].OK || *fx.st.log[0].ActorAccountID != fx.agent {
		t.Fatalf("log = %+v", fx.st.log)
	}

	// The same call again carries the same idempotency key; different text does not.
	_, _ = fx.svc.PostComment(context.Background(), fx.sc, gitprovider.SubjectChangeRequest, "7", "Looks good")
	if again := fx.pr.last.(gitprovider.CommentRequest); again.IdempotencyKey != req.IdempotencyKey {
		t.Fatal("a retried call must reuse its key")
	}
	_, _ = fx.svc.PostComment(context.Background(), fx.sc, gitprovider.SubjectChangeRequest, "7", "Different")
	if other := fx.pr.last.(gitprovider.CommentRequest); other.IdempotencyKey == req.IdempotencyKey {
		t.Fatal("different text must not share a key")
	}
}

func TestOps_ValidationHappensBeforeAnyProviderCall(t *testing.T) {
	fx := newOpsFx(t)
	ctx := context.Background()
	big := strings.Repeat("x", maxBodyBytes+1)
	cases := map[string]error{
		"empty body":     second(fx.svc.PostComment(ctx, fx.sc, gitprovider.SubjectIssue, "1", "  ")),
		"huge body":      second(fx.svc.PostComment(ctx, fx.sc, gitprovider.SubjectIssue, "1", big)),
		"bad kind":       second(fx.svc.PostComment(ctx, fx.sc, "pull", "1", "x")),
		"no number":      second(fx.svc.PostComment(ctx, fx.sc, gitprovider.SubjectIssue, "", "x")),
		"empty reply":    second(fx.svc.ReplyThread(ctx, fx.sc, "7", "t1", "")),
		"no thread":      second(fx.svc.ReplyThread(ctx, fx.sc, "7", "", "x")),
		"bad verdict":    second(fx.svc.SubmitReview(ctx, fx.sc, "7", ReviewInput{Verdict: "lgtm"})),
		"empty comment":  second(fx.svc.SubmitReview(ctx, fx.sc, "7", ReviewInput{Verdict: gitprovider.VerdictComment, Summary: "  "})),
		"empty rc":       second(fx.svc.SubmitReview(ctx, fx.sc, "7", ReviewInput{Verdict: gitprovider.VerdictRequestChanges})),
		"inline no line": second(fx.svc.SubmitReview(ctx, fx.sc, "7", ReviewInput{Verdict: gitprovider.VerdictComment, Comments: []gitprovider.InlineComment{{Path: "a.go", Body: "x"}}})),
		"open no title":  second(fx.svc.OpenChangeRequest(ctx, fx.sc, OpenInput{Head: "b"})),
		"update merged":  second(fx.svc.UpdateChangeRequest(ctx, fx.sc, "7", UpdateInput{State: ptr(gitprovider.CRMerged)})),
		"merge no sha":   second(fx.svc.MergeChangeRequest(ctx, fx.sc, "7", "", "")),
		"merge method":   second(fx.svc.MergeChangeRequest(ctx, fx.sc, "7", "fast", "abc")),
		"labels none":    fx.svc.AddLabels(ctx, fx.sc, gitprovider.SubjectIssue, "1", nil),
		"no file path":   second(fx.svc.GetFile(ctx, fx.sc, "main", " ", 0)),
		"no logs id":     second(fx.svc.GetCheckLogs(ctx, fx.sc, "", 0)),
		"no checks ref":  second(fx.svc.GetChecks(ctx, fx.sc, "", "")),
	}
	for name, err := range cases {
		if !errors.Is(err, gitdom.ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(fx.pr.calls) != 0 {
		t.Fatalf("provider was called for invalid input: %v", fx.pr.calls)
	}
}

func second[T any](_ T, err error) error { return err }
func ptr[T any](v T) *T                  { return &v }

func TestOps_ChangeRequestContextDegradesGracefully(t *testing.T) {
	fx := newOpsFx(t)
	fx.pr.cr = gitprovider.ChangeRequest{Number: "7", Title: "t", Head: gitprovider.Branch{Ref: "paca/x", SHA: "abc"}}
	fx.pr.files = gitprovider.ChangedFilePage{Items: []gitprovider.ChangedFile{{Path: "a.go"}}, NextCursor: "n2"}
	fx.pr.threads = gitprovider.ThreadPage{Items: []gitprovider.Thread{{ID: "t1"}, {ID: "t2", Resolved: true}, {ID: "t3", Outdated: true}}}
	fx.pr.checkErr = errors.New("ci is down")
	_, _ = fx.st.UpsertChangeRequest(context.Background(), &gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "7", OpenedByAgentID: &fx.agent})

	got, err := fx.svc.GetChangeRequestContext(context.Background(), fx.sc, "7")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.FilesNextCursor != "n2" || len(got.Threads) != 3 || got.UnresolvedCount != 1 {
		t.Fatalf("context = %+v", got)
	}
	if got.Checks == nil || len(got.Checks) != 0 || len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "checks") {
		t.Fatalf("a failing part must be a warning, not an error: %+v", got)
	}
	if !got.OpenedByYou {
		t.Fatal("opened_by_you")
	}
	if checks := fx.pr.last; checks == nil {
		t.Fatal("no calls")
	}
	var sawChecks bool
	for _, c := range fx.pr.calls {
		sawChecks = sawChecks || c == "GetChecks"
	}
	if !sawChecks {
		t.Fatalf("checks must be read at the head sha: %v", fx.pr.calls)
	}

	// The change request itself failing is an error.
	fx.pr.err = &gitprovider.Error{Code: gitprovider.CodeNotFound}
	if _, err := fx.svc.GetChangeRequestContext(context.Background(), fx.sc, "7"); !gitprovider.IsCode(err, gitprovider.CodeNotFound) {
		t.Fatalf("err = %v", err)
	}
	last := fx.st.log[len(fx.st.log)-1]
	if last.OK || last.Detail != "not_found" {
		t.Fatalf("a failure must be audited with its code: %+v", last)
	}
}

func TestOps_OpenChangeRequestDefaultsBaseOwnsAndLinksTask(t *testing.T) {
	fx := newOpsFx(t)
	fx.st.tasks["paca-12"] = uuid.New()
	cr, err := fx.svc.OpenChangeRequest(context.Background(), fx.sc, OpenInput{Title: "Fix PACA-12", Body: "Because", Head: "paca/PACA-12-fix"})
	if err != nil || cr.Number != "9" {
		t.Fatalf("cr = %+v %v", cr, err)
	}
	req := fx.pr.last.(gitprovider.OpenCRRequest)
	if req.Base != "main" || !strings.HasPrefix(req.Body, "Because") || !strings.Contains(req.Body, "Posted by **Reviewer**") || req.IdempotencyKey == "" {
		t.Fatalf("req = %+v", req)
	}
	saved := fx.st.crs[len(fx.st.crs)-1]
	if saved.OpenedByAgentID == nil || *saved.OpenedByAgentID != fx.agent || saved.OpenedConversationID == nil || saved.Number != "9" {
		t.Fatalf("saved = %+v", saved)
	}
	if len(fx.st.taskLinks) != 1 || len(fx.st.branches) != 1 {
		t.Fatalf("task link: %v %v", fx.st.taskLinks, fx.st.branches)
	}
}

func TestOps_SubmitReviewFootersEveryCommentAndIndexes(t *testing.T) {
	fx := newOpsFx(t)
	res, err := fx.svc.SubmitReview(context.Background(), fx.sc, "7", ReviewInput{Verdict: gitprovider.VerdictRequestChanges, Summary: "Needs work", HeadSHA: "abc",
		Comments: []gitprovider.InlineComment{{Path: "a.go", Line: 3, Body: "nil deref"}}})
	if err != nil || res.Posted != 1 {
		t.Fatalf("res = %+v %v", res, err)
	}
	req := fx.pr.last.(gitprovider.SubmitReviewRequest)
	if !strings.Contains(req.Summary, "Posted by") || !strings.Contains(req.Comments[0].Body, "Posted by") || req.HeadSHA != "abc" {
		t.Fatalf("req = %+v", req)
	}
	if len(fx.st.indexed) != 1 || fx.st.indexed[0].CommentID != "r-1" {
		t.Fatalf("index = %+v", fx.st.indexed)
	}
	if fx.st.log[0].Tool != "review.submit.request_changes" {
		t.Fatalf("log = %+v", fx.st.log[0])
	}
}

func TestOps_OptionalOperationsNeedTheCapability(t *testing.T) {
	fx := newOpsFx(t)
	fx.pr.info.Capabilities = gitprovider.Capabilities{}
	ctx := context.Background()
	for name, err := range map[string]error{
		"merge":     second(fx.svc.MergeChangeRequest(ctx, fx.sc, "7", "", "abc")),
		"labels":    fx.svc.AddLabels(ctx, fx.sc, gitprovider.SubjectIssue, "1", []string{"x"}),
		"reviewers": fx.svc.RequestReviewers(ctx, fx.sc, "7", []string{"a"}),
		"assignees": fx.svc.SetAssignees(ctx, fx.sc, gitprovider.SubjectIssue, "1", []string{"a"}),
		"resolve":   fx.svc.ResolveThread(ctx, fx.sc, "7", "t1", true),
		"rerun":     fx.svc.RerunChecks(ctx, fx.sc, "abc", ""),
		"file":      second(fx.svc.GetFile(ctx, fx.sc, "main", "a.go", 0)),
		"search":    second(fx.svc.SearchCode(ctx, fx.sc, "foo")),
	} {
		if !errors.Is(err, gitdom.ErrUnsupported) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(fx.pr.calls) != 0 {
		t.Fatalf("an unsupported operation must not reach the provider: %v", fx.pr.calls)
	}
}

func TestOps_RerunIsCappedPerRepositoryPerHour(t *testing.T) {
	fx := newOpsFx(t)
	for i := 0; i < maxRerunsPerHour; i++ {
		if err := fx.svc.RerunChecks(context.Background(), fx.sc, "abc", ""); err != nil {
			t.Fatalf("rerun %d: %v", i, err)
		}
	}
	if err := fx.svc.RerunChecks(context.Background(), fx.sc, "abc", ""); !errors.Is(err, gitdom.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	fx.svc.now = func() time.Time { return time.Now().Add(61 * time.Minute) }
	if err := fx.svc.RerunChecks(context.Background(), fx.sc, "abc", ""); err != nil {
		t.Fatalf("the window must slide: %v", err)
	}
}

func TestOps_CheckLogsAreCappedAndMasked(t *testing.T) {
	fx := newOpsFx(t)
	fx.pr.logs = gitprovider.Logs{Text: "token ghp_abcdefghijklmnopqrstuvwxyz0123456789 and password=hunter2 and https://u:pw@host/x"}
	logs, err := fx.svc.GetCheckLogs(context.Background(), fx.sc, "chk-1", 10<<20)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.Text, "ghp_") || strings.Contains(logs.Text, "hunter2") || strings.Contains(logs.Text, ":pw@") {
		t.Fatalf("secrets must be masked: %q", logs.Text)
	}
	if got := fx.pr.last.(gitprovider.CheckLogsRequest).TailBytes; got != maxLogBytes {
		t.Fatalf("tail = %d", got)
	}
}

func TestOps_ChecksByChangeRequestUseTheHeadSHA(t *testing.T) {
	fx := newOpsFx(t)
	fx.pr.cr = gitprovider.ChangeRequest{Number: "7", Head: gitprovider.Branch{SHA: "deadbeef"}}
	fx.pr.checks = []gitprovider.Check{{ID: "1", Name: "ci", State: gitprovider.CheckFailure}}
	got, err := fx.svc.GetChecks(context.Background(), fx.sc, "", "7")
	if err != nil || len(got) != 1 {
		t.Fatalf("got = %+v %v", got, err)
	}
	if ref := fx.pr.last.(gitprovider.ChecksRequest).Ref; ref != "deadbeef" {
		t.Fatalf("ref = %q", ref)
	}
}

func TestOps_AnAgentActsAsItsOwnIdentityWhenItHasOne(t *testing.T) {
	fx := newOpsFx(t)
	own := &gitdom.Identity{ID: uuid.New(), ProviderID: "p", Host: "git.example", ProviderIdentityID: "app-own", Handle: "reviewer-bot", Scope: gitdom.ScopeAgent}
	fx.st.agentIDs[fx.agent] = own
	if _, err := fx.svc.PostComment(context.Background(), fx.sc, gitprovider.SubjectIssue, "1", "hi"); err != nil {
		t.Fatal(err)
	}
	if id := fx.pr.last.(gitprovider.CommentRequest).Target.Identity.ID; id != "app-own" {
		t.Fatalf("identity = %q", id)
	}
}

func TestOps_NewScopeKeepsOnlyAConversationTheAgentOwns(t *testing.T) {
	fx := newOpsFx(t)
	conv := uuid.New()
	sc := fx.svc.NewScope(context.Background(), fx.sc.ProjectID, fx.sc.RepositoryID, fx.agent, conv.String())
	if sc.ConversationID == nil || *sc.ConversationID != conv {
		t.Fatalf("owned conversation dropped: %+v", sc)
	}
	if sc := fx.svc.NewScope(context.Background(), fx.sc.ProjectID, fx.sc.RepositoryID, uuid.New(), conv.String()); sc.ConversationID != nil {
		t.Fatal("a conversation of an agent that is not the caller's must be dropped")
	}
	if sc := fx.svc.NewScope(context.Background(), fx.sc.ProjectID, fx.sc.RepositoryID, fx.agent, "not-a-uuid"); sc.ConversationID != nil {
		t.Fatal("garbage must be dropped")
	}
}

func TestOps_TaskGitFillsInWhatEventsDidNotCarry(t *testing.T) {
	fx := newOpsFx(t)
	task := uuid.New()
	ts := &taskGitStore{opsStore: fx.st, links: &gitdom.TaskGit{ChangeRequests: []*gitdom.TaskChangeRequest{
		{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "5", State: "open"}, RepositoryPath: "acme/web"},
		{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "6", Title: "Known", URL: "https://x/6", State: "merged", HeadSHA: "h6"}},
	}}}
	svc := New(ts, fakeProviders{p: fx.pr})
	fx.pr.cr = gitprovider.ChangeRequest{Number: "5", Title: "Real title", URL: "https://x/5", State: gitprovider.CRMerged, Head: gitprovider.Branch{Ref: "feat/x"}, Base: gitprovider.Branch{Ref: "main"}}

	got, err := svc.ListTaskGit(context.Background(), fx.sc.ProjectID, task)
	if err != nil {
		t.Fatal(err)
	}
	c := got.ChangeRequests[0]
	if c.Title != "Real title" || c.URL != "https://x/5" || c.State != "merged" || c.HeadRef != "feat/x" {
		t.Fatalf("not filled in: %+v", c)
	}
	if n := countCalls(fx.pr.calls, "GetChangeRequest"); n != 1 {
		t.Fatalf("only the incomplete entry may cost a platform call, got %d", n)
	}
	if last := fx.st.crs[len(fx.st.crs)-1]; last.Number != "5" || last.Title != "Real title" {
		t.Fatalf("the answer must be remembered: %+v", last)
	}

	// A platform that fails leaves the entry as it was, and the listing still works.
	fx.pr.err = &gitprovider.Error{Code: gitprovider.CodeTransient}
	ts.links = &gitdom.TaskGit{ChangeRequests: []*gitdom.TaskChangeRequest{{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "9"}}}}
	if got, err := svc.ListTaskGit(context.Background(), fx.sc.ProjectID, task); err != nil || got.ChangeRequests[0].Title != "" {
		t.Fatalf("failing platform: %+v %v", got, err)
	}
}

type taskGitStore struct {
	*opsStore
	links   *gitdom.TaskGit
	foreign bool // the task belongs to another project
}

func (s *taskGitStore) ListTaskGit(context.Context, uuid.UUID, uuid.UUID) (*gitdom.TaskGit, error) {
	return s.links, nil
}
func (s *taskGitStore) TaskInProject(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return !s.foreign, nil
}

func countCalls(calls []string, name string) int {
	n := 0
	for _, c := range calls {
		if c == name {
			n++
		}
	}
	return n
}

func TestOps_TaskGitOfAnotherProjectsTaskIsNotFound(t *testing.T) {
	fx := newOpsFx(t)
	ts := &taskGitStore{opsStore: fx.st, links: &gitdom.TaskGit{}, foreign: true}
	svc := New(ts, fakeProviders{p: fx.pr})
	if _, err := svc.ListTaskGit(context.Background(), fx.sc.ProjectID, uuid.New()); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
