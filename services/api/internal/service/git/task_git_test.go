package gitsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

func TestSummarizeChecks(t *testing.T) {
	ck := func(n string, s gitprovider.CheckState) gitprovider.Check {
		return gitprovider.Check{Name: n, State: s}
	}
	if summarizeChecks(nil) != nil {
		t.Fatal("no checks, no summary")
	}
	cases := map[string]struct {
		in      []gitprovider.Check
		state   string
		failed  int
		pending int
		passed  int
	}{
		"all passing":              {[]gitprovider.Check{ck("a", "success"), ck("b", "success")}, gitdom.CIPassing, 0, 0, 2},
		"skipped counts as passed": {[]gitprovider.Check{ck("a", "success"), ck("b", "skipped")}, gitdom.CIPassing, 0, 0, 2},
		"a failure wins":           {[]gitprovider.Check{ck("a", "success"), ck("b", "failure"), ck("c", "pending")}, gitdom.CIFailing, 1, 1, 1},
		"cancelled is a failure":   {[]gitprovider.Check{ck("a", "cancelled")}, gitdom.CIFailing, 1, 0, 0},
		"unfinished is pending":    {[]gitprovider.Check{ck("a", "success"), ck("b", "pending")}, gitdom.CIPending, 0, 1, 1},
	}
	for name, c := range cases {
		got := summarizeChecks(c.in)
		if got.State != c.state || got.Failed != c.failed || got.Pending != c.pending || got.Passed != c.passed || got.Total != len(c.in) {
			t.Errorf("%s: %+v", name, got)
		}
	}
	// An agent run's own status is not CI.
	own := summarizeChecks([]gitprovider.Check{ck("Paca / Dev", "failure"), ck("ci/test", "success")})
	if own.State != gitdom.CIPassing || own.Total != 1 || own.Failed != 0 {
		t.Errorf("Paca's run status must not count: %+v", own)
	}
	if summarizeChecks([]gitprovider.Check{ck("Paca / Dev", "failure")}) != nil {
		t.Error("only Paca's run status means no CI")
	}
	many := make([]gitprovider.Check, 12)
	for i := range many {
		many[i] = ck("job", gitprovider.CheckFailure)
	}
	if got := summarizeChecks(many); got.Failed != 12 || len(got.Failing) != maxFailingNames {
		t.Errorf("failing names must be capped: %+v", got)
	}
}

// taskStore adds the task-link methods the service uses.
type taskStore struct {
	*opsStore
	links    *gitdom.TaskGit
	taskOK   bool
	linkedCR []uuid.UUID
	unlinkCR []uuid.UUID
	unlinkBr []string
}

func (s *taskStore) ListTaskGit(context.Context, uuid.UUID, uuid.UUID) (*gitdom.TaskGit, error) {
	return s.links, nil
}
func (s *taskStore) TaskInProject(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return s.taskOK, nil
}
func (s *taskStore) LinkTaskToChangeRequest(_ context.Context, _, cr uuid.UUID) (bool, error) {
	s.linkedCR = append(s.linkedCR, cr)
	return true, nil
}
func (s *taskStore) UnlinkTaskFromChangeRequest(_ context.Context, _, cr uuid.UUID) error {
	s.unlinkCR = append(s.unlinkCR, cr)
	return nil
}
func (s *taskStore) UnlinkTaskBranch(_ context.Context, _, _ uuid.UUID, b string) error {
	s.unlinkBr = append(s.unlinkBr, b)
	return nil
}

func newTaskFx(t *testing.T) (*opsFx, *taskStore, *Service, TaskRef) {
	fx := newOpsFx(t)
	ts := &taskStore{opsStore: fx.st, taskOK: true, links: &gitdom.TaskGit{}}
	svc := New(ts, fakeProviders{p: fx.pr}).WithPublicURL("https://paca.example")
	return fx, ts, svc, TaskRef{ProjectID: fx.sc.ProjectID, TaskID: uuid.New(), ActorID: uuid.New()}
}

