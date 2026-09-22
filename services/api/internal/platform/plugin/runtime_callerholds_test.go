package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz"
)

// callerStore serves permissions per identity so a test can give the shared bot
// user, a user and an agent different sets — the identities permission_check
// must not conflate.
type callerStore struct {
	userGlobal   map[uuid.UUID][]authz.Permission
	userProject  map[uuid.UUID][]authz.Permission // by user id, for any project
	agentGlobal  map[uuid.UUID][]authz.Permission
	agentProject map[uuid.UUID][]authz.Permission // by agent id, for any project
}

func (s *callerStore) ListGlobalPermissions(_ context.Context, id uuid.UUID) ([]authz.Permission, error) {
	return s.userGlobal[id], nil
}

func (s *callerStore) ListProjectPermissions(_ context.Context, id, _ uuid.UUID) ([]authz.Permission, error) {
	return s.userProject[id], nil
}

func (s *callerStore) ListAgentGlobalPermissions(_ context.Context, id uuid.UUID) ([]authz.Permission, error) {
	return s.agentGlobal[id], nil
}

func (s *callerStore) ListAgentProjectPermissions(_ context.Context, id, _ uuid.UUID) ([]authz.Permission, error) {
	return s.agentProject[id], nil
}

func runtimeWith(store authz.PermissionStore) *Runtime {
	return &Runtime{services: HostServices{Authorizer: authz.NewAuthorizer(store)}}
}

const manageAll authz.Permission = "time_logging.manage_all"

func TestCallerHolds_HumanIsJudgedByTheirRoles(t *testing.T) {
	user := uuid.New()
	rt := runtimeWith(&callerStore{
		userGlobal:  map[uuid.UUID][]authz.Permission{user: {authz.PermissionUsersRead}},
		userProject: map[uuid.UUID][]authz.Permission{user: {manageAll}},
	})
	ctx := context.Background()
	project := uuid.NewString()

	if !rt.callerHolds(ctx, &HTTPRequest{UserID: user.String(), ProjectID: project}, manageAll) {
		t.Error("a member holding the permission in the project must pass")
	}
	if rt.callerHolds(ctx, &HTTPRequest{UserID: user.String(), ProjectID: project}, "time_logging.other") {
		t.Error("a member lacking the permission must not pass")
	}
	if !rt.callerHolds(ctx, &HTTPRequest{UserID: user.String()}, authz.PermissionUsersRead) {
		t.Error("a request with no project is judged by the user's global role")
	}
	if rt.callerHolds(ctx, &HTTPRequest{UserID: user.String()}, manageAll) {
		t.Error("a project permission must not be found on a request with no project")
	}
}

// The bug: an agent-key request's UserID is the shared bot user, seeded
// SUPER_ADMIN, so judging by it let every agent pass any check a plugin makes.
func TestCallerHolds_AgentIsJudgedByItsOwnRoleNotTheBotBehindItsKey(t *testing.T) {
	bot, agent := uuid.New(), uuid.New()
	rt := runtimeWith(&callerStore{
		userGlobal:   map[uuid.UUID][]authz.Permission{bot: {authz.PermissionAll}},
		agentProject: map[uuid.UUID][]authz.Permission{agent: {authz.PermissionTasksRead}},
		agentGlobal:  map[uuid.UUID][]authz.Permission{agent: {authz.PermissionUsersRead}},
	})
	ctx := context.Background()
	project := uuid.NewString()
	req := func(projectID string) *HTTPRequest {
		return &HTTPRequest{UserID: bot.String(), AgentID: agent.String(), ProjectID: projectID}
	}

	if rt.callerHolds(ctx, req(project), manageAll) {
		t.Fatal("an agent whose role lacks the permission passed because its key resolves to the SUPER_ADMIN bot")
	}
	if !rt.callerHolds(ctx, req(project), authz.PermissionTasksRead) {
		t.Error("an agent must pass what its own project role grants")
	}
	if rt.callerHolds(ctx, req(""), manageAll) {
		t.Error("a request with no project must be judged by the agent's own global role, not the bot's")
	}
	if !rt.callerHolds(ctx, req(""), authz.PermissionUsersRead) {
		t.Error("an agent must pass what its own global role grants")
	}
}

func TestCallerHolds_AnAgentOutsideTheProjectHoldsNothingThere(t *testing.T) {
	bot, stranger := uuid.New(), uuid.New()
	rt := runtimeWith(&callerStore{userGlobal: map[uuid.UUID][]authz.Permission{bot: {authz.PermissionAll}}})

	req := &HTTPRequest{UserID: bot.String(), AgentID: stranger.String(), ProjectID: uuid.NewString()}
	if rt.callerHolds(context.Background(), req, authz.PermissionTasksRead) {
		t.Fatal("an agent with no membership in the project must hold nothing in it")
	}
}

func TestCallerHolds_FailsClosed(t *testing.T) {
	user := uuid.New()
	full := &callerStore{userGlobal: map[uuid.UUID][]authz.Permission{user: {authz.PermissionAll}}}
	ctx := context.Background()
	ok := &HTTPRequest{UserID: user.String()}

	tests := []struct {
		name string
		rt   *Runtime
		req  *HTTPRequest
		perm authz.Permission
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
			if tc.rt.callerHolds(ctx, tc.req, tc.perm) {
				t.Fatal("expected a refusal")
			}
		})
	}
	// Sanity: the same user does pass when nothing is malformed.
	if !runtimeWith(full).callerHolds(ctx, ok, manageAll) {
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
