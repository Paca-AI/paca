package iam

import "testing"

func varPolicy(t *testing.T, json string) *Policy {
	t.Helper()
	p, err := ParsePolicy([]byte(json))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrincipalVariable(t *testing.T) {
	p := varPolicy(t, `{"version":"2026-10-01","statements":[
	 {"effect":"Allow","actions":["tasks:read"],"resources":["project/p/task/*"],
	  "conditions":{"StringEquals":{"task.assignee_id":["${principal.id}"]}}}]}`)
	g := []Grant{{RoleID: "r", Policy: p}}
	req := func(self string, assignees ...string) Request {
		a := map[string][]string{"task.assignee_id": assignees}
		if self != "" {
			a["principal.id"] = []string{self}
		}
		return Request{Action: "tasks:read", Resource: "project/p/task/t", Attrs: a}
	}
	cases := []struct {
		name string
		r    Request
		want bool
	}{
		{"member", req("u1", "u1", "u2"), true},
		{"non member", req("u3", "u1", "u2"), false},
		{"no members", req("u1"), false},
		{"missing principal", req("", "u1"), false},
	}
	for _, c := range cases {
		if got := Evaluate(g, c.r).Allowed; got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestValidateVariables(t *testing.T) {
	schema := NewAttributeSchema()
	reg := NewRegistry()
	mk := func(op, operand string) []Issue {
		p := varPolicy(t, `{"version":"2026-10-01","statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/p/task/*"],"conditions":{"`+op+`":{"task.assignee_id":["`+operand+`"]}}}]}`)
		return Validate(p, reg, schema)
	}
	if got := mk("StringEquals", "${principal.id}"); len(got) != 0 {
		t.Errorf("valid variable rejected: %v", got)
	}
	for _, c := range [][2]string{
		{"StringEquals", "${task.status_id}"},
		{"StringEquals", "${principal.nope}"},
		{"StringLike", "${principal.id}"},
		{"StringEquals", "x${principal.id}"},
	} {
		if len(mk(c[0], c[1])) == 0 {
			t.Errorf("%v should be rejected", c)
		}
	}
}

func TestRootScopeMatchesEvaluate(t *testing.T) {
	// The direct-message policy: the collection unconditionally, each channel
	// only when the caller is a member.
	p := varPolicy(t, `{"version":"2026-10-01","statements":[
	 {"effect":"Allow","actions":["channels:read"],"resources":["channel"]},
	 {"effect":"Allow","actions":["channels:read"],"resources":["channel/*"],
	  "conditions":{"StringEquals":{"channel.kind":["dm"],"channel.member_ids":["${principal.id}"]}}}]}`)
	g := []Grant{{RoleID: "r", Policy: p}}
	me := Principal{Type: PrincipalUser, ID: "u1"}
	schema := NewAttributeSchema()

	// The collection check passes without attributes.
	if !Evaluate(g, Request{Action: "channels:read", Resource: "channel"}).Allowed {
		t.Error("collection check must pass")
	}
	node := CompileRootScope(g, me, "channels:read", "channel", schema)
	for _, c := range []struct {
		name  string
		attrs map[string][]string
		want  bool
	}{
		{"member dm", map[string][]string{"channel.kind": {"dm"}, "channel.member_ids": {"u1", "u2"}}, true},
		{"someone else's dm", map[string][]string{"channel.kind": {"dm"}, "channel.member_ids": {"u2", "u3"}}, false},
		{"member of a non-dm", map[string][]string{"channel.kind": {"private"}, "channel.member_ids": {"u1"}}, false},
	} {
		attrs := map[string][]string{"principal.id": {"u1"}}
		for k, v := range c.attrs {
			attrs[k] = v
		}
		// The collection statement allows "channel" only, not a channel id, so a
		// specific channel is decided by the conditional statement alone.
		got := Evaluate(g, Request{Action: "channels:read", Resource: "channel/c1", Attrs: attrs}).Allowed
		if !got && c.want || got && !c.want {
			// The unconditional "channel" statement also matches "channel/c1"
			// through its trailing-star rule? It must not.
			t.Errorf("%s: Evaluate=%v want %v", c.name, got, c.want)
		}
		scoped := node.Eval(attrs)
		if scoped != got {
			t.Errorf("%s: scope=%v but Evaluate=%v", c.name, scoped, got)
		}
	}
}
