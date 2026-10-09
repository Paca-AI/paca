package iam

import "testing"

func TestMatchesUnderWildcard(t *testing.T) {
	cases := []struct {
		name           string
		policyResource string
		queryResource  string
		want           bool
	}{
		{"project role specific ID", "project/P/role/EDITOR", "project/P/role/*", true},
		{"project role different project", "project/Q/role/EDITOR", "project/P/role/*", false},
		{"platform role specific ID", "role/VIEWER", "role/*", true},
		{"project agent specific ID", "project/P/agent/A", "project/P/agent/*", true},
		{"policy has wildcard", "project/P/role/*", "project/P/role/*", false},
		{"query not wildcard", "project/P/role/EDITOR", "project/P/role/EDITOR", false},
		{"extra path segments", "project/P/role/R/sub", "project/P/role/*", false},
		{"empty remainder", "project/P/role/", "project/P/role/*", false},
		{"wrong prefix", "project/P/task/T", "project/P/role/*", false},
		{"platform plugin", "plugin/slack", "plugin/*", true},
	}
	for _, c := range cases {
		got := MatchesUnderWildcard(c.policyResource, c.queryResource)
		if got != c.want {
			t.Errorf("%s: MatchesUnderWildcard(%q, %q) = %v, want %v", c.name, c.policyResource, c.queryResource, got, c.want)
		}
	}
}
