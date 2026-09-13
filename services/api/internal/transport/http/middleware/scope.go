package middleware

import (
	"regexp"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
)

// This file is entirely about narrow-scope tokens — see
// domainauth.ScopeAnnotation's doc comment for the one that exists today
// and why it exists at all. It's kept separate from authn.go on purpose:
// authn.go already carries JWT verification, API-key auth, and agent-
// identity checks, and a reviewer auditing "what can a browser-extension
// token actually do" shouldn't have to read all of that to find out — this
// file, top to bottom, is the whole answer.
//
// enforceTokenScope is called from applyAuthn's shared tail (in authn.go),
// not mounted as its own r.Use() middleware. That's deliberate: the
// annotation-extension routes live nested inside the same shared
// /projects/{projectId} middleware stack as tasks, docs, sprints, and
// everything else project-scoped, so there's no isolated mount point to
// attach a standalone middleware to without restructuring that group — and
// a middleware someone has to remember to mount on the right subtree has
// the same "forgot to guard it" failure mode this file exists to close,
// just moved from "forgot to guard a new route" to "forgot to guard a new
// route group". Calling this from the one function every route's JWT check
// already funnels through means a future route is safe by construction,
// not by remembering.

// AnnotationExtensionPathPattern matches the exact set of routes the Paca
// browser extension's content script needs to call directly from a
// forwarded environment port: rotating its own domainauth.ScopeAnnotation
// session, resolving which project/environment/port-forward it's looking
// at, and the page-annotation CRUD itself. Shared by two gates that must
// stay in lockstep — router.go's corsMiddleware same-hostname exception
// (why a cross-origin browser request to these paths is allowed to read
// its response at all) and enforceTokenScope just below (why a
// ScopeAnnotation token is honored only on these paths, not any other
// route) — so it's defined once, here, rather than as two hand-synced
// copies.
//
// The path check matters as much as the Scope check: the forwarded port
// serves the *user's own dev app*, not code Paca controls, so any script
// running there — not just the extension's content script — can present
// whatever cookies the browser attaches. Scoping ScopeAnnotation tokens to
// this pattern means such a script can, at most, act on page annotations
// (and read its own port-forward's identity); without it, a token that
// merely happens to be valid would grant free, ambient access to every
// other endpoint the underlying user's role/permissions allow.
var AnnotationExtensionPathPattern = regexp.MustCompile(
	`^/api/v1/(?:` +
		`auth/annotation-refresh` +
		`|port-forwards/resolve` +
		`|projects/[^/]+/environments/[^/]+/port-forwards/[^/]+/annotations(?:/.*)?` +
		`)$`,
)

// enforceTokenScope reports whether claims may authenticate a request to
// path, given whatever restriction its Scope carries.
//
// The default case matters more than the two named ones: an unrecognized
// Scope value — a future scope this build of the server was never taught
// to restrict, or a token whose claims were altered after issuance in a
// way that produced a value nothing here expects — is rejected, not
// trusted. That's what keeps this function safe to extend later: adding a
// third scope means adding a case, not remembering to also update every
// place that currently checks `== domainauth.ScopeAnnotation` explicitly
// the way an earlier version of this check did.
func enforceTokenScope(claims *domainauth.Claims, path string) bool {
	switch claims.Scope {
	case "":
		// Full scope — every token Login/Refresh issued before this
		// concept existed, and every token they issue today unless
		// explicitly narrowed.
		return true
	case domainauth.ScopeAnnotation:
		return AnnotationExtensionPathPattern.MatchString(path)
	default:
		return false
	}
}
