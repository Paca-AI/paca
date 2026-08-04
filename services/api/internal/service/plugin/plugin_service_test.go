// Package pluginsvc_test contains unit tests for the plugin service layer.
// Tests use in-memory fake repositories and do not require any infrastructure.
package pluginsvc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	pluginsvc "github.com/Paca-AI/api/internal/service/plugin"
)

// ---------------------------------------------------------------------------
// Fake repository
// ---------------------------------------------------------------------------

type fakePluginRepo struct {
	mu        sync.RWMutex
	plugins   map[uuid.UUID]*plugindom.Plugin
	nameIndex map[string]uuid.UUID
	settings  map[string]*plugindom.PluginExtensionSetting // key: pluginID+":"+extensionPoint
}

func newFakePluginRepo() *fakePluginRepo {
	return &fakePluginRepo{
		plugins:   make(map[uuid.UUID]*plugindom.Plugin),
		nameIndex: make(map[string]uuid.UUID),
		settings:  make(map[string]*plugindom.PluginExtensionSetting),
	}
}

func settingKey(pluginID uuid.UUID, ep string) string {
	return pluginID.String() + ":" + ep
}

func (r *fakePluginRepo) List(_ context.Context) ([]*plugindom.Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*plugindom.Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (r *fakePluginRepo) FindByID(_ context.Context, id uuid.UUID) (*plugindom.Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	if !ok {
		return nil, plugindom.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (r *fakePluginRepo) FindByName(_ context.Context, name string) (*plugindom.Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.nameIndex[name]
	if !ok {
		return nil, plugindom.ErrNotFound
	}
	p := r.plugins[id]
	cp := *p
	return &cp, nil
}

func (r *fakePluginRepo) Create(_ context.Context, p *plugindom.Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.nameIndex[p.Name]; exists {
		return plugindom.ErrNameTaken
	}
	cp := *p
	r.plugins[p.ID] = &cp
	r.nameIndex[p.Name] = p.ID
	return nil
}

func (r *fakePluginRepo) Update(_ context.Context, p *plugindom.Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.plugins[p.ID]; !ok {
		return plugindom.ErrNotFound
	}
	cp := *p
	r.plugins[p.ID] = &cp
	return nil
}

func (r *fakePluginRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[id]
	if !ok {
		return plugindom.ErrNotFound
	}
	delete(r.nameIndex, p.Name)
	delete(r.plugins, id)
	return nil
}

func (r *fakePluginRepo) ListSettings(_ context.Context, pluginID uuid.UUID) ([]*plugindom.PluginExtensionSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*plugindom.PluginExtensionSetting, 0)
	for _, s := range r.settings {
		if s.PluginID == pluginID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakePluginRepo) ListSettingsForPlugins(_ context.Context, pluginIDs []uuid.UUID) ([]*plugindom.PluginExtensionSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(pluginIDs) == 0 {
		return []*plugindom.PluginExtensionSetting{}, nil
	}

	idSet := make(map[uuid.UUID]struct{}, len(pluginIDs))
	for _, id := range pluginIDs {
		idSet[id] = struct{}{}
	}

	out := make([]*plugindom.PluginExtensionSetting, 0)
	for _, s := range r.settings {
		if _, ok := idSet[s.PluginID]; ok {
			cp := *s
			out = append(out, &cp)
		}
	}

	return out, nil
}

func (r *fakePluginRepo) UpsertSetting(_ context.Context, setting *plugindom.PluginExtensionSetting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := settingKey(setting.PluginID, setting.ExtensionPoint)
	cp := *setting
	r.settings[key] = &cp
	return nil
}

func (r *fakePluginRepo) FindByCapability(_ context.Context, _ string) ([]*plugindom.Plugin, error) {
	// For test purposes, return empty slice; capabilities not implemented in fake repo
	return []*plugindom.Plugin{}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newSvc(t *testing.T) (*pluginsvc.Service, *fakePluginRepo) {
	t.Helper()
	repo := newFakePluginRepo()
	return pluginsvc.New(repo), repo
}

func seedPlugin(t *testing.T, svc *pluginsvc.Service, name string, enabled bool) *plugindom.Plugin {
	t.Helper()
	p, err := svc.InstallPlugin(context.Background(), plugindom.InstallInput{
		Name:    name,
		Version: "1.0.0",
		Manifest: plugindom.PluginManifest{
			ID:          name,
			DisplayName: name,
			Version:     "1.0.0",
		},
		Enabled: enabled,
	})
	if err != nil {
		t.Fatalf("seedPlugin: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// ListPlugins
// ---------------------------------------------------------------------------

func TestListPlugins_Empty(t *testing.T) {
	svc, _ := newSvc(t)
	got, err := svc.ListPlugins(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %d items", len(got))
	}
}

func TestListPlugins_ReturnsAll(t *testing.T) {
	svc, _ := newSvc(t)
	seedPlugin(t, svc, "com.paca.checklist", true)
	seedPlugin(t, svc, "com.paca.bdd", true)

	got, err := svc.ListPlugins(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// InstallPlugin
// ---------------------------------------------------------------------------

func TestInstallPlugin_Success(t *testing.T) {
	svc, _ := newSvc(t)
	p, err := svc.InstallPlugin(context.Background(), plugindom.InstallInput{
		Name:    "com.paca.test",
		Version: "0.1.0",
		Manifest: plugindom.PluginManifest{
			ID:      "com.paca.test",
			Version: "0.1.0",
		},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID == uuid.Nil {
		t.Error("expected non-nil ID")
	}
	if p.Name != "com.paca.test" {
		t.Errorf("expected name %q, got %q", "com.paca.test", p.Name)
	}
	if p.InstalledAt.IsZero() {
		t.Error("expected InstalledAt to be set")
	}
}

func TestInstallPlugin_EmptyName_ReturnsError(t *testing.T) {
	svc, _ := newSvc(t)
	_, err := svc.InstallPlugin(context.Background(), plugindom.InstallInput{
		Name:    "",
		Version: "1.0.0",
	})
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestInstallPlugin_IncompatibleHostVersion_ReturnsError(t *testing.T) {
	repo := newFakePluginRepo()
	svc := pluginsvc.New(repo).WithHostVersion("1.0.0")

	_, err := svc.InstallPlugin(context.Background(), plugindom.InstallInput{
		Name:    "com.paca.toonew",
		Version: "1.0.0",
		Manifest: plugindom.PluginManifest{
			ID:             "com.paca.toonew",
			Version:        "1.0.0",
			MinCoreVersion: "2.0.0",
		},
	})
	if err == nil {
		t.Fatal("expected error installing a plugin that requires a newer host version, got nil")
	}
}

func TestInstallPlugin_CompatibleHostVersion_Succeeds(t *testing.T) {
	repo := newFakePluginRepo()
	svc := pluginsvc.New(repo).WithHostVersion("2.0.0")

	p, err := svc.InstallPlugin(context.Background(), plugindom.InstallInput{
		Name:    "com.paca.compatible",
		Version: "1.0.0",
		Manifest: plugindom.PluginManifest{
			ID:             "com.paca.compatible",
			Version:        "1.0.0",
			MinCoreVersion: "2.0.0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "com.paca.compatible" {
		t.Errorf("expected plugin to be installed, got %q", p.Name)
	}
}

func TestInstallPlugin_UnsetHostVersion_NeverBlocks(t *testing.T) {
	// The default Service (no WithHostVersion call) must not reject any
	// manifest, matching every other existing test in this file that
	// constructs a Service via newSvc/pluginsvc.New without opting in.
	svc, _ := newSvc(t)

	_, err := svc.InstallPlugin(context.Background(), plugindom.InstallInput{
		Name:    "com.paca.anyversion",
		Version: "1.0.0",
		Manifest: plugindom.PluginManifest{
			ID:             "com.paca.anyversion",
			Version:        "1.0.0",
			MinCoreVersion: "99.0.0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCheckHostCompatibility_UsableStandaloneBeforeSideEffects exercises
// CheckHostCompatibility directly (not via InstallPlugin/UpdatePlugin),
// mirroring how UpgradeMarketplacePlugin's handler must call it: as a
// pre-flight check, before downloading artifacts, running migrations, or
// touching the runtime, so an incompatible upgrade never runs any of that
// before being rejected.
func TestCheckHostCompatibility_UsableStandaloneBeforeSideEffects(t *testing.T) {
	repo := newFakePluginRepo()
	svc := pluginsvc.New(repo).WithHostVersion("1.0.0")

	err := svc.CheckHostCompatibility(plugindom.PluginManifest{
		ID:             "com.paca.toonew",
		Version:        "2.0.0",
		MinCoreVersion: "2.0.0",
	})
	if err == nil {
		t.Fatal("expected error for a manifest requiring a newer host version, got nil")
	}

	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *apierr.Error, got %T", err)
	}
	if apiErr.Code != apierr.CodePluginIncompatibleHostVersion {
		t.Errorf("expected code %q, got %q", apierr.CodePluginIncompatibleHostVersion, apiErr.Code)
	}
	if apiErr.Details["required_version"] != "2.0.0" {
		t.Errorf("expected required_version detail %q, got %q", "2.0.0", apiErr.Details["required_version"])
	}
	if apiErr.Details["host_version"] != "1.0.0" {
		t.Errorf("expected host_version detail %q, got %q", "1.0.0", apiErr.Details["host_version"])
	}

	if err := svc.CheckHostCompatibility(plugindom.PluginManifest{
		ID:             "com.paca.compatible",
		Version:        "1.0.0",
		MinCoreVersion: "1.0.0",
	}); err != nil {
		t.Fatalf("unexpected error for a compatible manifest: %v", err)
	}
}

func TestInstallPlugin_DuplicateName_ReturnsErrNameTaken(t *testing.T) {
	svc, _ := newSvc(t)
	seedPlugin(t, svc, "com.paca.dup", true)

	_, err := svc.InstallPlugin(context.Background(), plugindom.InstallInput{
		Name:     "com.paca.dup",
		Version:  "2.0.0",
		Manifest: plugindom.PluginManifest{ID: "com.paca.dup", Version: "2.0.0"},
	})
	if !errors.Is(err, plugindom.ErrNameTaken) {
		t.Errorf("expected ErrNameTaken, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// UpdatePlugin
// ---------------------------------------------------------------------------

func TestUpdatePlugin_Version(t *testing.T) {
	svc, _ := newSvc(t)
	p := seedPlugin(t, svc, "com.paca.update", true)

	newVersion := "2.0.0"
	updated, err := svc.UpdatePlugin(context.Background(), p.ID, plugindom.UpdateInput{
		Version: &newVersion,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Version != newVersion {
		t.Errorf("expected version %q, got %q", newVersion, updated.Version)
	}
}

func TestUpdatePlugin_Disable(t *testing.T) {
	svc, _ := newSvc(t)
	p := seedPlugin(t, svc, "com.paca.disable", true)

	disabled := false
	updated, err := svc.UpdatePlugin(context.Background(), p.ID, plugindom.UpdateInput{
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Enabled {
		t.Error("expected plugin to be disabled")
	}
}

func TestUpdatePlugin_UpdatedAt_Advances(t *testing.T) {
	svc, _ := newSvc(t)
	p := seedPlugin(t, svc, "com.paca.ts", true)
	before := p.UpdatedAt

	time.Sleep(time.Millisecond)
	newV := "1.0.1"
	updated, err := svc.UpdatePlugin(context.Background(), p.ID, plugindom.UpdateInput{Version: &newV})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated.UpdatedAt.After(before) {
		t.Error("expected UpdatedAt to advance after update")
	}
}

func TestUpdatePlugin_IncompatibleHostVersion_ReturnsError(t *testing.T) {
	repo := newFakePluginRepo()
	svc := pluginsvc.New(repo).WithHostVersion("1.0.0")
	p := seedPlugin(t, svc, "com.paca.upgrade-gate", true)

	newManifest := plugindom.PluginManifest{
		ID:             "com.paca.upgrade-gate",
		Version:        "2.0.0",
		MinCoreVersion: "5.0.0",
	}
	_, err := svc.UpdatePlugin(context.Background(), p.ID, plugindom.UpdateInput{
		Manifest: &newManifest,
	})
	if err == nil {
		t.Fatal("expected error updating to a manifest that requires a newer host version, got nil")
	}
}

func TestUpdatePlugin_NotFound(t *testing.T) {
	svc, _ := newSvc(t)
	newV := "1.0.0"
	_, err := svc.UpdatePlugin(context.Background(), uuid.New(), plugindom.UpdateInput{Version: &newV})
	if !errors.Is(err, plugindom.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdatePlugin_NoFields_Idempotent(t *testing.T) {
	svc, _ := newSvc(t)
	p := seedPlugin(t, svc, "com.paca.noop", true)
	updated, err := svc.UpdatePlugin(context.Background(), p.ID, plugindom.UpdateInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != p.Name {
		t.Errorf("expected name unchanged, got %q", updated.Name)
	}
}

// ---------------------------------------------------------------------------
// DeletePlugin
// ---------------------------------------------------------------------------

func TestDeletePlugin_Success(t *testing.T) {
	svc, _ := newSvc(t)
	p := seedPlugin(t, svc, "com.paca.delete", true)

	if err := svc.DeletePlugin(context.Background(), p.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	plugins, _ := svc.ListPlugins(context.Background())
	for _, pl := range plugins {
		if pl.ID == p.ID {
			t.Error("expected plugin to be deleted but it still exists")
		}
	}
}

func TestDeletePlugin_NotFound(t *testing.T) {
	svc, _ := newSvc(t)
	err := svc.DeletePlugin(context.Background(), uuid.New())
	if !errors.Is(err, plugindom.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// UpdateExtensionSetting / ListExtensionSettings
// ---------------------------------------------------------------------------

func TestUpdateExtensionSetting_CreatesEntry(t *testing.T) {
	svc, _ := newSvc(t)
	p := seedPlugin(t, svc, "com.paca.pref", true)

	setting, err := svc.UpdateExtensionSetting(context.Background(), plugindom.UpdateExtensionSettingInput{
		PluginID:       p.ID,
		ExtensionPoint: "task.detail.section",
		Settings:       plugindom.ExtensionSettingData{Order: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setting.ID == uuid.Nil {
		t.Error("expected non-nil setting ID")
	}
	if setting.ExtensionPoint != "task.detail.section" {
		t.Errorf("unexpected extension point: %q", setting.ExtensionPoint)
	}
}

func TestUpdateExtensionSetting_Upsert_OverwritesExisting(t *testing.T) {
	svc, _ := newSvc(t)
	p := seedPlugin(t, svc, "com.paca.pref2", true)
	input := plugindom.UpdateExtensionSettingInput{
		PluginID:       p.ID,
		ExtensionPoint: "sidebar.project.section",
		Settings:       plugindom.ExtensionSettingData{Order: 1},
	}
	_, _ = svc.UpdateExtensionSetting(context.Background(), input)
	input.Settings = plugindom.ExtensionSettingData{Order: 2}
	_, err := svc.UpdateExtensionSetting(context.Background(), input)
	if err != nil {
		t.Fatalf("upsert error: %v", err)
	}

	settings, err := svc.ListExtensionSettings(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("list settings error: %v", err)
	}
	if len(settings) != 1 {
		t.Errorf("expected 1 setting after upsert, got %d", len(settings))
	}
	if settings[0].Settings.Order != 2 {
		t.Errorf("expected order 2 after upsert, got %d", settings[0].Settings.Order)
	}
}

func TestListExtensionSettings_ScopedToPlugin(t *testing.T) {
	svc, _ := newSvc(t)
	p1 := seedPlugin(t, svc, "com.paca.pref3a", true)
	p2 := seedPlugin(t, svc, "com.paca.pref3b", true)

	_, _ = svc.UpdateExtensionSetting(context.Background(), plugindom.UpdateExtensionSettingInput{
		PluginID: p1.ID, ExtensionPoint: "ep1",
		Settings: plugindom.ExtensionSettingData{},
	})
	_, _ = svc.UpdateExtensionSetting(context.Background(), plugindom.UpdateExtensionSettingInput{
		PluginID: p2.ID, ExtensionPoint: "ep1",
		Settings: plugindom.ExtensionSettingData{},
	})

	s1, _ := svc.ListExtensionSettings(context.Background(), p1.ID)
	if len(s1) != 1 {
		t.Errorf("expected 1 setting for plugin1, got %d", len(s1))
	}

	s2, _ := svc.ListExtensionSettings(context.Background(), p2.ID)
	if len(s2) != 1 {
		t.Errorf("expected 1 setting for plugin2, got %d", len(s2))
	}
}

// ---------------------------------------------------------------------------
// ListExtensionSettingsForPlugins
// ---------------------------------------------------------------------------

func TestListExtensionSettingsForPlugins_EmptyInput(t *testing.T) {
	svc, _ := newSvc(t)
	grouped, err := svc.ListExtensionSettingsForPlugins(context.Background(), []uuid.UUID{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grouped) != 0 {
		t.Errorf("expected empty map for empty input, got %d entries", len(grouped))
	}
}

func TestListExtensionSettingsForPlugins_MultiplePlugins(t *testing.T) {
	svc, _ := newSvc(t)
	p1 := seedPlugin(t, svc, "com.paca.multi1", true)
	p2 := seedPlugin(t, svc, "com.paca.multi2", true)
	p3 := seedPlugin(t, svc, "com.paca.multi3", true)

	// Add settings for p1 and p2, but not p3
	_, _ = svc.UpdateExtensionSetting(context.Background(), plugindom.UpdateExtensionSettingInput{
		PluginID: p1.ID, ExtensionPoint: "ep1",
		Settings: plugindom.ExtensionSettingData{Order: 1},
	})
	_, _ = svc.UpdateExtensionSetting(context.Background(), plugindom.UpdateExtensionSettingInput{
		PluginID: p1.ID, ExtensionPoint: "ep2",
		Settings: plugindom.ExtensionSettingData{Order: 2},
	})
	_, _ = svc.UpdateExtensionSetting(context.Background(), plugindom.UpdateExtensionSettingInput{
		PluginID: p2.ID, ExtensionPoint: "ep1",
		Settings: plugindom.ExtensionSettingData{Order: 3},
	})

	grouped, err := svc.ListExtensionSettingsForPlugins(context.Background(), []uuid.UUID{p1.ID, p2.ID, p3.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// p1 should have 2 settings
	if len(grouped[p1.ID]) != 2 {
		t.Errorf("expected 2 settings for p1, got %d", len(grouped[p1.ID]))
	}

	// p2 should have 1 setting
	if len(grouped[p2.ID]) != 1 {
		t.Errorf("expected 1 setting for p2, got %d", len(grouped[p2.ID]))
	}

	// p3 should have no settings (key may not exist or be empty slice)
	if len(grouped[p3.ID]) != 0 {
		t.Errorf("expected 0 settings for p3, got %d", len(grouped[p3.ID]))
	}
}

func TestListExtensionSettingsForPlugins_PluginsWithNoSettings(t *testing.T) {
	svc, _ := newSvc(t)
	p1 := seedPlugin(t, svc, "com.paca.nosettings1", true)
	p2 := seedPlugin(t, svc, "com.paca.nosettings2", true)

	grouped, err := svc.ListExtensionSettingsForPlugins(context.Background(), []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both plugins should have no settings
	if len(grouped[p1.ID]) != 0 {
		t.Errorf("expected 0 settings for p1, got %d", len(grouped[p1.ID]))
	}
	if len(grouped[p2.ID]) != 0 {
		t.Errorf("expected 0 settings for p2, got %d", len(grouped[p2.ID]))
	}
}
