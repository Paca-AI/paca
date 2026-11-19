package handler_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// Notes are the documents of a note view: GET /docs?view_id= lists them and
// POST /docs with view_id creates one.

func TestListDocuments_ViewIDListsTheNotesOfThatView(t *testing.T) {
	viewID := uuid.New()
	note := &docdom.Document{ID: uuid.New(), Title: "Sprint goals", ViewID: &viewID}
	svc := &fakeDocSvc{treeDocs: []*docdom.Document{note}}
	r := newDocumentRouterWith(svc)

	w := doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs?view_id="+viewID.String(), nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if svc.docFilter.ViewID == nil || *svc.docFilter.ViewID != viewID || svc.docFilter.FolderID != nil || svc.docFilter.RootOnly {
		t.Fatalf("filter = %+v, want the view only", svc.docFilter)
	}
	var resp struct {
		Data struct {
			Items []struct {
				ID     uuid.UUID  `json:"id"`
				ViewID *uuid.UUID `json:"view_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Items) != 1 || resp.Data.Items[0].ViewID == nil || *resp.Data.Items[0].ViewID != viewID {
		t.Fatalf("response = %s, want the note with its view_id", w.Body.String())
	}

	// Without view_id the listing is the docs tree, which has no notes.
	doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs", nil, nil)
	if svc.docFilter.ViewID != nil {
		t.Fatalf("tree listing got filter %+v", svc.docFilter)
	}
}

func TestListDocuments_ViewIDRefusesFoldersAndBadIDs(t *testing.T) {
	r := newDocumentRouterWith(&fakeDocSvc{})
	viewID := uuid.NewString()
	for q, code := range map[string]string{
		"view_id=" + viewID + "&folder_id=" + uuid.NewString(): "DOC_NOTE_IN_FOLDER",
		"view_id=" + viewID + "&folder_id=null":                "DOC_NOTE_IN_FOLDER",
		"view_id=not-a-uuid":                                   "BAD_REQUEST",
	} {
		w := doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs?"+q, nil, nil)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), code) {
			t.Errorf("%s: got %d %s, want 400 %s", q, w.Code, w.Body.String(), code)
		}
	}
}

func TestCreateDocument_ViewIDCreatesANote(t *testing.T) {
	svc := &fakeDocSvc{}
	r := newDocumentRouterWith(svc)
	viewID := uuid.New()

	w := doDocRequest(t, r, http.MethodPost, "/projects/"+uuid.NewString()+"/docs",
		map[string]any{"title": "Retro", "view_id": viewID}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if svc.lastCreate == nil || svc.lastCreate.ViewID == nil || *svc.lastCreate.ViewID != viewID || svc.lastCreate.FolderID != nil {
		t.Fatalf("create input = %+v, want view %s", svc.lastCreate, viewID)
	}
	if !strings.Contains(w.Body.String(), `"view_id":"`+viewID.String()+`"`) {
		t.Fatalf("response = %s, want view_id", w.Body.String())
	}
}
