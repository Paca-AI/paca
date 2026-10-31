package totp

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B SHA-1 vectors (secret "12345678901234567890"), last six
// digits of the published 8-digit codes.
func TestCodeRFC6238Vectors(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, c := range cases {
		got, err := Code(secret, time.Unix(c.unix, 0))
		if err != nil || got != c.want {
			t.Errorf("Code(t=%d) = %q, %v; want %q", c.unix, got, err, c.want)
		}
	}
}

func TestVerifySkewAndReplayStep(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	code, _ := Code(secret, now)

	step, ok := Verify(secret, code, now)
	if !ok || step != stepAt(now) {
		t.Fatalf("Verify current = %d, %v", step, ok)
	}
	if _, ok := Verify(secret, code, now.Add(Period)); !ok {
		t.Error("code from previous step should be accepted within skew")
	}
	if _, ok := Verify(secret, code, now.Add(3*Period)); ok {
		t.Error("code from three steps ago must be rejected")
	}
	if _, ok := Verify(secret, "12345", now); ok {
		t.Error("short code must be rejected")
	}
	if _, ok := Verify(secret, " "+code[:3]+" "+code[3:], now); !ok {
		t.Error("spaces in the code should be tolerated")
	}
}

func TestProvisioningURI(t *testing.T) {
	u := ProvisioningURI("Paca", "alice", "ABC")
	for _, want := range []string{"otpauth://totp/Paca:alice?", "secret=ABC", "issuer=Paca"} {
		if !strings.Contains(u, want) {
			t.Errorf("uri %q missing %q", u, want)
		}
	}
}
