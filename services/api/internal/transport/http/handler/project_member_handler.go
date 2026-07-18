package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// callerProjectPermissions resolves the authenticated caller's own effective
// permission set for the given project, used as the grant ceiling when
// assigning member roles (PACA-4). Agents are resolved via their project role;
// human callers via their global + project roles. It fails closed: an
// unresolvable caller yields an error rather than a permissive-looking set.
func (h *ProjectHandler) callerProjectPermissions(r *http.Request, projectID uuid.UUID) (authz.PermissionSet, error) {
	if agentID, ok := middleware.AgentIDFromRequest(r); ok {
		return h.authorizer.EffectivePermissionsForAgent(r.Context(), agentID, projectID)
	}
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		return nil, apierr.New(apierr.CodeUnauthenticated, "unauthenticated")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "invalid subject claim")
	}
	return h.authorizer.EffectivePermissions(r.Context(), userID, &projectID, claims.Role)
}

// ListMembers handles GET /projects/:projectId/members.
func (h *ProjectHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	id, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	members, err := h.svc.ListMembers(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.ProjectMemberResponse, 0, len(members))
	for _, m := range members {
		resp = append(resp, dto.ProjectMemberFromEntity(m))
	}
	presenter.OK(w, r, resp)
}

// AddMember handles POST /projects/:projectId/members.
func (h *ProjectHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	id, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.AddProjectMemberRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.UserID == uuid.Nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "user_id is required"))
		return
	}
	if req.ProjectRoleID == uuid.Nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "project_role_id is required"))
		return
	}

	caller, err := h.callerProjectPermissions(r, id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	m, err := h.svc.AddMember(r.Context(), id, projectdom.AddMemberInput{
		UserID:        req.UserID,
		ProjectRoleID: req.ProjectRoleID,
	}, caller)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.ProjectMemberFromEntity(m))
}

// UpdateMemberRole handles PATCH /projects/:projectId/members/:memberId.
func (h *ProjectHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	memberID, err := uuid.Parse(chi.URLParam(r, "memberId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid member id"))
		return
	}

	var req dto.UpdateProjectMemberRoleRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.ProjectRoleID == uuid.Nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "project_role_id is required"))
		return
	}

	caller, err := h.callerProjectPermissions(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	m, err := h.svc.UpdateMemberRoleByMemberID(r.Context(), projectID, memberID, projectdom.UpdateMemberRoleInput{
		ProjectRoleID: req.ProjectRoleID,
	}, caller)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProjectMemberFromEntity(m))
}

// RemoveMember handles DELETE /projects/:projectId/members/:memberId.
func (h *ProjectHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	memberID, err := uuid.Parse(chi.URLParam(r, "memberId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid member id"))
		return
	}
	if err := h.svc.RemoveMemberByMemberID(r.Context(), projectID, memberID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "member removed"})
}

// GetMyProjectPermissions handles GET /projects/:projectId/members/me/permissions.
// It returns the permission map of the authenticated user's project role.
// Any authenticated project member can call this endpoint regardless of which
// permissions their role grants — the lookup is always scoped to themselves.
func (h *ProjectHandler) GetMyProjectPermissions(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	// Check if request is from an agent and use agent ID if available
	var agentID *uuid.UUID
	if claims.AgentID != nil {
		if parsedAgentID, parseErr := uuid.Parse(*claims.AgentID); parseErr == nil {
			agentID = &parsedAgentID
		}
	}

	perms, err := h.svc.GetMyProjectPermissions(r.Context(), projectID, userID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, map[string]any{"permissions": perms})
}
