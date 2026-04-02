package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/apierr"
	projectdom "github.com/paca/api/internal/domain/project"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

// ListMembers handles GET /projects/:projectId/members.
func (h *ProjectHandler) ListMembers(c *gin.Context) {
	id, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	members, err := h.svc.ListMembers(c.Request.Context(), id)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	resp := make([]dto.ProjectMemberResponse, 0, len(members))
	for _, m := range members {
		resp = append(resp, dto.ProjectMemberFromEntity(m))
	}
	presenter.OK(c, resp)
}

// AddMember handles POST /projects/:projectId/members.
func (h *ProjectHandler) AddMember(c *gin.Context) {
	id, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.AddProjectMemberRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	m, err := h.svc.AddMember(c.Request.Context(), id, projectdom.AddMemberInput{
		UserID:        req.UserID,
		ProjectRoleID: req.ProjectRoleID,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.Created(c, dto.ProjectMemberFromEntity(m))
}

// RemoveMember handles DELETE /projects/:projectId/members/:userId.
func (h *ProjectHandler) RemoveMember(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}
	if err := h.svc.RemoveMember(c.Request.Context(), projectID, userID); err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, gin.H{"message": "member removed"})
}
