package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/bundledskills"
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

func TestListSkills_ReturnsEnabledPluginSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "com.paca.dashboard", "paca-dashboard-builder", "---\nname: paca-dashboard-builder\n---\nbody")

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

	skills, err := ListSkills(context.Background(), svc, dir)
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d: %+v", len(skills), skills)
	}
	if skills[0].Name != "paca-dashboard-builder" {
		t.Errorf("expected paca-dashboard-builder, got %q", skills[0].Name)
	}
	if skills[0].Path != "com.paca.dashboard/paca-dashboard-builder/SKILL.md" {
		t.Errorf("unexpected path: %q", skills[0].Path)
	}
	if skills[0].Content == "" {
		t.Error("expected non-empty content")
	}
}

func TestListSkills_SkipsDisabledPlugins(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "com.paca.dashboard", "paca-dashboard-builder", "content")

	svc := &fakePluginLister{plugins: []*plugindom.Plugin{
		{
			Name:    "com.paca.dashboard",
			Enabled: false,
			Manifest: plugindom.PluginManifest{
				Skills: &plugindom.SkillsManifest{
					BaseURL: "/plugins-skills/com.paca.dashboard",
					Names:   []string{"paca-dashboard-builder"},
				},
			},
		},
	}}

	skills, err := ListSkills(context.Background(), svc, dir)
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected disabled plugin's skills to be excluded, got %+v", skills)
	}
}

func TestListSkills_SkipsPluginsWithoutSkillsManifest(t *testing.T) {
	dir := t.TempDir()
	svc := &fakePluginLister{plugins: []*plugindom.Plugin{
		{Name: "com.paca.webhook", Enabled: true, Manifest: plugindom.PluginManifest{}},
	}}

	skills, err := ListSkills(context.Background(), svc, dir)
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected no skills for a plugin with no skills manifest, got %+v", skills)
	}
}

func TestListSkills_SkipsUnreadableSkillFileRatherThanFailing(t *testing.T) {
	dir := t.TempDir()
	// paca-a exists on disk, paca-b was declared but never extracted.
	writeSkillFile(t, dir, "com.paca.example", "paca-a", "content-a")

	svc := &fakePluginLister{plugins: []*plugindom.Plugin{
		{
			Name:    "com.paca.example",
			Enabled: true,
			Manifest: plugindom.PluginManifest{
				Skills: &plugindom.SkillsManifest{
					BaseURL: "/plugins-skills/com.paca.example",
					Names:   []string{"paca-a", "paca-b"},
				},
			},
		},
	}}

	skills, err := ListSkills(context.Background(), svc, dir)
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "paca-a" {
		t.Fatalf("expected only paca-a to be returned, got %+v", skills)
	}
}

func TestListSkills_SkipsNameCollidingWithBundledSkill(t *testing.T) {
	dir := t.TempDir()
	// Manifest validation should already reject this at install time (see
	// entity_test.go's "collides with a bundled skill name" case), but this
	// exercises ListSkills' own defense-in-depth for a plugin that was
	// installed before that check existed.
	builtinName := bundledskills.List(bundledskills.TargetCLI)[0].Name
	writeSkillFile(t, dir, "com.paca.example", builtinName, "shadow content")
	writeSkillFile(t, dir, "com.paca.example", "paca-example", "content")

	svc := &fakePluginLister{plugins: []*plugindom.Plugin{
		{
			Name:    "com.paca.example",
			Enabled: true,
			Manifest: plugindom.PluginManifest{
				Skills: &plugindom.SkillsManifest{
					BaseURL: "/plugins-skills/com.paca.example",
					Names:   []string{builtinName, "paca-example"},
				},
			},
		},
	}}

	skills, err := ListSkills(context.Background(), svc, dir)
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "paca-example" {
		t.Fatalf("expected only the non-colliding skill to be returned, got %+v", skills)
	}
}

func TestListSkills_PropagatesListPluginsError(t *testing.T) {
	svc := &fakePluginLister{err: errors.New("db unavailable")}

	_, err := ListSkills(context.Background(), svc, t.TempDir())
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestListSkills_NoSkillsDirOrService(t *testing.T) {
	if skills, err := ListSkills(context.Background(), nil, "/some/dir"); err != nil || skills != nil {
		t.Fatalf("expected (nil, nil) for nil service, got (%+v, %v)", skills, err)
	}
	if skills, err := ListSkills(context.Background(), &fakePluginLister{}, ""); err != nil || skills != nil {
		t.Fatalf("expected (nil, nil) for empty skillsDir, got (%+v, %v)", skills, err)
	}
}
