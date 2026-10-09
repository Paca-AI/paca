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

// maxItemsPeekBody bounds the body read to learn which tasks a bulk request
// names (a few hundred bytes per item).
const maxItemsPeekBody = 8 << 20

// RequireItemTasks authorizes action on every task a bulk request names in
// body.items[].task_id, as project/<projectParam>/task/<id>. The route's
// project gate cannot do this: it never sees which tasks are touched, so a
// Deny on one task, or a role limited to some tasks, would not apply. Any
// task the caller is not allowed on refuses the whole request (nothing is
// partially applied). A body it cannot read is left to the handler's own 400,
// and a task id that does not exist in the project is denied, as for any
// resource outside the project. Declare it after the route's action gate.
func RequireItemTasks(a *iam.Authorizer, action iam.Action, projectParam string) func(http.Handler) http.Handler {
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
				Items []struct {
					TaskID uuid.UUID `json:"task_id"`
				} `json:"items"`
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
			seen := make(map[uuid.UUID]struct{}, len(body.Items))
			for _, it := range body.Items {
				if _, dup := seen[it.TaskID]; dup {
					continue
				}
				seen[it.TaskID] = struct{}{}
				res, err := a.Authorize(r.Context(), p, string(action), "project/"+projectID+"/task/"+it.TaskID.String())
				if !proceedIfAllowed(w, r, res.Allowed, err) {
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
