package handler

import (
	"net/http"

	"github.com/Paca-AI/api/internal/platform/bundledskills"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// SkillsHandler serves Paca's bundled Agent Skills — see the bundledskills
// package for why these are compiled into the binary rather than read from
// a database or the filesystem at request time.
type SkillsHandler struct{}

// NewSkillsHandler returns a SkillsHandler.
func NewSkillsHandler() *SkillsHandler { return &SkillsHandler{} }

// ListSkills responds with every bundled skill's name, path, and raw file
// content for the requested flavor.
//
//   - GET /api/v1/skills            → "cli" flavor (default): consumed by
//     scripts/install-paca-skills.sh in place of fetching from GitHub, so
//     installed content always matches the exact version this instance is
//     running.
//   - GET /api/v1/skills?target=agent → "agent" flavor: consumed by
//     services/ai-agent's builder.load_default_skills() for every
//     "llm"-type agent conversation, in place of reading its own bundled
//     src/skills/ directory.
func (h *SkillsHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	presenter.OK(w, r, dto.SkillListResponseFromEntities(bundledskills.List(target)))
}