func TestTaskGit_ListShowsCIOfOpenRequestsAndCachesIt(t *testing.T) {
	fx, ts, svc, ref := newTaskFx(t)
	open := &gitdom.TaskChangeRequest{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "5", Title: "T", URL: "u", State: "open", HeadSHA: "abc"}}
	merged := &gitdom.TaskChangeRequest{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "6", Title: "M", URL: "u", State: "merged", HeadSHA: "def"}}
	ts.links.ChangeRequests = []*gitdom.TaskChangeRequest{open, merged}
	fx.pr.checks = []gitprovider.Check{{Name: "ci/test", State: gitprovider.CheckFailure}, {Name: "lint", State: gitprovider.CheckSuccess}}

	if _, err := svc.ListTaskGit(context.Background(), ref.ProjectID, ref.TaskID); err != nil {
		t.Fatal(err)
	}
	if open.CI == nil || open.CI.State != gitdom.CIFailing || open.CI.Failing[0] != "ci/test" || open.CI.Total != 2 {
		t.Fatalf("ci = %+v", open.CI)
	}
	if merged.CI != nil {
		t.Fatal("a merged request's CI is not interesting")
	}
	if n := countCalls(fx.pr.calls, "GetChecks"); n != 1 {
		t.Fatalf("checks read %d times", n)
	}

	// Opening the task again within the cache window asks nothing.
	open.CI = nil
	_, _ = svc.ListTaskGit(context.Background(), ref.ProjectID, ref.TaskID)
	if n := countCalls(fx.pr.calls, "GetChecks"); n != 1 || open.CI == nil {
		t.Fatalf("cache: %d calls, ci %+v", n, open.CI)
	}

	// A pending summary is looked at again sooner than a finished one.
	fx.pr.checks = []gitprovider.Check{{Name: "ci", State: gitprovider.CheckPending}}
	svc.ci = nil
	open.CI = nil
	_, _ = svc.ListTaskGit(context.Background(), ref.ProjectID, ref.TaskID)
	svc.now = func() time.Time { return time.Now().Add(time.Minute) }
	_, _ = svc.ListTaskGit(context.Background(), ref.ProjectID, ref.TaskID)
	if n := countCalls(fx.pr.calls, "GetChecks"); n != 3 {
		t.Fatalf("a pending summary must be re-read after the short cache: %d calls", n)
	}

	// A platform that fails leaves CI unknown instead of failing the listing.
	svc.ci, fx.pr.checkErr, open.CI = nil, errors.New("down"), nil
	if _, err := svc.ListTaskGit(context.Background(), ref.ProjectID, ref.TaskID); err != nil || open.CI != nil {
		t.Fatalf("err = %v ci = %+v", err, open.CI)
	}
}

func TestTaskGit_LinkAndUnlinkChangeRequest(t *testing.T) {
	fx, ts, svc, ref := newTaskFx(t)
	fx.pr.cr = gitprovider.ChangeRequest{Number: "5", Title: "Real", State: gitprovider.CROpen, URL: "https://x/5",
		Head: gitprovider.Branch{Ref: "feat/x", SHA: "abc"}, Base: gitprovider.Branch{Ref: "main"}, Author: "alice"}
	ctx := context.Background()

	if err := svc.LinkTaskChangeRequest(ctx, ref, fx.repo.ID, " 5 "); err != nil {
		t.Fatal(err)
	}
	saved := fx.st.crs[len(fx.st.crs)-1]
	if saved.Number != "5" || saved.Title != "Real" || saved.URL != "https://x/5" || len(ts.linkedCR) != 1 {
		t.Fatalf("saved = %+v links = %v", saved, ts.linkedCR)
	}
	if fx.st.log[len(fx.st.log)-1].Tool != "task.link_change_request" {
		t.Fatal("the platform read must be audited")
	}

	// A number the platform does not know is refused and linked to nothing.
	fx.pr.err = &gitprovider.Error{Code: gitprovider.CodeNotFound}
	ts.linkedCR = nil
	if err := svc.LinkTaskChangeRequest(ctx, ref, fx.repo.ID, "99"); !gitprovider.IsCode(err, gitprovider.CodeNotFound) || len(ts.linkedCR) != 0 {
		t.Fatalf("err = %v links = %v", err, ts.linkedCR)
	}
	fx.pr.err = nil
	if err := svc.LinkTaskChangeRequest(ctx, ref, fx.repo.ID, " "); !errors.Is(err, gitdom.ErrInvalidInput) {
		t.Fatalf("empty number: %v", err)
	}

	if err := svc.UnlinkTaskChangeRequest(ctx, ref, fx.repo.ID, "5"); err != nil || len(ts.unlinkCR) != 1 {
		t.Fatalf("unlink: %v %v", err, ts.unlinkCR)
	}
}

