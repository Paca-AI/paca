package jwttoken_test

import (
	"strings"
	"testing"
	"time"

	jwttoken "github.com/paca/api/internal/platform/token"
)

func newTestManager() *jwttoken.Manager {
	return jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
}

func TestIssueAccess_ReturnsToken(t *testing.T) {
	m := newTestManager()
	tok, err := m.IssueAccess("sub123", "alice", "USER", "fam1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(tok, ".") {
		t.Fatalf("expected JWT format, got: %s", tok)
	}
}

func TestIssueRefresh_ReturnsToken(t *testing.T) {
	m := newTestManager()
	tok, err := m.IssueRefresh("sub123", "alice", "USER", "fam1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestVerify_AccessToken_ValidClaims(t *testing.T) {
	m := newTestManager()
	tok, _ := m.IssueAccess("sub123", "alice", "USER", "fam1")
	claims, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Subject != "sub123" {
		t.Errorf("expected subject sub123, got %s", claims.Subject)
	}
	if claims.Username != "alice" {
		t.Errorf("expected username alice, got %s", claims.Username)
	}
	if claims.Kind != "access" {
		t.Errorf("expected kind access, got %s", claims.Kind)
	}
	if claims.FamilyID != "fam1" {
		t.Errorf("expected familyID fam1, got %s", claims.FamilyID)
	}
}

func TestVerify_RefreshToken_ValidClaims(t *testing.T) {
	m := newTestManager()
	tok, _ := m.IssueRefresh("sub123", "alice", "USER", "fam1")
	claims, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Kind != "refresh" {
		t.Errorf("expected kind refresh, got %s", claims.Kind)
	}
}

func TestVerify_WrongSecret_ReturnsError(t *testing.T) {
	m1 := newTestManager()
	m2 := jwttoken.New("other-secret", 15*time.Minute, 7*24*time.Hour)
	tok, _ := m1.IssueAccess("sub", "alice", "USER", "fam1")
	if _, err := m2.Verify(tok); err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestVerify_ExpiredToken_ReturnsError(t *testing.T) {
	m := jwttoken.New("test-secret", -1*time.Second, 7*24*time.Hour)
	tok, _ := m.IssueAccess("sub", "alice", "USER", "fam1")
	if _, err := m.Verify(tok); err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestVerify_MalformedToken_ReturnsError(t *testing.T) {
	m := newTestManager()
	if _, err := m.Verify("not.a.token"); err == nil {
		t.Fatal("expected error for malformed token, got nil")
	}
}

func TestVerify_TokenHasJTI(t *testing.T) {
	m := newTestManager()
	tok, _ := m.IssueAccess("sub", "alice", "USER", "fam1")
	claims, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.ID == "" {
		t.Fatal("expected non-empty JTI")
	}
}
