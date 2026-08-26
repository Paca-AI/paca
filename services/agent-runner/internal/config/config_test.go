package config

import "testing"

func TestValidatePortRange(t *testing.T) {
	cases := []struct {
		name       string
		start, end int
		wantErr    bool
	}{
		{"both zero disables the feature", 0, 0, false},
		{"valid range", 2200, 2299, false},
		{"start equals end", 2200, 2200, false},
		{"only start set", 2200, 0, true},
		{"only end set", 0, 2299, true},
		{"end before start", 2299, 2200, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePortRange("SSH_BASTION_PORT_RANGE", tc.start, tc.end)
			if tc.wantErr && err == nil {
				t.Errorf("validatePortRange(%d, %d) = nil, want error", tc.start, tc.end)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validatePortRange(%d, %d) = %v, want nil", tc.start, tc.end, err)
			}
		})
	}
}
