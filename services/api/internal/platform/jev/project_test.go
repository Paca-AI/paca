package jev

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Paca-AI/api/internal/platform/secret"
)

func newTestEncryptor(t *testing.T) *secret.Encryptor {
	t.Helper()
	enc, err := secret.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("new encryptor: %v", err)
	}
	return enc
}

func TestClientForProject_EmptySecretIsDisabled(t *testing.T) {
	if c := ClientForProject("", "https://example.test", "m", nil); c.Enabled() {
		t.Fatal("expected a disabled client for an unconfigured project")
	}
}

func TestClientForProject_PlaintextWithoutEncryptor(t *testing.T) {
	c := ClientForProject("plain-key", "https://jev.example/v1", "custom-model", nil)
	if !c.Enabled() {
		t.Fatal("expected an enabled client")
	}
	if c.apiKey != "plain-key" || c.baseURL != "https://jev.example/v1" || c.model != "custom-model" {
		t.Errorf("unexpected client fields: key=%q base=%q model=%q", c.apiKey, c.baseURL, c.model)
	}
}

func TestClientForProject_DefaultsBaseURLAndModel(t *testing.T) {
	c := ClientForProject("k", "", "", nil)
	if c.baseURL != defaultBaseURL || c.model != DefaultModel {
		t.Errorf("expected defaults, got base=%q model=%q", c.baseURL, c.model)
	}
}

func TestClientForProject_DecryptsWithEncryptor(t *testing.T) {
	enc := newTestEncryptor(t)
	stored, err := enc.Encrypt("secret-key")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	c := ClientForProject(stored, "", "", enc)
	if !c.Enabled() {
		t.Fatal("expected an enabled client")
	}
	if c.apiKey != "secret-key" {
		t.Errorf("expected the decrypted key, got %q", c.apiKey)
	}
}

func TestClientForProject_UndecryptableSecretIsDisabled(t *testing.T) {
	enc := newTestEncryptor(t)
	if c := ClientForProject("not-a-valid-ciphertext", "", "", enc); c.Enabled() {
		t.Fatal("expected a disabled client when the stored key can't be decrypted")
	}
}

func TestAPIError_Error(t *testing.T) {
	err := &APIError{StatusCode: 401, Body: `{"error":"bad key"}`}
	got := err.Error()
	if !strings.Contains(got, "401") || !strings.Contains(got, "bad key") {
		t.Errorf("unexpected error string: %q", got)
	}
}

func TestTruncateBody(t *testing.T) {
	short := []byte("short")
	if got := truncateBody(short); got != "short" {
		t.Errorf("short body should be unchanged, got %q", got)
	}
	long := bytes.Repeat([]byte("x"), maxErrorBodyBytes+100)
	got := truncateBody(long)
	if !strings.HasSuffix(got, "... (truncated)") || len(got) != maxErrorBodyBytes+len("... (truncated)") {
		t.Errorf("unexpected truncated body (len %d)", len(got))
	}
}
