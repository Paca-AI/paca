package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// toProjectMemberResponse maps m to a ProjectMemberResponse and, if an
// AvatarService is configured, resolves the backing user/agent's avatar
// keys into presigned display URLs.
func (h *ProjectHandler) toProjectMemberResponse(ctx context.Context, m *projectdom.ProjectMember) dto.ProjectMemberResponse {
	resp := dto.ProjectMemberFromEntity(m)
	if h.avatarSvc != nil {
		key, thumbKey := dto.MemberAvatarKeys(m)
		resp.AvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, key)
		resp.AvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, thumbKey)
	}
	return resp
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
		resp = append(resp, h.toProjectMemberResponse(r.Context(), m))
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
	hasUser := req.UserID != uuid.Nil
	hasAgent := req.AgentID != nil && *req.AgentID != uuid.Nil
	if hasUser == hasAgent {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "exactly one of user_id or agent_id is required"))
		return
	}
	if len(req.RoleIDs) == 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeRoleRequired, "role_ids is required"))
		return
	}

	m, err := h.svc.AddMember(r.Context(), id, projectdom.AddMemberInput{
		UserID:      req.UserID,
		AgentID:     req.AgentID,
		RoleIDs:     req.RoleIDs,
		Description: req.Description,
		CreatedBy:   attachmentCreator(r),
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, h.toProjectMemberResponse(r.Context(), m))
}

// UpdateMember handles PATCH /projects/:projectId/members/:memberId. It edits
// the member's description; roles are replaced through
// PUT /projects/:projectId/members/:memberId/roles.
func (h *ProjectHandler) UpdateMember(w http.ResponseWriter, r *http.Request) {
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

	var req dto.UpdateProjectMemberRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Description == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "description is required"))
		return
	}

	m, err := h.svc.UpdateMemberDescription(r.Context(), projectID, memberID, *req.Description)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toProjectMemberResponse(r.Context(), m))
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
// It returns {"actions": [...]}: the caller's effective IAM actions in the
// project (see iam.Authorizer.EffectiveActions).
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

	// The caller's effective IAM actions in the project — every registered
	// action allowed on the project or possibly on something inside it —
	// from the engine that enforces them. An agent-key request naming an
	// agent is judged as that agent.
	principal := iam.User(userID.String())
	if claims.AgentID != nil {
		if parsedAgentID, parseErr := uuid.Parse(*claims.AgentID); parseErr == nil {
			principal = iam.Agent(parsedAgentID.String())
		}
	}
	if h.authorizer == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
		return
	}
	actions, err := h.authorizer.EffectiveActions(r.Context(), principal, projectID.String())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if actions == nil {
		actions = []string{}
	}

	presenter.OK(w, r, map[string]any{"actions": actions})
}
