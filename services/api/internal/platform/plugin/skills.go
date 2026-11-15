package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/bundledskills"
)

// Lister is the subset of plugindom.Service that ListSkills needs — narrowed
// from the full service interface so callers (and tests) don't need to
// satisfy install/update/delete/extension-setting methods just to list
// skills.
type Lister interface {
	ListPlugins(ctx context.Context) ([]*plugindom.Plugin, error)
}

// SkillReader reads one plugin skill's SKILL.md. every Store implements it for
// both the local and the bucket-backed plugin store.
type SkillReader interface {
	ReadSkill(ctx context.Context, pluginName, skill string) ([]byte, error)
}

// LocalSkillReader reads skills straight from a skills directory laid out as
// {dir}/{pluginName}/{skillName}/SKILL.md. An empty dir reads nothing.
type LocalSkillReader string

// ReadSkill implements SkillReader.
func (d LocalSkillReader) ReadSkill(_ context.Context, pluginName, skill string) ([]byte, error) {
	if strings.TrimSpace(string(d)) == "" {
		return nil, fmt.Errorf("plugin skills directory is not configured")
	}
	if err := validateSkillSegment(skill); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(string(d), pluginName, skill, "SKILL.md")) // #nosec G304 — trusted config + DB
}

// ListSkills returns every skill contributed by an enabled plugin that
// declares a `skills` section in its manifest (see plugindom.SkillsManifest),
// read through the SkillReader (the plugin Store, local or bucket) the
// Installer already wrote each bundle to — {pluginName}/{skillName}/SKILL.md —
// rather than over HTTP, so the result always reflects the plugin's currently
// installed/enabled state.
//
// A plugin with no skills section, a disabled plugin, or one declared skill
// whose SKILL.md can't be read is silently skipped rather than failing the
// whole list — the same per-skill isolation
// docs/plugins/skills-plugin-system.md documents for services/ai-agent's
// analogous (HTTP-based) load_plugin_skills.
//
// A declared name that collides with one of Paca's own bundled skills (see
// bundledskills.IsBuiltinName) is skipped and logged rather than returned.
// plugindom.SkillsManifest.validate() already rejects such a name at
// install/update time, but this is a second line of defense against a
// plugin installed before that check existed, or any other path that
// bypassed it — nothing here dedupes by name against the bundled list the
// caller (skills_handler.go) merges this result into, so letting a
// collision through would silently shadow a bundled skill both in the API
// response and in whatever scripts/install-paca-skills.sh writes to disk.
func ListSkills(ctx context.Context, svc Lister, skills SkillReader) ([]bundledskills.Skill, error) {
	result, _, err := ListSkillsAndRoutes(ctx, svc, skills)
	return result, err
}

// ListSkillsAndRoutes is ListSkills plus the router-table rows the same
// plugins declare (SkillsManifest.Routes). A route is returned only when its
// skill was actually returned too, so the `paca` router never points at a
// skill that is disabled, unreadable or filtered out.
func ListSkillsAndRoutes(ctx context.Context, svc Lister, reader SkillReader) ([]bundledskills.Skill, []bundledskills.Route, error) {
	if reader == nil || svc == nil {
		return nil, nil, nil
	}
	plugins, err := svc.ListPlugins(ctx)
	if err != nil {
		return nil, nil, err
	}
	var result []bundledskills.Skill
	var routes []bundledskills.Route
	for _, p := range plugins {
		if !p.Enabled || p.Manifest.Skills == nil {
			continue
		}
		whenBySkill := make(map[string]string, len(p.Manifest.Skills.Routes))
		for _, r := range p.Manifest.Skills.Routes {
			whenBySkill[r.Skill] = r.When
		}
		for _, name := range p.Manifest.Skills.Names {
			if bundledskills.IsBuiltinName(name) {
				slog.Warn("skills: plugin skill name collides with a bundled skill, skipping",
					"plugin", p.Name, "skill", name)
				continue
			}
			content, err := reader.ReadSkill(ctx, p.Name, name)
			if err != nil {
				continue
			}
			result = append(result, bundledskills.Skill{
				Name:    name,
				Path:    p.Name + "/" + name + "/SKILL.md",
				Content: string(content),
			})
			if when, ok := whenBySkill[name]; ok {
				routes = append(routes, bundledskills.Route{Skill: name, When: when})
			}
		}
	}
	return result, routes, nil
}
