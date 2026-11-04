package agentsvc

import (
	"errors"
	"strings"
	"testing"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

func TestNormalizeACPModelEffort(t *testing.T) {
	m, e, err := normalizeACPModelEffort("  claude-sonnet-5-5 ", " high ")
	if err != nil || m != "claude-sonnet-5-5" || e != "high" {
		t.Fatalf("got %q %q %v", m, e, err)
	}
	for _, bad := range []string{"a\x00b", "a\nb", strings.Repeat("x", maxACPModelEffortLen+1)} {
		if _, _, err := normalizeACPModelEffort(bad, ""); !errors.Is(err, agentdom.ErrACPModelInvalid) {
			t.Errorf("model %q: err = %v", bad, err)
		}
		if _, _, err := normalizeACPModelEffort("", bad); !errors.Is(err, agentdom.ErrACPModelInvalid) {
			t.Errorf("effort %q: err = %v", bad, err)
		}
	}
}
