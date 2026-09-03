package environmentsvc

import (
	"testing"

	"github.com/stretchr/testify/assert"

	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
)

func TestValidateCPULimit(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{name: "default", raw: "2", wantErr: nil},
		{name: "exact minimum", raw: "100m", wantErr: nil},
		{name: "fractional core", raw: "0.5", wantErr: nil},
		{name: "below minimum", raw: "1m", wantErr: environmentdom.ErrEnvironmentCPULimitInvalid},
		{name: "unparseable", raw: "not-a-number", wantErr: environmentdom.ErrEnvironmentCPULimitInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCPULimit(tc.raw)
			if tc.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestValidateMemoryLimit(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{name: "default", raw: "4Gi", wantErr: nil},
		{name: "exact minimum", raw: "256Mi", wantErr: nil},
		// The exact mistake that reached Docker's daemon as a raw error
		// before this validation existed — "m" is milli, not mega.
		{name: "milli suffix mistaken for mega", raw: "500m", wantErr: environmentdom.ErrEnvironmentMemoryLimitInvalid},
		{name: "below minimum", raw: "10Mi", wantErr: environmentdom.ErrEnvironmentMemoryLimitInvalid},
		{name: "unparseable", raw: "not-a-number", wantErr: environmentdom.ErrEnvironmentMemoryLimitInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMemoryLimit(tc.raw)
			if tc.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}
