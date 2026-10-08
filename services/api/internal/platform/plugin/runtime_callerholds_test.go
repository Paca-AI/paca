package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// callerStore serves IAM grants per principal so a test can give the shared
// bot user, a user and an agent different grants — the identities
// permission_check must not conflate.
type callerStore map[iam.Principal][]iam.Grant

func (s callerStore) ListGrants(_ context.Context, p iam.Principal) ([]iam.Grant, error) {
	return s[p], nil
}

func runtimeWith(store iam.Store) *Runtime {
	return &Runtime{services: HostServices{Authorizer: iam.NewAuthorizer(store, iam.NewRegistry(), iam.NewAttributeSchema())}}
}

// platformRoots are the resources a migrated named platform permission covers.
var platformRoots = []string{"user", "user/*", "role", "role/*", "plugin", "plugin/*", "settings", "sso", "agent", "agent/*", "project"}

func platformGrant(actions ...string) iam.Grant {
	return iam.Grant{RoleID: uuid.NewString(), Policy: &iam.Policy{Statements: []iam.Statement{
		{Effect: iam.EffectAllow, Actions: actions, Resources: platformRoots},
	}}}
}

func starGrant() iam.Grant {
	return iam.Grant{RoleID: uuid.NewString(), Policy: &iam.Policy{Statements: []iam.Statement{
		{Effect: iam.EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
	}}}
}

func projectGrant(projectID string, actions ...string) iam.Grant {
	return iam.Grant{RoleID: uuid.NewString(), ProjectID: projectID, Policy: &iam.Policy{Statements: []iam.Statement{
		{Effect: iam.EffectAllow, Actions: actions, Resources: []string{"project/" + projectID + "/*"}},
	}}}
}

func userP(id uuid.UUID) iam.Principal  { return iam.Principal{Type: "user", ID: id.String()} }
func agentP(id uuid.UUID) iam.Principal { return iam.Principal{Type: "agent", ID: id.String()} }

const manageAll iam.Action = "time_logging:manage_all"

const pluginID = "6f1d3c1e-8a0b-4c55-9d3e-2b7f0c4a9e11"

func TestCallerHolds_HumanIsJudgedByTheirRoles(t *testing.T) {
	user := uuid.New()
	project := uuid.NewString()
	rt := runtimeWith(callerStore{userP(user): {
		platformGrant("users:read"),
		projectGrant(project, "time_logging:manage_all"),
	}})
	ctx := context.Background()

	if !rt.callerHolds(ctx, &HTTPRequest{UserID: user.String(), ProjectID: project}, pluginID, manageAll) {
		t.Error("a member holding the permission in the project must pass")
	}
	if rt.callerHolds(ctx, &HTTPRequest{UserID: user.String(), ProjectID: project}, pluginID, "time_logging:other") {
		t.Error("a member lacking the permission must not pass")
	}
	if !rt.callerHolds(ctx, &HTTPRequest{UserID: user.String()}, pluginID, iam.ActionUsersRead) {
		t.Error("a request with no project is judged by the user's global role")
	}
	if rt.callerHolds(ctx, &HTTPRequest{UserID: user.String()}, pluginID, manageAll) {
		t.Error("a project permission must not be found on a request with no project")
	}
}

// The bug: an agent-key request's UserID is the shared bot user, seeded
// SUPER_ADMIN, so judging by it let every agent pass any check a plugin makes.
func TestCallerHolds_AgentIsJudgedByItsOwnRoleNotTheBotBehindItsKey(t *testing.T) {
	bot, agent := uuid.New(), uuid.New()
	project := uuid.NewString()
	rt := runtimeWith(callerStore{
		userP(bot):    {starGrant()},
		agentP(agent): {projectGrant(project, "tasks:read"), platformGrant("users:read")},
	})
	ctx := context.Background()
	req := func(projectID string) *HTTPRequest {
		return &HTTPRequest{UserID: bot.String(), AgentID: agent.String(), ProjectID: projectID}
	}

	if rt.callerHolds(ctx, req(project), pluginID, manageAll) {
		t.Fatal("an agent whose role lacks the permission passed because its key resolves to the SUPER_ADMIN bot")
	}
	if !rt.callerHolds(ctx, req(project), pluginID, iam.ActionTasksRead) {
		t.Error("an agent must pass what its own project role grants")
	}
	if rt.callerHolds(ctx, req(""), pluginID, manageAll) {
		t.Error("a request with no project must be judged by the agent's own global role, not the bot's")
	}
	if !rt.callerHolds(ctx, req(""), pluginID, iam.ActionUsersRead) {
		t.Error("an agent must pass what its own global role grants")
	}
}

func TestCallerHolds_AnAgentOutsideTheProjectHoldsNothingThere(t *testing.T) {
	bot, stranger := uuid.New(), uuid.New()
	rt := runtimeWith(callerStore{userP(bot): {starGrant()}})

	req := &HTTPRequest{UserID: bot.String(), AgentID: stranger.String(), ProjectID: uuid.NewString()}
	if rt.callerHolds(context.Background(), req, pluginID, iam.ActionTasksRead) {
		t.Fatal("an agent with no membership in the project must hold nothing in it")
	}
}

func TestCallerHolds_FailsClosed(t *testing.T) {
	user := uuid.New()
	full := callerStore{userP(user): {starGrant()}}
	ctx := context.Background()
	ok := &HTTPRequest{UserID: user.String()}

	tests := []struct {
		name string
		rt   *Runtime
		req  *HTTPRequest
		perm iam.Action
	}{
		{"no request", runtimeWith(full), nil, manageAll},
		{"blank permission", runtimeWith(full), ok, ""},
		{"no authorizer", &Runtime{}, ok, manageAll},
		{"bad user id", runtimeWith(full), &HTTPRequest{UserID: "not-a-uuid"}, manageAll},
		{"bad agent id", runtimeWith(full), &HTTPRequest{UserID: user.String(), AgentID: "not-a-uuid"}, manageAll},
		{"bad project id", runtimeWith(full), &HTTPRequest{UserID: user.String(), ProjectID: "not-a-uuid"}, manageAll},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.rt.callerHolds(ctx, tc.req, pluginID, tc.perm) {
				t.Fatal("expected a refusal")
			}
		})
	}
	// Sanity: the same user does pass when nothing is malformed.
	if !runtimeWith(full).callerHolds(ctx, ok, pluginID, manageAll) {
		t.Fatal("control case: a wildcard holder must pass")
	}
}

