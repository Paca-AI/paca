package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// RoleAttachmentLookup reads the roles currently attached to a principal, so
// the assignment gate can tell which roles a replace-set request adds and
// removes. A target that does not exist has no roles: implementations return
// an empty set for it (the handler answers 404), never an error, so a missing
// target cannot be told apart from a denied one by an unauthorized caller.
type RoleAttachmentLookup interface {
	// UserRoleIDs returns the platform-wide roles of a user.
	UserRoleIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	// AgentRoleIDs returns the platform-wide roles of a global agent.
	AgentRoleIDs(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error)
	// MemberRoleIDs returns the roles a project member holds in the project.
	MemberRoleIDs(ctx context.Context, projectID, memberID uuid.UUID) ([]uuid.UUID, error)
}

// roleAttachmentService is the part of the role service the lookup needs.
type roleAttachmentService interface {
	ListUserRoles(ctx context.Context, userID uuid.UUID) ([]*roledom.Role, error)
	ListAgentRoles(ctx context.Context, agentID uuid.UUID) ([]*roledom.Role, error)
	ListMemberRoles(ctx context.Context, projectID, memberID uuid.UUID) ([]*roledom.Role, error)
}

// RoleServiceAttachments implements RoleAttachmentLookup on top of the role
// service (the same reads GET .../roles serves).
type RoleServiceAttachments struct{ Svc roleAttachmentService }

// NewRoleServiceAttachments wraps the role service as a RoleAttachmentLookup.
func NewRoleServiceAttachments(svc roleAttachmentService) RoleServiceAttachments {
	return RoleServiceAttachments{Svc: svc}
}

func roleIDsOf(roles []*roledom.Role, err error) ([]uuid.UUID, error) {
	if err != nil {
		if errors.Is(err, roledom.ErrUserNotFound) || errors.Is(err, roledom.ErrAgentNotFound) ||
			errors.Is(err, roledom.ErrMemberNotFound) || errors.Is(err, roledom.ErrProjectNotFound) {
			return nil, nil
		}
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// UserRoleIDs implements RoleAttachmentLookup.
func (a RoleServiceAttachments) UserRoleIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return roleIDsOf(a.Svc.ListUserRoles(ctx, id))
}

// AgentRoleIDs implements RoleAttachmentLookup.
func (a RoleServiceAttachments) AgentRoleIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return roleIDsOf(a.Svc.ListAgentRoles(ctx, id))
}

// MemberRoleIDs implements RoleAttachmentLookup.
func (a RoleServiceAttachments) MemberRoleIDs(ctx context.Context, projectID, memberID uuid.UUID) ([]uuid.UUID, error) {
	return roleIDsOf(a.Svc.ListMemberRoles(ctx, projectID, memberID))
}

// AssignTarget says whose roles a request changes and in which scope.
type AssignTarget struct {
	// ProjectParam is the URL parameter holding the project of a project-scope
	// assignment; empty for a platform-scope one.
	ProjectParam string
	// Current returns the ids of the roles the target holds now in that scope.
	// A nil Current means the request creates the target (a new member or
	// agent): it holds nothing yet, and a body without role_ids assigns
	// nothing. With a Current, the request is a replace-set, and a body
	// without role_ids means the empty set (the handler detaches everything).
	Current func(r *http.Request) ([]uuid.UUID, error)
}

// UserRolesTarget is the platform-wide roles of the user in userParam.
func UserRolesTarget(l RoleAttachmentLookup, userParam string) AssignTarget {
	return AssignTarget{Current: func(r *http.Request) ([]uuid.UUID, error) {
		id, err := uuidParam(r, userParam, "user")
		if err != nil {
			return nil, err
		}
		return lookupOrFail(l, func() ([]uuid.UUID, error) { return l.UserRoleIDs(r.Context(), id) })
	}}
}

// AgentRolesTarget is the platform-wide roles of the global agent in agentParam.
func AgentRolesTarget(l RoleAttachmentLookup, agentParam string) AssignTarget {
	return AssignTarget{Current: func(r *http.Request) ([]uuid.UUID, error) {
		id, err := uuidParam(r, agentParam, "agent")
		if err != nil {
			return nil, err
		}
		return lookupOrFail(l, func() ([]uuid.UUID, error) { return l.AgentRoleIDs(r.Context(), id) })
	}}
}

