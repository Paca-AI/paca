package dto

import "github.com/Paca-AI/api/internal/platform/bundledskills"

// SkillResponse is the JSON representation of one bundled skill.
type SkillResponse struct {
	Name string `json:"name"`
	// Path is this skill's relative path within its source directory (e.g.
	// "paca-do/SKILL.md", or "paca.md" for the one legacy-format
	// exception) — see bundledskills.Skill.Path for why this matters to
	// services/ai-agent specifically.
	Path    string `json:"path"`
	Content string `json:"content"`
}

// SkillListResponse is the JSON wrapper for a list of bundled skills.
type SkillListResponse struct {
	Skills []SkillResponse `json:"skills"`
}

// SkillListResponseFromEntities maps bundled skills to the list DTO.
func SkillListResponseFromEntities(skills []bundledskills.Skill) SkillListResponse {
	items := make([]SkillResponse, len(skills))
	for i, s := range skills {
		items[i] = SkillResponse{Name: s.Name, Path: s.Path, Content: s.Content}
	}
	return SkillListResponse{Skills: items}
}
