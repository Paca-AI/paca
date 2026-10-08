package integration_test

import (
	"context"

	"github.com/google/uuid"

	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// IAM test doubles. The fake stores below describe callers as "a platform
// role holding these actions" and "a project role holding those in project
// P", and serve them to the real IAM engine shaped the way migration 000064
// writes roles: "*" becomes "*" on "*", every other platform action applies
// to the platform roots only (never inside a project — GHSA-hjcj), and a
// project role's actions apply to project/<P>/* through an attachment scoped
// to P.

// actionAll is the "*" wildcard (every action).
const actionAll iam.Action = "*"

var testPlatformRoots = []string{"user", "user/*", "role", "role/*", "plugin", "plugin/*", "settings", "sso", "agent", "agent/*", "project"}

func newIAM(store iam.Store) *iam.Authorizer {
	if store == nil {
		store = noGrants{}
	}
	return iam.NewAuthorizer(store, iam.NewRegistry(), iam.NewAttributeSchema())
}

// noGrants holds nothing for anyone.
type noGrants struct{}

func (noGrants) ListGrants(context.Context, iam.Principal) ([]iam.Grant, error) { return nil, nil }

func actionStrings(actions []iam.Action) (named []string, star bool) {
	for _, a := range actions {
		if a == actionAll {
			star = true
			continue
		}
		named = append(named, string(a))
	}
	return named, star
}

// platformGrant is a platform-wide attachment of actions.
func platformGrant(actions []iam.Action) []iam.Grant {
	named, star := actionStrings(actions)
	var sts []iam.Statement
	if star {
		sts = append(sts, iam.Statement{Effect: iam.EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}})
	}
	if len(named) > 0 {
		sts = append(sts, iam.Statement{Effect: iam.EffectAllow, Actions: named, Resources: testPlatformRoots})
	}
	if len(sts) == 0 {
		return nil
	}
	return []iam.Grant{{RoleID: "platform", Policy: &iam.Policy{Statements: sts}}}
}

// projectGrant is an attachment of actions scoped to projectID.
func projectGrant(projectID uuid.UUID, actions []iam.Action) []iam.Grant {
	named, star := actionStrings(actions)
	if star {
		named = []string{"*"}
	}
	if len(named) == 0 {
		return nil
	}
	p := "project/" + projectID.String()
	return []iam.Grant{{RoleID: "project-" + projectID.String(), ProjectID: projectID.String(), Policy: &iam.Policy{Statements: []iam.Statement{
		{Effect: iam.EffectAllow, Actions: named, Resources: []string{p + "/*"}},
	}}}}
}

// ListGrants serves projectPermStore's grants: for a user, globalPerms
// platform-wide plus, per project, userPerms[user][P] (or else
// projectPerms[P], which applies to every user); for an agent, its own
// agentGlobalPerms platform-wide plus agentPerms[P][agent] in P.
func (s *projectPermStore) ListGrants(_ context.Context, p iam.Principal) ([]iam.Grant, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, nil
	}
	var grants []iam.Grant
	if p.Type == iam.PrincipalAgent {
		grants = append(grants, platformGrant(s.agentGlobalPerms[id])...)
		for projectID, byAgent := range s.agentPerms {
			grants = append(grants, projectGrant(projectID, byAgent[id])...)
		}
		return grants, nil
	}
	grants = append(grants, platformGrant(s.globalPerms)...)
	own := s.userPerms[id]
	for projectID, perms := range own {
		grants = append(grants, projectGrant(projectID, perms)...)
	}
	for projectID, perms := range s.projectPerms {
		if _, overridden := own[projectID]; overridden {
			continue
		}
		grants = append(grants, projectGrant(projectID, perms)...)
	}
	return grants, nil
}

// ListGrants gives every user the store's platform actions.
func (s *integrationPermissionStore) ListGrants(_ context.Context, p iam.Principal) ([]iam.Grant, error) {
	if p.Type != iam.PrincipalUser {
		return nil, nil
	}
	return platformGrant(s.globalPerms), nil
}

// effectiveActionsReader lists a user's effective platform-level actions —
// the users service's GlobalPermissionReader, as bootstrap wires it.
type effectiveActionsReader struct{ a *iam.Authorizer }

func (r effectiveActionsReader) ListGlobalPermissions(ctx context.Context, userID uuid.UUID) ([]iam.Action, error) {
	acts, err := r.a.EffectiveActions(ctx, iam.User(userID.String()), "")
	out := make([]iam.Action, len(acts))
	for i, a := range acts {
		out[i] = iam.Action(a)
	}
	return out, err
}

// testRoles builds the platform role summaries of a fake user.
func testRoles(names ...string) []roledom.Summary {
	out := make([]roledom.Summary, 0, len(names))
	for _, n := range names {
		out = append(out, roledom.Summary{ID: uuid.New(), Name: n})
	}
	return out
}
