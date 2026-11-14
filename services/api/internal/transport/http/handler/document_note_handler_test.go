package handler_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

type writeNote struct {
	Note   string `json:"note"`
	Client string `json:"client"`
	Task   string `json:"task"`
}

func activityNote(t *testing.T, raw json.RawMessage) writeNote {
	t.Helper()
	var n writeNote
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("activity content %s: %v", raw, err)
	}
	return n
}

func TestUpdateDocument_NoteStoredInActivity(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	svc := &fakeDocSvc{doc: d, updateDoc: d, updateChange: docdom.DocChange{Content: true}}
	act := &fakeDocActivitySvc{}
	r := newDocRouterWithNotify(svc, act, &mockNotificationSvc{})

	body := map[string]any{"content": json.RawMessage(etagBlocks), "note": "fix the intro", "client": "claude-code/abc", "task": "PACA-88"}
	w := serveAs(r, newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), body), uuid.New())
	if w.Code != http.StatusOK || len(act.recorded) != 1 {
		t.Fatalf("got %d, %d activities", w.Code, len(act.recorded))
	}
	if n := activityNote(t, act.recorded[0].Content); n != (writeNote{"fix the intro", "claude-code/abc", "PACA-88"}) {
		t.Fatalf("note = %+v in %s", n, act.recorded[0].Content)
	}
	var c struct {
		Changes []docdom.FieldChange `json:"changes"`
	}
	_ = json.Unmarshal(act.recorded[0].Content, &c)
	if len(c.Changes) != 1 || c.Changes[0].Field != "content" {
		t.Fatalf("changes = %s", act.recorded[0].Content)
	}

	// Create and by-path writes record it too.
	cr := &createReturningSvc{fakeDocSvc: &fakeDocSvc{}, created: etagDoc(projectID, true)}
	act2 := &fakeDocActivitySvc{}
	w = serveAs(newDocRouterWithNotify(cr.fakeDocSvc, act2, &mockNotificationSvc{}), newReq(t, http.MethodPost, "/projects/"+projectID.String()+"/docs",
		map[string]any{"title": "T", "note": "first draft"}), uuid.New())
	if w.Code != http.StatusCreated || len(act2.recorded) != 1 || activityNote(t, act2.recorded[0].Content).Note != "first draft" {
		t.Fatalf("create: %d %+v", w.Code, act2.recorded)
	}
	act3 := &fakeDocActivitySvc{}
	bp := &fakeDocSvc{updateDoc: etagDoc(projectID, true)}
	w = serveAs(newByPathRouter(bp, act3, &fakeDocAuthz{allow: true}), newReq(t, http.MethodPut, "/projects/"+projectID.String()+"/docs/by-path",
		map[string]any{"path": "x", "content": json.RawMessage(`[]`), "note": "pushed", "task": "PACA-1"}), uuid.New())
	if w.Code != http.StatusCreated || len(act3.recorded) != 1 || activityNote(t, act3.recorded[0].Content) != (writeNote{Note: "pushed", Task: "PACA-1"}) {
		t.Fatalf("by-path: %d %+v", w.Code, act3.recorded)
	}
}

