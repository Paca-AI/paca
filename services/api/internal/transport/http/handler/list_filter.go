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
