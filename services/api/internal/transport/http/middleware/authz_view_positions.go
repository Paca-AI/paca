package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// RequireViewIDs authorizes action on every view a reorder request names in
// body.view_ids, as project/<projectParam>/view/<id>. The route's project gate
// never sees which views are touched, so a Deny on one view, or a role limited
// to the views of one sprint (view.sprint_id), would not apply. Any view the
// caller is not allowed on refuses the whole request (nothing is partially
// applied). A body it cannot read is left to the handler's own 400, and a view
// id that does not exist in the project is denied, as for any resource outside
// the project. Declare it after the route's action gate.
func RequireViewIDs(a *iam.Authorizer, action iam.Action, projectParam string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := IAMPrincipalFrom(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			projectID, err := urlUUID(r, projectParam, "project")
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			buf, err := peekBodyLimit(r, maxItemsPeekBody)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			var body struct {
				ViewIDs []uuid.UUID `json:"view_ids"`
			}
			dec := json.NewDecoder(bytes.NewReader(buf))
			if dec.Decode(&body) != nil || dec.More() {
				next.ServeHTTP(w, r) // not the shape the handler binds or has trailing bytes: its 400
				return
			}
			if a == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
				return
			}
			seen := make(map[uuid.UUID]struct{}, len(body.ViewIDs))
			for _, id := range body.ViewIDs {
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				res, err := a.Authorize(r.Context(), p, string(action), "project/"+projectID+"/view/"+id.String())
				if !proceedIfAllowed(w, r, res.Allowed, err) {
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
