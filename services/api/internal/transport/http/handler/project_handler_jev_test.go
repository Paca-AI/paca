package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// fakeJevConfigSvc records the arguments of the last UpdateJevConfig call.
type fakeJevConfigSvc struct {
	called                 bool
	apiKey, baseURL, model *string
	result                 *projectdom.Project
	err                    error
}

func (f *fakeJevConfigSvc) UpdateJevConfig(_ context.Context, projectID uuid.UUID, apiKey, baseURL, model *string) (*projectdom.Project, error) {
	f.called = true
	f.apiKey, f.baseURL, f.model = apiKey, baseURL, model
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &projectdom.Project{ID: projectID}, nil
}

func newJevConfigRouter(jevSvc *fakeJevConfigSvc) chi.Router {
	r := chi.NewRouter()
	r.Use(adminClaimsMiddleware())
	var opts []handler.ProjectHandlerOption
	if jevSvc != nil {
		opts = append(opts, handler.WithProjectJevConfigService(jevSvc, nil))
	}
	h := handler.NewProjectHandler(&mockProjectSvc{}, adminAuthorizer(), opts...)
	r.Patch("/projects/{projectId}/jev-config", h.UpdateJevConfig)
	return r
}

func TestUpdateJevConfig_Success(t *testing.T) {
	id := uuid.New()
	svc := &fakeJevConfigSvc{result: &projectdom.Project{
		ID: id, JevAPIKeySecret: "stored-secret", JevBaseURL: "https://jev.example", JevModel: "m1",
	}}
	r := newJevConfigRouter(svc)

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/jev-config", id),
		jsonBody(t, map[string]any{"api_key": "k", "base_url": "https://jev.example"}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !svc.called || svc.apiKey == nil || *svc.apiKey != "k" || svc.baseURL == nil || svc.model != nil {
		t.Fatalf("unexpected service call: key=%v base=%v model=%v", svc.apiKey, svc.baseURL, svc.model)
	}

	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data["configured"] != true || env.Data["base_url"] != "https://jev.example" || env.Data["model"] != "m1" {
		t.Errorf("unexpected response: %v", env.Data)
	}
	// The API key (encrypted or not) must never be echoed back.
	for k, v := range env.Data {
		if v == "stored-secret" {
			t.Errorf("response field %q leaks the stored key", k)
		}
	}
	var typed dto.ProjectJevConfigResponse
	b, _ := json.Marshal(env.Data)
	if err := json.Unmarshal(b, &typed); err != nil || !typed.Configured {
		t.Errorf("response does not match ProjectJevConfigResponse: %v %+v", err, typed)
	}
}

func TestUpdateJevConfig_ServiceNotWired(t *testing.T) {
	r := newJevConfigRouter(nil)
	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/jev-config", uuid.New()),
		jsonBody(t, map[string]any{"api_key": "k"}))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when no Jev config service is wired, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateJevConfig_InvalidProjectID(t *testing.T) {
	r := newJevConfigRouter(&fakeJevConfigSvc{})
	w := do(t, r, http.MethodPatch, "/projects/not-a-uuid/jev-config", jsonBody(t, map[string]any{}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateJevConfig_ProjectNotFound(t *testing.T) {
	r := newJevConfigRouter(&fakeJevConfigSvc{err: projectdom.ErrNotFound})
	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/jev-config", uuid.New()),
		jsonBody(t, map[string]any{"api_key": "k"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateJevConfig_MalformedBody(t *testing.T) {
	svc := &fakeJevConfigSvc{}
	r := newJevConfigRouter(svc)
	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/jev-config", uuid.New()),
		jsonBody(t, map[string]any{"api_key": 123}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if svc.called {
		t.Error("service must not be called for a malformed body")
	}
}
