package filesvc

import (
	"context"
	"encoding/json"
	"testing"

	filedom "github.com/Paca-AI/api/internal/domain/file"
	"github.com/Paca-AI/api/internal/events"
)

type recordedMsg struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
	Authz   *events.Authz  `json:"authz"`
}

type recordingSink struct{ msgs []recordedMsg }

func (s *recordingSink) Publish(_ context.Context, channel string, payload any) error {
	if channel != events.ChannelRealtime {
		return nil
	}
	raw, _ := json.Marshal(payload)
	var m recordedMsg
	_ = json.Unmarshal(raw, &m)
	s.msgs = append(s.msgs, m)
	return nil
}

func folderIDs(t *testing.T, m recordedMsg) []string {
	t.Helper()
	raw, _ := m.Payload["folder_ids"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

func TestMoveAnnouncesBothFolders(t *testing.T) {
	from := folder("From", "", &project)
	to := folder("To", "", &project)
	f := fileNode("a.txt", filedom.SourceUpload, from)
	svc, _, _ := newSvc(newFakeRepo(from, to, f))
	sink := &recordingSink{}
	svc.WithRealtime(sink)

	if _, err := svc.Move(t.Context(), scope, actor, f.ID, &to.ID); err != nil {
		t.Fatal(err)
	}
	if len(sink.msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(sink.msgs))
	}
	m := sink.msgs[0]
	if m.Type != events.TopicFileChanged || m.Payload["change"] != "moved" || m.Payload["scope"] != "project" {
		t.Fatalf("unexpected message %+v", m)
	}
	ids := folderIDs(t, m)
	if len(ids) != 2 || ids[0] != from.ID.String() || ids[1] != to.ID.String() {
		t.Fatalf("folder_ids = %v, want [%s %s]", ids, from.ID, to.ID)
	}
	if m.Authz == nil || m.Authz.Action != "files:read" || m.Authz.Resource != "project/"+project.String() {
		t.Fatalf("authz = %+v, want files:read on the project", m.Authz)
	}
}

func TestRootChangesAreFlaggedAndGeneralScopeHasNoAuthz(t *testing.T) {
	svc, _, _ := newSvc(newFakeRepo())
	sink := &recordingSink{}
	svc.WithRealtime(sink)

	general := filedom.Scope{}
	if _, err := svc.CreateFolder(t.Context(), general, actor, nil, "Docs"); err != nil {
		t.Fatal(err)
	}
	m := sink.msgs[0]
	if m.Payload["root"] != true || m.Payload["scope"] != "general" || len(folderIDs(t, m)) != 0 {
		t.Fatalf("unexpected message %+v", m)
	}
	if m.Authz != nil {
		t.Fatalf("a general-scope event must carry no project authz, got %+v", m.Authz)
	}
}

func TestNoSinkIsSilent(t *testing.T) {
	svc, _, _ := newSvc(newFakeRepo())
	if _, err := svc.CreateFolder(t.Context(), scope, actor, nil, "X"); err != nil {
		t.Fatal(err)
	}
}
