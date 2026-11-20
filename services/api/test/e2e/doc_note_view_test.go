package e2e_test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/uuid"
)

// listDocIDsViaAPI returns the ids of the documents GET /docs?<query> lists.
func listDocIDsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, query string) []string {
	t.Helper()
	status, e, err := apiCall(env, t, client, token, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/docs?%s", env.base, projectID, query), nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("list docs ?%s: status=%d code=%q err=%v", query, status, e.ErrorCode, err)
	}
	items, _ := assertDataMap(t, e)["items"].([]any)
	ids := make([]string, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		id, _ := m["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// A note view holds documents that belong to it: they are read and edited
// like any document, are never part of the docs tree, and go with the view.
func TestE2ENoteView_NotesBelongToTheirView(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "note-view-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "notepass1")
	client, token := taskMemberLogin(t, env, username, "notepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint 1")
	createViewViaAPI(t, env, client, token, projID, sprintID, "Board", "board")
	notesView := createViewViaAPI(t, env, client, token, projID, sprintID, "Notes", "note")
	docs := fmt.Sprintf("%s/api/v1/projects/%s/docs", env.base, projID)

	status, e, err := apiCall(env, t, client, token, http.MethodPost, docs, map[string]any{"title": "Sprint goals", "view_id": notesView})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("create note: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}
	note := assertDataMap(t, e)
	noteID, _ := note["id"].(string)
	if note["view_id"] != notesView || note["folder_id"] != nil {
		t.Fatalf("note = %v, want view_id %s and no folder", note, notesView)
	}
	treeDoc := createDocViaAPI(t, env, client, token, projID, "Sprint goals")

	t.Run("listed_by_its_view_only", func(t *testing.T) {
		if ids := listDocIDsViaAPI(t, env, client, token, projID, "view_id="+notesView); len(ids) != 1 || ids[0] != noteID {
			t.Fatalf("notes of the view = %v, want [%s]", ids, noteID)
		}
		for _, q := range []string{"", "folder_id=null", "title=" + url.QueryEscape("Sprint goals")} {
			if ids := listDocIDsViaAPI(t, env, client, token, projID, q); len(ids) != 1 || ids[0] != treeDoc {
				t.Errorf("docs tree ?%s = %v, want only [%s]", q, ids, treeDoc)
			}
		}
		status, e, _ := apiCall(env, t, client, token, http.MethodGet, docs+"?view_id="+notesView+"&folder_id=null", nil)
		if status != http.StatusBadRequest || e.ErrorCode != "DOC_NOTE_IN_FOLDER" {
			t.Errorf("view_id with folder_id: status=%d code=%q, want 400 DOC_NOTE_IN_FOLDER", status, e.ErrorCode)
		}
	})

	t.Run("no_path_reaches_it", func(t *testing.T) {
		status, e, _ := apiCall(env, t, client, token, http.MethodGet, docs+"/by-path?path="+url.QueryEscape("Sprint goals"), nil)
		if status != http.StatusOK || assertDataMap(t, e)["id"] != treeDoc {
			t.Fatalf("by-path Sprint goals: status=%d data=%v, want the tree document", status, e.Data)
		}
	})

	t.Run("edited_like_any_document", func(t *testing.T) {
		status, e, _ := apiCall(env, t, client, token, http.MethodPatch, docs+"/"+noteID, map[string]any{
			"title":   "Goals",
			"content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Ship notes", "styles": map[string]any{}}}}},
		})
		if status != http.StatusOK || assertDataMap(t, e)["title"] != "Goals" || assertDataMap(t, e)["view_id"] != notesView {
			t.Fatalf("edit note: status=%d code=%q data=%v", status, e.ErrorCode, e.Data)
		}
		folder := createFolderViaAPI(t, env, client, token, projID, "F", nil)
		status, e, _ = apiCall(env, t, client, token, http.MethodPatch, docs+"/"+noteID, map[string]any{"folder_id": folder})
		if status != http.StatusBadRequest || e.ErrorCode != "DOC_NOTE_IN_FOLDER" {
			t.Fatalf("move note to a folder: status=%d code=%q, want 400 DOC_NOTE_IN_FOLDER", status, e.ErrorCode)
		}
		status, e, _ = apiCall(env, t, client, token, http.MethodPost, docs, map[string]any{"title": "x", "view_id": notesView, "folder_id": folder})
		if status != http.StatusBadRequest || e.ErrorCode != "DOC_NOTE_IN_FOLDER" {
			t.Fatalf("create a note in a folder: status=%d code=%q, want 400 DOC_NOTE_IN_FOLDER", status, e.ErrorCode)
		}
	})

	t.Run("only_note_views_hold_notes", func(t *testing.T) {
		var taskView string
		for _, id := range listViewIDsViaAPI(t, env, client, token, projID, sprintID) {
			if id != notesView {
				taskView = id
				break
			}
		}
		status, e, _ := apiCall(env, t, client, token, http.MethodPost, docs, map[string]any{"title": "x", "view_id": taskView})
		if status != http.StatusBadRequest || e.ErrorCode != "DOC_NOTE_VIEW_INVALID" {
			t.Fatalf("note in a task view: status=%d code=%q, want 400 DOC_NOTE_VIEW_INVALID", status, e.ErrorCode)
		}
		status, e, _ = apiCall(env, t, client, token, http.MethodPost, docs, map[string]any{"title": "x", "view_id": uuid.NewString()})
		if status != http.StatusBadRequest || e.ErrorCode != "DOC_NOTE_VIEW_INVALID" {
			t.Fatalf("note in an unknown view: status=%d code=%q, want 400 DOC_NOTE_VIEW_INVALID", status, e.ErrorCode)
		}
		views := fmt.Sprintf("%s/api/v1/projects/%s/views/", env.base, projID)
		status, e, _ = apiCall(env, t, client, token, http.MethodPatch, views+notesView, map[string]any{"view_type": "table"})
		if status != http.StatusBadRequest || e.ErrorCode != "VIEW_NOTE_TYPE_CHANGE" {
			t.Fatalf("note view -> table: status=%d code=%q, want 400 VIEW_NOTE_TYPE_CHANGE", status, e.ErrorCode)
		}
		status, e, _ = apiCall(env, t, client, token, http.MethodPatch, views+taskView, map[string]any{"view_type": "note"})
		if status != http.StatusBadRequest || e.ErrorCode != "VIEW_NOTE_TYPE_CHANGE" {
			t.Fatalf("task view -> note view: status=%d code=%q, want 400 VIEW_NOTE_TYPE_CHANGE", status, e.ErrorCode)
		}
	})

	t.Run("deleted_with_the_view", func(t *testing.T) {
		deleteViewViaAPI(t, env, client, token, projID, sprintID, notesView)
		status, _, _ := apiCall(env, t, client, token, http.MethodGet, docs+"/"+noteID, nil)
		if status != http.StatusNotFound {
			t.Fatalf("note of a deleted view: status=%d, want 404", status)
		}
		var deleted bool
		if err := env.db.GetContext(env.ctx, &deleted, `SELECT deleted_at IS NOT NULL FROM documents WHERE id = $1`, noteID); err != nil {
			t.Fatalf("the note row is gone instead of soft-deleted: %v", err)
		}
		if !deleted {
			t.Fatal("the note is not soft-deleted")
		}
	})

	t.Run("deleted_with_the_sprint", func(t *testing.T) {
		other := createSprintViaAPI(t, env, client, token, projID, "Sprint 2")
		view := createViewViaAPI(t, env, client, token, projID, other, "Notes", "note")
		status, e, _ := apiCall(env, t, client, token, http.MethodPost, docs, map[string]any{"title": "Retro", "view_id": view})
		if status != http.StatusCreated {
			t.Fatalf("create note: status=%d code=%q", status, e.ErrorCode)
		}
		retro, _ := assertDataMap(t, e)["id"].(string)
		status, e, _ = apiCall(env, t, client, token, http.MethodDelete, fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s", env.base, projID, other), nil)
		if status != http.StatusNoContent && status != http.StatusOK {
			t.Fatalf("delete sprint: status=%d code=%q", status, e.ErrorCode)
		}
		if status, _, _ := apiCall(env, t, client, token, http.MethodGet, docs+"/"+retro, nil); status != http.StatusNotFound {
			t.Fatalf("note of a deleted sprint: status=%d, want 404", status)
		}
	})
}

// Migration 000094 can run again over a database it already migrated.
func TestE2ENoteView_MigrationIsRerunnable(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, thisFile, _, _ := runtime.Caller(0)
	migration, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "000094_add_note_view_type.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.ExecContext(env.ctx, string(migration)); err != nil {
		t.Fatalf("second run of the migration: %v", err)
	}
	var triggers int
	if err := env.db.GetContext(env.ctx, &triggers, `SELECT COUNT(*) FROM pg_trigger WHERE tgname = 'trg_sprint_views_delete_notes'`); err != nil {
		t.Fatal(err)
	}
	if triggers != 1 {
		t.Fatalf("%d note deletion triggers, want 1", triggers)
	}
}