func TestUpdateDocument_NoteTooLong400(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	for name, body := range map[string]map[string]any{
		"note":   {"note": strings.Repeat("é", 501)},
		"client": {"client": strings.Repeat("c", 201)},
		"task":   {"task": strings.Repeat("t", 51)},
	} {
		svc := &fakeDocSvc{doc: d, updateDoc: d, updateChange: docdom.DocChange{Content: true}}
		body["content"] = json.RawMessage(etagBlocks)
		w := serveAs(newDocRouterWithNotify(svc, &fakeDocActivitySvc{}, &mockNotificationSvc{}),
			newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), body), uuid.New())
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"DOC_NOTE_INVALID"`) || svc.lastUpdate != nil {
			t.Errorf("%s too long: %d %s (update ran: %v)", name, w.Code, w.Body.String(), svc.lastUpdate != nil)
		}
	}
	// At the limits it is accepted.
	svc := &fakeDocSvc{doc: d, updateDoc: d, updateChange: docdom.DocChange{Content: true}}
	w := serveAs(newDocRouterWithNotify(svc, &fakeDocActivitySvc{}, &mockNotificationSvc{}),
		newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), map[string]any{
			"content": json.RawMessage(etagBlocks), "note": strings.Repeat("é", 500), "client": strings.Repeat("c", 200), "task": strings.Repeat("t", 50),
		}), uuid.New())
	if w.Code != http.StatusOK {
		t.Fatalf("at the limits: %d %s", w.Code, w.Body.String())
	}
}

// A 412 names who changed the content last and why: the note, client and
// task of the newest content-changing activity.
func TestUpdateDocument_412CarriesLastNote(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	writer := uuid.New()
	svc := &fakeDocSvc{doc: d, updateErr: &docdom.VersionConflictError{CurrentHash: *d.ContentHash, UpdatedAt: time.Now(), UpdatedBy: &writer}}
	act := &fakeDocActivitySvc{list: []*docdom.Activity{
		{ID: uuid.New(), ActivityType: docdom.ActivityTypeComment, Content: json.RawMessage(`[]`)},
		{ID: uuid.New(), ActivityType: docdom.ActivityTypeDocUpdated, Content: json.RawMessage(`{"changes":[{"field":"title","old":"a","new":"b"}],"note":"renamed"}`)},
		{ID: uuid.New(), ActivityType: docdom.ActivityTypeDocUpdated, Content: json.RawMessage(`{"changes":[{"field":"content","old":[],"new":[]}],"note":"rewrote step 2","client":"claude-code/s1","task":"PACA-88"}`)},
		{ID: uuid.New(), ActivityType: docdom.ActivityTypeDocCreated, Content: json.RawMessage(`{"title":"Doc","note":"first"}`)},
	}}
	r := newDocRouterWithNotify(svc, act, &mockNotificationSvc{})
	stale := func() *http.Request {
		req := newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), map[string]any{"content": json.RawMessage(etagBlocks)})
		req.Header.Set("If-Match", `"`+strings.Repeat("0", 64)+`"`)
		return req
	}
	w := serveAs(r, stale(), uuid.New())
	var body struct {
		Details map[string]string `json:"error_details"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	want := map[string]string{"note": "rewrote step 2", "client": "claude-code/s1", "task": "PACA-88", "content_hash": *d.ContentHash, "updated_by": writer.String()}
	for k, v := range want {
		if body.Details[k] != v {
			t.Errorf("details[%s] = %q, want %q (%v)", k, body.Details[k], v, body.Details)
		}
	}

	// With no content-changing activity, the details carry no note.
	act.list = []*docdom.Activity{{ID: uuid.New(), ActivityType: docdom.ActivityTypeComment, Content: json.RawMessage(`[]`)}}
	svc.updateErr = &docdom.VersionConflictError{CurrentHash: *d.ContentHash, UpdatedAt: time.Now()}
	w = serveAs(r, stale(), uuid.New())
	body.Details = nil
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body.Details["note"]; ok || body.Details["content_hash"] == "" {
		t.Fatalf("details without a note: %v", body.Details)
	}
}

func TestUpdateDocument_NoOpStoresNoNote(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	svc := &fakeDocSvc{doc: d, updateDoc: d, updateChange: docdom.DocChange{}}
	act := &fakeDocActivitySvc{}
	r := newDocRouterWithNotify(svc, act, &mockNotificationSvc{})
	w := serveAs(r, newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(),
		map[string]any{"content": json.RawMessage(etagBlocks), "note": "nothing really"}), uuid.New())
	if w.Code != http.StatusOK || len(act.recorded) != 0 {
		t.Fatalf("no-op: %d, %d activities", w.Code, len(act.recorded))
	}
	// Nor does a by-path push that changes only the source.
	bp := &fakeDocSvc{doc: d, updateDoc: d, updateChange: docdom.DocChange{Source: true}}
	act2 := &fakeDocActivitySvc{}
	w = serveAs(newByPathRouter(bp, act2, &fakeDocAuthz{allow: true}), newReq(t, http.MethodPut, "/projects/"+projectID.String()+"/docs/by-path",
		map[string]any{"path": "Doc", "content": json.RawMessage(etagBlocks), "note": "new commit"}), uuid.New())
	if w.Code != http.StatusOK || len(act2.recorded) != 0 {
		t.Fatalf("source-only push: %d, %d activities", w.Code, len(act2.recorded))
	}
}
