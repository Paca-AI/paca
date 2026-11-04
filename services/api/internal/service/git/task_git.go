package gitsvc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// maxTaskGitEnrich bounds the platform calls one listing may spend filling in
// missing details, and again the ones it may spend reading CI.
const maxTaskGitEnrich = 5

// ciCacheTTL is how long a change request's CI summary is reused, so opening
// a task repeatedly does not ask the platform every time.
const ciCacheTTL = 30 * time.Second

type ciKey struct {
	repo uuid.UUID
	sha  string
}

type cachedCI struct {
	ci      *gitdom.CISummary
	expires time.Time
}

// ListTaskGit returns the pull/merge requests and branches linked to a task,
// each open request with the state of its CI.
//
// A request first seen through a review or CI event has no title or page yet
// (those events do not carry them); the listing asks the platform for those
// and remembers the answer, a few at a time. A platform that does not answer
// leaves the entry as it is.
func (s *Service) ListTaskGit(ctx context.Context, projectID, taskID uuid.UUID) (*gitdom.TaskGit, error) {
	ok, err := s.store.TaskInProject(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, gitdom.ErrNotFound
	}
	out, err := s.store.ListTaskGit(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	budget := maxTaskGitEnrich
	for _, c := range out.ChangeRequests {
		if budget == 0 {
			break
		}
		if c.Title != "" && c.URL != "" && c.HeadSHA != "" {
			continue
		}
		budget--
		s.enrichChangeRequest(ctx, projectID, c)
	}
	budget = maxTaskGitEnrich
	for _, c := range out.ChangeRequests {
		if c.State != "open" || c.HeadSHA == "" {
			continue
		}
		if ci, ok := s.cachedCI(c.RepositoryID, c.HeadSHA); ok {
			c.CI = ci
			continue
		}
		if budget == 0 {
			continue
		}
		budget--
		c.CI = s.readCI(ctx, projectID, c)
	}
	return out, nil
}

func (s *Service) enrichChangeRequest(ctx context.Context, projectID uuid.UUID, c *gitdom.TaskChangeRequest) {
	t, err := s.resolve(ctx, Scope{ProjectID: projectID, RepositoryID: c.RepositoryID})
	if err != nil {
		return
	}
	cr, err := t.api.GetChangeRequest(ctx, gitprovider.CRRequest{Target: t.target, Number: c.Number})
	if err != nil {
		return
	}
	if c.Title == "" {
		c.Title = cr.Title
	}
	if c.URL == "" {
		c.URL = cr.URL
	}
	c.State, c.Draft = string(cr.State), cr.Draft
	if cr.Head.Ref != "" {
		c.HeadRef = cr.Head.Ref
	}
	if cr.Head.SHA != "" {
		c.HeadSHA = cr.Head.SHA
	}
	if cr.Base.Ref != "" {
		c.BaseRef = cr.Base.Ref
	}
	if cr.Author != "" && c.Author == "" {
		c.Author = cr.Author
	}
	// Remember it, keeping the owner and head the cache already has.
	_, _ = s.store.UpsertChangeRequest(ctx, &c.ChangeRequest)
}

// readCI reads the checks of a change request's head and summarizes them.
// nil when the platform has none or does not answer.
func (s *Service) readCI(ctx context.Context, projectID uuid.UUID, c *gitdom.TaskChangeRequest) *gitdom.CISummary {
	t, err := s.resolve(ctx, Scope{ProjectID: projectID, RepositoryID: c.RepositoryID})
	if err != nil {
		return nil
	}
	checks, err := t.api.GetChecks(ctx, gitprovider.ChecksRequest{Target: t.target, Ref: c.HeadSHA})
	if err != nil {
		return nil
	}
	ci := summarizeChecks(checks)
	s.rememberCI(c.RepositoryID, c.HeadSHA, ci)
	return ci
}

// maxFailingNames caps the failing check names a summary lists.
const maxFailingNames = 5

// summarizeChecks reduces a ref's checks to one state. A failure or a
// cancellation makes it failing; otherwise anything unfinished makes it
// pending; skipped checks count as passed. No checks means no summary. The
// status Paca itself shows for an agent run is not CI and is left out: a run
// that failed must not make the pull request's tests look broken.
func summarizeChecks(all []gitprovider.Check) *gitdom.CISummary {
	checks := make([]gitprovider.Check, 0, len(all))
	for _, c := range all {
		if !strings.HasPrefix(c.Name, runStatusPrefix) {
			checks = append(checks, c)
		}
	}
	if len(checks) == 0 {
		return nil
	}
	ci := &gitdom.CISummary{Total: len(checks), Failing: []string{}}
	for _, c := range checks {
		switch c.State {
		case gitprovider.CheckSuccess, gitprovider.CheckSkipped:
			ci.Passed++
		case gitprovider.CheckFailure, gitprovider.CheckCancelled:
			ci.Failed++
			if len(ci.Failing) < maxFailingNames {
				ci.Failing = append(ci.Failing, c.Name)
			}
		default:
			ci.Pending++
		}
	}
	switch {
	case ci.Failed > 0:
		ci.State = gitdom.CIFailing
	case ci.Pending > 0:
		ci.State = gitdom.CIPending
	default:
		ci.State = gitdom.CIPassing
	}
	return ci
}

func (s *Service) cachedCI(repo uuid.UUID, sha string) (*gitdom.CISummary, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.ci[ciKey{repo, sha}]
	if !ok || !s.now().Before(c.expires) {
		delete(s.ci, ciKey{repo, sha})
		return nil, false
	}
	return c.ci, true
}

func (s *Service) rememberCI(repo uuid.UUID, sha string, ci *gitdom.CISummary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ci == nil {
		s.ci = map[ciKey]cachedCI{}
	}
	// A finished run does not change; one in flight is looked at again soon.
	ttl := ciCacheTTL
	if ci != nil && ci.State != gitdom.CIPending {
		ttl = 5 * time.Minute
	}
	s.ci[ciKey{repo, sha}] = cachedCI{ci: ci, expires: s.now().Add(ttl)}
}

// TaskRef names a task of a project and the person acting on it.
type TaskRef struct {
	ProjectID uuid.UUID
	TaskID    uuid.UUID
	ActorID   uuid.UUID
}

func (s *Service) checkTask(ctx context.Context, ref TaskRef, repositoryID uuid.UUID) (*opTarget, error) {
	ok, err := s.store.TaskInProject(ctx, ref.ProjectID, ref.TaskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, gitdom.ErrNotFound
	}
	return s.resolve(ctx, Scope{ProjectID: ref.ProjectID, RepositoryID: repositoryID, AgentID: ref.ActorID})
}

// LinkTaskChangeRequest links an existing pull/merge request to a task. The
// platform is asked for it, so a number that does not exist is refused.
func (s *Service) LinkTaskChangeRequest(ctx context.Context, ref TaskRef, repositoryID uuid.UUID, number string) error {
	number = strings.TrimSpace(number)
	if number == "" {
		return fmt.Errorf("%w: number is required", gitdom.ErrInvalidInput)
	}
	t, err := s.checkTask(ctx, ref, repositoryID)
	if err != nil {
		return err
	}
	sc := Scope{ProjectID: ref.ProjectID, RepositoryID: repositoryID, AgentID: ref.ActorID}
	cr, err := run(s, ctx, sc, t, "task.link_change_request", subjectLabel(gitprovider.SubjectChangeRequest, number), func() (gitprovider.ChangeRequest, error) {
		return t.api.GetChangeRequest(ctx, gitprovider.CRRequest{Target: t.target, Number: number})
	})
	if err != nil {
		return err
	}
	saved, err := s.saveChangeRequest(ctx, t.repo, &gitdom.ChangeRequest{RepositoryID: repositoryID, Number: cr.Number, Title: cr.Title,
		State: string(cr.State), Draft: cr.Draft, HeadRef: cr.Head.Ref, HeadSHA: cr.Head.SHA, BaseRef: cr.Base.Ref, Author: cr.Author, URL: cr.URL})
	if err != nil {
		return err
	}
	return s.linkChangeRequest(ctx, t.repo, ref.TaskID, saved)
}

// UnlinkTaskChangeRequest removes a task's link to a pull/merge request.
func (s *Service) UnlinkTaskChangeRequest(ctx context.Context, ref TaskRef, repositoryID uuid.UUID, number string) error {
	ok, err := s.store.TaskInProject(ctx, ref.ProjectID, ref.TaskID)
	if err != nil {
		return err
	}
	repo, rerr := s.store.FindRepository(ctx, repositoryID)
	if !ok || rerr != nil || repo.ProjectID != ref.ProjectID {
		return gitdom.ErrNotFound
	}
	cr, err := s.store.FindChangeRequest(ctx, repositoryID, number)
	if err != nil {
		return err
	}
	return s.store.UnlinkTaskFromChangeRequest(ctx, ref.TaskID, cr.ID)
}

func checkBranchName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 250 || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: branch name is invalid", gitdom.ErrInvalidInput)
	}
	return nil
}

