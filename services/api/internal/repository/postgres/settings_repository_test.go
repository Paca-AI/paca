package postgres

import (
	"testing"
	"time"
)

func TestWorkspaceSettingsToEntityMapsOIDCSettings(t *testing.T) {
	issuer := "https://id.example.com/realms/company/"
	clientID := "paca"
	secretEnc := "encrypted-client-secret"
	scopes := "openid,profile,email"
	redirectURL := "https://paca.example.com/api/v1/auth/oidc/callback"
	displayName := "Company SSO"
	usernameClaim := "preferred_username"

	got, err := workspaceSettingsToEntity(&workspaceSettingsRecord{
		OIDCConfigured:      true,
		OIDCEnabled:         true,
		OIDCIssuerURL:       &issuer,
		OIDCClientID:        &clientID,
		OIDCClientSecretEnc: &secretEnc,
		OIDCScopes:          &scopes,
		OIDCRedirectURL:     &redirectURL,
		OIDCDisplayName:     &displayName,
		OIDCUsernameClaim:   &usernameClaim,
		LocalLoginEnabled:   false,
		UpdatedAt:           time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("workspaceSettingsToEntity: %v", err)
	}
	if !got.OIDCConfigured || !got.OIDCEnabled || got.LocalLoginEnabled {
		t.Fatalf("unexpected OIDC switches: %+v", got)
	}
	if got.OIDCIssuerURL == nil || *got.OIDCIssuerURL != issuer {
		t.Fatalf("issuer not mapped exactly: %v", got.OIDCIssuerURL)
	}
	if got.OIDCClientSecretEnc == nil || *got.OIDCClientSecretEnc != secretEnc {
		t.Fatalf("encrypted client secret not mapped: %v", got.OIDCClientSecretEnc)
	}
	if got.OIDCScopes == nil || *got.OIDCScopes != scopes ||
		got.OIDCRedirectURL == nil || *got.OIDCRedirectURL != redirectURL ||
		got.OIDCDisplayName == nil || *got.OIDCDisplayName != displayName ||
		got.OIDCUsernameClaim == nil || *got.OIDCUsernameClaim != usernameClaim ||
		got.OIDCClientID == nil || *got.OIDCClientID != clientID {
		t.Fatalf("OIDC configuration mapping incomplete: %+v", got)
	}
}
