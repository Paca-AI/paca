package middleware

import (
	"mime"
	"net/http"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// RequireJSONContentType rejects any request whose Content-Type isn't
// application/json (parameters like charset are ignored).
//
// Apply this to every route that decodes a JSON body via plain
// encoding/json (BindJSON or a handler's own json.NewDecoder) and is
// reachable using a SameSite=None credential (see
// domainauth.ScopeAnnotation) — annotation.go's write endpoints being the
// case that motivated this. Those decoders happily parse JSON bytes no
// matter what Content-Type header arrived with them, which matters because
// three Content-Type values (application/x-www-form-urlencoded,
// multipart/form-data, text/plain) are CORS-safelisted: a cross-site
// request using one of them counts as a "simple request" under the Fetch
// spec and skips the CORS preflight entirely, reaching the handler —
// cookie attached — regardless of corsMiddleware's same-hostname check,
// which only ever gates a *preflighted* request. A SameSite=Lax/Strict
// cookie wouldn't even be attached to that cross-site request in the first
// place, but a SameSite=None one is attached exactly as designed.
//
// Requiring application/json (never CORS-safelisted on its own) forces
// every cross-origin write through preflight, where corsMiddleware's
// same-hostname check (or CORS_ORIGINS) is the thing actually doing access
// control.
func RequireJSONContentType() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "Content-Type must be application/json"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
