package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// RequireDocView authorizes action on the note view a document request names,
// as project/<projectParam>/view/<id>: the view_id query parameter of a
// listing (GET), or the view_id field of a create body (any other method).
// Notes are documents, so the route's docs:read/docs:write gate still applies;
// this one makes them follow their view too, so a Deny on the view, or a role
// limited to the views of one sprint (view.sprint_id), hides its notes and
// keeps new ones out of it. Whether the view exists, is in the project and is
// a note view is the handler's check (DOC_NOTE_VIEW_INVALID); a policy whose
// conditions need the view's attributes already denies one it cannot find. A
// request that names no view is not affected. A view id that is not a UUID,
// or a body it cannot read, is left to the handler's own 400; data after the
// body's JSON value is refused with a 400, since a handler reading only the
// first value would act on a view this gate never saw. Declare it after the
// route's action gate.
func RequireDocView(a *iam.Authorizer, action iam.Action, projectParam string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			viewID, ok, err := requestViewID(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
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
			if a == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
				return
			}
			res, err := a.Authorize(r.Context(), p, string(action), "project/"+projectID+"/view/"+viewID.String())
			if proceedIfAllowed(w, r, res.Allowed, err) {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// requestViewID returns the view a document request names. ok is false when
// it names none, or names it in a form the handler refuses with its own 400.
func requestViewID(r *http.Request) (uuid.UUID, bool, error) {
	if r.Method == http.MethodGet {
		id, err := uuid.Parse(r.URL.Query().Get("view_id"))
		return id, err == nil, nil
	}
	buf, err := peekBodyLimit(r, maxAttrPeekBody)
	if err != nil {
		return uuid.Nil, false, err
	}
	var body map[string]json.RawMessage
	switch err := decodeSingleJSON(bytes.NewReader(buf), &body); {
	case errors.Is(err, errTrailingJSON):
		return uuid.Nil, false, apierr.New(apierr.CodeBadRequest, err.Error())
	case err != nil:
		return uuid.Nil, false, nil // not a JSON object: the handler's own 400
	}
	// Keys are matched the way encoding/json matches them to the handler's
	// struct fields, so the gate sees the view the handler will use.
	if body, err = foldBodyKeys(body); err != nil {
		return uuid.Nil, false, err
	}
	raw, present := body["view_id"]
	if !present || string(bytes.TrimSpace(raw)) == "null" {
		return uuid.Nil, false, nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return uuid.Nil, false, nil
	}
	return id, true, nil
}
