package roledom

import (
	"encoding/json"
	"testing"
)

func TestPolicyIsProjectTemplate(t *testing.T) {
	cases := []struct {
		name, policy string
		want         bool
	}{
		{"project star", `{"statements":[{"effect":"Allow","actions":["*"],"resources":["project/*"]}]}`, true},
		{"several project resources", `{"statements":[{"effect":"Allow","actions":["a:b"],"resources":["project/*","project/*/role/*"]},{"effect":"Allow","actions":["c:d"],"resources":["project/*/task/*"]}]}`, true},
		{"specific project", `{"statements":[{"effect":"Allow","actions":["*"],"resources":["project/3f1c2a/*","project/3f1c2a"]}]}`, false},
		{"platform roots", `{"statements":[{"effect":"Allow","actions":["users:read"],"resources":["user","user/*"]}]}`, false},
		{"everything", `{"statements":[{"effect":"Allow","actions":["*"],"resources":["*"]}]}`, false},
		{"mixed", `{"statements":[{"effect":"Allow","actions":["*"],"resources":["project/*","user/*"]}]}`, false},
		{"no statements", `{"statements":[]}`, false},
		{"statement without resources", `{"statements":[{"effect":"Allow","actions":["*"]}]}`, false},
		{"not json", `nope`, false},
	}
	for _, c := range cases {
		if got := PolicyIsProjectTemplate(json.RawMessage(c.policy)); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
