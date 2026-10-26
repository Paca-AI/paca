package activitysvc

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
)

// memberLister is the optional capability linkMentions needs from the
// service's MemberLookup; lookups without it simply skip mention linking.
type memberLister interface {
	ListAllMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error)
}

// mentionPattern matches "@Full Name (id: <uuid>)" — how a teamMention chip
// reads back through text-only clients such as the MCP server, which agents
// copy verbatim — and the short "@username" form. Group 1 is the leading
// boundary (so e-mail addresses are left alone), 2 the uuid, 3 the username.
var mentionPattern = regexp.MustCompile(`(^|[^\w@])@(?:[^@\n()]{1,80}?\s*\(id:\s*([0-9a-fA-F-]{36})\)|([\w.-]+))`)

// linkMentions turns plain-text @mentions of project members in a BlockNote
// comment into structured teamMention chips, so they render as badges and
// reach handleMentions (notify the person, wake the agent). Clients that
// build comments from text — the MCP server, agents — cannot produce chips
// themselves. Best-effort: on any problem the content is returned unchanged.
func (s *Service) linkMentions(ctx context.Context, projectID uuid.UUID, raw json.RawMessage) json.RawMessage {
	if !strings.Contains(string(raw), "@") {
		return raw
	}
	lister, ok := s.members.(memberLister)
	if !ok {
		return raw
	}
	var blocks []any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return raw
	}
	members, err := lister.ListAllMembers(ctx, projectID)
	if err != nil || len(members) == 0 {
		return raw
	}
	if !linkBlocks(blocks, members) {
		return raw
	}
	out, err := json.Marshal(blocks)
	if err != nil {
		return raw
	}
	return out
}

// linkBlocks rewrites blocks in place, reporting whether anything changed.
func linkBlocks(blocks []any, members []*projectdom.ProjectMember) bool {
	changed := false
	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok || block["type"] == "codeBlock" {
			continue
		}
		if inline, ok := block["content"].([]any); ok {
			if out, c := linkInline(inline, members); c {
				block["content"] = out
				changed = true
			}
		}
		if children, ok := block["children"].([]any); ok && linkBlocks(children, members) {
			changed = true
		}
	}
	return changed
}

func linkInline(inline []any, members []*projectdom.ProjectMember) ([]any, bool) {
	out := make([]any, 0, len(inline))
	changed := false
	for _, n := range inline {
		node, ok := n.(map[string]any)
		text, isText := "", false
		if ok && node["type"] == "text" {
			text, isText = node["text"].(string)
		}
		if styles, _ := node["styles"].(map[string]any); isText && styles["code"] != nil {
			isText = false
		}
		if !isText {
			out = append(out, n)
			continue
		}
		last := 0
		for _, m := range mentionPattern.FindAllStringSubmatchIndex(text, -1) {
			end := m[1]
			var member *projectdom.ProjectMember
			if m[4] >= 0 {
				member = findMemberByID(members, text[m[4]:m[5]])
			} else if m[6] >= 0 {
				name := text[m[6]:m[7]]
				trimmed := strings.TrimRight(name, ".-")
				end -= len(name) - len(trimmed)
				member = findMemberByUsername(members, trimmed)
			}
			if member == nil {
				continue
			}
			start := m[0] + (m[3] - m[2]) // skip the boundary character
			if start > last {
				out = append(out, withText(node, text[last:start]))
			}
			out = append(out, map[string]any{
				"type":  "teamMention",
				"props": map[string]any{"id": mentionID(member), "name": mentionName(member), "avatar": ""},
			})
			last = end
			changed = true
		}
		switch {
		case last == 0:
			out = append(out, n)
		case last < len(text):
			out = append(out, withText(node, text[last:]))
		}
	}
	return out, changed
}

func withText(node map[string]any, text string) map[string]any {
	c := make(map[string]any, len(node))
	for k, v := range node {
		c[k] = v
	}
	c["text"] = text
	return c
}

func findMemberByID(members []*projectdom.ProjectMember, id string) *projectdom.ProjectMember {
	for _, m := range members {
		if strings.EqualFold(m.ID.String(), id) || strings.EqualFold(m.UserID.String(), id) ||
			strings.EqualFold(m.AccountID.String(), id) ||
			(m.AgentID != nil && strings.EqualFold(m.AgentID.String(), id)) {
			return m
		}
	}
	return nil
}

func findMemberByUsername(members []*projectdom.ProjectMember, name string) *projectdom.ProjectMember {
	for _, m := range members {
		if name != "" && (strings.EqualFold(m.Username, name) || (m.IsAgent() && strings.EqualFold(m.AgentHandle, name))) {
			return m
		}
	}
	return nil
}

// mentionID is the id the web UI embeds in a teamMention: the agent id for an
// agent member (handleMentions starts it by that id), the user id otherwise.
func mentionID(m *projectdom.ProjectMember) string {
	if m.IsAgent() && m.AgentID != nil {
		return m.AgentID.String()
	}
	if m.UserID != uuid.Nil {
		return m.UserID.String()
	}
	return m.AccountID.String()
}

func mentionName(m *projectdom.ProjectMember) string {
	switch {
	case m.FullName != "":
		return m.FullName
	case m.AgentName != "":
		return m.AgentName
	}
	return m.Username
}
