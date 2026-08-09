package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// ---------------------------------------------------------------------------
// Minimal fake settings service
// ---------------------------------------------------------------------------

type fakeSettingsSvc struct {
	ws              *settingsdom.WorkspaceSettings
	updateColorsErr error

	// lastCompleteUpdatedBy/lastRemoveUpdatedBy record the updatedBy the
	// handler passed through, so tests can assert the acting user's ID
	// actually reaches the service rather than being silently dropped.
	lastCompleteUpdatedBy uuid.UUID
	lastRemoveUpdatedBy   uuid.UUID
}

func (f *fakeSettingsSvc) Get(context.Context) (*settingsdom.WorkspaceSettings, error) {
	if f.ws != nil {
		return f.ws, nil
	}
	return &settingsdom.WorkspaceSettings{}, nil
}

func (f *fakeSettingsSvc) InitiateImageUpload(context.Context, settingsdom.ImageSlot, string, string, int64, uuid.UUID) (*attachmentdom.UploadSession, error) {
	return &attachmentdom.UploadSession{FileID: uuid.New(), UploadURL: "https://fake/upload"}, nil
}

func (f *fakeSettingsSvc) CompleteImageUpload(_ context.Context, _ settingsdom.ImageSlot, _ uuid.UUID, updatedBy uuid.UUID) (*settingsdom.WorkspaceSettings, error) {
	f.lastCompleteUpdatedBy = updatedBy
	return &settingsdom.WorkspaceSettings{}, nil
}

func (f *fakeSettingsSvc) RemoveImage(_ context.Context, _ settingsdom.ImageSlot, updatedBy uuid.UUID) (*settingsdom.WorkspaceSettings, error) {
	f.lastRemoveUpdatedBy = updatedBy
	return &settingsdom.WorkspaceSettings{}, nil
}

func (f *fakeSettingsSvc) UpdateSettings(_ context.Context, brandName, light, dark *string, _ uuid.UUID) (*settingsdom.WorkspaceSettings, error) {
	if f.updateColorsErr != nil {
		return nil, f.updateColorsErr
	}
	return &settingsdom.WorkspaceSettings{BrandName: brandName, PrimaryColorLight: light, PrimaryColorDark: dark}, nil
}

var _ settingsdom.Service = (*fakeSettingsSvc)(nil)

// ---------------------------------------------------------------------------
// Router helper
// ---------------------------------------------------------------------------

// newSettingsRouter mounts GetBranding unauthenticated (as router.go does,
// under the public /v1 routes) and the admin write endpoints behind
// injectAuthClaimsMiddleware (reused from attachment_handler_test.go) only
// when authed is true — mirroring how router.go always gates them with
// httpmw.Authn + RequirePermissions(settings.write), never reachable
// unauthenticated in the real app.
func newSettingsRouter(svc settingsdom.Service, authed bool) chi.Router {
	h := handler.NewSettingsHandler(svc)
	r := chi.NewRouter()
	r.Get("/branding", h.GetBranding)

	r.Route("/admin/settings", func(r chi.Router) {
		if authed {
			r.Use(injectAuthClaimsMiddleware(uuid.New().String()))
		}
		r.Patch("/", h.UpdateSettings)
		r.Post("/logo/avatar/initiate-upload", h.InitiateLogoUpload)
		r.Post("/logo/avatar/complete-upload", h.CompleteLogoUpload)
		r.Delete("/logo/avatar", h.DeleteLogo)
		r.Post("/favicon/avatar/initiate-upload", h.InitiateFaviconUpload)
		r.Post("/favicon/avatar/complete-upload", h.CompleteFaviconUpload)
		r.Delete("/favicon/avatar", h.DeleteFavicon)
	})
	return r
}

