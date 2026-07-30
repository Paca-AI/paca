package e2e_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func seedAttachmentUser(t *testing.T, env *e2eEnv, username, password string) {
	t.Helper()
	seedUser(t, env, username, password, "Attachment Member")
	roleName := "ATTACH_MEMBER_" + uuid.NewString()
	if err := env.roleRepo.Create(env.ctx, &globalroledom.GlobalRole{
		ID:   uuid.New(),
		Name: roleName,
		Permissions: map[string]any{
			"projects.create": true,
			"projects.read":   true,
			"projects.write":  true,
			"tasks.read":      true,
			"tasks.write":     true,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create attachment-member role: %v", err)
	}
	assignGlobalRolesByName(t, env, username, roleName)
}

func attachmentUserLogin(t *testing.T, env *e2eEnv, username, password string) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	resp := login(env.ctx, t, client, env.base, username, password)
	defer func() { _ = resp.Body.Close() }()
	token := cookieValue(resp, "access_token")
	return client, token
}

func createTaskForAttachmentsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projectID)
	body := jsonBody(t, map[string]any{
		"title":      "attachment-test-task-" + uuid.NewString(),
		"importance": 1,
	})
	req := mustRequest(env.ctx, t, http.MethodPost, url, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	id, _ := data["id"].(string)
	return id
}

func attachmentPath(base, projectID, taskID, suffix string) string {
	return fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/attachments%s", base, projectID, taskID, suffix)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestE2EAttachmentManagement_CRUD(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	if env.attachmentSvc == nil {
		t.Skip("attachment service not available (MinIO container did not start)")
	}

	seedAttachmentUser(t, env, "attach-crud-user", "attachpass1!")
	client, token := attachmentUserLogin(t, env, "attach-crud-user", "attachpass1!")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	taskID := createTaskForAttachmentsViaAPI(t, env, client, token, projID)

	fileContent := "hello attachment world"
	fileSize := int64(len(fileContent))

	var fileID, uploadURL, attachmentID string

	t.Run("initiate_upload", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"file_name":    "hello.txt",
			"content_type": "text/plain",
			"file_size":    fileSize,
		})
		req := mustRequest(env.ctx, t, http.MethodPost, attachmentPath(env.base, projID, taskID, "/initiate-upload"), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)

		fileID, _ = data["file_id"].(string)
		if fileID == "" {
			t.Fatal("expected non-empty file_id in initiate-upload response")
		}
		if data["is_multipart"] == true {
			t.Fatal("expected single-part upload for small file")
		}
		uploadURL, _ = data["upload_url"].(string)
		if uploadURL == "" {
			t.Fatal("expected non-empty upload_url for single-part upload")
		}
	})

	t.Run("put_to_presigned_url", func(t *testing.T) {
		if uploadURL == "" {
			t.Skip("upload_url not available (previous subtest failed)")
		}
		putReq, err := http.NewRequestWithContext(env.ctx, http.MethodPut, uploadURL, strings.NewReader(fileContent))
		if err != nil {
			t.Fatalf("build PUT request: %v", err)
		}
		putReq.Header.Set("Content-Type", "text/plain")
		putReq.ContentLength = fileSize

		// Use a plain client — no cookies for external object-store URLs.
		plainClient := &http.Client{Timeout: 30 * time.Second}
		putResp, err := plainClient.Do(putReq)
		if err != nil {
			t.Fatalf("PUT to presigned URL: %v", err)
		}
		defer func() { _ = putResp.Body.Close() }()
		if putResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(putResp.Body)
			t.Fatalf("PUT to presigned URL returned %d: %s", putResp.StatusCode, body)
		}
	})

	t.Run("complete_upload", func(t *testing.T) {
		if fileID == "" {
			t.Skip("file_id not available (previous subtest failed)")
		}
		body := jsonBody(t, map[string]any{
			"file_id": fileID,
		})
		req := mustRequest(env.ctx, t, http.MethodPost, attachmentPath(env.base, projID, taskID, "/complete-upload"), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)

		attachmentID, _ = data["id"].(string)
		if attachmentID == "" {
			t.Fatal("expected non-empty attachment id in complete-upload response")
		}
		if tid, _ := data["task_id"].(string); tid != taskID {
			t.Errorf("expected task_id %q, got %q", taskID, tid)
		}
	})

	t.Run("list_attachments", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, attachmentPath(env.base, projID, taskID, ""), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected 1 attachment, got %d", len(items))
		}
		item, _ := items[0].(map[string]any)
		if id, _ := item["id"].(string); id != attachmentID {
			t.Errorf("listed attachment id %q doesn't match created %q", id, attachmentID)
		}
		// Embedded file metadata present.
		if item["file"] == nil {
			t.Error("expected embedded file metadata in list response")
		}
	})

	t.Run("get_download_url_inline", func(t *testing.T) {
		if attachmentID == "" {
			t.Skip("attachment_id not available (previous subtest failed)")
		}
		path := attachmentPath(env.base, projID, taskID, "/"+attachmentID+"/download-url")
		req := mustRequest(env.ctx, t, http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		dlURL, _ := data["url"].(string)
		if dlURL == "" {
			t.Fatal("expected non-empty url in download-url response")
		}
		if strings.Contains(strings.ToLower(dlURL), "response-content-disposition") {
			t.Errorf("inline preview URL should not contain content-disposition param, got %q", dlURL)
		}
	})

	t.Run("get_download_url_force_download", func(t *testing.T) {
		if attachmentID == "" {
			t.Skip("attachment_id not available (previous subtest failed)")
		}
		path := attachmentPath(env.base, projID, taskID, "/"+attachmentID+"/download-url?download=true")
		req := mustRequest(env.ctx, t, http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		dlURL, _ := data["url"].(string)
		if dlURL == "" {
			t.Fatal("expected non-empty url in download-url response")
		}
		if !strings.Contains(strings.ToLower(dlURL), "response-content-disposition") {
			t.Errorf("force-download URL should contain response-content-disposition param, got %q", dlURL)
		}
	})

	t.Run("delete_attachment", func(t *testing.T) {
		if attachmentID == "" {
			t.Skip("attachment_id not available (previous subtest failed)")
		}
		path := attachmentPath(env.base, projID, taskID, "/"+attachmentID)
		req := mustRequest(env.ctx, t, http.MethodDelete, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("list_attachments_after_delete", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, attachmentPath(env.base, projID, taskID, ""), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 0 {
			t.Fatalf("expected 0 attachments after delete, got %d", len(items))
		}
	})
}

func TestE2EAttachmentManagement_Unauthenticated(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	if env.attachmentSvc == nil {
		t.Skip("attachment service not available (MinIO container did not start)")
	}

	projID := uuid.NewString()
	taskID := uuid.NewString()

	t.Run("initiate_upload_requires_auth", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"file_name":    "x.txt",
			"content_type": "text/plain",
			"file_size":    1,
		})
		req := mustRequest(env.ctx, t, http.MethodPost, attachmentPath(env.base, projID, taskID, "/initiate-upload"), body)
		req.Header.Set("Content-Type", "application/json")
		// No Authorization header.
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("list_attachments_requires_auth", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, attachmentPath(env.base, projID, taskID, ""), nil)
		// No Authorization header.
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})
}
