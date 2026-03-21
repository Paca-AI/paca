// Package authz provides a lightweight role-based authorization policy.
package authz

import "fmt"

// Role constants used across the application.
const (
	RoleUser  = "USER"
	RoleAdmin = "ADMIN"
)

// Policy enforces role-based access checks.
type Policy struct{}

// NewPolicy returns a Policy instance.
func NewPolicy() *Policy { return &Policy{} }

// Require returns an error if the given role does not satisfy any of the
// required roles.
func (p *Policy) Require(role string, required ...string) error {
	for _, r := range required {
		if role == r {
			return nil
		}
	}
	return fmt.Errorf("authz: role %q not permitted; required one of %v", role, required)
}

// IsAdmin reports whether the given role is ADMIN.
func (p *Policy) IsAdmin(role string) bool { return role == RoleAdmin }
