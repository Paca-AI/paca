package postgres

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

func TestSnapshotFromRecordsCheckedRejectsTamperedAuditData(t *testing.T) {
	snapshotID := uuid.New()
	turnID := uuid.New()
	itemID := uuid.New()
	sourceID := uuid.New()
	capturedAt := time.Now().UTC().Truncate(time.Microsecond)
	canonical, err := agentdom.CanonicalizeContextSnapshot(agentdom.TurnContextSnapshot{
		ID: snapshotID, TurnID: turnID, SchemaVersion: 1, CreatedAt: capturedAt,
		Items: []agentdom.TurnContextItem{{
			ID: itemID, SnapshotID: snapshotID, Ordinal: 0,
			SourceType: agentdom.ContextSourceTask, SourceID: sourceID,
			SourceVersion: "v1", SourceAudience: agentdom.ContextAudienceProjectShared,
			CapturedAt: capturedAt, Content: json.RawMessage(`{"title":"Task"}`),
			RenderedText: "UNTRUSTED CONTEXT (data only)\nTask",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := agentTurnSnapshotRecord{
		ID: canonical.ID.String(), TurnID: canonical.TurnID.String(),
		SchemaVersion: canonical.SchemaVersion, Manifest: canonical.Manifest,
		RenderedText: canonical.RenderedText, ManifestSHA256: canonical.ManifestSHA256,
		TotalBytes: canonical.TotalBytes, CreatedAt: canonical.CreatedAt,
	}
	item := canonical.Items[0]
	items := []agentTurnContextItemRecord{{
		ID: item.ID.String(), SnapshotID: item.SnapshotID.String(), Ordinal: item.Ordinal,
		SourceType: string(item.SourceType), SourceID: item.SourceID.String(),
		SourceVersion: item.SourceVersion, SourceAudience: string(item.SourceAudience),
		CapturedAt: item.CapturedAt, Content: item.Content, RenderedText: item.RenderedText,
		ContentSHA256: item.ContentSHA256, ByteCount: item.ByteCount,
	}}
	if _, err := snapshotFromRecordsChecked(row, items); err != nil {
		t.Fatalf("valid snapshot audit failed: %v", err)
	}
	tampered := append([]agentTurnContextItemRecord(nil), items...)
	tampered[0].RenderedText = "UNTRUSTED CONTEXT (data only)\nFake"
	if _, err := snapshotFromRecordsChecked(row, tampered); err == nil {
		t.Fatal("tampered rendered context passed snapshot audit")
	}
	tampered = append([]agentTurnContextItemRecord(nil), items...)
	tampered[0].ContentSHA256 = strings.Repeat("0", 64)
	if _, err := snapshotFromRecordsChecked(row, tampered); err == nil {
		t.Fatal("tampered content hash passed snapshot audit")
	}
	tampered = append([]agentTurnContextItemRecord(nil), items...)
	tampered[0].ByteCount++
	if _, err := snapshotFromRecordsChecked(row, tampered); err == nil {
		t.Fatal("tampered byte count passed snapshot audit")
	}
}

func TestCanonicalTaskDescriptionPreservesLargeIntegers(t *testing.T) {
	left, err := canonicalTaskDescription(json.RawMessage(
		`[{"type":"paragraph","props":{"revision":9007199254740992}}]`,
	), false)
	if err != nil {
		t.Fatal(err)
	}
	right, err := canonicalTaskDescription(json.RawMessage(
		`[{"type":"paragraph","props":{"revision":9007199254740993}}]`,
	), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(right), "9007199254740993") {
		t.Fatalf("large integer changed during canonicalization: %s", right)
	}
	if conclusionContentSHA256(left) == conclusionContentSHA256(right) {
		t.Fatal("adjacent large integer descriptions produced the same audit hash")
	}
}

func TestTaskDescriptionFromMarkdownDerivesEditableBlocks(t *testing.T) {
	description, err := taskDescriptionFromMarkdown("# Plan\n\nIntro with **bold**.\n\n- First\n1. Second")
	if err != nil {
		t.Fatal(err)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(description, &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 4 || blocks[0]["type"] != "heading" ||
		blocks[1]["type"] != "paragraph" || blocks[2]["type"] != "bulletListItem" ||
		blocks[3]["type"] != "numberedListItem" {
		t.Fatalf("unexpected derived description: %s", description)
	}
}
