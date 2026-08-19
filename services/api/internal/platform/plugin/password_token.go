package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// PasswordSetTokenIssuer issues single-use password-set tokens for the
// paca.password_set_token_issue host function.
type PasswordSetTokenIssuer interface {
	IssuePasswordSetToken(ctx context.Context, userID uuid.UUID) (token string, expiresAt time.Time, err error)
}

// PasswordSetTokenIssuerFunc adapts a plain function to PasswordSetTokenIssuer,
// so bootstrap wiring can close over the user service's existing method
// without a dedicated adapter type.
type PasswordSetTokenIssuerFunc func(ctx context.Context, userID uuid.UUID) (string, time.Time, error)

// IssuePasswordSetToken calls f.
func (f PasswordSetTokenIssuerFunc) IssuePasswordSetToken(ctx context.Context, userID uuid.UUID) (string, time.Time, error) {
	return f(ctx, userID)
}

// passwordSetTokenIssueRequest is the JSON payload a plugin sends to
// paca.password_set_token_issue.
type passwordSetTokenIssueRequest struct {
	UserID string `json:"user_id"`
}

// passwordSetTokenIssueResult is the JSON response: Token/ExpiresAt on
// success, Error on failure (never both).
type passwordSetTokenIssueResult struct {
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

// registerPasswordSetTokenFunction adds paca.password_set_token_issue to the
// host module builder. A password-set token is a bearer credential —
// whoever holds the raw value can set the named user's password — so unlike
// user.created's other denormalized fields (username, full name, email) it
// is never embedded in the event payload every subscriber receives (see
// usersvc.publishUserCreatedEvent). Instead, a plugin that decides, having
// received a user.created event, that it wants to deliver an invite link,
// calls back into this host function on demand and is minted a token scoped
// to exactly the user_id it asks for. Restricted to plugins that explicitly
// declare the "users.password_set_token.issue" permission in their
// manifest, the same convention as email.send.
func (r *Runtime) registerPasswordSetTokenFunction(b wazero.HostModuleBuilder, p plugindom.Plugin) {
	// paca.password_set_token_issue(reqPtr, reqLen, resPtrPtr, resLenPtr)
	// Request JSON: passwordSetTokenIssueRequest.
	// Response JSON: passwordSetTokenIssueResult.
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			writeBack := func(ptrLen []uint64) {
				m.Memory().WriteUint32Le(uint32(stack[2]), uint32(ptrLen[0]))
				m.Memory().WriteUint32Le(uint32(stack[3]), uint32(ptrLen[1]))
			}
			writeErr := func(msg string) {
				writeBack(writeJSONResult(m, passwordSetTokenIssueResult{Error: msg}))
			}

			if !hasPermission(p.Manifest.Permissions, "users.password_set_token.issue") {
				writeErr("password_set_token_issue: plugin manifest does not declare the users.password_set_token.issue permission")
				return
			}
			if r.services.PasswordSetTokenIssuer == nil {
				writeErr("password_set_token_issue: not configured")
				return
			}

			reqBytes, err := readFromMemory(m, stack[0], stack[1])
			if err != nil {
				writeErr("password_set_token_issue: read request: " + err.Error())
				return
			}
			var req passwordSetTokenIssueRequest
			if err := json.Unmarshal(reqBytes, &req); err != nil {
				writeErr("password_set_token_issue: decode request: " + err.Error())
				return
			}
			userID, err := uuid.Parse(req.UserID)
			if err != nil {
				writeErr("password_set_token_issue: invalid user_id")
				return
			}

			token, expiresAt, err := r.services.PasswordSetTokenIssuer.IssuePasswordSetToken(ctx, userID)
			if err != nil {
				// Only the well-known "no such user" case gets a specific
				// message; anything else (e.g. a repository/driver error)
				// is collapsed to a generic message so internal DB detail
				// never reaches the plugin.
				if errors.Is(err, userdom.ErrNotFound) {
					writeErr("password_set_token_issue: user not found")
				} else {
					writeErr("password_set_token_issue: failed to issue token")
				}
				return
			}
			writeBack(writeJSONResult(m, passwordSetTokenIssueResult{
				Token:     token,
				ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
			}))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64}, nil).
		Export("password_set_token_issue")
}
