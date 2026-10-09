package iam

import (
	"context"
	"errors"
)

// Principal types.
const (
	PrincipalUser  = "user"
	PrincipalAgent = "agent"
)

// User is the principal of the user with the given (UUID) id.
func User(id string) Principal { return Principal{Type: PrincipalUser, ID: id} }

// Agent is the principal of the agent with the given (UUID) id.
func Agent(id string) Principal { return Principal{Type: PrincipalAgent, ID: id} }

// ProjectResource names a project: "project/<id>".
func ProjectResource(projectID string) string { return "project/" + projectID }

// PlatformResource is the resource a platform-level (no project) check of
// action targets: its platform root (PlatformRootFor), or "*" for an action
// without one, which only a "*" holder matches (fail closed).
func PlatformResource(action Action) string {
	if root := PlatformRootFor(string(action)); root != "" {
		return root
	}
	return "*"
}

// Checker is the part of *Authorizer a single decision needs; consumers take
// it so they can be tested with fakes.
type Checker interface {
	Authorize(ctx context.Context, p Principal, action, resource string, opts ...AuthzOption) (Result, error)
}

// ErrNotConfigured is returned by AllowedAll when no checker is wired.
var ErrNotConfigured = errors.New("iam: authorization not configured")

// AllowedAll reports whether p may perform EVERY one of actions on resource.
// No actions is "no". Any error means the answer is unknown: deny.
func AllowedAll(ctx context.Context, c Checker, p Principal, resource string, actions ...Action) (bool, error) {
	if c == nil {
		return false, ErrNotConfigured
	}
	if len(actions) == 0 {
		return false, nil
	}
	for _, a := range actions {
		res, err := c.Authorize(ctx, p, string(a), resource)
		if err != nil {
			return false, err
		}
		if !res.Allowed {
			return false, nil
		}
	}
	return true, nil
}
