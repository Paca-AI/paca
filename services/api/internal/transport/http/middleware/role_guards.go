package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// PolicyGranter decides whether a principal may grant a policy (put it into a
// role or hand a role carrying it to someone). *iam.Authorizer implements it.
type PolicyGranter interface {
	CanGrant(ctx context.Context, p iam.Principal, policy *iam.Policy) (bool, error)
}

// RolePolicyLookup returns the stored policy documents of the roles that
// exist among ids (unknown ids are simply absent from the result).
type RolePolicyLookup interface {
	RolePolicies(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]byte, error)
}

var errNotGrantable = apierr.New(apierr.CodeForbidden, "you cannot grant permissions you do not hold yourself")

// RequireGrantablePolicy is the privilege-escalation guard of the role write
// routes (POST/PUT, platform and project roles alike): the caller may only
// save a policy that grants what the caller holds unconditionally themselves
// and that no Deny of theirs touches (see iam.GrantsCover). Deny statements
// in the saved policy are always fine.
//
// The request body is read (bounded) and restored for the handler, and
// decoded the way the handler decodes it (first JSON value, same field), so
// the two can never disagree about which policy was sent. A body that has no
// usable policy (invalid JSON, a policy that does not parse, none at all) is
// passed through untouched: the handler and service answer it with their own
// 400 / 422 and nothing is granted. Anything else fails closed: an
// authorizer error is a 500, "no" is a 403.
//
// Declare it after the route's action gate, so a caller with no access to the
// route never has their body inspected.
func RequireGrantablePolicy(g PolicyGranter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := IAMPrincipalFrom(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			buf, err := peekBody(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			var body struct {
				Policy json.RawMessage `json:"policy"`
			}
			if json.NewDecoder(bytes.NewReader(buf)).Decode(&body) != nil {
				next.ServeHTTP(w, r)
				return
			}
			policy, perr := iam.ParsePolicy(bytes.TrimSpace(body.Policy))
			if perr != nil {
				next.ServeHTTP(w, r)
				return
			}
			if g == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
				return
			}
			ok, err := g.CanGrant(r.Context(), p, policy)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			if !ok {
				presenter.Error(w, r, errNotGrantable)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireGrantableRoleInPath is the escalation guard of a route that acts on
// the one role named by the roleParam URL parameter (making a role the
// default hands it to every future account). An id that names no role passes
// through for the handler's 404.
func RequireGrantableRoleInPath(lookup RolePolicyLookup, g PolicyGranter, roleParam string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := IAMPrincipalFrom(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			id, err := uuid.Parse(chi.URLParam(r, roleParam))
			if err != nil {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid role id"))
				return
			}
			if lookup == nil || g == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
				return
			}
			policies, err := lookup.RolePolicies(r.Context(), []uuid.UUID{id})
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			if raw, found := policies[id]; found {
				policy, perr := iam.ParsePolicy(raw)
				if perr != nil {
					presenter.Error(w, r, apierr.New(apierr.CodeForbidden, "a role's policy cannot be evaluated"))
					return
				}
				ok, err := g.CanGrant(r.Context(), p, policy)
				if err != nil {
					presenter.Error(w, r, err)
					return
				}
				if !ok {
					presenter.Error(w, r, errNotGrantable)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

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
