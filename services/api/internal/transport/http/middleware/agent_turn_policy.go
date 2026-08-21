package middleware

import (
	"net/http"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// EnforceAgentTurnReadOnly is the API-side backstop for Paca MCP calls made
// under an authoritative private turn. The first version has no
// proposal/preview/apply workflow, so every project mutation is rejected even
// if the long-lived agent role would otherwise grant it. The hosted private
// runtime additionally receives no long-lived Paca credential at all.
func EnforceAgentTurnReadOnly() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Agent-Turn-ID") != "" {
				if _, isAgent := AgentIDFromRequest(r); isAgent && !safeAgentTurnMethod(r.Method) {
					presenter.Error(w, r, apierr.New(apierr.CodeForbidden,
						"private agent turns are read-only; task and project mutations require explicit human confirmation"))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func safeAgentTurnMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
