package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	rolesvc "github.com/Paca-AI/api/internal/service/role"
)

// These tests run the identity and membership flows (project creation, member
// and agent add/remove, user and global-agent creation) against a real
// database and judge their outcome with the real IAM authorizer: what a
// principal may do is whatever the stored attachments say.

type flowEnv struct {
	t     *testing.T
	db    *sqlx.DB
	authz *iam.Authorizer
	users *UserRepository
	projs *ProjectRepository
	agent *AgentRepository
	svc   *projectsvc.Service
	inv   *flowInvalidator
}

type flowInvalidator struct{ ids []string }

func (f *flowInvalidator) Invalidate(ids ...string) { f.ids = append(f.ids, ids...) }

func newFlowEnv(t *testing.T) *flowEnv {
	t.Helper()
	db := newIAMPGTestDB(t)
	e := &flowEnv{
		t: t, db: db, authz: NewIAMAuthorizer(db),
		users: NewUserRepository(db), projs: NewProjectRepository(db), agent: NewAgentRepository(db),
		inv: &flowInvalidator{},
	}
	e.svc = projectsvc.New(e.projs, nil, e.agent).WithRoleInvalidator(e.inv)
	return e
}

func (e *flowEnv) user(name string) uuid.UUID {
	e.t.Helper()
	u := &userdom.User{ID: uuid.New(), Username: name, PasswordHash: "x"}
	if err := e.users.Create(context.Background(), u); err != nil {
		e.t.Fatalf("create user %s: %v", name, err)
	}
	return u.ID
}

func (e *flowEnv) can(p iam.Principal, action, resource string) bool {
	e.t.Helper()
	res, err := e.authz.Authorize(context.Background(), p, action, resource)
	if err != nil {
		e.t.Fatalf("authorize %s on %s: %v", action, resource, err)
	}
	return res.Allowed
}

func (e *flowEnv) role(project uuid.UUID, name string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Get(&id, `SELECT id FROM roles WHERE project_id = $1 AND name = $2`, project, name); err != nil {
		e.t.Fatalf("role %s: %v", name, err)
	}
	return id
}

func (e *flowEnv) createProject(creator uuid.UUID, name string) *projectdom.Project {
	e.t.Helper()
	p, err := e.svc.Create(context.Background(), projectdom.CreateProjectInput{Name: name, CreatedBy: &creator})
	if err != nil {
		e.t.Fatalf("create project: %v", err)
	}
	return p
}

func TestFlow_CreateProject_RolesCreatorAndAccess(t *testing.T) {
	e := newFlowEnv(t)
	creator := e.user("creator")
	p := e.createProject(creator, "Flow One")
	project := "project/" + p.ID.String()

	// The four templates exist as roles owned by the project, with the real id
	// in the stored policy and no template token anywhere.
	var rows []struct {
		Name     string `db:"name"`
		Policy   string `db:"policy"`
		IsSystem bool   `db:"is_system"`
	}
	if err := e.db.Select(&rows, `SELECT name, policy::text AS policy, is_system FROM roles WHERE project_id = $1 ORDER BY name`, p.ID); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
		if strings.Contains(r.Policy, "PROJECT_ID") {
			t.Errorf("%s: token left in the stored policy", r.Name)
		}
		if !strings.Contains(r.Policy, p.ID.String()) {
			t.Errorf("%s: stored policy does not name the project", r.Name)
		}
		if r.IsSystem != (r.Name == "Admin") {
			t.Errorf("%s: is_system = %v", r.Name, r.IsSystem)
		}
	}
	if fmt.Sprint(names) != "[Admin Editor Viewer]" {
		t.Errorf("project roles = %v", names)
	}

	// The creator is a member holding Admin inside the project, and can act at once.
	members, err := e.projs.ListMembers(context.Background(), p.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("members = %v, %v", members, err)
	}
	if got := members[0].RoleNames(); fmt.Sprint(got) != "[Admin]" {
		t.Errorf("creator roles = %v", got)
	}
	me := iam.User(creator.String())
	if !e.can(me, "projects:read", project) || !e.can(me, "tasks:write", project+"/task/x") ||
		!e.can(me, "project.members:write", project) {
		t.Error("the creator cannot act in the project right after creating it")
	}
	// ...and nowhere else, nor on the platform.
	other := "project/" + uuid.NewString()
	if e.can(me, "projects:read", other) || e.can(me, "users:write", "user/x") {
		t.Error("the project Admin role leaks outside its project")
	}
	// Project creation without a creator makes the roles and no member.
	bare, err := e.svc.Create(context.Background(), projectdom.CreateProjectInput{Name: "Bare"})
	if err != nil {
		t.Fatal(err)
	}
	if ms, _ := e.projs.ListMembers(context.Background(), bare.ID); len(ms) != 0 {
		t.Errorf("bare project has members: %v", ms)
	}
}