func TestTaskGit_ATaskOfAnotherProjectOrRepositoryIsNotFound(t *testing.T) {
	fx, ts, svc, ref := newTaskFx(t)
	ctx := context.Background()
	ts.taskOK = false
	if err := svc.LinkTaskChangeRequest(ctx, ref, fx.repo.ID, "5"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("link, foreign task: %v", err)
	}
	if err := svc.LinkTaskBranch(ctx, ref, fx.repo.ID, "b"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("branch, foreign task: %v", err)
	}
	if _, err := svc.CreateTaskBranch(ctx, ref, fx.repo.ID, "b", ""); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("create, foreign task: %v", err)
	}
	if err := svc.UnlinkTaskChangeRequest(ctx, ref, fx.repo.ID, "5"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("unlink, foreign task: %v", err)
	}
	ts.taskOK = true
	foreign := ref
	foreign.ProjectID = uuid.New()
	if err := svc.UnlinkTaskBranch(ctx, foreign, fx.repo.ID, "b"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("unlink through another project: %v", err)
	}
	if len(fx.pr.calls) != 0 || len(ts.unlinkBr) != 0 {
		t.Fatalf("nothing may reach the platform or the store: %v %v", fx.pr.calls, ts.unlinkBr)
	}
}

func TestTaskGit_Branches(t *testing.T) {
	fx, ts, svc, ref := newTaskFx(t)
	fx.pr.info.Capabilities.Branches = true
	ctx := context.Background()

	if err := svc.LinkTaskBranch(ctx, ref, fx.repo.ID, "feat/x"); err != nil || len(fx.st.branches) != 1 {
		t.Fatalf("link: %v %v", err, fx.st.branches)
	}
	fx.pr.branchMissing = true
	if err := svc.LinkTaskBranch(ctx, ref, fx.repo.ID, "typo"); !gitprovider.IsCode(err, gitprovider.CodeNotFound) || len(fx.st.branches) != 1 {
		t.Fatalf("a branch that does not exist must not be linked: %v %v", err, fx.st.branches)
	}
	fx.pr.branchMissing = false

	info, err := svc.CreateTaskBranch(ctx, ref, fx.repo.ID, " paca/PACA-1 ", "main")
	if err != nil || info.Name != "paca/PACA-1" {
		t.Fatalf("create: %+v %v", info, err)
	}
	req := fx.pr.last.(gitprovider.CreateBranchRequest)
	if req.Name != "paca/PACA-1" || req.From != "main" || req.IdempotencyKey == "" {
		t.Fatalf("req = %+v", req)
	}
	if fx.st.branches[len(fx.st.branches)-1] != "paca/PACA-1" {
		t.Fatalf("a created branch is linked: %v", fx.st.branches)
	}

	if err := svc.UnlinkTaskBranch(ctx, ref, fx.repo.ID, "feat/x"); err != nil || ts.unlinkBr[0] != "feat/x" {
		t.Fatalf("unlink: %v %v", err, ts.unlinkBr)
	}
	for _, bad := range []string{"", "  ", "-rf"} {
		if _, err := svc.CreateTaskBranch(ctx, ref, fx.repo.ID, bad, ""); !errors.Is(err, gitdom.ErrInvalidInput) {
			t.Errorf("%q: %v", bad, err)
		}
	}

	// A platform that cannot create branches refuses; linking then trusts the user.
	fx.pr.info.Capabilities.Branches = false
	fx.pr.calls = nil
	if _, err := svc.CreateTaskBranch(ctx, ref, fx.repo.ID, "x", ""); !errors.Is(err, gitdom.ErrUnsupported) {
		t.Fatalf("create without the capability: %v", err)
	}
	if err := svc.LinkTaskBranch(ctx, ref, fx.repo.ID, "free-form"); err != nil || countCalls(fx.pr.calls, "GetBranch") != 0 {
		t.Fatalf("link without the capability: %v %v", err, fx.pr.calls)
	}
}
