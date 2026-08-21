package agentdom

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCanonicalizeContextSnapshotHashesRenderedText(t *testing.T) {
	snapshotID := uuid.New()
	turnID := uuid.New()
	sourceID := uuid.New()
	capturedAt := time.Now().UTC().Truncate(time.Microsecond)
	makeSnapshot := func(rendered string) TurnContextSnapshot {
		return TurnContextSnapshot{
			ID: snapshotID, TurnID: turnID, SchemaVersion: 1,
			Items: []TurnContextItem{{
				ID: uuid.New(), SnapshotID: snapshotID, Ordinal: 0,
				SourceType: ContextSourceTask, SourceID: sourceID,
				SourceVersion: "v1", SourceAudience: ContextAudienceProjectShared,
				CapturedAt: capturedAt, Content: []byte(`{"b":2,"a":1}`), RenderedText: rendered,
			}},
		}
	}
	left, err := CanonicalizeContextSnapshot(makeSnapshot("safe A"))
	if err != nil {
		t.Fatalf("canonicalize left snapshot: %v", err)
	}
	right, err := CanonicalizeContextSnapshot(makeSnapshot("safe B"))
	if err != nil {
		t.Fatalf("canonicalize right snapshot: %v", err)
	}
	if left.ManifestSHA256 == right.ManifestSHA256 {
		t.Fatal("same-length rendered text produced the same manifest hash")
	}
	if string(left.Items[0].Content) != `{"a":1,"b":2}` {
		t.Fatalf("content was not canonicalized: %s", left.Items[0].Content)
	}
}

func TestCanonicalizeContextSnapshotNormalizesCapturedAtToDatabasePrecision(t *testing.T) {
	capturedAt := time.Date(2026, time.August, 21, 12, 34, 56, 123456789, time.FixedZone("test", 8*60*60))
	snapshotID := uuid.New()
	snapshot, err := CanonicalizeContextSnapshot(TurnContextSnapshot{
		ID: snapshotID, TurnID: uuid.New(), SchemaVersion: 1,
		Items: []TurnContextItem{{
			ID: uuid.New(), SnapshotID: snapshotID, Ordinal: 0,
			SourceType: ContextSourceTask, SourceID: uuid.New(), SourceVersion: "v1",
			SourceAudience: ContextAudienceProjectShared, CapturedAt: capturedAt,
			Content: []byte(`{"title":"task"}`), RenderedText: "UNTRUSTED\ntask",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := capturedAt.UTC().Truncate(time.Microsecond)
	if !snapshot.Items[0].CapturedAt.Equal(want) || snapshot.Items[0].CapturedAt.Location() != time.UTC {
		t.Fatalf("captured_at = %s (%s), want %s (UTC)", snapshot.Items[0].CapturedAt, snapshot.Items[0].CapturedAt.Location(), want)
	}
	roundTrip := snapshot
	roundTrip.Items = append([]TurnContextItem(nil), snapshot.Items...)
	roundTrip.Items[0].CapturedAt = want
	canonical, err := CanonicalizeContextSnapshot(roundTrip)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.ManifestSHA256 != snapshot.ManifestSHA256 {
		t.Fatalf("database precision round trip changed manifest hash: %s != %s", canonical.ManifestSHA256, snapshot.ManifestSHA256)
	}
}

func TestCanonicalizeContextSnapshotBoundsContentAndRenderedText(t *testing.T) {
	snapshotID := uuid.New()
	content := append([]byte(`{"value":"`), bytes.Repeat([]byte("x"), MaxContextItemBytes)...)
	content = append(content, []byte(`"}`)...)
	_, err := CanonicalizeContextSnapshot(TurnContextSnapshot{
		ID: snapshotID, TurnID: uuid.New(), SchemaVersion: 1,
		Items: []TurnContextItem{{
			ID: uuid.New(), SnapshotID: snapshotID, Ordinal: 0,
			SourceType: ContextSourceTask, SourceID: uuid.New(), SourceVersion: "v1",
			SourceAudience: ContextAudienceProjectShared, CapturedAt: time.Now(),
			Content: content, RenderedText: "small",
		}},
	})
	if err != ErrContextSnapshotTooLarge {
		t.Fatalf("oversized content error = %v, want %v", err, ErrContextSnapshotTooLarge)
	}
}

func TestContextSnapshotRequestHashExcludesGeneratedIdentityAndCaptureTime(t *testing.T) {
	sourceID := uuid.New()
	makeSnapshot := func(capturedAt time.Time, content string) TurnContextSnapshot {
		snapshotID := uuid.New()
		return TurnContextSnapshot{
			ID: snapshotID, TurnID: uuid.New(), SchemaVersion: 1,
			Items: []TurnContextItem{{
				ID: uuid.New(), SnapshotID: snapshotID, Ordinal: 0,
				SourceType: ContextSourceTask, SourceID: sourceID,
				SourceVersion: "v1", SourceAudience: ContextAudienceProjectShared,
				CapturedAt: capturedAt, Content: []byte(content), RenderedText: "UNTRUSTED\n" + content,
			}},
		}
	}
	left, err := ContextSnapshotRequestSHA256(makeSnapshot(time.Now().UTC(), `{"title":"same"}`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := ContextSnapshotRequestSHA256(makeSnapshot(time.Now().UTC().Add(time.Minute), `{"title":"same"}`))
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("generated identity or capture time changed request hash: %s != %s", left, right)
	}
	changed, err := ContextSnapshotRequestSHA256(makeSnapshot(time.Now().UTC(), `{"title":"changed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if changed == left {
		t.Fatal("changed source content did not change request hash")
	}
}

func TestProjectChatCommandHashNormalizesEmptyContextSources(t *testing.T) {
	command := ProjectChatCommand{
		NewSession: true, ProjectID: uuid.New(), AgentID: uuid.New(),
		RequestedByMemberID: uuid.New(), InputText: "hello",
	}
	nilHash, err := command.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	command.ContextSources = []ContextSourceRef{}
	emptyHash, err := command.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	if nilHash != emptyHash {
		t.Fatalf("nil and empty context sources changed command hash: %s != %s", nilHash, emptyHash)
	}
}

func TestTurnToolPolicyCanonicalOrderAndDuplicates(t *testing.T) {
	left := PrivateChatToolPolicy()
	right := PrivateChatToolPolicy()
	for i, j := 0, len(right.AllowedCapabilities)-1; i < j; i, j = i+1, j-1 {
		right.AllowedCapabilities[i], right.AllowedCapabilities[j] = right.AllowedCapabilities[j], right.AllowedCapabilities[i]
	}
	leftJSON, err := left.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	rightJSON, err := right.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftJSON, rightJSON) {
		t.Fatalf("semantic policy order changed canonical JSON:\n%s\n%s", leftJSON, rightJSON)
	}
	right.AllowedCapabilities = append(right.AllowedCapabilities, right.AllowedCapabilities[0])
	if _, err := right.CanonicalJSON(); err == nil {
		t.Fatal("duplicate capability was accepted")
	}
}

func TestCanonicalizeJSONPreservesLargeIntegers(t *testing.T) {
	canonical, err := CanonicalizeJSON([]byte(`{"z":9007199254740993,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != `{"a":1,"z":9007199254740993}` {
		t.Fatalf("large integer changed during canonicalization: %s", canonical)
	}
	if _, err := CanonicalizeJSON([]byte(`{} {}`)); err == nil {
		t.Fatal("multiple JSON values were accepted")
	}
}
