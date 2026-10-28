package router

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// git:read lists repositories and mints read-only credentials; git:write is
// what links, unlinks and mints a credential that can push. A role holding
// only read must never reach the write routes.
func TestGuardsIAM_GitReadAndWrite(t *testing.T) {
	project := uuid.NewString()
	other := uuid.NewString()
	res := "project/" + project + "/*"
	read := func(g guards) func(http.Handler) http.Handler { return g.Project(iam.ActionGitRead) }
	write := func(g guards) func(http.Handler) http.Handler { return g.Project(iam.ActionGitWrite) }

	runIAMGateCases(t, project, []iamGateCase{
		{name: "read role reads", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:read"}, res))}},
		{name: "wildcard reads", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:*"}, res))}},
		{name: "other project's role", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(other, allow([]string{"git:read"}, "project/"+other+"/*"))}},
		{name: "no grants", authenticated: true, want: http.StatusForbidden},
		{name: "anonymous", want: http.StatusUnauthorized},
	}, read)

	runIAMGateCases(t, project, []iamGateCase{
		{name: "read-only role cannot write", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:read"}, res))}},
		{name: "write role writes", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:write"}, res))}},
		{name: "explicit deny wins", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:*"}, res), deny([]string{"git:write"}, res))}},
	}, write)
}

// Bot identities are a workspace-level privilege: a project role holding git:*
// reaches neither the platform gate nor identity management.
func TestGuardsIAM_GitIdentitiesManage(t *testing.T) {
	project := uuid.NewString()
	manage := func(g guards) func(http.Handler) http.Handler { return g.Global(iam.ActionGitIdentitiesManage) }
	runIAMGateCases(t, project, []iamGateCase{
		{name: "platform role with the action", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{platformRole(allow([]string{"git.identities:manage"}, "git_identity/*"))}},
		{name: "wildcard on the domain", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{platformRole(allow([]string{"git.identities:*"}, "git_identity/*"))}},
		{name: "everything", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{platformRole(allow([]string{"*"}, "*"))}},
		{name: "project role with git:*", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:*"}, "project/"+project+"/*"))}},
		{name: "project role naming the action", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"git.identities:manage"}, "project/"+project+"/*"))}},
		{name: "platform git:* does not cover identities", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{platformRole(allow([]string{"git:*"}, "*"))}},
		{name: "explicit deny wins", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{platformRole(allow([]string{"git.identities:manage"}, "git_identity/*"), deny([]string{"git.identities:manage"}, "git_identity/*"))}},
		{name: "anonymous", want: http.StatusUnauthorized},
	}, manage)
}

// Installing an agent's own identity on another account needs both the agent
// (agents:write) and the project's git:write; neither alone is enough.
func TestGuardsIAM_AgentAccessGrantNeedsAgentAndGit(t *testing.T) {
	project := uuid.NewString()
	res := "project/" + project + "/*"
	gate := func(g guards) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return g.AgentUse(iam.ActionAgentsWrite)(g.Project(iam.ActionGitWrite)(next))
		}
	}
	runIAMGateCases(t, project, []iamGateCase{
		{name: "agents:write + git:write", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{projectRole(project, allow([]string{"agents:write", "git:write"}, res))}},
		{name: "agents:write only", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"agents:write"}, res))}},
		{name: "git:write only", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:write"}, res))}},
		{name: "anonymous", want: http.StatusUnauthorized},
	}, gate)
}

// Approving and merging are their own actions: git:write comments and requests
// changes but must satisfy no required review and land no code.
func TestGuardsIAM_GitApproveAndMergeAreSeparate(t *testing.T) {
	project := uuid.NewString()
	res := "project/" + project + "/*"
	for name, action := range map[string]iam.Action{"approve": iam.ActionGitApprove, "merge": iam.ActionGitMerge} {
		action := action
		gate := func(g guards) func(http.Handler) http.Handler { return g.Project(action) }
		runIAMGateCases(t, project, []iamGateCase{
			{name: name + ": write and read are not enough", authenticated: true, want: http.StatusForbidden,
				grants: []iam.Grant{projectRole(project, allow([]string{"git:read", "git:write"}, res))}},
			{name: name + ": the action itself", authenticated: true, want: http.StatusNoContent,
				grants: []iam.Grant{projectRole(project, allow([]string{string(action)}, res))}},
			{name: name + ": git:* covers it", authenticated: true, want: http.StatusNoContent,
				grants: []iam.Grant{projectRole(project, allow([]string{"git:*"}, res))}},
			{name: name + ": deny wins over git:*", authenticated: true, want: http.StatusForbidden,
				grants: []iam.Grant{projectRole(project, allow([]string{"git:*"}, res), deny([]string{string(action)}, res))}},
		}, gate)
	}
}

// Linking a task to a request or branch changes the task and reads the
// repository through the bot (tasks:write + git:read); creating a branch writes
// to the repository, so it needs git:write on top. Neither half is enough alone.
func TestGuardsIAM_TaskGitLinking(t *testing.T) {
	project := uuid.NewString()
	res := "project/" + project + "/*"
	both := func(task, git iam.Action) func(g guards) func(http.Handler) http.Handler {
		return func(g guards) func(http.Handler) http.Handler {
			return func(next http.Handler) http.Handler {
				return g.Project(task)(g.Project(git)(next))
			}
		}
	}
	runIAMGateCases(t, project, []iamGateCase{
		{name: "link: tasks:write + git:read", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{projectRole(project, allow([]string{"tasks:write", "git:read"}, res))}},
		{name: "link: tasks:write alone", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"tasks:write"}, res))}},
		{name: "link: git:read alone", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:read"}, res))}},
	}, both(iam.ActionTasksWrite, iam.ActionGitRead))
	runIAMGateCases(t, project, []iamGateCase{
		{name: "create branch: tasks:write + git:write", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{projectRole(project, allow([]string{"tasks:write", "git:write"}, res))}},
		{name: "create branch: git:read is not enough", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"tasks:write", "git:read"}, res))}},
	}, both(iam.ActionTasksWrite, iam.ActionGitWrite))
}

// A project's own connection needs the project's git:write and the
// project-level git.connections:manage; neither alone is enough, and the
// workspace-level git.identities:manage does not stand in for it.
func TestGuardsIAM_ProjectConnectionNeedsWriteAndConnectionsManage(t *testing.T) {
	project := uuid.NewString()
	res := "project/" + project + "/*"
	gate := func(g guards) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return g.Project(iam.ActionGitWrite)(g.Project(iam.ActionGitConnectionsManage)(next))
		}
	}
	runIAMGateCases(t, project, []iamGateCase{
		{name: "git:write + connections:manage", authenticated: true, want: http.StatusNoContent,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:write", "git.connections:manage"}, res))}},
		{name: "git:write only", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"git:write"}, res))}},
		{name: "connections:manage only", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{projectRole(project, allow([]string{"git.connections:manage"}, res))}},
		{name: "workspace identities:manage is not enough", authenticated: true, want: http.StatusForbidden,
			grants: []iam.Grant{
				projectRole(project, allow([]string{"git:write"}, res)),
				platformRole(allow([]string{"git.identities:manage"}, "git_identity/*")),
			}},
		{name: "anonymous", want: http.StatusUnauthorized},
	}, gate)
}
