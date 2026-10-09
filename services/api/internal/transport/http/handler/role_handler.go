package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// RoleHandler serves the IAM roles and attachments API. It contains no
// authorization: every route is gated, and the roles:assign gate and the
// escalation guards run, in the router's middleware.
//
// The same handlers serve the platform routes (/admin/roles) and the project
// routes (/projects/{projectId}/roles): the scope is the project id in the
// URL, when the route has one.
type RoleHandler struct {
	svc roledom.Service
}

// NewRoleHandler returns a RoleHandler over the role service.
func NewRoleHandler(svc roledom.Service) *RoleHandler { return &RoleHandler{svc: svc} }

// scope returns the project the route is scoped to, or nil on a platform route.
func scope(r *http.Request) (*uuid.UUID, error) {
	raw := chi.URLParam(r, "projectId")
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "invalid project id")
	}
	return &id, nil
}

func urlID(r *http.Request, param, what string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid "+what+" id")
	}
	return id, nil
}

// attachmentCreator is the account recorded as the creator of new attachments.
func attachmentCreator(r *http.Request) *uuid.UUID {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		return nil
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil
	}
	return &id
}

func toInput(req dto.RoleRequest) roledom.RoleInput {
	return roledom.RoleInput{Name: req.Name, Description: req.Description, Policy: req.Policy}
}

// List handles GET /admin/roles and GET /projects/{projectId}/roles.
func (h *RoleHandler) List(w http.ResponseWriter, r *http.Request) {
	project, err := scope(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var roles []*roledom.Role
	if project == nil {
		roles, err = h.svc.ListPlatform(r.Context())
	} else {
		roles, err = h.svc.ListForProject(r.Context(), *project)
	}
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RolesFromEntities(roles))
}

// Get handles GET /admin/roles/{roleId} and GET /projects/{projectId}/roles/{roleId}.
func (h *RoleHandler) Get(w http.ResponseWriter, r *http.Request) {
	project, err := scope(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	id, err := urlID(r, "roleId", "role")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	role, err := h.svc.Get(r.Context(), project, id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RoleFromEntity(role))
}

// Create handles POST /admin/roles and POST /projects/{projectId}/roles.
func (h *RoleHandler) Create(w http.ResponseWriter, r *http.Request) {
	project, err := scope(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.RoleRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	role, err := h.svc.Create(r.Context(), project, toInput(req))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.RoleFromEntity(role))
}

// Update handles PUT /admin/roles/{roleId} and PUT /projects/{projectId}/roles/{roleId}.
func (h *RoleHandler) Update(w http.ResponseWriter, r *http.Request) {
	project, err := scope(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	id, err := urlID(r, "roleId", "role")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.RoleRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	role, err := h.svc.Update(r.Context(), project, id, toInput(req))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RoleFromEntity(role))
}

// Delete handles DELETE /admin/roles/{roleId} and DELETE /projects/{projectId}/roles/{roleId}.
func (h *RoleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	project, err := scope(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	id, err := urlID(r, "roleId", "role")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.Delete(r.Context(), project, id); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// SetDefault handles PUT /admin/roles/{roleId}/default.
func (h *RoleHandler) SetDefault(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "roleId", "role")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	role, err := h.svc.SetDefault(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RoleFromEntity(role))
}

// ListUserRoles handles GET /admin/users/{userId}/roles.
func (h *RoleHandler) ListUserRoles(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "userId", "user")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	roles, err := h.svc.ListUserRoles(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RolesFromEntities(roles))
}

// ReplaceUserRoles handles PUT /admin/users/{userId}/roles.
func (h *RoleHandler) ReplaceUserRoles(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "userId", "user")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.ReplaceRolesRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	roles, err := h.svc.ReplaceUserRoles(r.Context(), id, req.RoleIDs, attachmentCreator(r))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RolesFromEntities(roles))
}

// ListAgentRoles handles GET /admin/agents/{agentId}/roles.
func (h *RoleHandler) ListAgentRoles(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "agentId", "agent")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	roles, err := h.svc.ListAgentRoles(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RolesFromEntities(roles))
}