func TestFlow_CreateProject_IsAtomic(t *testing.T) {
	e := newFlowEnv(t)
	// A creator that does not exist fails the membership insert; nothing of
	// the project may remain.
	ghost := uuid.New()
	if _, err := e.svc.Create(context.Background(), projectdom.CreateProjectInput{Name: "Ghost", CreatedBy: &ghost}); err == nil {
		t.Fatal("expected the creation to fail for an unknown creator")
	}
	var n int
	if err := e.db.Get(&n, `SELECT (SELECT COUNT(*) FROM projects WHERE name = 'Ghost') + (SELECT COUNT(*) FROM roles WHERE project_id IS NOT NULL)`); err != nil || n != 0 {
		t.Fatalf("a half-created project remains: %d %v", n, err)
	}
}

func TestFlow_Members_ViewerEditorRemoveReadd(t *testing.T) {
	e := newFlowEnv(t)
	ctx := context.Background()
	creator := e.user("creator")
	p := e.createProject(creator, "Flow Members")
	project := "project/" + p.ID.String()
	viewerRole, editorRole := e.role(p.ID, "Viewer"), e.role(p.ID, "Editor")

	alice := e.user("alice")
	m, err := e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{
		UserID: alice, RoleIDs: []uuid.UUID{viewerRole}, CreatedBy: &creator, Description: " dev ",
	})
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	if fmt.Sprint(m.RoleNames()) != "[Viewer]" || m.Description != "dev" {
		t.Errorf("member = %+v", m)
	}
	if !containsStr(e.inv.ids, viewerRole.String()) {
		t.Errorf("the policy cache was not told about the role that gained a holder: %v", e.inv.ids)
	}
	var by *string
	if err := e.db.Get(&by, `SELECT created_by::text FROM role_attachments WHERE principal_id = $1 AND role_id = $2`, alice, viewerRole); err != nil || by == nil || *by != creator.String() {
		t.Errorf("created_by = %v %v", by, err)
	}

	a := iam.User(alice.String())
	// Ruling Z2: membership implies reading the project, now by the template's projects:read.
	if !e.can(a, "projects:read", project) || !e.can(a, "tasks:read", project+"/task/x") {
		t.Error("a Viewer cannot read the project")
	}
	if e.can(a, "tasks:write", project+"/task/x") || e.can(a, "project.members:write", project) {
		t.Error("a Viewer can write")
	}

	// A role of another project cannot be attached; the membership is not left behind.
	p2 := e.createProject(creator, "Other Project")
	bob := e.user("bob")
	_, err = e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{UserID: bob, RoleIDs: []uuid.UUID{e.role(p2.ID, "Admin")}})
	if !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("foreign role: expected ErrNotAttachable, got %v", err)
	}
	if _, err := e.projs.FindMember(ctx, p.ID, bob); !errors.Is(err, projectdom.ErrMemberNotFound) {
		t.Errorf("the failed add left a member behind: %v", err)
	}
	// Neither can an unknown id, and an empty set is refused before the database.
	if _, err = e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{UserID: bob, RoleIDs: []uuid.UUID{uuid.New()}}); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Errorf("unknown role: %v", err)
	}
	if _, err = e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{UserID: bob}); !errors.Is(err, roledom.ErrRoleRequired) {
		t.Errorf("no roles: %v", err)
	}
	// A platform role may be attached inside a project.
	var userRole uuid.UUID
	if err := e.db.Get(&userRole, `SELECT id FROM roles WHERE project_id IS NULL AND name = 'USER'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{UserID: bob, RoleIDs: []uuid.UUID{userRole}}); err != nil {
		t.Errorf("a platform role must be attachable in a project: %v", err)
	}

	// Removal: the member loses access at once and its attachments are gone.
	if err := e.svc.RemoveMemberByMemberID(ctx, p.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	if e.can(a, "projects:read", project) {
		t.Error("a removed member still reads the project")
	}
	var n int
	if err := e.db.Get(&n, `SELECT COUNT(*) FROM role_attachments WHERE principal_id = $1 AND project_id = $2`, alice, p.ID); err != nil || n != 0 {
		t.Errorf("attachments left after removal: %d %v", n, err)
	}
	// Re-adding starts from exactly the new roles.
	if _, err := e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{UserID: alice, RoleIDs: []uuid.UUID{editorRole}}); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	back, _ := e.projs.FindMember(ctx, p.ID, alice)
	if fmt.Sprint(back.RoleNames()) != "[Editor]" || !e.can(a, "tasks:write", project+"/task/x") || e.can(a, "project.members:write", project) {
		t.Errorf("re-added member = %v", back.RoleNames())
	}
	if _, err := e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{UserID: alice, RoleIDs: []uuid.UUID{editorRole}}); !errors.Is(err, projectdom.ErrMemberAlreadyAdded) {
		t.Errorf("duplicate: %v", err)
	}

	// Stale project-scoped attachments of a non-member (left by an older
	// removal path) never leak into a new membership.
	pgExec(t, e.db, `UPDATE project_members SET deleted_at = NOW() WHERE project_id = $1 AND user_id = $2`, p.ID, alice)
	pgExec(t, e.db, `INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id) VALUES ($1, 'user', $2, $3)`, e.role(p.ID, "Admin"), alice, p.ID)
	if _, err := e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{UserID: alice, RoleIDs: []uuid.UUID{viewerRole}}); err != nil {
		t.Fatal(err)
	}
	back, _ = e.projs.FindMember(ctx, p.ID, alice)
	if fmt.Sprint(back.RoleNames()) != "[Viewer]" {
		t.Errorf("stale attachment survived the re-add: %v", back.RoleNames())
	}
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestFlow_RoleReplacementThroughTheRoleService(t *testing.T) {
	e := newFlowEnv(t)
	ctx := context.Background()
	creator := e.user("creator")
	p := e.createProject(creator, "Flow Replace")
	project := "project/" + p.ID.String()
	alice := e.user("alice")
	viewer, editor := e.role(p.ID, "Viewer"), e.role(p.ID, "Editor")
	m, err := e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{UserID: alice, RoleIDs: []uuid.UUID{viewer}})
	if err != nil {
		t.Fatal(err)
	}

	cache := &membersCacheSpy{}
	inv := &flowInvalidator{}
	roles := rolesvc.New(NewRoleRepository(e.db), inv, e.authz, e.authz.Registry(), e.authz.Schema()).WithMembersCache(cache)

	a := iam.User(alice.String())
	if e.can(a, "tasks:write", project+"/task/x") {
		t.Fatal("precondition: a Viewer cannot write tasks")
	}
	if _, err := roles.ReplaceMemberRoles(ctx, p.ID, m.ID, []uuid.UUID{editor}, &creator); err != nil {
		t.Fatalf("replace member roles: %v", err)
	}
	if !e.can(a, "tasks:write", project+"/task/x") {
		t.Error("the new role is not effective")
	}
	if got, _ := e.projs.FindMember(ctx, p.ID, alice); fmt.Sprint(got.RoleNames()) != "[Editor]" {
		t.Errorf("member roles after replace = %v", got.RoleNames())
	}
	if len(inv.ids) == 0 {
		t.Error("the policy cache was not invalidated by the attachment change")
	}
	if len(cache.projects) != 1 || cache.projects[0] != p.ID {
		t.Errorf("the members cache of the project was not dropped: %v", cache.projects)
	}

	// Users: replace the platform roles; the user list shows them.
	var adminRole, userRole uuid.UUID
	if err := e.db.Get(&adminRole, `SELECT id FROM roles WHERE project_id IS NULL AND name = 'ADMIN'`); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Get(&userRole, `SELECT id FROM roles WHERE project_id IS NULL AND name = 'USER'`); err != nil {
		t.Fatal(err)
	}
	if !e.can(a, "users:read", "user/x") || e.can(a, "users:write", "user/x") {
		t.Fatal("precondition: a new user holds USER only")
	}
	if _, err := roles.ReplaceUserRoles(ctx, alice, []uuid.UUID{adminRole}, &creator); err != nil {
		t.Fatal(err)
	}
	if !e.can(a, "users:write", "user/x") {
		t.Error("ADMIN is not effective")
	}
	if u, _ := e.users.FindByID(ctx, alice); fmt.Sprint(u.RoleNames()) != "[ADMIN]" {
		t.Errorf("user roles = %v", u.RoleNames())
	}
	_ = userRole

	// Agents: a global agent starts with the default role; replace it.
	g := &agentdom.Agent{ID: uuid.New(), AgentScope: agentdom.AgentScopeGlobal, Name: "G", Handle: "g-bot", AgentType: "llm",
		LLMProvider: "openai", LLMModel: "gpt", LLMAPIKeySecret: "k", MaxIterations: 1, TimeoutMinutes: 1, ParallelismLimit: 1}
	if err := e.agent.CreateGlobalAgent(ctx, g); err != nil {
		t.Fatalf("create global agent: %v", err)
	}
	if len(g.Roles) != 1 || g.Roles[0].Name != "USER" {
		t.Fatalf("global agent roles after create = %+v", g.Roles)
	}
	ga := iam.Agent(g.ID.String())
	if !e.can(ga, "users:read", "user/x") {
		t.Error("the default role is not effective for the new global agent")
	}
	if _, err := roles.ReplaceAgentRoles(ctx, g.ID, []uuid.UUID{adminRole}, &creator); err != nil {
		t.Fatal(err)
	}
	if !e.can(ga, "users:write", "user/x") {
		t.Error("the agent's new platform role is not effective")
	}
	loaded, err := e.agent.FindAgentByID(ctx, g.ID)
	if err != nil || len(loaded.Roles) != 1 || loaded.Roles[0].Name != "ADMIN" {
		t.Errorf("FindAgentByID roles = %+v %v", loaded, err)
	}
	listed, err := e.agent.ListGlobalAgents(ctx)
	if err != nil || len(listed) != 1 || len(listed[0].Roles) != 1 {
		t.Errorf("ListGlobalAgents roles = %+v %v", listed, err)
	}
}

type membersCacheSpy struct{ projects []uuid.UUID }

func (m *membersCacheSpy) InvalidateMembersCache(_ context.Context, id uuid.UUID) error {
	m.projects = append(m.projects, id)
	return nil
}

func TestFlow_AgentMembers(t *testing.T) {
	e := newFlowEnv(t)
	ctx := context.Background()
	creator := e.user("creator")
	p := e.createProject(creator, "Flow Agents")
	project := "project/" + p.ID.String()
	editor, viewer := e.role(p.ID, "Editor"), e.role(p.ID, "Viewer")

	// A project agent created with its membership holds the roles in the project.
	a := &agentdom.Agent{ID: uuid.New(), ProjectID: p.ID, AgentScope: agentdom.AgentScopeProject, Name: "A", Handle: "a-bot",
		AgentType: "llm", LLMProvider: "openai", LLMModel: "gpt", LLMAPIKeySecret: "k", MaxIterations: 1, TimeoutMinutes: 1, ParallelismLimit: 1}
	if err := e.agent.CreateAgentWithMembership(ctx, a, uuid.New(), p.ID, []uuid.UUID{editor}, &creator); err != nil {
		t.Fatalf("create agent with membership: %v", err)
	}
	ap := iam.Agent(a.ID.String())
	if !e.can(ap, "tasks:write", project+"/task/x") || !e.can(ap, "projects:read", project) {
		t.Error("the project agent cannot act with its role")
	}
	// A foreign role rolls the whole creation back.
	p2 := e.createProject(creator, "Flow Agents 2")
	bad := &agentdom.Agent{ID: uuid.New(), ProjectID: p.ID, AgentScope: agentdom.AgentScopeProject, Name: "B", Handle: "b-bot",
		AgentType: "llm", LLMProvider: "openai", LLMModel: "gpt", LLMAPIKeySecret: "k", MaxIterations: 1, TimeoutMinutes: 1, ParallelismLimit: 1}
	err := e.agent.CreateAgentWithMembership(ctx, bad, uuid.New(), p.ID, []uuid.UUID{e.role(p2.ID, "Admin")}, &creator)
	if !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("foreign role: %v", err)
	}
	var n int
	if err := e.db.Get(&n, `SELECT COUNT(*) FROM agents WHERE id = $1`, bad.ID); err != nil || n != 0 {
		t.Fatalf("the agent was stored despite the failure: %d %v", n, err)
	}
	// Deleting the project agent drops its attachments.
	if err := e.agent.SoftDeleteAgentWithMembership(ctx, p.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Get(&n, `SELECT COUNT(*) FROM role_attachments WHERE principal_id = $1`, a.ID); err != nil || n != 0 {
		t.Errorf("attachments left after deleting the agent: %d %v", n, err)
	}

	// Inviting a global agent: attachments of principal type agent, in the project.
	g := &agentdom.Agent{ID: uuid.New(), AgentScope: agentdom.AgentScopeGlobal, Name: "G", Handle: "g-bot", AgentType: "llm",
		LLMProvider: "openai", LLMModel: "gpt", LLMAPIKeySecret: "k", MaxIterations: 1, TimeoutMinutes: 1, ParallelismLimit: 1}
	if err := e.agent.CreateGlobalAgent(ctx, g); err != nil {
		t.Fatal(err)
	}
	m, err := e.svc.AddMember(ctx, p.ID, projectdom.AddMemberInput{AgentID: &g.ID, RoleIDs: []uuid.UUID{viewer}, CreatedBy: &creator})
	if err != nil {
		t.Fatalf("invite agent: %v", err)
	}
	if !m.IsAgent() || fmt.Sprint(m.RoleNames()) != "[Viewer]" {
		t.Errorf("agent member = %+v", m)
	}
	gp := iam.Agent(g.ID.String())
	if !e.can(gp, "tasks:read", project+"/task/x") || e.can(gp, "tasks:write", project+"/task/x") {
		t.Error("the invited agent's project role is not what was attached")
	}
	if err := e.projs.RemoveAgentMember(ctx, p.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	if e.can(gp, "tasks:read", project+"/task/x") {
		t.Error("the agent kept access after leaving the project")
	}
	// Deleting the global agent removes its platform attachments as well.
	if err := e.agent.SoftDeleteGlobalAgentCascade(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Get(&n, `SELECT COUNT(*) FROM role_attachments WHERE principal_id = $1`, g.ID); err != nil || n != 0 {
		t.Errorf("attachments left after deleting the global agent: %d %v", n, err)
	}
}
