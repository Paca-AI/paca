package iam

import (
	"reflect"
	"testing"
)

func pol(sts ...Statement) *Policy { return &Policy{Version: "1", Statements: sts} }

func allow(actions, resources string, cond Conditions) Statement {
	return Statement{Effect: EffectAllow, Actions: []string{actions}, Resources: []string{resources}, Conditions: cond}
}

func deny(actions, resources string, cond Conditions) Statement {
	return Statement{Effect: EffectDeny, Actions: []string{actions}, Resources: []string{resources}, Conditions: cond}
}

func TestEvaluate(t *testing.T) {
	allowAll := Grant{RoleID: "r-allow", Policy: pol(allow("*", "*", nil))}
	denyAll := Grant{RoleID: "r-deny", Policy: pol(deny("*", "*", nil))}
	scopedAllow := []Grant{{ProjectID: "p1", Policy: pol(allow("*", "*", nil))}}
	denyProd := Grant{Policy: pol(deny("*", "*", Conditions{"StringEquals": {"resource.type": {"production"}}}))}
	denyNotU1 := Grant{Policy: pol(deny("*", "*", Conditions{"NotIn": {"principal.id": {"u1"}}}))}
	denyNotProd := Grant{Policy: pol(deny("*", "*", Conditions{"StringNotEquals": {"resource.type": {"production"}}}))}
	onlyAllow := func(op, key string, vals ...string) []Grant {
		return []Grant{{Policy: pol(allow("*", "*", Conditions{op: {key: vals}}))}}
	}
	attrs := func(kv ...string) map[string][]string {
		m := map[string][]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = []string{kv[i+1]}
		}
		return m
	}
	readTask := []Grant{{RoleID: "r", Policy: pol(allow("tasks:read", "project/*", nil))}}

	cases := []struct {
		name   string
		grants []Grant
		req    Request
		want   bool
	}{
		{"no grants denied", nil, Request{Action: "tasks:read", Resource: "project/p1/task/t1"}, false},
		{"allow matches", readTask, Request{Action: "tasks:read", Resource: "project/p1/task/t1"}, true},
		{"allow wrong action", readTask, Request{Action: "tasks:write", Resource: "project/p1/task/t1"}, false},
		{"allow wrong resource", readTask, Request{Action: "tasks:read", Resource: "user/u1"}, false},
		{"deny beats allow (allow first)", []Grant{allowAll, denyAll}, Request{Action: "a:b", Resource: "x"}, false},
		{"deny beats allow (deny first)", []Grant{denyAll, allowAll}, Request{Action: "a:b", Resource: "x"}, false},
		{"deny and allow in same policy", []Grant{{Policy: pol(deny("tasks:write", "*", nil), allow("*", "*", nil))}},
			Request{Action: "tasks:write", Resource: "project/p1"}, false},

		// project scope
		{"scoped allows inside project", scopedAllow, Request{Action: "agents:run", Resource: "project/p1/agent/a1"}, true},
		{"scoped allows project itself", scopedAllow, Request{Action: "agents:run", Resource: "project/p1"}, true},
		{"scoped denies other project", scopedAllow, Request{Action: "agents:run", Resource: "project/p2/agent/a1"}, false},
		{"scoped denies non-project resource", scopedAllow, Request{Action: "users:read", Resource: "user/u1"}, false},
		{"scoped prefix trap p1 vs p10", scopedAllow, Request{Action: "x:y", Resource: "project/p10"}, false},
		{"scoped prefix trap p1 vs p10 child", scopedAllow, Request{Action: "x:y", Resource: "project/p10/agent/a1"}, false},
		{"scoped deny does not leak to other project", []Grant{allowAll, {ProjectID: "p1", Policy: pol(deny("*", "*", nil))}},
			Request{Action: "x:y", Resource: "project/p2/task/t"}, true},
		{"scoped deny applies inside project", []Grant{allowAll, {ProjectID: "p1", Policy: pol(deny("*", "*", nil))}},
			Request{Action: "x:y", Resource: "project/p1/task/t"}, false},

		// conditions
		{"StringEquals deny fires for production", []Grant{allowAll, denyProd},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("resource.type", "production")}, false},
		{"StringEquals deny not for staging", []Grant{allowAll, denyProd},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("resource.type", "staging")}, true},
		{"StringEquals deny not when attr missing", []Grant{allowAll, denyProd},
			Request{Action: "a:b", Resource: "r"}, true},
		{"NotIn deny fires for u2", []Grant{allowAll, denyNotU1},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("principal.id", "u2")}, false},
		{"NotIn deny not for u1", []Grant{allowAll, denyNotU1},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("principal.id", "u1")}, true},
		{"missing key StringNotEquals true (deny fires)", []Grant{allowAll, denyNotProd},
			Request{Action: "a:b", Resource: "r"}, false},
		{"missing key NotIn true (deny fires)", []Grant{allowAll, denyNotU1},
			Request{Action: "a:b", Resource: "r"}, false},
		{"StringNotEquals present equal false", []Grant{allowAll, denyNotProd},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("resource.type", "production")}, true},
		{"missing key StringEquals false (allow skipped)", onlyAllow("StringEquals", "resource.type", "production"),
			Request{Action: "a:b", Resource: "r"}, false},
		{"StringEquals present match", onlyAllow("StringEquals", "resource.type", "production"),
			Request{Action: "a:b", Resource: "r", Attrs: attrs("resource.type", "production")}, true},
		{"missing key In false", onlyAllow("In", "principal.id", "u1"),
			Request{Action: "a:b", Resource: "r"}, false},
		{"In multiple values", onlyAllow("In", "principal.id", "u1", "u2"),
			Request{Action: "a:b", Resource: "r", Attrs: attrs("principal.id", "u2")}, true},
		{"Bool true matches", onlyAllow("Bool", "principal.is_owner", "true"),
			Request{Action: "a:b", Resource: "r", Attrs: attrs("principal.is_owner", "true")}, true},
		{"Bool mismatch", onlyAllow("Bool", "principal.is_owner", "true"),
			Request{Action: "a:b", Resource: "r", Attrs: attrs("principal.is_owner", "false")}, false},
		{"Bool missing", onlyAllow("Bool", "principal.is_owner", "true"),
			Request{Action: "a:b", Resource: "r"}, false},
		{"Bool with two values false", onlyAllow("Bool", "k", "true", "false"),
			Request{Action: "a:b", Resource: "r", Attrs: attrs("k", "true")}, false},
		{"StringLike match", onlyAllow("StringLike", "resource.id", "a*"),
			Request{Action: "a:b", Resource: "r", Attrs: attrs("resource.id", "abc")}, true},
		{"StringLike no match", onlyAllow("StringLike", "resource.id", "a*"),
			Request{Action: "a:b", Resource: "r", Attrs: attrs("resource.id", "bac")}, false},
		{"StringLike missing", onlyAllow("StringLike", "resource.id", "a*"),
			Request{Action: "a:b", Resource: "r"}, false},
		{"multiple keys all must hold", []Grant{{Policy: pol(allow("*", "*", Conditions{
			"StringEquals": {"a": {"1"}, "b": {"2"}}}))}},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("a", "1", "b", "3")}, false},
		{"multiple keys all hold", []Grant{{Policy: pol(allow("*", "*", Conditions{
			"StringEquals": {"a": {"1"}, "b": {"2"}}}))}},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("a", "1", "b", "2")}, true},

		// unknown operator fails closed
		{"unknown operator allow skipped", onlyAllow("Bogus", "k", "v"),
			Request{Action: "a:b", Resource: "r", Attrs: attrs("k", "v")}, false},
		{"unknown operator deny applies", []Grant{allowAll, {Policy: pol(deny("*", "*", Conditions{"Bogus": {"k": {"v"}}}))}},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("k", "other")}, false},

		{"deny unknown operator plus failing known condition still denies",
			[]Grant{allowAll, {Policy: pol(deny("*", "*", Conditions{
				"Bogus":        {"k": {"v"}},
				"StringEquals": {"resource.type": {"production"}}}))}},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("resource.type", "staging")}, false},
		{"allow unknown operator plus passing known condition skipped",
			[]Grant{{Policy: pol(allow("*", "*", Conditions{
				"Bogus":        {"k": {"v"}},
				"StringEquals": {"resource.type": {"production"}}}))}},
			Request{Action: "a:b", Resource: "r", Attrs: attrs("resource.type", "production")}, false},
		{"allow with empty-map unknown operator not granted",
			[]Grant{{Policy: pol(allow("*", "*", Conditions{"Bogus": {}}))}},
			Request{Action: "a:b", Resource: "r"}, false},
		{"deny with empty-map unknown operator denies",
			[]Grant{allowAll, {Policy: pol(deny("*", "*", Conditions{"Bogus": {}}))}},
			Request{Action: "a:b", Resource: "r"}, false},
		{"known operator with empty key map holds",
			[]Grant{{Policy: pol(allow("*", "*", Conditions{"StringEquals": {}}))}},
			Request{Action: "a:b", Resource: "r"}, true},

		// degenerate policies
		{"nil policy grants nothing", []Grant{{RoleID: "r"}}, Request{Action: "a:b", Resource: "r"}, false},
		{"empty statements grant nothing", []Grant{{Policy: pol()}}, Request{Action: "a:b", Resource: "r"}, false},
		{"empty actions never match", []Grant{{Policy: pol(Statement{Effect: EffectAllow, Resources: []string{"*"}})}},
			Request{Action: "a:b", Resource: "r"}, false},
		{"empty resources never match", []Grant{{Policy: pol(Statement{Effect: EffectAllow, Actions: []string{"*"}})}},
			Request{Action: "a:b", Resource: "r"}, false},
		{"empty actions deny never matches", []Grant{allowAll, {Policy: pol(Statement{Effect: EffectDeny, Resources: []string{"*"}})}},
			Request{Action: "a:b", Resource: "r"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Evaluate(c.grants, c.req).Allowed; got != c.want {
				t.Errorf("Allowed=%v want %v", got, c.want)
			}
		})
	}
}

