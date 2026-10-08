package postgres

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// IAMStore implements iam.Store (and iam.Invalidator) over the roles and
// role_attachments tables. Parsed role policies are cached by role ID; a miss
// re-reads the role, and Invalidate drops entries so an edit takes effect on
// the next request.
type IAMStore struct {
	db *sqlx.DB

	mu    sync.Mutex
	cache map[string]*iam.Policy // nil value = cached "unparsable policy"
	gen   uint64                 // bumped by Invalidate; stale reads are not cached
}

// NewIAMStore returns a Postgres-backed IAM store.
func NewIAMStore(db *sqlx.DB) *IAMStore {
	return &IAMStore{db: db, cache: map[string]*iam.Policy{}}
}

// listGrantsSQL selects a principal's effective attachments:
//   - the principal must be alive (users.deleted_at / agents.deleted_at IS NULL);
//   - a project-owned role (roles.project_id) only counts when attached to that
//     same project (never at platform scope or in another project);
//   - platform attachments (project_id IS NULL) always count;
//   - a project-scoped attachment counts only while the principal has an
//     active (deleted_at IS NULL) project_members row in that project
//     (human members: user_id; agent members: agent_id).
const listGrantsSQL = `
SELECT ra.role_id::text AS role_id, COALESCE(ra.project_id::text, '') AS project_id
FROM role_attachments ra
WHERE ra.principal_type = $1
  AND ra.principal_id = $2::uuid
  AND EXISTS (SELECT 1 FROM roles r WHERE r.id = ra.role_id AND (r.project_id IS NULL OR r.project_id = ra.project_id))
  AND CASE ra.principal_type
        WHEN 'user'  THEN EXISTS (SELECT 1 FROM users  u WHERE u.id = ra.principal_id AND u.deleted_at IS NULL)
        WHEN 'agent' THEN EXISTS (SELECT 1 FROM agents a WHERE a.id = ra.principal_id AND a.deleted_at IS NULL)
        ELSE FALSE
      END
  AND (ra.project_id IS NULL OR EXISTS (
        SELECT 1 FROM project_members pm
        WHERE pm.project_id = ra.project_id
          AND pm.deleted_at IS NULL
          AND ((ra.principal_type = 'user'  AND pm.user_id  = ra.principal_id)
            OR (ra.principal_type = 'agent' AND pm.agent_id = ra.principal_id))))
ORDER BY ra.created_at, ra.id`

// ListGrants implements iam.Store. A role whose stored policy does not parse
// yields a Grant with a nil Policy (grants nothing); it is not an error.
func (s *IAMStore) ListGrants(ctx context.Context, p iam.Principal) ([]iam.Grant, error) {
	var rows []struct {
		RoleID    string `db:"role_id"`
		ProjectID string `db:"project_id"`
	}
	if err := s.db.SelectContext(ctx, &rows, listGrantsSQL, p.Type, p.ID); err != nil {
		return nil, fmt.Errorf("iam store: list attachments: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	policies := make(map[string]*iam.Policy, len(rows))
	var missing []string
	s.mu.Lock()
	gen := s.gen
	for _, r := range rows {
		if _, seen := policies[r.RoleID]; seen {
			continue
		}
		if pol, ok := s.cache[r.RoleID]; ok {
			policies[r.RoleID] = pol
		} else {
			policies[r.RoleID] = nil
			missing = append(missing, r.RoleID)
		}
	}
	s.mu.Unlock()

	if len(missing) > 0 {
		var roles []struct {
			ID     string `db:"id"`
			Policy []byte `db:"policy"`
		}
		if err := s.db.SelectContext(ctx, &roles, `SELECT id::text AS id, policy FROM roles WHERE id = ANY($1::uuid[])`, missing); err != nil {
			return nil, fmt.Errorf("iam store: load roles: %w", err)
		}
		parsed := make(map[string]*iam.Policy, len(roles))
		for _, r := range roles {
			pol, err := iam.ParsePolicy(r.Policy)
			if err != nil {
				pol = nil
			}
			parsed[r.ID] = pol
			policies[r.ID] = pol
		}
		s.mu.Lock()
		if s.gen == gen { // no Invalidate raced with the read
			for id, pol := range parsed {
				s.cache[id] = pol
			}
		}
		s.mu.Unlock()
	}

	grants := make([]iam.Grant, 0, len(rows))
	for _, r := range rows {
		grants = append(grants, iam.Grant{RoleID: r.RoleID, ProjectID: r.ProjectID, Policy: policies[r.RoleID]})
	}
	return grants, nil
}

// Invalidate drops cached policies for the given roles. Call it after the
// role update has committed, otherwise a concurrent request may re-cache the
// pre-commit policy.
func (s *IAMStore) Invalidate(roleIDs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	for _, id := range roleIDs {
		delete(s.cache, id)
	}
}

// NewIAMAuthorizer builds the IAM authorizer over Postgres: an IAMStore
// (with its policy cache), the built-in action registry and attribute
// schema, and every attribute loader conditional grants need.
func NewIAMAuthorizer(db *sqlx.DB) *iam.Authorizer {
	a := iam.NewAuthorizer(NewIAMStore(db), iam.NewRegistry(), iam.NewAttributeSchema())
	for _, l := range NewIAMAttributeLoaders(db) {
		a.RegisterLoader(l)
	}
	return a
}
