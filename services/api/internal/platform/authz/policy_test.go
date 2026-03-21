package authz_test

import (
	"testing"

	"github.com/paca/api/internal/platform/authz"
)

func TestRequire_AllowsMatchingRole(t *testing.T) {
	p := authz.NewPolicy()
	if err := p.Require(authz.RoleUser, authz.RoleUser); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestRequire_AllowsOneOfMultipleRoles(t *testing.T) {
	p := authz.NewPolicy()
	if err := p.Require(authz.RoleAdmin, authz.RoleUser, authz.RoleAdmin); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestRequire_BlocksMismatchedRole(t *testing.T) {
	p := authz.NewPolicy()
	if err := p.Require("GUEST", authz.RoleUser, authz.RoleAdmin); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRequire_EmptyRequired_AlwaysBlocks(t *testing.T) {
	p := authz.NewPolicy()
	if err := p.Require(authz.RoleAdmin); err == nil {
		t.Fatal("expected error with no required roles, got nil")
	}
}

func TestIsAdmin_Admin(t *testing.T) {
	p := authz.NewPolicy()
	if !p.IsAdmin(authz.RoleAdmin) {
		t.Fatal("expected true for ADMIN")
	}
}

func TestIsAdmin_User(t *testing.T) {
	p := authz.NewPolicy()
	if p.IsAdmin(authz.RoleUser) {
		t.Fatal("expected false for USER")
	}
}