func TestEvaluateMatched(t *testing.T) {
	grants := []Grant{
		{RoleID: "ra", Policy: pol(
			Statement{Sid: "no-match", Effect: EffectAllow, Actions: []string{"other:*"}, Resources: []string{"*"}},
			Statement{Sid: "allow-all", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
		)},
		{RoleID: "rd", Policy: pol(
			Statement{Sid: "deny-x", Effect: EffectDeny, Actions: []string{"tasks:*"}, Resources: []string{"project/*"}},
		)},
	}
	res := Evaluate(grants, Request{Action: "tasks:write", Resource: "project/p1"})
	if res.Allowed {
		t.Fatal("expected denied")
	}
	want := []MatchedStatement{
		{RoleID: "ra", Sid: "allow-all", Effect: EffectAllow, Index: 1},
		{RoleID: "rd", Sid: "deny-x", Effect: EffectDeny, Index: 0},
	}
	if !reflect.DeepEqual(res.Matched, want) {
		t.Errorf("Matched=%+v want %+v", res.Matched, want)
	}

	// Unknown-operator Allow is not listed; unknown-operator Deny is.
	res = Evaluate([]Grant{{RoleID: "r", Policy: pol(
		allow("*", "*", Conditions{"Bogus": {"k": {"v"}}}),
		deny("*", "*", Conditions{"Bogus": {"k": {"v"}}}),
	)}}, Request{Action: "a:b", Resource: "r"})
	if len(res.Matched) != 1 || res.Matched[0].Effect != EffectDeny || res.Matched[0].Index != 1 {
		t.Errorf("Matched=%+v", res.Matched)
	}
}

func TestReferencedConditionKeys(t *testing.T) {
	grants := []Grant{
		{Policy: pol(allow("*", "*", Conditions{"StringEquals": {"resource.type": {"x"}}}))},
		{Policy: pol(deny("*", "*", Conditions{"NotIn": {"principal.id": {"u1"}}, "Bool": {"resource.type": {"true"}}}))},
		{Policy: nil},
		{Policy: pol(allow("*", "*", nil))},
	}
	got := ReferencedConditionKeys(grants)
	want := map[string]struct{}{"resource.type": {}, "principal.id": {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if n := len(ReferencedConditionKeys(nil)); n != 0 {
		t.Errorf("nil grants: %d keys", n)
	}
}

func TestEvaluateMultiValued(t *testing.T) {
	one := func(op, key string, vals ...string) []Grant {
		return []Grant{{Policy: pol(allow("*", "*", Conditions{op: {key: vals}}))}}
	}
	denyWith := func(op, key string, vals ...string) []Grant {
		return []Grant{
			{Policy: pol(allow("*", "*", nil))},
			{Policy: pol(deny("*", "*", Conditions{op: {key: vals}}))},
		}
	}
	const k = "doc.ancestor_folder_ids"
	anc := func(v ...string) map[string][]string { return map[string][]string{k: v} }

	cases := []struct {
		name   string
		grants []Grant
		attrs  map[string][]string
		want   bool
	}{
		{"In any value matches", one("In", k, "ABC"), anc("root", "ABC", "leaf"), true},
		{"In no value matches", one("In", k, "ABC"), anc("root", "leaf"), false},
		{"In list operand any-any", one("In", k, "X", "ABC"), anc("root", "ABC"), true},
		{"StringEquals any value", one("StringEquals", k, "ABC"), anc("root", "ABC"), true},
		{"StringEquals no value", one("StringEquals", k, "ABC"), anc("root"), false},
		{"StringLike any value", one("StringLike", k, "AB*"), anc("root", "ABC"), true},
		{"StringLike no value", one("StringLike", k, "AB*"), anc("root", "XYZ"), false},
		{"Bool any value", one("Bool", "k", "true"), map[string][]string{"k": {"false", "true"}}, true},
		{"Bool none", one("Bool", "k", "true"), map[string][]string{"k": {"false"}}, false},

		// negated: deny fires only if the negated condition holds
		{"NotIn deny: a value in list -> condition false -> allowed", denyWith("NotIn", k, "ABC"), anc("root", "ABC"), true},
		{"NotIn deny: no value in list -> condition true -> denied", denyWith("NotIn", k, "ABC"), anc("root", "leaf"), false},
		{"StringNotEquals deny: a value equal -> allowed", denyWith("StringNotEquals", k, "ABC"), anc("ABC", "x"), true},
		{"StringNotEquals deny: none equal -> denied", denyWith("StringNotEquals", k, "ABC"), anc("x", "y"), false},

		// absent vs empty
		{"In absent false", one("In", k, "ABC"), nil, false},
		{"In empty list false", one("In", k, "ABC"), anc(), false},
		{"NotIn absent true (deny fires)", denyWith("NotIn", k, "ABC"), nil, false},
		{"NotIn empty list true (deny fires)", denyWith("NotIn", k, "ABC"), anc(), false},
		{"StringNotEquals empty list true (deny fires)", denyWith("StringNotEquals", k, "ABC"), anc(), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Evaluate(c.grants, Request{Action: "a:b", Resource: "r", Attrs: c.attrs}).Allowed
			if got != c.want {
				t.Errorf("Allowed=%v want %v", got, c.want)
			}
		})
	}
}
