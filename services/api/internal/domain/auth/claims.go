// Package auth holds JWT claims and the auth service contract.
package auth

import "github.com/golang-jwt/jwt/v5"

// ScopeAnnotation marks a token minted specifically for the browser
// extension's page-annotation flow (see AuthHandler.AnnotationRefresh in
// services/api's handler package, and apps/extension/README.md). Such a
// token is a real, validly-signed credential for a real user — Login mints
// it from the exact same identity as the main session — but
// middleware.applyAuthn only ever honors it against
// middleware.AnnotationExtensionPathPattern, never as a substitute for a
// full-scope access token elsewhere. This exists because the extension's
// content script runs on a forwarded environment preview that shares the
// Paca app's hostname but not necessarily its scheme, and a cross-scheme
// request is "cross-site" under modern browsers' Schemeful Same-Site
// rules — so the main access_token/refresh_token (SameSite=Lax/Strict)
// can't reliably reach the API from there. This narrower pair uses
// SameSite=None instead, which only a token this restricted in scope
// should ever be issued with.
const ScopeAnnotation = "annotation"

// Claims is the payload embedded in every JWT issued by the service.
type Claims struct {
	jwt.RegisteredClaims
	Username string `json:"username"`
	Role     string `json:"role"`
	// Kind distinguishes "access" from "refresh" tokens.
	Kind string `json:"kind"`
	// FamilyID links all tokens that originate from the same login session.
	// Used for refresh-token rotation and reuse detection.
	FamilyID string `json:"fid"`
	// RememberMe is true when the user opted into a persistent session.
	// Propagated through refresh-token rotation to preserve the original
	// session lifetime preference.
	RememberMe bool `json:"rme"`
	// MustChangePassword is true when the user is required to change their
	// password before accessing any other endpoint (e.g. after admin creation
	// or admin password reset).
	MustChangePassword bool `json:"mcp,omitempty"`
	// AgentID is optionally set when authenticating via agent API key with
	// X-Agent-ID header to identify which agent is performing the action.
	AgentID *string `json:"agent_id,omitempty"`
	// Scope narrows what an access/refresh token may be used for. Empty
	// (the zero value) means full scope — every token Login/Refresh have
	// ever issued, usable on any endpoint the subject's role/permissions
	// allow. ScopeAnnotation is the one narrower value that exists today.
	Scope string `json:"scope,omitempty"`
}
