package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// RequireActionsForSimulatedPrincipal gates what-if simulation with a named
// principal: simulating the given policy alone is open to any authenticated
// caller (it reads no data), but naming a principal exposes that principal's
// grants, so the checks must pass in that case. The body is read (bounded) and
// restored for the handler; a body without a principal is passed through.
func RequireActionsForSimulatedPrincipal(a *iam.Authorizer, checks ...ActionCheck) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf, err := peekBody(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			var body struct {
				Principal json.RawMessage `json:"principal"`
			}
			if json.NewDecoder(bytes.NewReader(buf)).Decode(&body) != nil {
				next.ServeHTTP(w, r)
				return
			}
			if t := bytes.TrimSpace(body.Principal); len(t) == 0 || bytes.Equal(t, []byte("null")) {
				next.ServeHTTP(w, r)
				return
			}
			allowed, err := authorizeAll(r, a, checks)
			if proceedIfAllowed(w, r, allowed, err) {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// peekBody reads the request body (at most maxPeekBody bytes) and restores it
// for the next handler. An oversized or unreadable body is a 400.
func peekBody(r *http.Request) ([]byte, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, maxPeekBody+1))
	_ = r.Body.Close()
	if err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "could not read request body")
	}
	if len(buf) > maxPeekBody {
		return nil, apierr.New(apierr.CodeBadRequest, "request body too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))
	return buf, nil
}
