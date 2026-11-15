package handler

import "testing"

func TestIfNoneMatchHit(t *testing.T) {
	cases := []struct {
		header, etag string
		want         bool
	}{
		{`"a"`, `"a"`, true},
		{`W/"a"`, `"a"`, true},
		{`"a"`, `W/"a"`, true},
		{`"b", "a"`, `"a"`, true},
		{`*`, `"a"`, true},
		{`"b"`, `"a"`, false},
		{``, `"a"`, false},
	}
	for _, c := range cases {
		if got := ifNoneMatchHit(c.header, c.etag); got != c.want {
			t.Errorf("ifNoneMatchHit(%q, %q) = %v, want %v", c.header, c.etag, got, c.want)
		}
	}
}
