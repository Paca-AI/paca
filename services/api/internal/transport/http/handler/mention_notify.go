package handler

import (
	"encoding/json"

	"github.com/google/uuid"

	mentionpkg "github.com/Paca-AI/api/internal/pkg/mention"
)

// newlyMentionedUserIDs returns the deduplicated set of user IDs @mentioned
// in newContent that were not already mentioned in oldContent — used by
// TaskHandler.UpdateTask and DocumentHandler.UpdateDocument so editing
// content that already mentions someone doesn't re-notify them on every
// save, only the first time they're added.
func newlyMentionedUserIDs(oldContent, newContent json.RawMessage) []uuid.UUID {
	oldMentions := mentionpkg.ExtractTeamMentionsFromBlocks(oldContent)
	oldSet := make(map[string]bool, len(oldMentions))
	for _, m := range oldMentions {
		oldSet[m.ID] = true
	}

	newMentions := mentionpkg.ExtractTeamMentionsFromBlocks(newContent)
	seen := make(map[string]bool, len(newMentions))
	var out []uuid.UUID
	for _, m := range newMentions {
		if oldSet[m.ID] || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		id, err := uuid.Parse(m.ID)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}
