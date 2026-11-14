package handler_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// A person unlocks a managed copy with PATCH source_locked:false. The unlock is
// recorded as a doc.updated activity (so open editors hear about it); PATCH
// never sets the source itself.
func TestUpdateDocument_PatchSourceLocked(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	d.Source = &docdom.DocSource{Repository: "paca", Path: "docs/x.md"}
	d.SourceLocked = true
	after := *d
	after.SourceLocked = false
	svc := &fakeDocSvc{doc: d, updateDoc: &after, updateChange: docdom.DocChange{Lock: true}}
	act := &fakeDocActivitySvc{}
	r := newDocRouterWithNotify(svc, act, &mockNotificationSvc{})

	body := map[string]any{"source_locked": false, "source": map[string]string{"repository": "evil", "path": "x"}}
	w := serveAs(r, newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), body), uuid.New())
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if svc.lastUpdate == nil || svc.lastUpdate.Lock == nil || *svc.lastUpdate.Lock {
		t.Fatalf("Lock not passed as false: %+v", svc.lastUpdate)
	}
	if svc.lastUpdate.Source != nil {
		t.Fatalf("PATCH must not set the source: %+v", svc.lastUpdate.Source)
	}
	if !strings.Contains(w.Body.String(), `"source_locked":false`) {
		t.Fatalf("body: %s", w.Body.String())
	}
	if len(act.recorded) != 1 || act.recorded[0].ActivityType != docdom.ActivityTypeDocUpdated {
		t.Fatalf("activities: %+v", act.recorded)
	}
	var content struct {
		Changes []docdom.FieldChange `json:"changes"`
	}
	_ = json.Unmarshal(act.recorded[0].Content, &content)
	if len(content.Changes) != 1 || content.Changes[0].Field != "source_locked" ||
		content.Changes[0].Old != true || content.Changes[0].New != false {
		t.Fatalf("changes: %s", act.recorded[0].Content)
	}
}

func TestUpdateDocument_LockedWriteIs409(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	svc := &fakeDocSvc{doc: d, updateErr: docdom.ErrDocLocked}
	r := newDocRouterWithNotify(svc, &fakeDocActivitySvc{}, &mockNotificationSvc{})
	w := serveAs(r, newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(),
		map[string]any{"content": json.RawMessage(etagBlocks)}), uuid.New())
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"DOC_LOCKED"`) {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}

	svc.updateErr = docdom.ErrDocLockNeedsSource
	w = serveAs(r, newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(),
		map[string]any{"source_locked": true}), uuid.New())
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"DOC_LOCK_NEEDS_SOURCE"`) {
		t.Fatalf("lock without source: got %d: %s", w.Code, w.Body.String())
	}
}
