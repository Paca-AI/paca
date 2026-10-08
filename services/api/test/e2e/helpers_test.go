package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/bootstrap/defaultroles"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
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

// platformRoots are the resources a platform role is limited to (never
// "project/*": reaching into a project takes a project-scoped attachment).
const platformRoots = `"user","user/*","role","role/*","plugin","plugin/*","settings","sso","agent","agent/*","project"`

// platformPolicy is the IAM policy of a platform role allowing exactly actions
// on the platform resources.
func platformPolicy(actions ...string) string {
	quoted := make([]string, len(actions))
	for i, a := range actions {
		quoted[i] = strconv.Quote(a)
	}
	return `{"version":"2026-10-01","statements":[{"effect":"Allow","actions":[` + strings.Join(quoted, ",") +
		`],"resources":[` + platformRoots + `]}]}`
}

// seedShippedRoles creates the shipped platform roles (SUPER_ADMIN, ADMIN,
// USER) exactly as the API does at startup, and makes USER the default.
func seedShippedRoles(t *testing.T, db *sqlx.DB) {
	t.Helper()
	seeder := pgRepo.NewRoleSeedRepository(db)
	for _, def := range defaultroles.Platform() {
		if _, err := seeder.UpsertSystemRole(context.Background(), pgRepo.SeedRole{
			Name: def.Name, Description: def.Description, Policy: def.Policy,
		}); err != nil {
			t.Fatalf("seed role %q: %v", def.Name, err)
		}
	}
	if _, err := seeder.EnsureDefaultRole(context.Background(), defaultroles.User); err != nil {
		t.Fatalf("seed default role: %v", err)
	}
}

// createPlatformRole creates a platform role that allows exactly actions on the
// platform resources and returns its id.
func createPlatformRole(t *testing.T, env *e2eEnv, name string, actions ...string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := env.db.ExecContext(t.Context(), `INSERT INTO roles (id, name, policy) VALUES ($1, $2, $3::jsonb)`,
		id, name, platformPolicy(actions...)); err != nil {
		t.Fatalf("create platform role %q: %v", name, err)
	}
	return id
}

// setPlatformRoleActions replaces the policy of the platform role named name
// with one allowing exactly actions, and drops the cached policy.
func setPlatformRoleActions(t *testing.T, env *e2eEnv, name string, actions ...string) {
	t.Helper()
	var id uuid.UUID
	if err := env.db.Get(&id, `UPDATE roles SET policy = $2::jsonb, updated_at = NOW()
		WHERE name = $1 AND project_id IS NULL RETURNING id`, name, platformPolicy(actions...)); err != nil {
		t.Fatalf("update platform role %q: %v", name, err)
	}
	env.authz.Invalidate(id.String())
}

// platformRoleID returns the id of the platform role named name.
func platformRoleID(t *testing.T, env *e2eEnv, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := env.db.Get(&id, `SELECT id FROM roles WHERE name = $1 AND project_id IS NULL`, name); err != nil {
		t.Fatalf("find platform role %q: %v", name, err)
	}
	return id
}

// platformRoleExists reports whether a platform role named name exists.
func platformRoleExists(t *testing.T, env *e2eEnv, name string) bool {
	t.Helper()
	var n int
	if err := env.db.Get(&n, `SELECT COUNT(*) FROM roles WHERE name = $1 AND project_id IS NULL`, name); err != nil {
		t.Fatalf("count platform roles %q: %v", name, err)
	}
	return n > 0
}

// attachPlatformRole replaces userID's platform-wide attachments with the
// platform roles named roleNames.
func attachPlatformRole(t *testing.T, env *e2eEnv, userID uuid.UUID, roleNames ...string) {
	t.Helper()
	if _, err := env.db.ExecContext(t.Context(), `DELETE FROM role_attachments WHERE principal_type = 'user' AND principal_id = $1 AND project_id IS NULL`, userID); err != nil {
		t.Fatalf("clear platform roles: %v", err)
	}
	for _, name := range roleNames {
		res, err := env.db.ExecContext(t.Context(), `INSERT INTO role_attachments (role_id, principal_type, principal_id)
			SELECT id, 'user', $1 FROM roles WHERE name = $2 AND project_id IS NULL`, userID, name)
		if err != nil {
			t.Fatalf("attach platform role %q: %v", name, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			t.Fatalf("platform role %q does not exist", name)
		}
	}
}

// roleNames returns the names of the roles listed under data["roles"] of a
// user, member or agent response.
func roleNames(data map[string]any) []string {
	var names []string
	roles, _ := data["roles"].([]any)
	for _, r := range roles {
		m, _ := r.(map[string]any)
		if name, _ := m["name"].(string); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func seedUser(t *testing.T, env *e2eEnv, username, password, fullName string) {
	t.Helper()
	_, err := env.userService.Create(env.ctx, userdom.CreateInput{
		Username:           username,
		Password:           password,
		FullName:           fullName,
		MustChangePassword: false,
	})
	if err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
}

// assignPlatformRole replaces username's platform roles with the roles named
// roleNames (USER when none are given).
func assignPlatformRole(t *testing.T, env *e2eEnv, username string, roleNames ...string) {
	t.Helper()

	user, err := env.userRepo.FindByUsername(env.ctx, username)
	if err != nil {
		t.Fatalf("find user %q: %v", username, err)
	}
	if len(roleNames) == 0 {
		roleNames = []string{"USER"}
	}
	attachPlatformRole(t, env, user.ID, roleNames...)
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

func loginWithRememberMe(
	ctx context.Context,
	t *testing.T,
	client *http.Client,
	baseURL, username, password string,
	rememberMe bool,
) *http.Response {
	t.Helper()
	body := jsonBody(t, map[string]any{
		"username":    username,
		"password":    password,
		"remember_me": rememberMe,
	})
	req := mustRequest(ctx, t, http.MethodPost, baseURL+"/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	resp := mustDo(t, client, req)
	assertStatus(t, resp, http.StatusOK)
	return resp
}

// findCookie returns the named Set-Cookie from resp, or nil if not present.
func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// e2eActionsReader lists a user's effective platform-level IAM actions — the
// users service's GlobalPermissionReader, as bootstrap wires it.
type e2eActionsReader struct{ a *iam.Authorizer }

func (r e2eActionsReader) ListGlobalPermissions(ctx context.Context, userID uuid.UUID) ([]iam.Action, error) {
	acts, err := r.a.EffectiveActions(ctx, iam.User(userID.String()), "")
	out := make([]iam.Action, len(acts))
	for i, a := range acts {
		out[i] = iam.Action(a)
	}
	return out, err
}
