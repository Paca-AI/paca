package iam

import (
	"strings"
	"sync"
	"testing"
)

func TestNewAttributeSchemaBuiltins(t *testing.T) {
	s := NewAttributeSchema()
	want := map[string]struct {
		kind  string
		multi bool
	}{
		"principal.id":                {"", false},
		"principal.type":              {"", false},
		"resource.id":                 {"", false},
		"task.sprint_id":              {"task", false},
		"task.status_id":              {"task", false},
		"task.type_id":                {"task", false},
		"task.assignee_id":            {"task", true},
		"view.sprint_id":              {"view", false},
		"doc.folder_id":               {"doc", false},
		"doc.ancestor_folder_ids":     {"doc", true},
		"agent.environment_id":        {"agent", false},
		"environment.type":            {"environment", false},
		"conversation.environment_id": {"conversation", false},
	}
	if got := len(s.Defs()); got != len(want) {
		t.Errorf("Defs len=%d want %d", got, len(want))
	}
	for key, w := range want {
		d, ok := s.Lookup(key)
		if !ok {
			t.Errorf("missing %s", key)
			continue
		}
		if d.Key != key || d.ResourceKind != w.kind || d.MultiValued != w.multi || d.Type != TypeString {
			t.Errorf("%s: %+v", key, d)
		}
		if d.LabelKey != "roles.attributes."+key {
			t.Errorf("%s label %q", key, d.LabelKey)
		}
	}
	if _, ok := s.Lookup("nope.x"); ok {
		t.Error("unexpected key")
	}
}

func TestAttributeSchemaDefsSorted(t *testing.T) {
	defs := NewAttributeSchema().Defs()
	for i := 1; i < len(defs); i++ {
		if defs[i-1].Key >= defs[i].Key {
			t.Fatalf("not sorted: %s >= %s", defs[i-1].Key, defs[i].Key)
		}
	}
}

func TestAttributeSchemaRegister(t *testing.T) {
	s := NewAttributeSchema()
	ok := AttributeDef{Key: "milestone.due", ResourceKind: "milestone", Type: TypeString, LabelKey: "roles.attributes.milestone.due"}
	if err := s.Register(ok); err != nil {
		t.Fatal(err)
	}
	if d, found := s.Lookup("milestone.due"); !found || d != ok {
		t.Fatalf("lookup: %+v %v", d, found)
	}
	bad := map[string]AttributeDef{
		"duplicate":         ok,
		"duplicate builtin": {Key: "task.sprint_id", ResourceKind: "task", Type: TypeString, LabelKey: "x"},
		"no dot":            {Key: "nodot", Type: TypeString, LabelKey: "x"},
		"empty":             {Key: "", Type: TypeString, LabelKey: "x"},
		"empty kind part":   {Key: ".name", Type: TypeString, LabelKey: "x"},
		"empty name part":   {Key: "kind.", Type: TypeString, LabelKey: "x"},
		"two dots":          {Key: "a.b.c", Type: TypeString, LabelKey: "x"},
		"slash":             {Key: "a/b.c", Type: TypeString, LabelKey: "x"},
		"space":             {Key: "a b.c", Type: TypeString, LabelKey: "x"},
		"missing label":     {Key: "x.y", Type: TypeString},
		"bad type":          {Key: "x.y", Type: "int", LabelKey: "x"},
	}
	for name, d := range bad {
		if err := s.Register(d); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, found := s.Lookup("x.y"); found {
		t.Error("rejected def was stored")
	}
}

func TestAttributeSchemaConcurrent(t *testing.T) {
	s := NewAttributeSchema()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Register(AttributeDef{Key: "k" + strings.Repeat("a", i) + ".v", Type: TypeBool, LabelKey: "l"})
			s.Lookup("task.sprint_id")
			s.Defs()
		}(i)
	}
	wg.Wait()
}

func TestResourceKindOf(t *testing.T) {
	cases := map[string]string{
		"project/p/task/t":       "task",
		"project/p/task/t/extra": "task",
		"project/p/task/*":       "task",
		"project/p/task":         "task",
		"project/*/agent/*":      "agent",
		"project/*/agent/a1":     "agent",
		"project/p":              "project",
		"project":                "project",
		"project/p1/*":           "", // trailing * covers many kinds
		"project/*":              "", // covers project and every child kind
		"project/p/*/x":          "", // single-segment wildcard in kind position
		"project/*/*":            "",
		"user/x":                 "user",
		"user/*":                 "user",
		"role/x":                 "role",
		"plugin/x":               "plugin",
		"agent/x":                "agent",
		"settings":               "settings",
		"sso":                    "sso",
		"*":                      "",
		"":                       "",
	}
	for in, want := range cases {
		if got := ResourceKindOf(in); got != want {
			t.Errorf("ResourceKindOf(%q)=%q want %q", in, got, want)
		}
	}
}
