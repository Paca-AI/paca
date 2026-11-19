package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

const (
	// maxAccessProbeChecks bounds one batch. Realtime coalesces the events of
	// a tick into a batch; anything larger is a client bug.
	maxAccessProbeChecks = 200
	maxAccessProbeBody   = 1 << 20
)

// AccessProbeCheck is one question of an access probe: may the caller perform
// Action on Resource?
type AccessProbeCheck struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
}

type accessProbeKey struct{}

// AccessProbeResultsFrom returns the answers ProbeAccess computed for the
// request, in the order the body asked them.
func AccessProbeResultsFrom(r *http.Request) ([]bool, bool) {
	v, ok := r.Context().Value(accessProbeKey{}).([]bool)
	return v, ok
}

// ProbeAccess answers, for the account the request authenticated as, whether
// each action-on-resource pair in the body is allowed. It reports and gates
// nothing: it is the batch form of the "my permissions" endpoints, so a
// consumer that must authorize something per event (services/realtime) asks
// the engine instead of re-implementing it. The evaluation lives here, in the
// router's middleware chain, and the handler only writes the answers.
//
// Every resource must lie inside the project in the URL (a probe is a
// project-scoped question), and an action the registry does not know simply
// comes back false, as it would from a gate. A malformed body is a 400; an
// authorizer failure is a 500, never an allow.
func ProbeAccess(a *iam.Authorizer, projectParam string) func(http.Handler) http.Handler {
	return probeAccess(a, projectParam)
}

// ProbeRootAccess is ProbeAccess for resources outside any project: each check
// must name a resource under a platform root, such as "channel/<id>" for a
// direct message. services/realtime asks it before placing a socket in a
// direct message's room.
func ProbeRootAccess(a *iam.Authorizer) func(http.Handler) http.Handler {
	return probeAccess(a, "")
}

func probeAccess(a *iam.Authorizer, projectParam string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := IAMPrincipalFrom(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			var projectID string
			if projectParam != "" {
				if projectID, err = urlUUID(r, projectParam, "project"); err != nil {
					presenter.Error(w, r, err)
					return
				}
			}
			buf, err := peekBodyLimit(r, maxAccessProbeBody)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			var body struct {
				Checks []AccessProbeCheck `json:"checks"`
			}
			if err := json.Unmarshal(buf, &body); err != nil || len(body.Checks) == 0 || len(body.Checks) > maxAccessProbeChecks {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "checks must hold between 1 and 200 action/resource pairs"))
				return
			}
			queries := make([]iam.Query, len(body.Checks))
			for i, c := range body.Checks {
				kind, _, inProject := iam.ParseResource(c.Resource)
				if inProject != projectID || c.Action == "" || (projectID == "" && kind == "") {
					presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "every check needs an action and a resource inside this project"))
					return
				}
				queries[i] = iam.Query{Action: c.Action, Resource: c.Resource}
			}
			if a == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
				return
			}
			results, err := a.AuthorizeMany(r.Context(), p, queries)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			allowed := make([]bool, len(results))
			for i, res := range results {
				allowed[i] = res.Allowed
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accessProbeKey{}, allowed)))
		})
	}
}
