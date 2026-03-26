package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	userdom "github.com/paca/api/internal/domain/user"
)

// envelope mirrors the presenter.envelope shape for JSON decoding.
type envelope struct {
	Success   bool   `json:"success"`
	Data      any    `json:"data"`
	ErrorCode string `json:"error_code"`
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
}

func mustRequest(ctx context.Context, t *testing.T, method, url string, body *bytes.Buffer) *http.Request {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, url, body)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, http.NoBody)
	}
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, url, err)
	}
	return req
}

func mustDo(t *testing.T, c *http.Client, req *http.Request) *http.Response {
	t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", req.Method, req.URL, err)
	}
	return resp
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("expected HTTP %d, got %d", want, resp.StatusCode)
	}
}

func assertErrorCode(t *testing.T, resp *http.Response, wantCode string) {
	t.Helper()
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.ErrorCode != wantCode {
		t.Errorf("expected error_code %q, got %q (error: %q)", wantCode, env.ErrorCode, env.Error)
	}
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json body: %v", err)
	}
	return bytes.NewBuffer(b)
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
}

func assertDataMap(t *testing.T, env envelope) map[string]any {
	t.Helper()
	m, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be a JSON object, got %T: %v", env.Data, env.Data)
	}
	return m
}

func cookieValue(resp *http.Response, name string) string {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func seedUser(t *testing.T, env *e2eEnv, username, password, fullName string) {
	t.Helper()
	_, err := env.userService.Create(env.ctx, userdom.CreateInput{
		Username: username,
		Password: password,
		FullName: fullName,
	})
	if err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
}

func login(
	ctx context.Context,
	t *testing.T,
	client *http.Client,
	baseURL, username, password string,
) *http.Response {
	t.Helper()
	body := jsonBody(t, map[string]string{"username": username, "password": password})
	req := mustRequest(ctx, t, http.MethodPost, baseURL+"/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	resp := mustDo(t, client, req)
	assertStatus(t, resp, http.StatusOK)
	return resp
}
