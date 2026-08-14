package executor

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// diffContentBlock mirrors apps/web's ToolDiffBlock (see
// conversation-to-thread-messages.ts's extractDiffBlocks) — a content-block
// variant sibling to the {"type":"content",...} text wrapper Goose already
// sends, appended into the same tool_call_update.content array so the
// frontend's existing diff renderer (already built for Claude Code/Codex
// ACP sessions) picks it up with no further changes beyond wiring
// extractDiffBlocks into the tool_call_update case too.
type diffContentBlock struct {
	Type    string  `json:"type"`
	Path    string  `json:"path,omitempty"`
	OldText *string `json:"oldText"`
	NewText string  `json:"newText"`
}

// toolCallUpdateShape is the subset of a tool_call_update's fields this file
// needs to read — see acp.SessionUpdateKind's doc comment on why the full
// shape isn't decoded into a shared struct: content differs by kind.
type toolCallUpdateShape struct {
	Status    string `json:"status"`
	Locations []struct {
		Path string `json:"path"`
	} `json:"locations"`
}

// attachDiffs wraps onEvent so a completed edit-type tool_call_update (one
// carrying ACP "locations") gets diff content blocks appended before the
// caller ever sees it, computed by running git directly inside handle's own
// container.
//
// This exists because Goose's ACP-over-HTTP implementation has no
// fs/read_text_file-style callback into the client the way some other ACP
// agents do — confirmed against the agent-client-protocol project's own
// goosed-over-ACP tracking issue (block/goose#6642): the only
// server-initiated request type it implements at all is
// request_permission. Its built-in Edit/Write tools always write straight
// to the sandbox's own filesystem and report only a "N lines -> M lines"
// summary — there is no protocol-level channel to recover a diff from, so
// this reaches into the container's filesystem directly instead, via the
// git checkout every code task already has (see repo-tools.ts's
// clone_repository).
//
// Never fails the turn: any error computing a diff for one location just
// means that one tool call's card renders without one, same as it does
// today — swallowed here rather than surfaced, since a diff is enrichment,
// not something the turn depends on.
func (e *Executor) attachDiffs(ctx context.Context, handle *sandbox.Handle, onEvent func(acp.Event)) func(acp.Event) {
	if onEvent == nil || handle == nil {
		return onEvent
	}
	return func(ev acp.Event) {
		if ev.Kind == acp.UpdateToolCallUpdate {
			ev.Raw = e.enrichToolCallUpdateWithDiffs(ctx, handle.ContainerID, ev.Raw)
		}
		onEvent(ev)
	}
}

func (e *Executor) enrichToolCallUpdateWithDiffs(ctx context.Context, containerID string, raw json.RawMessage) json.RawMessage {
	var shape toolCallUpdateShape
	if err := json.Unmarshal(raw, &shape); err != nil || shape.Status != "completed" || len(shape.Locations) == 0 {
		return raw
	}

	seen := make(map[string]bool, len(shape.Locations))
	var diffBlocks []json.RawMessage
	for _, loc := range shape.Locations {
		if loc.Path == "" || seen[loc.Path] {
			continue
		}
		seen[loc.Path] = true

		oldText, newText, ok := e.gitDiffForPath(ctx, containerID, loc.Path)
		if !ok || (oldText != nil && *oldText == newText) {
			continue
		}
		block, err := json.Marshal(diffContentBlock{
			Type: "diff", Path: loc.Path, OldText: oldText, NewText: newText,
		})
		if err != nil {
			continue
		}
		diffBlocks = append(diffBlocks, block)
	}
	if len(diffBlocks) == 0 {
		return raw
	}

	// Decode as a raw field map (not a fixed struct) so every field this
	// package doesn't otherwise know about — toolCallId, sessionUpdate, a
	// future addition — round-trips untouched; only "content" is replaced.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw
	}
	var content []json.RawMessage
	if existing, ok := fields["content"]; ok {
		_ = json.Unmarshal(existing, &content)
	}
	content = append(content, diffBlocks...)
	newContentRaw, err := json.Marshal(content)
	if err != nil {
		return raw
	}
	fields["content"] = newContentRaw

	enriched, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return enriched
}

// gitDiffForPath returns path's content before and after its most recent
// edit, reading directly from containerID's filesystem — oldText is nil for
// a file with no HEAD version (newly created, not yet committed), matching
// apps/web's ToolDiffBlock.oldText semantics for a new file. ok is false
// when path can't be read at all (deleted, permission error) or isn't
// inside a git checkout, in which case no diff can be produced.
//
// path is used as-is (no assumption about the repo's clone location —
// clone_repository's targetDir is caller-chosen, see repo-tools.ts): `-C
// <path's own directory>` lets git discover the enclosing checkout by
// walking upward itself, the same way it would from a real shell cd'd
// there, rather than this package having to know or guess the repo root.
func (e *Executor) gitDiffForPath(ctx context.Context, containerID, path string) (oldText *string, newText string, ok bool) {
	newContent, exitCode, err := e.sandboxMgr.Exec(ctx, containerID, []string{"cat", path})
	if err != nil || exitCode != 0 {
		return nil, "", false
	}

	dir := filepath.Dir(path)
	rootOut, exitCode, err := e.sandboxMgr.Exec(ctx, containerID, []string{"git", "-C", dir, "rev-parse", "--show-toplevel"})
	if err != nil || exitCode != 0 {
		return nil, "", false
	}
	root := strings.TrimSpace(rootOut)
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, "", false
	}

	oldContent, exitCode, err := e.sandboxMgr.Exec(ctx, containerID, []string{"git", "-C", root, "show", "HEAD:" + rel})
	if err != nil || exitCode != 0 {
		// No HEAD version — a file the agent created this turn, not one it
		// modified. Still a valid diff (an all-additions one), just with a
		// nil oldText.
		return nil, newContent, true
	}
	return &oldContent, newContent, true
}