// MemberRolesTarget is the roles of the member in memberParam inside the
// project in projectParam.
func MemberRolesTarget(l RoleAttachmentLookup, projectParam, memberParam string) AssignTarget {
	return AssignTarget{ProjectParam: projectParam, Current: func(r *http.Request) ([]uuid.UUID, error) {
		pid, err := uuidParam(r, projectParam, "project")
		if err != nil {
			return nil, err
		}
		mid, err := uuidParam(r, memberParam, "member")
		if err != nil {
			return nil, err
		}
		return lookupOrFail(l, func() ([]uuid.UUID, error) { return l.MemberRoleIDs(r.Context(), pid, mid) })
	}}
}

// NewProjectPrincipalTarget is a member or agent being created inside the
// project in projectParam, with the roles in the body's role_ids.
func NewProjectPrincipalTarget(projectParam string) AssignTarget {
	return AssignTarget{ProjectParam: projectParam}
}

func lookupOrFail(l RoleAttachmentLookup, f func() ([]uuid.UUID, error)) ([]uuid.UUID, error) {
	if l == nil {
		return nil, apierr.New(apierr.CodeInternalError, "authorization not configured")
	}
	return f()
}

func uuidParam(r *http.Request, param, what string) (uuid.UUID, error) {
	s, err := urlUUID(r, param, what)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.MustParse(s), nil
}

// RequireAssignRoles is the gate of every role assignment, modelled on AWS IAM
// iam:PassRole: the caller needs roles:assign on the resource of EACH role the
// request adds to or removes from the target. Nothing else is asked about the
// role — in particular the caller need not hold its permissions — so what an
// assigner may hand out is decided entirely by the resource of their
// roles:assign statements ("role/*", "role/<id>", "project/<P>/role/*", ...).
//
// The resource is "role/<roleId>" for a platform-scope assignment and
// "project/<projectId>/role/<roleId>" for a project-scope one, for every role,
// whoever owns it: the attachment lives in that project, so the role is judged
// as seen inside it.
//
// The affected roles are the symmetric difference of the roles the target
// holds now and the request's role_ids: unchanged roles need no permission,
// added and removed ones do (an empty role_ids that detaches roles needs a
// permission for each). Ids that name no role are checked like any other (the
// service answers them with a 422 once the caller is allowed). A body that
// does not decode, or whose role_ids are not UUIDs, is passed through for the
// handler's 400. Declare it after the route's other gates, so a caller with no
// access to the route never has their body inspected.
//
// Failures are closed: a missing authorizer or lookup, a failed lookup or a
// failed authorization is a 500; a refusal is a 403.
func RequireAssignRoles(a *iam.Authorizer, t AssignTarget) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := IAMPrincipalFrom(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			prefix := "role/"
			if t.ProjectParam != "" {
				pid, err := urlUUID(r, t.ProjectParam, "project")
				if err != nil {
					presenter.Error(w, r, err)
					return
				}
				prefix = "project/" + pid + "/role/"
			}
			buf, err := peekBody(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			var body struct {
				RoleIDs []uuid.UUID `json:"role_ids"`
			}
			if json.NewDecoder(bytes.NewReader(buf)).Decode(&body) != nil {
				next.ServeHTTP(w, r)
				return
			}
			var current []uuid.UUID
			if t.Current != nil {
				if current, err = t.Current(r); err != nil {
					presenter.Error(w, r, err)
					return
				}
			}
			affected := symmetricDifference(current, body.RoleIDs)
			if len(affected) > 0 && a == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
				return
			}
			for _, id := range affected {
				res, err := a.Authorize(r.Context(), p, string(iam.ActionRolesAssign), prefix+id.String())
				if err != nil {
					presenter.Error(w, r, err)
					return
				}
				if !res.Allowed {
					presenter.Error(w, r, errNotAssignable)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

var errNotAssignable = apierr.New(apierr.CodeForbidden, "you are not allowed to assign or remove one of these roles")

// symmetricDifference returns the ids in exactly one of old and updated
// (deduplicated; the ones added in request order first, then the removed).
func symmetricDifference(old, updated []uuid.UUID) []uuid.UUID {
	had := make(map[uuid.UUID]bool, len(old))
	for _, id := range old {
		had[id] = true
	}
	want := make(map[uuid.UUID]bool, len(updated))
	var out []uuid.UUID
	for _, id := range updated {
		if want[id] {
			continue
		}
		want[id] = true
		if !had[id] {
			out = append(out, id)
		}
	}
	seen := make(map[uuid.UUID]bool, len(old))
	for _, id := range old {
		if seen[id] {
			continue
		}
		seen[id] = true
		if !want[id] {
			out = append(out, id)
		}
	}
	return out
}
