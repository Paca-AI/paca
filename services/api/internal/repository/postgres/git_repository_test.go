package postgres

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
)

func newGitFx(t *testing.T) (*GitRepository, *iamFx) {
	t.Helper()
	db := newIAMPGTestDB(t)
	return NewGitRepository(db), newIAMFx(t, db)
}

func sharedIdentity(host string) *gitdom.Identity {
	return &gitdom.Identity{ProviderID: "com.paca.github", Host: host, ProviderIdentityID: "app-1", Kind: "app",
		Scope: gitdom.ScopeShared, Handle: "paca-bot", DisplayName: "Paca", ProviderRef: json.RawMessage(`{"installation":7}`)}
}

func TestGitStore_SeveralConnectionsPerProviderHost(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	a := sharedIdentity("github.com")
	if err := s.CreateIdentity(ctx, a); err != nil || a.ID == uuid.Nil {
		t.Fatalf("create: %v id=%v", err, a.ID)
	}
	// A second workspace connection on the same host (another App, or a token).
	b := sharedIdentity("github.com")
	b.ProviderIdentityID = "app-b"
	if err := s.CreateIdentity(ctx, b); err != nil {
		t.Fatalf("second shared connection on a host: %v", err)
	}
	agentOwn := sharedIdentity("github.com")
	agentOwn.Scope, agentOwn.ProviderIdentityID = gitdom.ScopeAgent, "app-2"
	if err := s.CreateIdentity(ctx, agentOwn); err != nil {
		t.Fatalf("agent identity on the same host: %v", err)
	}
	list, _ := s.ListSharedIdentities(ctx, "com.paca.github")
	if len(list) != 2 {
		t.Fatalf("shared identities = %d, want 2 (the agent one is not shared)", len(list))
	}

	project := fx.project()
	mine := sharedIdentity("github.com")
	mine.Scope, mine.ProviderIdentityID, mine.ProjectID = gitdom.ScopeProject, "project-app", &project
	if err := s.CreateIdentity(ctx, mine); err != nil {
		t.Fatalf("project connection: %v", err)
	}
	visible, err := s.ListProjectIdentities(ctx, project)
	if err != nil || len(visible) != 3 {
		t.Fatalf("a project sees the workspace's two and its own: %d %v", len(visible), err)
	}
	other, err := s.ListProjectIdentities(ctx, fx.project())
	if err != nil || len(other) != 2 {
		t.Fatalf("another project sees only the workspace's: %d %v", len(other), err)
	}
	byRef, err := s.FindIdentityByProviderRef(ctx, "com.paca.github", "app-2")
	if err != nil || byRef.ID != agentOwn.ID {
		t.Fatalf("by provider ref: %+v %v", byRef, err)
	}
}

