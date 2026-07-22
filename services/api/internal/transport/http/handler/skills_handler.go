package handler

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/Paca-AI/api/internal/platform/bundledskills"
	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// SkillsHandler serves Paca's Agent Skills: its own bundled skills (see the
// bundledskills package for why those are compiled into the binary) plus,
// for the "cli" flavor only, skills contributed by enabled plugins.
type SkillsHandler struct {
	pluginSvc pluginrt.Lister
	skillsDir string
}

// NewSkillsHandler returns a SkillsHandler. pluginSvc and skillsDir may be
// left zero-valued (nil / ""), in which case ListSkills falls back to
// bundled skills only.
func NewSkillsHandler(pluginSvc pluginrt.Lister, skillsDir string) *SkillsHandler {
	return &SkillsHandler{pluginSvc: pluginSvc, skillsDir: skillsDir}
}

// ListSkills responds with every skill's name, path, and raw file content
// for the requested flavor.
//
//   - GET /api/v1/skills              → "cli" flavor (default): every
//     bundled skill plus every skill contributed by an enabled plugin (read
//     from the local skills store — see pluginrt.ListSkills). Consumed by
//     scripts/install-paca-skills.sh in place of fetching from GitHub, so
//     installed content always matches the exact version this instance is
//     running.
//   - GET /api/v1/skills?target=agent → "agent" flavor: Paca's bundled
//     skills only, deliberately excluding plugin skills. Consumed by
//     services/ai-agent's builder.load_default_skills(), which caches the
//     response for the life of the worker process — mixing plugin skills
//     into that cache would leave a disabled/uninstalled plugin's skill
//     stuck there until the process restarts, breaking the "install/enable/
//     disable takes effect on the next conversation" guarantee documented
//     in docs/plugins/skills-plugin-system.md. Every conversation already
//     gets live, uncached plugin skills through a separate path
//     (agent_repository.list_enabled_plugin_skills +
//     builder.load_plugin_skills), so nothing is missing for the "agent"
//     flavor by leaving plugin skills out of it here.
func (h *SkillsHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	skills := bundledskills.List(target)

	if target != bundledskills.TargetAgent {
		pluginSkills, err := pluginrt.ListSkills(r.Context(), h.pluginSvc, h.skillsDir)
		if err != nil {
			slog.Error("skills: failed to list plugin-contributed skills", "error", err)
		} else if len(pluginSkills) > 0 {
			skills = append(skills, pluginSkills...)
			sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
		}
	}

	presenter.OK(w, r, dto.SkillListResponseFromEntities(skills))
}
