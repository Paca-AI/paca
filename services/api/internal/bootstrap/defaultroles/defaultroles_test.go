package defaultroles_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/bootstrap/defaultroles"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

func parse(t *testing.T, r defaultroles.Role) *iam.Policy {
	t.Helper()
	p, err := iam.ParsePolicy(r.Policy)
	if err != nil {
		t.Fatalf("%s: parse: %v", r.Name, err)
	}
	return p
}

func validate(t *testing.T, name string, p *iam.Policy) {
	t.Helper()
	for _, is := range iam.Validate(p, iam.NewRegistry(), iam.NewAttributeSchema()) {
		t.Errorf("%s: %s: %s", name, is.Path, is.Message)
	}
}

func actionsOf(p *iam.Policy) []string {
	var out []string
	for _, s := range p.Statements {
		out = append(out, s.Actions...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// allows reports whether the policy's Allow statements permit action on
// resource (the shipped policies have no Deny and no conditions).
func allows(p *iam.Policy, action, resource string) bool {
	for _, s := range p.Statements {
		if s.Effect != iam.EffectAllow {
			continue
		}
		if slices.ContainsFunc(s.Actions, func(a string) bool { return iam.MatchAction(a, action) }) &&
			slices.ContainsFunc(s.Resources, func(r string) bool { return iam.MatchResource(r, resource) }) {
			return true
		}
	}
	return false
}

func TestEveryShippedPolicyParsesAndValidates(t *testing.T) {
	platform := defaultroles.Platform()
	templates := defaultroles.ProjectTemplates()
	if len(platform) != 3 || len(templates) != 3 {
		t.Fatalf("platform=%d templates=%d, want 3 and 3", len(platform), len(templates))
	}
	for _, r := range platform {
		validate(t, r.Name, parse(t, r))
	}
	// Templates carry the token, which is not a valid project id but is a
	// valid resource segment; instantiate before validating anyway, as the
	// API does.
	for _, r := range defaultroles.Instantiate(uuid.New()) {
		validate(t, r.Name, parse(t, r))
	}
	for _, r := range templates {
		validate(t, r.Name+" (template)", parse(t, r))
	}
}

func TestPlatformRoles(t *testing.T) {
	byName := map[string]defaultroles.Role{}
	for _, r := range defaultroles.Platform() {
		byName[r.Name] = r
		if !r.System {
			t.Errorf("%s must be a system role", r.Name)
		}
		if r.Kind != defaultroles.KindPlatform {
			t.Errorf("%s kind = %s", r.Name, r.Kind)
		}
		if strings.Contains(string(r.Policy), defaultroles.ProjectIDToken) {
			t.Errorf("%s: platform policy mentions the project token", r.Name)
		}
	}

	sa := parse(t, byName[defaultroles.SuperAdmin])
	if len(sa.Statements) != 1 || !slices.Equal(sa.Statements[0].Actions, []string{"*"}) ||
		!slices.Equal(sa.Statements[0].Resources, []string{"*"}) || sa.Statements[0].Effect != iam.EffectAllow {
		t.Errorf("SUPER_ADMIN must be one Allow of * on *, got %+v", sa.Statements)
	}

	// Only SUPER_ADMIN holds "*"; no named global role reaches project/*
	// (GHSA-hjcj-373w-vq8m).
	for _, name := range []string{defaultroles.Admin, defaultroles.User} {
		p := parse(t, byName[name])
		for _, s := range p.Statements {
			if slices.Contains(s.Actions, "*") {
				t.Errorf("%s holds *", name)
			}
			for _, res := range s.Resources {
				if res == "*" || strings.HasPrefix(res, "project/") {
					t.Errorf("%s names resource %q; named global roles never reach into projects", name, res)
				}
			}
		}
		if allows(p, "projects:read", "project/"+uuid.NewString()) {
			t.Errorf("%s reaches a project resource", name)
		}
	}

	// The legacy ADMIN held users.*, global_roles.read, projects.*,
	// settings.write, agents.* and plugins.* — and deliberately not
	// global_roles.write/assign or settings.sso.write.
	admin := parse(t, byName[defaultroles.Admin])
	if got, want := actionsOf(admin), []string{"agents:*", "plugins:*", "projects:*", "roles:read", "settings:write", "users:*"}; !slices.Equal(got, want) {
		t.Errorf("ADMIN actions = %v, want %v", got, want)
	}
	for _, a := range []string{"roles:write", "roles:assign", "settings.sso:write"} {
		if allows(admin, a, "role/x") || allows(admin, a, "sso") || allows(admin, a, "user/x") {
			t.Errorf("ADMIN must not allow %s", a)
		}
	}

	user := parse(t, byName[defaultroles.User])
	if got, want := actionsOf(user), []string{"users:read"}; !slices.Equal(got, want) {
		t.Errorf("USER actions = %v, want %v", got, want)
	}
	if !byName[defaultroles.User].Default {
		t.Error("USER must be the default role")
	}
	for _, name := range []string{defaultroles.SuperAdmin, defaultroles.Admin} {
		if byName[name].Default {
			t.Errorf("%s must not be the default role", name)
		}
	}
}

func TestProjectTemplates(t *testing.T) {
	want := []string{"Admin", "Editor", "Viewer"}
	var got []string
	for _, r := range defaultroles.ProjectTemplates() {
		got = append(got, r.Name)
		if r.Kind != defaultroles.KindProject {
			t.Errorf("%s kind = %s", r.Name, r.Kind)
		}
		if r.Default {
			t.Errorf("%s: a project template cannot be the platform default", r.Name)
		}
		p := parse(t, r)
		// Ruling E: membership implied projects:read, so every project role
		// grants it on the project itself.
		found := false
		for _, s := range p.Statements {
			if s.Effect == iam.EffectAllow && slices.Contains(s.Actions, "projects:read") &&
				slices.Contains(s.Resources, "project/"+defaultroles.ProjectIDToken) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no projects:read on project/PROJECT_ID", r.Name)
		}
		// Every resource stays inside the project.
		for _, s := range p.Statements {
			for _, res := range s.Resources {
				if res != "project/"+defaultroles.ProjectIDToken && !strings.HasPrefix(res, "project/"+defaultroles.ProjectIDToken+"/") {
					t.Errorf("%s: resource %q leaves the project", r.Name, res)
				}
			}
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("templates = %v, want %v", got, want)
	}
}

func TestInstantiateReplacesEveryToken(t *testing.T) {
	pid := uuid.New()
	roles := defaultroles.Instantiate(pid)
	if len(roles) != 3 {
		t.Fatalf("got %d roles", len(roles))
	}
	for _, r := range roles {
		if strings.Contains(string(r.Policy), defaultroles.ProjectIDToken) {
			t.Errorf("%s: token left in %s", r.Name, r.Policy)
		}
		if !strings.Contains(string(r.Policy), pid.String()) {
			t.Errorf("%s: project id missing", r.Name)
		}
	}
	// Instantiating must not mutate the templates.
	for _, r := range defaultroles.ProjectTemplates() {
		if !strings.Contains(string(r.Policy), defaultroles.ProjectIDToken) {
			t.Errorf("%s: template lost its token", r.Name)
		}
	}
	// Two projects never share ids.
	other := defaultroles.Instantiate(uuid.New())
	if string(other[0].Policy) == string(roles[0].Policy) {
		t.Error("policies of two projects are identical")
	}
}

// What each template grants, checked against real resources of a project.
func TestProjectTemplateBehaviour(t *testing.T) {
	pid := uuid.New()
	project := "project/" + pid.String()
	policies := map[string]*iam.Policy{}
	for _, r := range defaultroles.Instantiate(pid) {
		policies[r.Name] = parse(t, r)
	}

	type probe struct {
		action, resource string
		admin, editor    bool
		member, viewer   bool
	}
	probes := []probe{
		{"projects:read", project, true, true, true, true},
		{"projects:write", project, true, false, false, false},
		{"tasks:read", project + "/task/t1", true, true, true, true},
		{"tasks:write", project + "/task/t1", true, true, true, false},
		{"sprints:write", project + "/sprint/s1", true, true, false, false},
		{"sprints:read", project + "/sprint/s1", true, true, true, true},
		{"views:write", project + "/view/v", true, true, true, false},
		{"docs:write", project + "/doc/d", true, true, true, false},
		{"agents:write", project + "/agent/a", true, true, true, false},
		{"environments:connect", project + "/environment/e", true, true, true, false},
		{"environments:read", project + "/environment/e", true, true, true, true},
		{"annotations:resolve", project + "/annotation/x", true, true, true, false},
		{"project.members:read", project, true, true, true, true},
		{"project.members:write", project, true, false, false, false},
		{"project.settings.task_types:write", project, true, false, false, false},
		{"roles:read", project + "/role/r1", true, true, true, true},
		{"roles:write", project + "/role/r1", true, false, false, false},
		// Assigning (iam:PassRole style): a project Admin may attach any role
		// inside its project; Editor and Viewer may attach none.
		{"roles:assign", project + "/role/r1", true, false, false, false},
		{"roles:assign", project + "/role/" + uuid.NewString(), true, false, false, false},
		{"roles:assign", "project/" + uuid.NewString() + "/role/r1", false, false, false, false},
		{"roles:assign", "role/r1", false, false, false, false},
		{"project.activities:read", project, true, false, false, false},
		{"project:export", project, true, false, false, false},
		// Nothing reaches another project or the platform.
		{"tasks:read", "project/" + uuid.NewString() + "/task/t1", false, false, false, false},
		{"projects:read", "project/" + uuid.NewString(), false, false, false, false},
		{"users:read", "user/x", false, false, false, false},
	}
	for _, pr := range probes {
		for name, want := range map[string]bool{"Admin": pr.admin, "Editor": pr.editor, "Viewer": pr.viewer} {
			if got := allows(policies[name], pr.action, pr.resource); got != want {
				t.Errorf("%s %s on %s = %v, want %v", name, pr.action, pr.resource, got, want)
			}
		}
	}
}
