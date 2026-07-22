package plugin

import (
	"context"
	"os"
	"path/filepath"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/bundledskills"
)

// PluginLister is the subset of plugindom.Service that ListSkills needs —
// narrowed from the full service interface so callers (and tests) don't
// need to satisfy install/update/delete/extension-setting methods just to
// list skills.
type PluginLister interface {
	ListPlugins(ctx context.Context) ([]*plugindom.Plugin, error)
}

// ListSkills returns every skill contributed by an enabled plugin that
// declares a `skills` section in its manifest (see plugindom.SkillsManifest),
// read directly from the local skills store the Installer already extracted
// each bundle to — skillsDir/{pluginName}/{skillName}/SKILL.md — rather than
// over HTTP. This is the same process that populated that directory (see
// Installer.Install), so there's no network round trip and the result always
// reflects the plugin's currently installed/enabled state.
//
// A plugin with no skills section, a disabled plugin, or one declared skill
// whose SKILL.md can't be read is silently skipped rather than failing the
// whole list — the same per-skill isolation
// docs/plugins/skills-plugin-system.md documents for services/ai-agent's
// analogous (HTTP-based) load_plugin_skills.
func ListSkills(ctx context.Context, svc PluginLister, skillsDir string) ([]bundledskills.Skill, error) {
	if skillsDir == "" || svc == nil {
		return nil, nil
	}
	plugins, err := svc.ListPlugins(ctx)
	if err != nil {
		return nil, err
	}
	var result []bundledskills.Skill
	for _, p := range plugins {
		if !p.Enabled || p.Manifest.Skills == nil {
			continue
		}
		for _, name := range p.Manifest.Skills.Names {
			skillPath := filepath.Join(skillsDir, p.Name, name, "SKILL.md")
			content, err := os.ReadFile(skillPath)
			if err != nil {
				continue
			}
			result = append(result, bundledskills.Skill{
				Name:    name,
				Path:    p.Name + "/" + name + "/SKILL.md",
				Content: string(content),
			})
		}
	}
	return result, nil
}
