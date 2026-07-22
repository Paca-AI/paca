package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/bundledskills"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

type fakePluginLister struct {
	plugins []*plugindom.Plugin
	err     error
}

func (f *fakePluginLister) ListPlugins(_ context.Context) ([]*plugindom.Plugin, error) {
	return f.plugins, f.err
}

func writeSkillFile(t *testing.T, dir, pluginName, skillName, content string) {
	t.Helper()
	path := filepath.Join(dir, pluginName, skillName, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func decodeSkills(t *testing.T, w *httptest.ResponseRecorder) []dto.SkillResponse {
	t.Helper()
	var envelope struct {
		Data dto.SkillListResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return envelope.Data.Skills
}

func hasSkillNamed(skills []dto.SkillResponse, name string) bool {
	for _, s := range skills {
		if s.Name == name {
			return true
		}
	}
	return false
}

func TestSkillsHandler_ListSkills_MergesPluginSkillsIntoCLIFlavor(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "com.paca.dashboard", "paca-dashboard-builder", "content")

	svc := &fakePluginLister{plugins: []*plugindom.Plugin{
		{
			Name:    "com.paca.dashboard",
			Enabled: true,
			Manifest: plugindom.PluginManifest{
				Skills: &plugindom.SkillsManifest{
					BaseURL: "/plugins-skills/com.paca.dashboard",
					Names:   []string{"paca-dashboard-builder"},
				},
			},
		},
	}}
	h := handler.NewSkillsHandler(svc, dir)

	w := httptest.NewRecorder()
	h.ListSkills(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/skills", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	skills := decodeSkills(t, w)
	bundledCount := len(bundledskills.List(bundledskills.TargetCLI))
	if len(skills) != bundledCount+1 {
		t.Fatalf("expected %d skills (bundled + 1 plugin), got %d", bundledCount+1, len(skills))
	}
	if !hasSkillNamed(skills, "paca-dashboard-builder") {
		t.Errorf("expected plugin skill paca-dashboard-builder in response, got %+v", skills)
	}
}

func TestSkillsHandler_ListSkills_AgentFlavorExcludesPluginSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "com.paca.dashboard", "paca-dashboard-builder", "content")

	svc := &fakePluginLister{plugins: []*plugindom.Plugin{
		{
			Name:    "com.paca.dashboard",
			Enabled: true,
			Manifest: plugindom.PluginManifest{
				Skills: &plugindom.SkillsManifest{
					BaseURL: "/plugins-skills/com.paca.dashboard",
					Names:   []string{"paca-dashboard-builder"},
				},
			},
		},
	}}
	h := handler.NewSkillsHandler(svc, dir)

	w := httptest.NewRecorder()
	h.ListSkills(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/skills?target=agent", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	skills := decodeSkills(t, w)
	bundledCount := len(bundledskills.List(bundledskills.TargetAgent))
	if len(skills) != bundledCount {
		t.Fatalf("expected agent flavor to stay bundled-only (%d skills), got %d", bundledCount, len(skills))
	}
	if hasSkillNamed(skills, "paca-dashboard-builder") {
		t.Error("expected agent flavor to exclude plugin-contributed skills, but found paca-dashboard-builder")
	}
}

func TestSkillsHandler_ListSkills_FallsBackToBundledOnPluginListError(t *testing.T) {
	svc := &fakePluginLister{err: errors.New("db unavailable")}
	h := handler.NewSkillsHandler(svc, t.TempDir())

	w := httptest.NewRecorder()
	h.ListSkills(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/skills", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even when the plugin lister errors, got %d: %s", w.Code, w.Body.String())
	}
	skills := decodeSkills(t, w)
	bundledCount := len(bundledskills.List(bundledskills.TargetCLI))
	if len(skills) != bundledCount {
		t.Fatalf("expected bundled-only fallback (%d skills), got %d", bundledCount, len(skills))
	}
}

func TestSkillsHandler_ListSkills_NoPluginServiceConfigured(t *testing.T) {
	h := handler.NewSkillsHandler(nil, "")

	w := httptest.NewRecorder()
	h.ListSkills(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/skills", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	skills := decodeSkills(t, w)
	bundledCount := len(bundledskills.List(bundledskills.TargetCLI))
	if len(skills) != bundledCount {
		t.Fatalf("expected bundled-only (%d skills) with no plugin service configured, got %d", bundledCount, len(skills))
	}
}
