package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
)

// ListScoper is the part of *iam.Authorizer list endpoints use to learn which
// children of a project the caller may act on. Route gates only check the
// parent project, so a Deny on one agent (or an Allow limited to one sprint's
// tasks) must also constrain what the list returns.
type ListScoper interface {
	ListScope(ctx context.Context, p iam.Principal, action, projectID, kind string) (*iam.Node, error)
	ListScopes(ctx context.Context, p iam.Principal, action string, projectIDs []string, kind string) (map[string]*iam.Node, error)
}

// projectScopesContext returns r's context carrying one scope per project in
// projectIDs, resolved with one batched authorizer query. A nil scoper leaves
// the context unchanged. An authorizer error is returned so the request fails
// closed instead of listing everything.
func (h *TaskHandler) projectScopesContext(r *http.Request, projectIDs []uuid.UUID) (context.Context, error) {
	if h.listScoper == nil {
		return r.Context(), nil
	}
	principal, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(projectIDs))
	for _, pid := range projectIDs {
		ids = append(ids, pid.String())
	}
	byProject, err := h.listScoper.ListScopes(r.Context(), principal, string(iam.ActionTasksRead), ids, "task")
	if err != nil {
		return nil, err
	}
	return iam.WithProjectScopes(r.Context(), "task", byProject), nil
}

// scopedContext returns r's context carrying the caller's scope for kind, which
// the repository applies inside the list query: filtering happens in the
// database, before pagination, counts and sums, never on a fetched page. A nil
// scoper leaves the context unchanged. An authorizer error is returned so the
// request fails closed instead of listing everything.
func scopedContext(r *http.Request, s ListScoper, action iam.Action, projectID uuid.UUID, kind string) (context.Context, error) {
	if s == nil {
		return r.Context(), nil
	}
	principal, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		return nil, err
	}
	n, err := s.ListScope(r.Context(), principal, string(action), projectID.String(), kind)
	if err != nil {
		return nil, err
	}
	return iam.WithScope(r.Context(), kind, n), nil
}