// LinkTaskBranch links an existing branch to a task. Where the platform can
// look branches up, a branch that does not exist is refused.
func (s *Service) LinkTaskBranch(ctx context.Context, ref TaskRef, repositoryID uuid.UUID, branch string) error {
	branch = strings.TrimSpace(branch)
	if err := checkBranchName(branch); err != nil {
		return err
	}
	t, err := s.checkTask(ctx, ref, repositoryID)
	if err != nil {
		return err
	}
	if info, derr := s.providers.Describe(ctx, t.repo.ProviderID); derr == nil && info.Capabilities.Branches {
		sc := Scope{ProjectID: ref.ProjectID, RepositoryID: repositoryID, AgentID: ref.ActorID}
		if _, err := run(s, ctx, sc, t, "task.link_branch", branch, func() (gitprovider.BranchInfo, error) {
			return t.api.GetBranch(ctx, gitprovider.GetBranchRequest{Target: t.target, Name: branch})
		}); err != nil {
			return err
		}
	}
	return s.linkBranch(ctx, t.repo, ref.TaskID, branch)
}

// CreateTaskBranch creates a branch on the platform and links it to the task.
// The router requires git:write for it: it writes to the repository.
func (s *Service) CreateTaskBranch(ctx context.Context, ref TaskRef, repositoryID uuid.UUID, branch, from string) (gitprovider.BranchInfo, error) {
	branch = strings.TrimSpace(branch)
	if err := checkBranchName(branch); err != nil {
		return gitprovider.BranchInfo{}, err
	}
	t, err := s.checkTask(ctx, ref, repositoryID)
	if err != nil {
		return gitprovider.BranchInfo{}, err
	}
	if err := s.need(ctx, t.repo.ProviderID, func(c gitprovider.Capabilities) bool { return c.Branches }); err != nil {
		return gitprovider.BranchInfo{}, err
	}
	sc := Scope{ProjectID: ref.ProjectID, RepositoryID: repositoryID, AgentID: ref.ActorID}
	info, err := run(s, ctx, sc, t, "task.create_branch", branch, func() (gitprovider.BranchInfo, error) {
		return t.api.CreateBranch(ctx, gitprovider.CreateBranchRequest{Target: t.target, Name: branch, From: strings.TrimSpace(from),
			IdempotencyKey: idempotencyKey(sc, "branch", ref.TaskID.String(), branch)})
	})
	if err != nil {
		return info, err
	}
	return info, s.linkBranch(ctx, t.repo, ref.TaskID, branch)
}

// UnlinkTaskBranch removes a task's link to a branch. The branch itself stays.
func (s *Service) UnlinkTaskBranch(ctx context.Context, ref TaskRef, repositoryID uuid.UUID, branch string) error {
	ok, err := s.store.TaskInProject(ctx, ref.ProjectID, ref.TaskID)
	if err != nil {
		return err
	}
	repo, rerr := s.store.FindRepository(ctx, repositoryID)
	if !ok || rerr != nil || repo.ProjectID != ref.ProjectID {
		return gitdom.ErrNotFound
	}
	return s.store.UnlinkTaskBranch(ctx, ref.TaskID, repositoryID, branch)
}
