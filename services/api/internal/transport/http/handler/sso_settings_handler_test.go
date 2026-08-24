package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/service/oidc"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

type fakeSSOSettingsService struct {
	config      oidc.AdminConfig
	updateErr   error
	lastUpdate  oidc.UpdateConfig
	lastActorID uuid.UUID
}

func (s *fakeSSOSettingsService) AdminConfig() oidc.AdminConfig { return s.config }

func (s *fakeSSOSettingsService) Update(_ context.Context, in oidc.UpdateConfig, actor uuid.UUID) (oidc.AdminConfig, error) {
	s.lastUpdate = in
	s.lastActorID = actor
	if s.updateErr != nil {
		return oidc.AdminConfig{}, s.updateErr
	}
	s.config = oidc.AdminConfig{
		Source:                          oidc.ConfigSourceDatabase,
		Enabled:                         in.Enabled,
		IssuerURL:                       in.IssuerURL,
		ClientID:                        in.ClientID,
		ClientSecretConfigured:          in.ClientSecret != "",
		Scopes:                          in.Scopes,
		RedirectURL:                     in.RedirectURL,
		DisplayName:                     in.DisplayName,
		UsernameClaim:                   in.UsernameClaim,
		LocalLoginEnabled:               in.LocalLoginEnabled,
		EncryptedSecretStorageAvailable: true,
	}
	return s.config, nil
}

func newSSOSettingsRouter(svc *fakeSSOSettingsService, userID uuid.UUID) chi.Router {
	h := handler.NewSSOSettingsHandler(svc)
	r := chi.NewRouter()
	if userID != uuid.Nil {
		r.Use(injectAuthClaimsMiddleware(userID.String()))
	}
	r.Get("/admin/settings/sso", h.Get)
	r.Patch("/admin/settings/sso", h.Update)
	return r
}

func TestSSOSettingsGet_ReturnsNonSecretConfiguration(t *testing.T) {
	svc := &fakeSSOSettingsService{config: oidc.AdminConfig{
		Source:                          oidc.ConfigSourceEnvironment,
		Enabled:                         true,
		IssuerURL:                       "https://id.example.com",
		ClientID:                        "paca",
		ClientSecretConfigured:          true,
		Scopes:                          []string{"openid", "profile"},
		RedirectURL:                     "https://paca.example.com/api/v1/auth/oidc/callback",
		DisplayName:                     "Company SSO",
		UsernameClaim:                   "preferred_username",
		LocalLoginEnabled:               true,
		EncryptedSecretStorageAvailable: true,
	}}
	r := newSSOSettingsRouter(svc, uuid.New())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/settings/sso", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"client_secret_configured":true`) {
		t.Fatalf("expected secret presence flag: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "client_secret_enc") || strings.Contains(w.Body.String(), `"client_secret":`) {
		t.Fatalf("response exposed a secret field: %s", w.Body.String())
	}
}

func TestSSOSettingsUpdate_MapsRequestAndActor(t *testing.T) {
	actor := uuid.New()
	svc := &fakeSSOSettingsService{}
	r := newSSOSettingsRouter(svc, actor)
	body := []byte(`{
		"enabled":true,
		"issuer_url":"https://id.example.com",
		"client_id":"paca",
		"client_secret":"replacement",
		"scopes":["openid","profile"],
		"redirect_url":"https://paca.example.com/api/v1/auth/oidc/callback",
		"display_name":"Company SSO",
		"username_claim":"preferred_username",
		"local_login_enabled":true
	}`)
	req := httptest.NewRequest(http.MethodPatch, "/admin/settings/sso", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if svc.lastActorID != actor {
		t.Fatalf("expected actor %s, got %s", actor, svc.lastActorID)
	}
	if svc.lastUpdate.ClientSecret != "replacement" || svc.lastUpdate.DisplayName != "Company SSO" || len(svc.lastUpdate.Scopes) != 2 {
		t.Fatalf("request was not mapped: %+v", svc.lastUpdate)
	}
	if strings.Contains(w.Body.String(), "replacement") {
		t.Fatalf("response exposed submitted client secret: %s", w.Body.String())
	}
}

func TestSSOSettingsUpdate_MapsStableErrorsWithoutProviderDetails(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"invalid config", oidc.ErrInvalidConfig, http.StatusBadRequest, "AUTH_SSO_CONFIG_INVALID"},
		{"provider", errors.Join(oidc.ErrProviderValidation, errors.New("dial tcp 10.0.0.1: secret detail")), http.StatusUnprocessableEntity, "AUTH_SSO_PROVIDER_VALIDATION_FAILED"},
		{"encryption", oidc.ErrEncryptionUnavailable, http.StatusServiceUnavailable, "AUTH_SSO_ENCRYPTION_UNAVAILABLE"},
		{"admin guard", oidc.ErrSSOAdminRequired, http.StatusConflict, "AUTH_SSO_ADMIN_REQUIRED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeSSOSettingsService{updateErr: tt.err}
			r := newSSOSettingsRouter(svc, uuid.New())
			req := httptest.NewRequest(http.MethodPatch, "/admin/settings/sso", bytes.NewBufferString(`{"enabled":false,"local_login_enabled":true}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}
			var body map[string]any
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["error_code"] != tt.wantCode {
				t.Fatalf("expected code %s, got %v", tt.wantCode, body["error_code"])
			}
			if strings.Contains(w.Body.String(), "10.0.0.1") {
				t.Fatalf("response leaked provider details: %s", w.Body.String())
			}
		})
	}
}

func TestSSOSettingsUpdate_RequiresAuthenticatedActor(t *testing.T) {
	svc := &fakeSSOSettingsService{}
	r := newSSOSettingsRouter(svc, uuid.Nil)
	req := httptest.NewRequest(http.MethodPatch, "/admin/settings/sso", bytes.NewBufferString(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
