package apierr

import "testing"

func TestNew(t *testing.T) {
	err := New(CodeBadRequest, "bad input")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Code != CodeBadRequest {
		t.Fatalf("expected code %q, got %q", CodeBadRequest, err.Code)
	}
	if err.Message != "bad input" {
		t.Fatalf("expected message %q, got %q", "bad input", err.Message)
	}
}

func TestErrorImplementsError(t *testing.T) {
	var err error = New(CodeInternalError, "boom")
	if err.Error() != "boom" {
		t.Fatalf("expected Error() to return message, got %q", err.Error())
	}
}

func TestNewWithDetails(t *testing.T) {
	err := NewWithDetails(CodePluginIncompatibleHostVersion, "plugin requires a newer host", map[string]string{
		"plugin_id":        "com.paca.example",
		"required_version": "v0.11.2",
		"host_version":     "v0.10.0",
	})
	if err.Code != CodePluginIncompatibleHostVersion {
		t.Fatalf("expected code %q, got %q", CodePluginIncompatibleHostVersion, err.Code)
	}
	if err.Details["required_version"] != "v0.11.2" {
		t.Fatalf("expected required_version detail %q, got %q", "v0.11.2", err.Details["required_version"])
	}
}

func TestNew_DetailsNilByDefault(t *testing.T) {
	err := New(CodeBadRequest, "bad input")
	if err.Details != nil {
		t.Fatalf("expected nil Details from New, got %v", err.Details)
	}
}
