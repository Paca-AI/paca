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
