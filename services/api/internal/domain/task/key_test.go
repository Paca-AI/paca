package taskdom

import "testing"

func TestParseTaskKey(t *testing.T) {
	cases := []struct {
		raw        string
		wantPrefix string
		wantNum    int64
		wantOK     bool
	}{
		{"PRJ-1", "PRJ", 1, true},
		{"prj-42", "PRJ", 42, true},
		{"MP2-100", "MP2", 100, true},
		{" PRJ-5 ", "PRJ", 5, true},
		{"", "", 0, false},
		{"PRJ", "", 0, false},
		{"PRJ-0", "", 0, false},
		{"PRJ--1", "", 0, false},
		{"PRJ-abc", "", 0, false},
		{"TOOLONGPREFIX-1", "", 0, false},
		{"PRJ_1-1", "", 0, false},
	}
	for _, tc := range cases {
		prefix, num, ok := ParseTaskKey(tc.raw)
		if ok != tc.wantOK || prefix != tc.wantPrefix || num != tc.wantNum {
			t.Errorf("ParseTaskKey(%q) = (%q, %d, %v), want (%q, %d, %v)",
				tc.raw, prefix, num, ok, tc.wantPrefix, tc.wantNum, tc.wantOK)
		}
	}
}
