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
// bundledskills package for why those are compiled into the binary) plus
// skills contributed by enabled plugins.
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
//   - GET /api/v1/skills?target=agent → "agent" flavor: the same set in the
//     in-product form. Consumed by services/agent-runner, which re-fetches
//     it on a short TTL (not for the life of the process) precisely so that
//     installing, enabling or disabling a plugin takes effect on a following
//     conversation.
//
// In both flavors, routes a plugin declares (SkillsManifest.Routes) are added
// to the `paca` router skill's table only for plugin skills served in the
// same response, so the router never mentions a skill that is not there.
func (h *SkillsHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	skills := bundledskills.List(target)

	pluginSkills, routes, err := pluginrt.ListSkillsAndRoutes(r.Context(), h.pluginSvc, h.skillsDir)
	if err != nil {
		slog.Error("skills: failed to list plugin-contributed skills", "error", err)
	} else {
		// Plugin routes become rows of the `paca` router's table, so the
		// router mentions exactly the plugin skills served here.
		skills = bundledskills.ListWithRoutes(target, routes)
		if len(pluginSkills) > 0 {
			skills = append(skills, pluginSkills...)
			sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
		}
	}

	presenter.OK(w, r, dto.SkillListResponseFromEntities(skills))
}