func doSettingsRequest(t *testing.T, r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// GetBranding — public
// ---------------------------------------------------------------------------

func TestGetBranding_NoAuthRequired_ReturnsOK(t *testing.T) {
	light := "#5a9e1c"
	r := newSettingsRouter(&fakeSettingsSvc{ws: &settingsdom.WorkspaceSettings{PrimaryColorLight: &light}}, false)

	w := doSettingsRequest(t, r, http.MethodGet, "/branding", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for public branding read, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(light)) {
		t.Errorf("expected response to contain primary color %q, got %s", light, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// UpdateSettings
// ---------------------------------------------------------------------------

func TestUpdateSettings_NoAuth_Returns401(t *testing.T) {
	r := newSettingsRouter(&fakeSettingsSvc{}, false)

	w := doSettingsRequest(t, r, http.MethodPatch, "/admin/settings/", map[string]any{"primary_color_light": "#5a9e1c"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without claims, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSettings_InvalidHex_Returns400(t *testing.T) {
	r := newSettingsRouter(&fakeSettingsSvc{updateColorsErr: settingsdom.ErrInvalidColor}, true)

	w := doSettingsRequest(t, r, http.MethodPatch, "/admin/settings/", map[string]any{"primary_color_light": "not-a-color"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid color, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSettings_Valid_ReturnsOK(t *testing.T) {
	r := newSettingsRouter(&fakeSettingsSvc{}, true)

	w := doSettingsRequest(t, r, http.MethodPatch, "/admin/settings/", map[string]any{
		"primary_color_light": "#5a9e1c",
		"primary_color_dark":  "#9ed957",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSettings_BrandName_ReturnsOK(t *testing.T) {
	r := newSettingsRouter(&fakeSettingsSvc{}, true)

	w := doSettingsRequest(t, r, http.MethodPatch, "/admin/settings/", map[string]any{
		"brand_name": "My Workspace",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("My Workspace")) {
		t.Errorf("expected response to contain brand_name, got %s", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Logo/favicon upload — auth + body validation
// ---------------------------------------------------------------------------

func TestInitiateLogoUpload_NoAuth_Returns401(t *testing.T) {
	r := newSettingsRouter(&fakeSettingsSvc{}, false)

	w := doSettingsRequest(t, r, http.MethodPost, "/admin/settings/logo/avatar/initiate-upload",
		map[string]any{"file_name": "logo.png", "content_type": "image/png", "file_size": 1024})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without claims, got %d: %s", w.Code, w.Body.String())
	}
}

// SettingsHandler doesn't re-validate InitiateUploadRequest/
// CompleteAvatarUploadRequest fields itself (matching UserHandler's avatar
// endpoints, not AttachmentHandler's, which does inline-validate) — a blank
// file_name or absent file_id decodes fine and is left to the real
// attachment service to reject. What BindJSON does reject is a body that
// fails to decode at all, e.g. a non-UUID file_id.
func TestCompleteLogoUpload_MalformedFileID_Returns400(t *testing.T) {
	r := newSettingsRouter(&fakeSettingsSvc{}, true)

	w := doSettingsRequest(t, r, http.MethodPost, "/admin/settings/logo/avatar/complete-upload",
		map[string]any{"file_id": "not-a-uuid"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed file_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInitiateFaviconUpload_ValidBody_Returns201(t *testing.T) {
	r := newSettingsRouter(&fakeSettingsSvc{}, true)

	w := doSettingsRequest(t, r, http.MethodPost, "/admin/settings/favicon/avatar/initiate-upload",
		map[string]any{"file_name": "favicon.png", "content_type": "image/png", "file_size": 1024})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteFavicon_Authed_ReturnsOK(t *testing.T) {
	r := newSettingsRouter(&fakeSettingsSvc{}, true)

	w := doSettingsRequest(t, r, http.MethodDelete, "/admin/settings/favicon/avatar", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// newSettingsRouterWithSubject is like newSettingsRouter(svc, true) but lets
// the test control the injected claims subject, so it can assert the
// service received that exact user ID as updatedBy.
func newSettingsRouterWithSubject(svc settingsdom.Service, sub string) chi.Router {
	h := handler.NewSettingsHandler(svc)
	r := chi.NewRouter()
	r.Route("/admin/settings", func(r chi.Router) {
		r.Use(injectAuthClaimsMiddleware(sub))
		r.Post("/logo/avatar/complete-upload", h.CompleteLogoUpload)
		r.Delete("/favicon/avatar", h.DeleteFavicon)
	})
	return r
}

// TestCompleteLogoUpload_PassesActingUserIDToService guards against the bug
// flagged in review: the handler extracted the acting user ID but never
// forwarded it to CompleteImageUpload, so uploaded logos/favicons never
// recorded who uploaded them (UpdatedBy stayed nil/stale).
func TestCompleteLogoUpload_PassesActingUserIDToService(t *testing.T) {
	svc := &fakeSettingsSvc{}
	userID := uuid.New()
	r := newSettingsRouterWithSubject(svc, userID.String())

	w := doSettingsRequest(t, r, http.MethodPost, "/admin/settings/logo/avatar/complete-upload",
		map[string]any{"file_id": uuid.New().String()})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if svc.lastCompleteUpdatedBy != userID {
		t.Errorf("expected CompleteImageUpload to receive acting user %s, got %s", userID, svc.lastCompleteUpdatedBy)
	}
}

// TestDeleteFavicon_PassesActingUserIDToService is RemoveImage's counterpart
// to TestCompleteLogoUpload_PassesActingUserIDToService above.
func TestDeleteFavicon_PassesActingUserIDToService(t *testing.T) {
	svc := &fakeSettingsSvc{}
	userID := uuid.New()
	r := newSettingsRouterWithSubject(svc, userID.String())

	w := doSettingsRequest(t, r, http.MethodDelete, "/admin/settings/favicon/avatar", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if svc.lastRemoveUpdatedBy != userID {
		t.Errorf("expected RemoveImage to receive acting user %s, got %s", userID, svc.lastRemoveUpdatedBy)
	}
}