// The agent id is for the host's own decision; plugins get the envelope as
// before, so nothing about the plugin ABI changes.
func TestHTTPRequest_AgentIDIsNotSerialisedToPlugins(t *testing.T) {
	raw, err := json.Marshal(&HTTPRequest{UserID: "u", AgentID: "a-secret-agent-id"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "a-secret-agent-id") || strings.Contains(string(raw), "agent_id") {
		t.Fatalf("the agent id leaked into the plugin-facing envelope: %s", raw)
	}
}

// A plugin's own action with no project is checked on the plugin's platform
// resource plugin/<id>, which a migrated platform-wide grant covers; a grant
// limited to another plugin does not.
func TestCallerHolds_PluginActionWithoutProjectIsCheckedOnThePlugin(t *testing.T) {
	user := uuid.New()
	ctx := context.Background()
	req := &HTTPRequest{UserID: user.String()}
	if !runtimeWith(callerStore{userP(user): {platformGrant("time_logging:manage_all")}}).callerHolds(ctx, req, pluginID, manageAll) {
		t.Error("a platform grant of the plugin action must pass on plugin/<id>")
	}
	other := iam.Grant{RoleID: "r", Policy: &iam.Policy{Statements: []iam.Statement{
		{Effect: iam.EffectAllow, Actions: []string{"time_logging:manage_all"}, Resources: []string{"plugin/" + uuid.NewString()}},
	}}}
	if runtimeWith(callerStore{userP(user): {other}}).callerHolds(ctx, req, pluginID, manageAll) {
		t.Error("a grant on another plugin must not pass")
	}
}

// A role can grant a plugin's own action on the plugin inside one project: it
// works there, not in another project, and not for a different plugin.
func TestCallerHolds_PluginActionGrantedOnPluginInOneProject(t *testing.T) {
	user := uuid.New()
	p, q := uuid.NewString(), uuid.NewString()
	grant := iam.Grant{RoleID: uuid.NewString(), Policy: &iam.Policy{Statements: []iam.Statement{
		{Effect: iam.EffectAllow, Actions: []string{string(manageAll)}, Resources: []string{"project/" + p + "/plugin/" + pluginID}},
	}}}
	rt := runtimeWith(callerStore{userP(user): {grant}})
	ctx := context.Background()
	at := func(project, plugin string) bool {
		return rt.callerHolds(ctx, &HTTPRequest{UserID: user.String(), ProjectID: project}, plugin, manageAll)
	}
	if !at(p, pluginID) {
		t.Error("granted on the plugin in project P: want allowed")
	}
	if at(q, pluginID) {
		t.Error("project Q must not be reached by a grant on project P's plugin")
	}
	if at(p, uuid.NewString()) {
		t.Error("another plugin must not be reached")
	}
}
