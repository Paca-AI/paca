package iam

import (
	"fmt"
	"sort"
	"sync"
	"testing"
)

func TestBuiltinActionsAreRegisteredAndWellFormed(t *testing.T) {
	r := NewRegistry()
	seen := map[Action]bool{}
	for _, a := range BuiltinActions() {
		if seen[a] {
			t.Errorf("duplicate builtin action %q", a)
		}
		seen[a] = true
		if !r.HasAction(string(a)) {
			t.Errorf("builtin %q not registered", a)
		}
		if err := NewRegistry().Register(string(a)); err != nil {
			t.Errorf("builtin %q is malformed: %v", a, err)
		}
	}
	if got := len(r.Actions()); got != len(BuiltinActions()) {
		t.Errorf("registry has %d actions, BuiltinActions %d", got, len(BuiltinActions()))
	}
	// The explicit catalogue (migration 000064 produced exactly these).
	want := []string{
		"agents:read", "agents:write", "annotations:read", "annotations:resolve", "annotations:write",
		"conversations:read", "conversations:write", "docs:read", "docs:write",
		"environments:connect", "environments:read", "environments:write",
		"plugins:read", "plugins:write", "project.activities:read", "project.members:read", "project.members:write",
		"project.settings.custom_fields:write", "project.settings.task_statuses:write", "project.settings.task_types:write",
		"project:export", "projects:create", "projects:delete", "projects:read", "projects:write",
		"roles:assign", "roles:read", "roles:write", "settings.sso:write", "settings:write",
		"sprints:read", "sprints:write", "tasks:read", "tasks:write", "users:delete", "users:read", "users:write",
		"views:read", "views:write", "workflows:read", "workflows:write",
	}
	got := r.Actions()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Actions() = %v\nwant %v", got, want)
	}
}

func TestRegistryBuiltins(t *testing.T) {
	r := NewRegistry()
	for _, a := range []string{
		"environments:connect", "annotations:resolve", "settings.sso:write", "plugins:write",
		"roles:assign", "roles:read", "roles:write", "tasks:*", "roles:*", "*",
	} {
		if !r.HasAction(a) {
			t.Errorf("HasAction(%q) = false, want true", a)
		}
	}
	for _, a := range []string{"nope:write", "nope:*", "global_roles:read", "tasks:nope", ""} {
		if r.HasAction(a) {
			t.Errorf("HasAction(%q) = true, want false", a)
		}
	}
}

func TestRegistryRegisterAndActions(t *testing.T) {
	r := NewRegistry()
	if r.HasAction("plugin.jev:sync") || r.HasAction("plugin.jev:*") {
		t.Fatal("plugin action present before Register")
	}
	if err := r.Register("plugin.jev:sync"); err != nil {
		t.Fatal(err)
	}
	if !r.HasAction("plugin.jev:sync") || !r.HasAction("plugin.jev:*") {
		t.Fatal("registered plugin action/domain wildcard missing")
	}
	acts := r.Actions()
	if !sort.StringsAreSorted(acts) {
		t.Error("Actions() not sorted")
	}
	seen := map[string]bool{}
	for _, a := range acts {
		if seen[a] {
			t.Errorf("duplicate action %q", a)
		}
		seen[a] = true
	}
	if !seen["plugin.jev:sync"] || !seen["tasks:read"] {
		t.Error("Actions() missing expected entries")
	}
	acts[0] = "mutated"
	if r.Actions()[0] == "mutated" {
		t.Error("Actions() must return a copy")
	}
}

func TestRegisterRejectsMalformed(t *testing.T) {
	r := NewRegistry()
	for _, a := range []string{"tasks:wri*", "*:read", "*", "tasks:*", "tasks", ":read", "tasks:", "a b:c", "a:b:c", "", "a\t:b"} {
		if err := r.Register(a); err == nil {
			t.Errorf("Register(%q) = nil, want error", a)
		}
	}
	for _, a := range []string{"*:read", "tasks:wri*", "*:*"} {
		if r.HasAction(a) {
			t.Errorf("HasAction(%q) = true after rejected Register", a)
		}
	}
	if err := r.Register("plugin.jev:sync"); err != nil {
		t.Fatal(err)
	}
	if !r.HasAction("plugin.jev:*") || r.HasAction("*:read") || r.HasAction("tasks:wri*") || r.HasAction("*:*") {
		t.Error("unexpected HasAction results after valid Register")
	}
}

func TestRegistryConcurrent(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := fmt.Sprintf("plugin.p%d:act", i)
			for j := 0; j < 50; j++ {
				_ = r.Register(a)
				_ = r.HasAction(a)
				_ = r.HasAction("tasks:*")
				_ = r.Actions()
			}
		}(i)
	}
	wg.Wait()
	if !r.HasAction("plugin.p31:act") {
		t.Error("missing concurrently registered action")
	}
}

func TestRegistryPluginActions(t *testing.T) {
	r := NewRegistry()
	if err := r.SetPluginActions("p1", []string{"time_logging:manage_all", "time_logging:log"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"time_logging:manage_all", "time_logging:log", "time_logging:*"} {
		if !r.HasAction(a) {
			t.Errorf("%s should be known", a)
		}
	}
	// Replacing the set drops what the plugin no longer declares.
	if err := r.SetPluginActions("p1", []string{"time_logging:log"}); err != nil {
		t.Fatal(err)
	}
	if r.HasAction("time_logging:manage_all") || !r.HasAction("time_logging:log") {
		t.Error("the plugin's set must be replaced as a whole")
	}
	// Invalid, built-in and foreign actions are refused and change nothing.
	for name, as := range map[string][]string{
		"wildcard": {"x:*"}, "no verb": {"x"}, "built-in": {"tasks:read"},
	} {
		if err := r.SetPluginActions("p1", as); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if !r.HasAction("time_logging:log") {
		t.Error("a refused update must leave the previous set in place")
	}
	if err := r.SetPluginActions("p2", []string{"time_logging:log"}); err == nil {
		t.Error("another plugin must not claim the same action")
	}
	// Removing the plugin removes its domain too; built-ins are untouched.
	r.RemovePluginActions("p1")
	if r.HasAction("time_logging:log") || r.HasAction("time_logging:*") {
		t.Error("removed plugin actions must be gone")
	}
	if !r.HasAction("tasks:read") || !r.HasAction("tasks:*") {
		t.Error("built-ins must survive")
	}
}