// ReplaceAgentRoles handles PUT /admin/agents/{agentId}/roles.
func (h *RoleHandler) ReplaceAgentRoles(w http.ResponseWriter, r *http.Request) {
	id, err := urlID(r, "agentId", "agent")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.ReplaceRolesRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	roles, err := h.svc.ReplaceAgentRoles(r.Context(), id, req.RoleIDs, attachmentCreator(r))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RolesFromEntities(roles))
}

// ListMemberRoles handles GET /projects/{projectId}/members/{memberId}/roles.
func (h *RoleHandler) ListMemberRoles(w http.ResponseWriter, r *http.Request) {
	projectID, err := urlID(r, "projectId", "project")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	memberID, err := urlID(r, "memberId", "member")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	roles, err := h.svc.ListMemberRoles(r.Context(), projectID, memberID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RolesFromEntities(roles))
}

// ReplaceMemberRoles handles PUT /projects/{projectId}/members/{memberId}/roles.
func (h *RoleHandler) ReplaceMemberRoles(w http.ResponseWriter, r *http.Request) {
	projectID, err := urlID(r, "projectId", "project")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	memberID, err := urlID(r, "memberId", "member")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.ReplaceRolesRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	roles, err := h.svc.ReplaceMemberRoles(r.Context(), projectID, memberID, req.RoleIDs, attachmentCreator(r))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RolesFromEntities(roles))
}

// Actions handles GET /roles/actions.
func (h *RoleHandler) Actions(w http.ResponseWriter, r *http.Request) {
	presenter.OK(w, r, h.svc.Actions())
}

// AttributeSchema handles GET /roles/attribute-schema.
func (h *RoleHandler) AttributeSchema(w http.ResponseWriter, r *http.Request) {
	defs := h.svc.AttributeDefs()
	out := make([]dto.AttributeDefResponse, len(defs))
	for i, d := range defs {
		out[i] = dto.AttributeDefResponse{Key: d.Key, ResourceKind: d.ResourceKind, Type: d.Type, MultiValued: d.MultiValued, LabelKey: d.LabelKey}
	}
	presenter.OK(w, r, out)
}

// Validate handles POST /roles/validate. It always answers 200: problems with
// the policy are the result, not a failure of the request.
func (h *RoleHandler) Validate(w http.ResponseWriter, r *http.Request) {
	var req dto.ValidatePolicyRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	projectID, err := scope(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	issues := h.svc.ValidatePolicy(req.Policy, projectID)
	resp := dto.ValidatePolicyResponse{Valid: len(issues) == 0, Issues: make([]dto.PolicyIssueResponse, 0, len(issues))}
	for _, is := range issues {
		resp.Issues = append(resp.Issues, dto.PolicyIssueResponse{Path: is.Path, Message: is.Message})
	}
	presenter.OK(w, r, resp)
}

// Simulate handles POST /roles/simulate.
func (h *RoleHandler) Simulate(w http.ResponseWriter, r *http.Request) {
	var req dto.SimulateRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	in := roledom.SimulateInput{Policy: req.Policy, Action: req.Action, Resource: req.Resource}
	if req.Principal != nil {
		in.Principal = &roledom.PrincipalRef{Type: req.Principal.Type, ID: req.Principal.ID}
	}
	if len(req.Attributes) > 0 {
		in.Attributes = make(map[string][]string, len(req.Attributes))
		for k, v := range req.Attributes {
			in.Attributes[k] = v
		}
	}
	res, err := h.svc.Simulate(r.Context(), in)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := dto.SimulateResponse{Allowed: res.Allowed, Matched: make([]dto.MatchedStatementResponse, 0, len(res.Matched))}
	for _, m := range res.Matched {
		resp.Matched = append(resp.Matched, dto.MatchedStatementResponse{RoleID: m.RoleID, Sid: m.Sid, Effect: m.Effect, Index: m.Index})
	}
	presenter.OK(w, r, resp)
}