func TestGitStore_AgentIdentityMapping(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	agent := fx.agent(nil, false)
	mine := sharedIdentity("github.com")
	mine.Scope, mine.ProviderIdentityID = gitdom.ScopeAgent, "agent-app"
	if err := s.CreateIdentity(ctx, mine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindAgentIdentity(ctx, agent, "com.paca.github"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("before mapping: %v", err)
	}
	if err := s.SetAgentIdentity(ctx, agent, "com.paca.github", mine.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.FindAgentIdentity(ctx, agent, "com.paca.github")
	if err != nil || got.ID != mine.ID {
		t.Fatalf("after mapping: %+v %v", got, err)
	}
	// Replacing the mapping keeps one row per (agent, provider).
	other := sharedIdentity("github.com")
	other.Scope, other.ProviderIdentityID = gitdom.ScopeAgent, "agent-app-2"
	_ = s.CreateIdentity(ctx, other)
	_ = s.SetAgentIdentity(ctx, agent, "com.paca.github", other.ID)
	if got, _ := s.FindAgentIdentity(ctx, agent, "com.paca.github"); got.ID != other.ID {
		t.Fatalf("mapping not replaced: %+v", got)
	}
	// Deleting the identity drops the mapping.
	if err := s.DeleteIdentity(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindAgentIdentity(ctx, agent, "com.paca.github"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("mapping should cascade: %v", err)
	}
	if err := s.DeleteIdentity(ctx, other.ID); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("deleting twice: %v", err)
	}
}

func TestGitStore_WebhookEndpointsBindLater(t *testing.T) {
	s, _ := newGitFx(t)
	ctx := t.Context()
	ep := &gitdom.WebhookEndpoint{Key: "k-" + uuid.NewString(), ProviderID: "com.paca.github"}
	if err := s.CreateWebhookEndpoint(ctx, ep); err != nil {
		t.Fatal(err)
	}
	got, err := s.FindWebhookEndpoint(ctx, ep.Key)
	if err != nil || got.IdentityID != nil {
		t.Fatalf("pending endpoint: %+v %v", got, err)
	}
	id := sharedIdentity("github.com")
	_ = s.CreateIdentity(ctx, id)
	if err := s.BindWebhookEndpoint(ctx, ep.Key, id.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.FindWebhookEndpoint(ctx, ep.Key)
	if got.IdentityID == nil || *got.IdentityID != id.ID {
		t.Fatalf("not bound: %+v", got)
	}
	if forID, err := s.FindWebhookEndpointForIdentity(ctx, id.ID); err != nil || forID.Key != ep.Key {
		t.Fatalf("for identity: %+v %v", forID, err)
	}
	if err := s.BindWebhookEndpoint(ctx, "missing", id.ID); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("bind missing: %v", err)
	}
	if _, err := s.FindWebhookEndpoint(ctx, "missing"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("find missing: %v", err)
	}
}

func linkRepo(t *testing.T, s *GitRepository, project uuid.UUID, path string, identity *uuid.UUID) *gitdom.Repository {
	t.Helper()
	r := &gitdom.Repository{ProjectID: project, ProviderID: "com.paca.github", Host: "github.com", ExternalID: "1", FullPath: path,
		DefaultBranch: "main", CloneURL: "https://github.com/" + path + ".git", IdentityID: identity, ProviderRef: json.RawMessage(`{"installation":7}`)}
	if err := s.CreateRepository(t.Context(), r); err != nil {
		t.Fatalf("link %s: %v", path, err)
	}
	return r
}

func TestGitStore_RepositoriesAndFanOut(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	p1, p2 := fx.project(), fx.project()
	id := sharedIdentity("github.com")
	_ = s.CreateIdentity(ctx, id)

	a := linkRepo(t, s, p1, "Acme/Web", &id.ID)
	if a.ID == uuid.Nil || a.CreatedAt.IsZero() {
		t.Fatalf("not filled: %+v", a)
	}
	if err := s.CreateRepository(ctx, &gitdom.Repository{ProjectID: p1, ProviderID: "com.paca.github", Host: "github.com", FullPath: "Acme/Web"}); !errors.Is(err, gitdom.ErrAlreadyLinked) {
		t.Fatalf("duplicate link: %v", err)
	}
	linkRepo(t, s, p2, "acme/web", nil) // the same repository linked by another project, different case

	// An event for acme/web fans out to every project that linked it.
	hits, err := s.FindLinkedRepositories(ctx, "com.paca.github", "github.com", "ACME/WEB")
	if err != nil || len(hits) != 2 {
		t.Fatalf("fan-out: %d %v", len(hits), err)
	}
	if none, _ := s.FindLinkedRepositories(ctx, "com.paca.github", "gitlab.com", "acme/web"); len(none) != 0 {
		t.Fatal("a different host is a different repository")
	}
	list, _ := s.ListProjectRepositories(ctx, p1)
	if len(list) != 1 || list[0].IdentityID == nil || *list[0].IdentityID != id.ID {
		t.Fatalf("project list: %+v", list)
	}
	// Unlinking is scoped to the project: another project's id cannot delete it.
	if err := s.DeleteRepository(ctx, p2, a.ID); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("cross-project delete: %v", err)
	}
	if err := s.DeleteRepository(ctx, p1, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindRepository(ctx, a.ID); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	// Deleting the identity does not unlink repositories (it just detaches).
	r := linkRepo(t, s, p1, "acme/api", &id.ID)
	_ = s.DeleteIdentity(ctx, id.ID)
	got, err := s.FindRepository(ctx, r.ID)
	if err != nil || got.IdentityID != nil {
		t.Fatalf("repository after identity delete: %+v %v", got, err)
	}
}

func TestGitStore_InboxIsIdempotentPerEventAndRepository(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	r1 := linkRepo(t, s, fx.project(), "acme/a", nil)
	r2 := linkRepo(t, s, fx.project(), "acme/b", nil)
	ev := func(repo uuid.UUID) *gitdom.InboxEvent {
		return &gitdom.InboxEvent{EventID: "delivery-1:comment", RepositoryID: repo, EndpointKey: "k", Kind: "comment", Action: "created", Payload: json.RawMessage(`{"a":1}`)}
	}
	if ok, err := s.InsertInboxEvent(ctx, ev(r1.ID)); err != nil || !ok {
		t.Fatalf("first insert: %v %v", ok, err)
	}
	if ok, err := s.InsertInboxEvent(ctx, ev(r1.ID)); err != nil || ok {
		t.Fatalf("redelivery must be a no-op: %v %v", ok, err)
	}
	if ok, _ := s.InsertInboxEvent(ctx, ev(r2.ID)); !ok {
		t.Fatal("the same event for another repository is a separate row")
	}
}

func TestGitStore_ChangeRequestsAndTaskLinks(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project := fx.project()
	pgExec(t, fx.db, `UPDATE projects SET task_id_prefix = 'PACA' WHERE id = $1`, project)
	task := uuid.New()
	pgExec(t, fx.db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,12,'t')`, task, project)
	repo := linkRepo(t, s, project, "acme/web", nil)

	cr, err := s.UpsertChangeRequest(ctx, &gitdom.ChangeRequest{RepositoryID: repo.ID, Number: "42", Title: "v1", State: "open", HeadRef: "feat/PACA-12-x", HeadSHA: "a1"})
	if err != nil || cr.ID == uuid.Nil {
		t.Fatalf("upsert: %+v %v", cr, err)
	}
	again, err := s.UpsertChangeRequest(ctx, &gitdom.ChangeRequest{RepositoryID: repo.ID, Number: "42", Title: "v2", State: "merged", HeadRef: "feat/PACA-12-x", HeadSHA: "b2"})
	if err != nil || again.ID != cr.ID || again.Title != "v2" || again.State != "merged" || again.HeadSHA != "b2" {
		t.Fatalf("an upsert must update in place: %+v %v", again, err)
	}

	got, ok, err := s.FindTaskByRef(ctx, project, "paca", 12) // case-insensitive prefix
	if err != nil || !ok || got != task {
		t.Fatalf("find task: %v %v %v", got, ok, err)
	}
	if _, ok, _ := s.FindTaskByRef(ctx, project, "PACA", 99); ok {
		t.Fatal("unknown number")
	}
	if _, ok, _ := s.FindTaskByRef(ctx, fx.project(), "PACA", 12); ok {
		t.Fatal("a task of another project must not resolve")
	}

	for i := 0; i < 2; i++ { // idempotent
		if err := s.LinkTaskToChangeRequest(ctx, task, cr.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.LinkTaskBranch(ctx, task, repo.ID, "feat/PACA-12-x"); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	_ = fx.db.GetContext(ctx, &n, `SELECT count(*) FROM git_task_links WHERE task_id = $1`, task)
	if n != 1 {
		t.Fatalf("links = %d", n)
	}
}

func TestGitStore_ActionLogAndCascades(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project := fx.project()
	repo := linkRepo(t, s, project, "acme/web", nil)
	if err := s.AppendActionLog(ctx, &gitdom.ActionLogEntry{ProjectID: &project, RepositoryID: &repo.ID, Tool: "credential.mint", Subject: "acme/web", OK: true, Detail: "read"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendActionLog(ctx, &gitdom.ActionLogEntry{Tool: "webhook", OK: false}); err != nil {
		t.Fatal(err)
	}
	// Deleting the project removes its repositories and everything hanging off them.
	_, _ = s.InsertInboxEvent(ctx, &gitdom.InboxEvent{EventID: "e", RepositoryID: repo.ID, EndpointKey: "k", Kind: "comment", Payload: json.RawMessage(`{}`)})
	pgExec(t, fx.db, `DELETE FROM projects WHERE id = $1`, project)
	var n int
	_ = fx.db.GetContext(ctx, &n, `SELECT count(*) FROM git_repositories WHERE project_id = $1`, project)
	_ = fx.db.GetContext(ctx, &n, `SELECT n FROM (SELECT count(*) AS n FROM git_event_inbox WHERE repository_id = $1) x`, repo.ID)
	if n != 0 {
		t.Fatalf("inbox rows survived the project: %d", n)
	}
}

func TestGitStore_SetupSessions(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	user := fx.user(false)
	agent := fx.agent(nil, false)
	ep := &gitdom.WebhookEndpoint{Key: "ep-" + uuid.NewString(), ProviderID: "com.paca.github"}
	if err := s.CreateWebhookEndpoint(ctx, ep); err != nil {
		t.Fatal(err)
	}
	sess := &gitdom.SetupSession{State: "st-" + uuid.NewString(), ProviderID: "com.paca.github", Host: "github.com", Scope: gitdom.ScopeShared,
		EndpointKey: ep.Key, ReturnURL: "https://paca.test/x", CreatedBy: user, ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.CreateSetupSession(ctx, sess); err != nil || sess.CreatedAt.IsZero() {
		t.Fatalf("create: %v", err)
	}
	got, err := s.FindSetupSession(ctx, sess.State)
	if err != nil || got.CreatedBy != user || got.Scope != gitdom.ScopeShared || got.AgentID != nil || got.CompletedAt != nil || got.ReturnURL != "https://paca.test/x" || got.EndpointKey != ep.Key {
		t.Fatalf("find: %+v %v", got, err)
	}
	if err := s.CompleteSetupSession(ctx, sess.State); err != nil {
		t.Fatal(err)
	}
	first, _ := s.FindSetupSession(ctx, sess.State)
	if first.CompletedAt == nil {
		t.Fatal("not completed")
	}
	_ = s.CompleteSetupSession(ctx, sess.State)
	if again, _ := s.FindSetupSession(ctx, sess.State); !again.CompletedAt.Equal(*first.CompletedAt) {
		t.Error("completing twice must keep the first completion time")
	}
	if err := s.CompleteSetupSession(ctx, "missing"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("complete missing: %v", err)
	}
	if _, err := s.FindSetupSession(ctx, "missing"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("find missing: %v", err)
	}

	// An agent session carries the agent; a shared one must not, and vice versa.
	ep2 := &gitdom.WebhookEndpoint{Key: "ep-" + uuid.NewString(), ProviderID: "p"}
	_ = s.CreateWebhookEndpoint(ctx, ep2)
	asess := &gitdom.SetupSession{State: "st-" + uuid.NewString(), ProviderID: "p", Host: "h", Scope: gitdom.ScopeAgent, AgentID: &agent, EndpointKey: ep2.Key, CreatedBy: user, ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.CreateSetupSession(ctx, asess); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.FindSetupSession(ctx, asess.State); got.AgentID == nil || *got.AgentID != agent {
		t.Errorf("agent session: %+v", got)
	}
	bad := &gitdom.SetupSession{State: "st-" + uuid.NewString(), ProviderID: "p", Host: "h", Scope: gitdom.ScopeAgent, EndpointKey: ep2.Key, CreatedBy: user, ExpiresAt: time.Now()}
	if err := s.CreateSetupSession(ctx, bad); err == nil {
		t.Error("an agent session without an agent must be refused by the schema")
	}
	// A session cannot outlive its endpoint, and an unknown endpoint is refused.
	if err := s.DeleteWebhookEndpoint(ctx, ep2.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindSetupSession(ctx, asess.State); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("session should cascade with its endpoint: %v", err)
	}
	orphan := &gitdom.SetupSession{State: "st-" + uuid.NewString(), ProviderID: "p", Host: "h", Scope: gitdom.ScopeShared, EndpointKey: "nope", CreatedBy: user, ExpiresAt: time.Now()}
	if err := s.CreateSetupSession(ctx, orphan); err == nil {
		t.Error("unknown endpoint must be refused")
	}
	if err := s.DeleteWebhookEndpoint(ctx, "already-gone"); err != nil {
		t.Errorf("deleting a missing endpoint is a no-op: %v", err)
	}
}

func TestGitStore_DeleteAgentIdentity(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	agent := fx.agent(nil, false)
	id := sharedIdentity("github.com")
	id.Scope, id.ProviderIdentityID = gitdom.ScopeAgent, "own"
	if err := s.CreateIdentity(ctx, id); err != nil {
		t.Fatal(err)
	}
	_ = s.SetAgentIdentity(ctx, agent, "com.paca.github", id.ID)
	if err := s.DeleteAgentIdentity(ctx, agent, "com.paca.github"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindAgentIdentity(ctx, agent, "com.paca.github"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("still mapped: %v", err)
	}
	if _, err := s.FindIdentity(ctx, id.ID); err != nil {
		t.Errorf("the identity itself must survive unmapping: %v", err)
	}
	if err := s.DeleteAgentIdentity(ctx, agent, "com.paca.github"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("none left: %v", err)
	}
}

func TestGitStore_AccessSessions(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	user, project := fx.user(false), fx.project()
	ident := sharedIdentity("github.com")
	if err := s.CreateIdentity(ctx, ident); err != nil {
		t.Fatal(err)
	}
	sess := &gitdom.AccessSession{State: "g-" + uuid.NewString(), ProviderID: "com.paca.github", IdentityID: ident.ID, ProjectID: project,
		Owner: "acme", ReturnURL: "https://paca.test/p", CreatedBy: user, ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.CreateAccessSession(ctx, sess); err != nil || sess.CreatedAt.IsZero() {
		t.Fatalf("create: %v", err)
	}
	got, err := s.FindAccessSession(ctx, sess.State)
	if err != nil || got.CreatedBy != user || got.ProjectID != project || got.IdentityID != ident.ID || got.Owner != "acme" || got.CompletedAt != nil {
		t.Fatalf("find: %+v %v", got, err)
	}
	if err := s.CompleteAccessSession(ctx, sess.State); err != nil {
		t.Fatal(err)
	}
	first, _ := s.FindAccessSession(ctx, sess.State)
	_ = s.CompleteAccessSession(ctx, sess.State)
	if again, _ := s.FindAccessSession(ctx, sess.State); first.CompletedAt == nil || !again.CompletedAt.Equal(*first.CompletedAt) {
		t.Error("completing twice must keep the first completion time")
	}
	if err := s.CompleteAccessSession(ctx, "missing"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("complete missing: %v", err)
	}
	if _, err := s.FindAccessSession(ctx, "missing"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("find missing: %v", err)
	}
	dup := *sess
	if err := s.CreateAccessSession(ctx, &dup); err == nil {
		t.Error("a state is unique")
	}

	// A session cannot outlive its identity or its project.
	other := &gitdom.AccessSession{State: "g-" + uuid.NewString(), ProviderID: "p", IdentityID: ident.ID, ProjectID: project, CreatedBy: user, ExpiresAt: time.Now().Add(time.Hour)}
	_ = s.CreateAccessSession(ctx, other)
	if err := s.DeleteIdentity(ctx, ident.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindAccessSession(ctx, other.State); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("session should cascade with its identity: %v", err)
	}
	ident2 := sharedIdentity("ghe.acme.com")
	_ = s.CreateIdentity(ctx, ident2)
	byProject := &gitdom.AccessSession{State: "g-" + uuid.NewString(), ProviderID: "p", IdentityID: ident2.ID, ProjectID: project, CreatedBy: user, ExpiresAt: time.Now().Add(time.Hour)}
	_ = s.CreateAccessSession(ctx, byProject)
	pgExec(t, fx.db, `DELETE FROM projects WHERE id = $1`, project)
	if _, err := s.FindAccessSession(ctx, byProject.State); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("session should cascade with its project: %v", err)
	}
}

func TestGitStore_CommentIndexAndOwnedChangeRequests(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	repo := linkRepo(t, s, fx.project(), "acme/web", nil)
	agent, conv := fx.agent(nil, false), uuid.New()

	for i := 0; i < 2; i++ { // indexing twice is a no-op
		if err := s.IndexComment(ctx, &gitdom.IndexedComment{RepositoryID: repo.ID, CommentID: "c1", SubjectKind: "change_request",
			SubjectNumber: "7", ThreadID: "t1", AgentID: &agent, ConversationID: &conv}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.FindIndexedComment(ctx, repo.ID, "c1")
	if err != nil || got.AgentID == nil || *got.AgentID != agent || got.ThreadID != "t1" {
		t.Fatalf("find: %+v %v", got, err)
	}
	if _, err := s.FindIndexedComment(ctx, repo.ID, "nope"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("unknown comment: %v", err)
	}
	latest, err := s.LatestIndexedForSubject(ctx, repo.ID, "change_request", "7")
	if err != nil || latest.CommentID != "c1" {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	if _, err := s.LatestIndexedForSubject(ctx, repo.ID, "issue", "7"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("another subject kind: %v", err)
	}

	cr, err := s.UpsertChangeRequest(ctx, &gitdom.ChangeRequest{RepositoryID: repo.ID, Number: "7", Title: "t", State: "open",
		HeadRef: "paca/x", HeadSHA: "a1", OpenedByAgentID: &agent, OpenedConversationID: &conv})
	if err != nil || cr.OpenedByAgentID == nil || *cr.OpenedByAgentID != agent {
		t.Fatalf("opened by: %+v %v", cr, err)
	}
	// A webhook update knows nothing of Paca: it must not clear the owner, nor
	// blank the title and head it did not carry.
	after, err := s.UpsertChangeRequest(ctx, &gitdom.ChangeRequest{RepositoryID: repo.ID, Number: "7", State: "open", HeadSHA: "b2"})
	if err != nil || after.OpenedByAgentID == nil || *after.OpenedByAgentID != agent || after.Title != "t" || after.HeadRef != "paca/x" || after.HeadSHA != "b2" {
		t.Fatalf("webhook update: %+v %v", after, err)
	}
	found, err := s.FindChangeRequest(ctx, repo.ID, "7")
	if err != nil || found.ID != cr.ID {
		t.Fatalf("find cr: %+v %v", found, err)
	}
	if _, err := s.FindChangeRequest(ctx, repo.ID, "8"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("unknown cr: %v", err)
	}
}

func TestGitStore_SettingsAndTriggers(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project, agent := fx.project(), fx.agent(nil, false)

	def, err := s.GetProjectSettings(ctx, project)
	if err != nil || def.Paused || def.DefaultResponderAgentID != nil || def.MinActorPermission != "write" {
		t.Fatalf("defaults: %+v %v", def, err)
	}
	if err := s.UpsertProjectSettings(ctx, &gitdom.ProjectSettings{ProjectID: project, DefaultResponderAgentID: &agent, Paused: true, MinActorPermission: "triage"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProjectSettings(ctx, project)
	if !got.Paused || got.DefaultResponderAgentID == nil || *got.DefaultResponderAgentID != agent || got.MinActorPermission != "triage" {
		t.Fatalf("stored: %+v", got)
	}
	if err := s.UpsertProjectSettings(ctx, &gitdom.ProjectSettings{ProjectID: project, MinActorPermission: "bogus"}); err == nil {
		t.Fatal("an unknown permission must be rejected by the database")
	}

	if err := s.UpsertAgentTrigger(ctx, &gitdom.AgentTrigger{AgentID: agent, Kind: gitdom.TriggerAutoReview, Enabled: true, Config: json.RawMessage(`{"branches":["main"]}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAgentTrigger(ctx, &gitdom.AgentTrigger{AgentID: agent, Kind: gitdom.TriggerMention, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAgentTrigger(ctx, &gitdom.AgentTrigger{AgentID: agent, Kind: gitdom.TriggerMention, Enabled: false}); err != nil {
		t.Fatalf("upsert twice: %v", err)
	}
	list, err := s.ListAgentTriggers(ctx, agent)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	for _, tr := range list {
		if tr.Kind == gitdom.TriggerAutoReview && (!tr.Enabled || string(tr.Config) != `{"branches": ["main"]}`) {
			t.Fatalf("auto review: %+v", tr)
		}
		if tr.Kind == gitdom.TriggerMention && tr.Enabled {
			t.Fatal("mention must be off")
		}
	}
	if err := s.UpsertAgentTrigger(ctx, &gitdom.AgentTrigger{AgentID: agent, Kind: "bogus", Enabled: true}); err == nil {
		t.Fatal("an unknown trigger kind must be rejected by the database")
	}
}

func TestGitStore_InboxClaimingAndRuns(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	repo := linkRepo(t, s, fx.project(), "acme/web", nil)
	agent := fx.agent(nil, false)
	for _, id := range []string{"e1", "e2", "e3"} {
		_, _ = s.InsertInboxEvent(ctx, &gitdom.InboxEvent{EventID: id, RepositoryID: repo.ID, EndpointKey: "k", Kind: "comment", Action: "created", Payload: json.RawMessage(`{}`)})
		time.Sleep(2 * time.Millisecond) // distinct received_at, so the order is defined
	}

	first, err := s.ClaimInboxEvents(ctx, 2, 0)
	if err != nil || len(first) != 2 || first[0].EventID != "e1" || first[1].EventID != "e2" {
		t.Fatalf("first claim: %+v %v", first, err)
	}
	second, _ := s.ClaimInboxEvents(ctx, 10, 0)
	if len(second) != 1 || second[0].EventID != "e3" {
		t.Fatalf("claimed events must not be handed out again: %+v", second)
	}
	if err := s.FinishInboxEvent(ctx, "e1", repo.ID, gitdom.InboxRouted, "agent started"); err != nil {
		t.Fatal(err)
	}
	pgExec(t, fx.db, `UPDATE git_event_inbox SET processed_at = NOW() - INTERVAL '10 minutes' WHERE event_id = 'e2'`)
	again, _ := s.ClaimInboxEvents(ctx, 10, 0)
	if len(again) != 1 || again[0].EventID != "e2" {
		t.Fatalf("an abandoned claim must be handed out again, a finished one never: %+v", again)
	}

	// An event not yet old enough is left for the next pass, so a burst routes together.
	_, _ = s.InsertInboxEvent(ctx, &gitdom.InboxEvent{EventID: "e4", RepositoryID: repo.ID, EndpointKey: "k", Kind: "comment", Action: "created", Payload: json.RawMessage(`{}`)})
	if young, _ := s.ClaimInboxEvents(ctx, 10, time.Minute); len(young) != 0 {
		t.Fatalf("a young event must wait: %+v", young)
	}
	// Releasing hands an event out again after the delay.
	if err := s.ReleaseInboxEvent(ctx, "e3", repo.ID, 0); err != nil {
		t.Fatal(err)
	}
	if rel, _ := s.ClaimInboxEvents(ctx, 10, 0); len(rel) != 2 {
		t.Fatalf("e3 (released) and e4 must be claimable: %+v", rel)
	}

	conv := uuid.New()
	run := &gitdom.Run{EventID: "e2", RepositoryID: repo.ID, AgentID: agent, Trigger: gitdom.TriggerMention, SubjectKind: "change_request", SubjectNumber: "7", ConversationID: &conv}
	if ok, err := s.CreateRun(ctx, run); err != nil || !ok || run.ID == uuid.Nil || run.Status != gitdom.RunRunning {
		t.Fatalf("create: %v %v %+v", ok, err, run)
	}
	dup := &gitdom.Run{EventID: "e2", RepositoryID: repo.ID, AgentID: agent, Trigger: gitdom.TriggerMention}
	if ok, err := s.CreateRun(ctx, dup); err != nil || ok {
		t.Fatalf("an event starts one run per agent: %v %v", ok, err)
	}
	active, _ := s.ListActiveRuns(ctx)
	if len(active) != 1 {
		t.Fatalf("active: %+v", active)
	}
	found, err := s.FindActiveRunForSubject(ctx, repo.ID, agent, "change_request", "7")
	if err != nil || found.ID != run.ID {
		t.Fatalf("find active: %+v %v", found, err)
	}
	run.Status, run.StatusRef = gitdom.RunSucceeded, "comment-9"
	if err := s.UpdateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if active, _ = s.ListActiveRuns(ctx); len(active) != 0 {
		t.Fatalf("a finished run is not active: %+v", active)
	}
	if _, err := s.FindActiveRunForSubject(ctx, repo.ID, agent, "change_request", "7"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("finished run: %v", err)
	}
	if n, err := s.CountRuns(ctx, repo.ID, agent, gitdom.TriggerMention, "change_request", "7"); err != nil || n != 1 {
		t.Fatalf("count = %d %v", n, err)
	}
	if n, _ := s.CountRuns(ctx, repo.ID, agent, gitdom.TriggerCIFailure, "change_request", "7"); n != 0 {
		t.Fatalf("another trigger: %d", n)
	}

	// An agent-scoped identity maps back to its agent.
	ident := sharedIdentity("github.com")
	ident.Scope, ident.ProviderIdentityID = gitdom.ScopeAgent, "app-agent"
	if err := s.CreateIdentity(ctx, ident); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.FindAgentForIdentity(ctx, ident.ID); ok {
		t.Fatal("an unmapped identity has no agent")
	}
	if err := s.SetAgentIdentity(ctx, agent, ident.ProviderID, ident.ID); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.FindAgentForIdentity(ctx, ident.ID); err != nil || !ok || got != agent {
		t.Fatalf("agent for identity: %v %v %v", got, ok, err)
	}
}

func TestGitStore_TaskGitIsScopedToTheTasksProject(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project, other := fx.project(), fx.project()
	task := uuid.New()
	pgExec(t, fx.db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,1,'t')`, task, project)
	repo := linkRepo(t, s, project, "acme/web", nil)
	cr, err := s.UpsertChangeRequest(ctx, &gitdom.ChangeRequest{RepositoryID: repo.ID, Number: "4", Title: "Fix", State: "open", HeadRef: "feat/x", URL: "https://github.com/acme/web/pull/4"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkTaskToChangeRequest(ctx, task, cr.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkTaskBranch(ctx, task, repo.ID, "feat/x"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListTaskGit(ctx, project, task)
	if err != nil || len(got.ChangeRequests) != 1 || len(got.Branches) != 1 {
		t.Fatalf("got = %+v %v", got, err)
	}
	c := got.ChangeRequests[0]
	if c.Number != "4" || c.RepositoryPath != "acme/web" || c.URL != "https://github.com/acme/web/pull/4" || got.Branches[0].Branch != "feat/x" {
		t.Fatalf("change request = %+v branch = %+v", c, got.Branches[0])
	}
	// Asked through another project, the task's links must not show.
	if foreign, err := s.ListTaskGit(ctx, other, task); err != nil || len(foreign.ChangeRequests) != 0 || len(foreign.Branches) != 0 {
		t.Fatalf("another project must see nothing: %+v %v", foreign, err)
	}
	if none, _ := s.ListTaskGit(ctx, project, uuid.New()); len(none.ChangeRequests) != 0 || none.ChangeRequests == nil {
		t.Fatalf("an unlinked task has empty, non-nil lists: %+v", none)
	}
}

func TestGitStore_TaskLinksCanBeRemoved(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project := fx.project()
	task := uuid.New()
	pgExec(t, fx.db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,1,'t')`, task, project)
	repo := linkRepo(t, s, project, "acme/web", nil)
	cr, _ := s.UpsertChangeRequest(ctx, &gitdom.ChangeRequest{RepositoryID: repo.ID, Number: "4", State: "open"})
	_ = s.LinkTaskToChangeRequest(ctx, task, cr.ID)
	_ = s.LinkTaskBranch(ctx, task, repo.ID, "a/b")

	if ok, err := s.TaskInProject(ctx, project, task); err != nil || !ok {
		t.Fatalf("own project: %v %v", ok, err)
	}
	if ok, _ := s.TaskInProject(ctx, fx.project(), task); ok {
		t.Fatal("another project's task must not match")
	}
	if ok, _ := s.TaskInProject(ctx, project, uuid.New()); ok {
		t.Fatal("unknown task")
	}

	if err := s.UnlinkTaskFromChangeRequest(ctx, task, cr.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkTaskBranch(ctx, task, repo.ID, "a/b"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkTaskBranch(ctx, task, repo.ID, "a/b"); err != nil {
		t.Fatalf("unlinking twice is harmless: %v", err)
	}
	got, _ := s.ListTaskGit(ctx, project, task)
	if len(got.ChangeRequests) != 0 || len(got.Branches) != 0 {
		t.Fatalf("still linked: %+v", got)
	}
	// The change request itself stays cached: it still exists on the platform.
	if _, err := s.FindChangeRequest(ctx, repo.ID, "4"); err != nil {
		t.Fatalf("the cached request must remain: %v", err)
	}
}

func TestGitStore_ReviewFixes(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project := fx.project()

	// A repository path is case-insensitive: the second spelling is already linked.
	linkRepo(t, s, project, "Acme/Web", nil)
	dup := &gitdom.Repository{ProjectID: project, ProviderID: "com.paca.github", Host: "github.com", FullPath: "acme/web"}
	if err := s.CreateRepository(ctx, dup); !errors.Is(err, gitdom.ErrAlreadyLinked) {
		t.Fatalf("a path differing only in case must be refused: %v", err)
	}

	// One unfinished run per agent and subject, even across different events.
	repo := linkRepo(t, s, project, "acme/api", nil)
	agent := fx.agent(nil, false)
	first := &gitdom.Run{EventID: "e1", RepositoryID: repo.ID, AgentID: agent, Trigger: gitdom.TriggerMention, SubjectKind: "change_request", SubjectNumber: "7"}
	if ok, err := s.CreateRun(ctx, first); err != nil || !ok {
		t.Fatalf("first run: %v %v", ok, err)
	}
	second := &gitdom.Run{EventID: "e2", RepositoryID: repo.ID, AgentID: agent, Trigger: gitdom.TriggerMention, SubjectKind: "change_request", SubjectNumber: "7"}
	if ok, err := s.CreateRun(ctx, second); !errors.Is(err, gitdom.ErrRunActive) || ok {
		t.Fatalf("a second unfinished run on the subject must be refused: %v %v", ok, err)
	}
	if ok, err := s.CreateRun(ctx, &gitdom.Run{EventID: "e3", RepositoryID: repo.ID, AgentID: agent, Trigger: gitdom.TriggerMention, SubjectKind: "change_request", SubjectNumber: "8"}); err != nil || !ok {
		t.Fatalf("another subject is fine: %v %v", ok, err)
	}
	other := fx.agent(nil, false)
	if ok, err := s.CreateRun(ctx, &gitdom.Run{EventID: "e4", RepositoryID: repo.ID, AgentID: other, Trigger: gitdom.TriggerMention, SubjectKind: "change_request", SubjectNumber: "7"}); err != nil || !ok {
		t.Fatalf("another agent is fine: %v %v", ok, err)
	}
	first.Status = gitdom.RunSucceeded
	if err := s.UpdateRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CreateRun(ctx, second); err != nil || !ok {
		t.Fatalf("once the first run finished the subject is free: %v %v", ok, err)
	}

	// A deleted task cannot be linked to.
	task := uuid.New()
	pgExec(t, fx.db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,5,'t')`, task, project)
	if ok, _ := s.TaskInProject(ctx, project, task); !ok {
		t.Fatal("a live task is in its project")
	}
	pgExec(t, fx.db, `UPDATE tasks SET deleted_at = NOW() WHERE id = $1`, task)
	if ok, _ := s.TaskInProject(ctx, project, task); ok {
		t.Fatal("a soft-deleted task must not be linkable")
	}

	// Removing an inbox row lets the same delivery be stored again.
	ev := &gitdom.InboxEvent{EventID: "d1", RepositoryID: repo.ID, EndpointKey: "k", Kind: "comment", Action: "created", Payload: json.RawMessage(`{}`)}
	_, _ = s.InsertInboxEvent(ctx, ev)
	if err := s.DeleteInboxEvent(ctx, "d1", repo.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.InsertInboxEvent(ctx, ev); !ok {
		t.Fatal("after deletion the delivery must be accepted again")
	}
}

func TestGitStore_ListedRepositoriesCarryTheirBotsHandle(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project := fx.project()
	ident := sharedIdentity("github.com")
	if err := s.CreateIdentity(ctx, ident); err != nil {
		t.Fatal(err)
	}
	with := linkRepo(t, s, project, "acme/with", &ident.ID)
	without := linkRepo(t, s, project, "acme/without", nil)

	list, err := s.ListProjectRepositories(ctx, project)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	for _, r := range list {
		switch r.ID {
		case with.ID:
			if r.IdentityHandle != "paca-bot" {
				t.Errorf("handle = %q", r.IdentityHandle)
			}
		case without.ID:
			if r.IdentityHandle != "" {
				t.Errorf("a repository with no identity has no handle: %q", r.IdentityHandle)
			}
		}
	}
}

func TestGitStore_WebhookAttemptsComeFromTheAuditLog(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project := fx.project()
	tried := linkRepo(t, s, project, "acme/tried", nil)
	failed := linkRepo(t, s, project, "acme/failed", nil)
	never := linkRepo(t, s, project, "acme/never", nil)
	for _, e := range []*gitdom.ActionLogEntry{
		{ProjectID: &project, RepositoryID: &tried.ID, Tool: "webhook.register", OK: true},
		{ProjectID: &project, RepositoryID: &failed.ID, Tool: "webhook.register", OK: false, Detail: ": forbidden"},
		{ProjectID: &project, RepositoryID: &never.ID, Tool: "git.push", OK: true},
	} {
		if err := s.AppendActionLog(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.WebhookAttempts(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	if !got[tried.ID] || !got[failed.ID] || got[never.ID] || len(got) != 2 {
		t.Fatalf("attempts = %v", got)
	}
}

func TestGitStore_RepositoryDescription(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project := fx.project()
	r := linkRepo(t, s, project, "acme/web", nil)
	if got, err := s.FindRepository(ctx, r.ID); err != nil || got.Description != "" {
		t.Fatalf("default description = %+v %v", got, err)
	}
	if err := s.SetRepositoryDescription(ctx, project, r.ID, "Web client"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.FindRepository(ctx, r.ID); err != nil || got.Description != "Web client" {
		t.Fatalf("description = %+v %v", got, err)
	}
	if err := s.SetRepositoryDescription(ctx, fx.project(), r.ID, "x"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("another project: %v", err)
	}
}

func TestGitStore_DeletingAConnectionLeavesItsRepositoriesToBeRebound(t *testing.T) {
	s, fx := newGitFx(t)
	ctx := t.Context()
	project := fx.project()
	a, b := sharedIdentity("github.com"), sharedIdentity("github.com")
	b.ProviderIdentityID = "app-b"
	for _, i := range []*gitdom.Identity{a, b} {
		if err := s.CreateIdentity(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	r1 := linkRepo(t, s, project, "acme/web", &a.ID)
	linkRepo(t, s, fx.project(), "acme/web", &a.ID) // another project, same connection
	if err := s.SetRepositoryWebhook(ctx, r1.ID, json.RawMessage(`{"id":"5"}`)); err != nil {
		t.Fatal(err)
	}

	counts, err := s.CountRepositories(ctx, []uuid.UUID{a.ID, b.ID})
	if err != nil || counts[a.ID] != 2 || counts[b.ID] != 0 {
		t.Fatalf("counts = %v %v", counts, err)
	}
	if empty, err := s.CountRepositories(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("no ids: %v %v", empty, err)
	}

	if err := s.DeleteIdentity(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.FindRepository(ctx, r1.ID)
	if err != nil || got.IdentityID != nil {
		t.Fatalf("the repository stays, without a connection: %+v %v", got, err)
	}

	if err := s.SetRepositoryIdentity(ctx, r1.ID, b.ID, json.RawMessage(`{"installation":9}`)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.FindRepository(ctx, r1.ID)
	if got.IdentityID == nil || *got.IdentityID != b.ID || string(got.ProviderRef) != `{"installation": 9}` && string(got.ProviderRef) != `{"installation":9}` {
		t.Fatalf("rebound: %+v", got)
	}
	if string(got.WebhookRef) != `{}` {
		t.Fatalf("the webhook of the old connection must be forgotten: %s", got.WebhookRef)
	}
	if err := s.SetRepositoryIdentity(ctx, uuid.New(), b.ID, nil); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("unknown repository: %v", err)
	}
}
