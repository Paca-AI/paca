package iam

import "testing"

func TestParsePolicy(t *testing.T) {
	raw := []byte(`{"version":"2026-10-01","statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/*"],
	  "conditions":{"StringEquals":{"resource.type":"production"},"Bool":{"principal.is_agent":true},"In":{"principal.id":["a","b"]}}}]}`)
	p, err := ParsePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Statements[0].Conditions
	if got := c["StringEquals"]["resource.type"]; len(got) != 1 || got[0] != "production" {
		t.Fatalf("string value: %v", got)
	}
	if got := c["Bool"]["principal.is_agent"]; got[0] != "true" {
		t.Fatalf("bool value: %v", got)
	}
	if got := c["In"]["principal.id"]; len(got) != 2 {
		t.Fatalf("list value: %v", got)
	}
}

func TestParsePolicyRejectsUnknownFieldsAndBadEffect(t *testing.T) {
	for _, raw := range []string{
		`{"version":"1","statements":[{"effect":"Allow","actions":["*"],"resources":["*"],"bogus":1}]}`,
		`{"version":"1","statements":[{"effect":"Maybe","actions":["*"],"resources":["*"]}]}`,
		`not json`,
	} {
		if _, err := ParsePolicy([]byte(raw)); err == nil {
			t.Errorf("expected error for %s", raw)
		}
	}
}

func TestValueListRejectsNull(t *testing.T) {
	cases := []string{
		// Condition with null value
		`{"version":"1","statements":[{"effect":"Allow","actions":["*"],"resources":["*"],"conditions":{"In":{"k":null}}}]}`,
		// List with null element
		`{"version":"1","statements":[{"effect":"Allow","actions":["*"],"resources":["*"],"conditions":{"In":{"k":["a",null]}}}]}`,
		// Non-string/bool/list value (integer)
		`{"version":"1","statements":[{"effect":"Allow","actions":["*"],"resources":["*"],"conditions":{"In":{"k":5}}}]}`,
	}
	for _, raw := range cases {
		if _, err := ParsePolicy([]byte(raw)); err == nil {
			t.Errorf("expected error for %s", raw)
		}
	}
}

func TestParsePolicyRejectsTrailingData(t *testing.T) {
	cases := []string{
		// Trailing garbage
		`{"version":"1","statements":[]} garbage`,
		// Concatenated documents
		`{"version":"1","statements":[]}{"version":"1","statements":[]}`,
		// Top-level null
		`null`,
		// Stray closing brace
		`{"version":"1","statements":[]}}`,
		// Stray closing bracket
		`{"version":"1","statements":[]}]`,
	}
	for _, raw := range cases {
		if _, err := ParsePolicy([]byte(raw)); err == nil {
			t.Errorf("expected error for %s", raw)
		}
	}
}

func TestParsePolicyAcceptsTrailingWhitespace(t *testing.T) {
	// Valid policy with trailing whitespace and newlines should parse successfully
	cases := []string{
		`{"version":"1","statements":[]}  `,
		`{"version":"1","statements":[]}
`,
		`{"version":"1","statements":[]}
  `,
	}
	for _, raw := range cases {
		if _, err := ParsePolicy([]byte(raw)); err != nil {
			t.Errorf("expected success for %q, got error: %v", raw, err)
		}
	}
}
