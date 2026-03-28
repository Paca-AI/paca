package handler_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	domainuser "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/transport/http/handler"
)

// ---------------------------------------------------------------------------
// mock
// ---------------------------------------------------------------------------

type mockUserSvc struct {
	getByID               func(ctx context.Context, id uuid.UUID) (*domainuser.User, error)
	listGlobalPermissions func(ctx context.Context, id uuid.UUID) ([]string, error)
	create                func(ctx context.Context, in domainuser.CreateInput) (*domainuser.User, error)
	update                func(ctx context.Context, id uuid.UUID, in domainuser.UpdateInput) (*domainuser.User, error)
	delete                func(ctx context.Context, id uuid.UUID) error
}

func (m *mockUserSvc) GetByID(ctx context.Context, id uuid.UUID) (*domainuser.User, error) {
	if m.getByID != nil {
		return m.getByID(ctx, id)
	}
	return nil, domainuser.ErrNotFound
}
func (m *mockUserSvc) ListGlobalPermissions(ctx context.Context, id uuid.UUID) ([]string, error) {
	if m.listGlobalPermissions != nil {
		return m.listGlobalPermissions(ctx, id)
	}
	return []string{}, nil
}
func (m *mockUserSvc) Create(ctx context.Context, in domainuser.CreateInput) (*domainuser.User, error) {
	if m.create != nil {
		return m.create(ctx, in)
	}
	return nil, errors.New("mock: create not configured")
}
func (m *mockUserSvc) Update(ctx context.Context, id uuid.UUID, in domainuser.UpdateInput) (*domainuser.User, error) {
	if m.update != nil {
		return m.update(ctx, id, in)
	}
	return nil, domainuser.ErrNotFound
}
func (m *mockUserSvc) Delete(ctx context.Context, id uuid.UUID) error {
	if m.delete != nil {
		return m.delete(ctx, id)
	}
	return nil
}

// verify mock satisfies the interface at compile time
var _ domainuser.Service = (*mockUserSvc)(nil)

// ---------------------------------------------------------------------------
// helper
// ---------------------------------------------------------------------------

func newUserRouter(svc domainuser.Service) *gin.Engine {
	r := gin.New()
	h := handler.NewUserHandler(svc)
	r.POST("/users", h.Create)
	r.GET("/users/me", h.GetMe)
	r.GET("/users/me/global-permissions", h.GetMyGlobalPermissions)
	r.PATCH("/users/:id", h.Update)
	r.DELETE("/users/:id", h.Delete)
	return r
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestUserCreate_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		create: func(_ context.Context, in domainuser.CreateInput) (*domainuser.User, error) {
			return &domainuser.User{ID: id, Username: in.Username, FullName: in.FullName, Role: domainuser.RoleUser}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPost, "/users",
		jsonBody(t, map[string]string{"username": "alice", "password": "pass1234", "full_name": "Alice"}))
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUserCreate_MalformedJSON(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPost, "/users", bytes.NewBufferString("{bad body"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestUserCreate_UsernameTaken(t *testing.T) {
	svc := &mockUserSvc{
		create: func(_ context.Context, _ domainuser.CreateInput) (*domainuser.User, error) {
			return nil, domainuser.ErrUsernameTaken
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPost, "/users",
		jsonBody(t, map[string]string{"username": "bob", "password": "pass1234", "full_name": "Bob"}))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_USERNAME_TAKEN" {
		t.Errorf("expected error_code USER_USERNAME_TAKEN, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// GetMe
// ---------------------------------------------------------------------------

func TestGetMe_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*domainuser.User, error) {
			return &domainuser.User{ID: id, Username: "me", Role: domainuser.RoleUser}, nil
		},
	}
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.GET("/users/me", injectClaims(claims), handler.NewUserHandler(svc).GetMe)

	w := do(t, r, http.MethodGet, "/users/me", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetMe_Unauthenticated(t *testing.T) {
	r := gin.New()
	// No claims middleware — ClaimsFrom will return nil.
	r.GET("/users/me", handler.NewUserHandler(&mockUserSvc{}).GetMe)

	w := do(t, r, http.MethodGet, "/users/me", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "AUTH_UNAUTHENTICATED" {
		t.Errorf("expected error_code AUTH_UNAUTHENTICATED, got %q", code)
	}
}

func TestGetMe_NotFound(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*domainuser.User, error) {
			return nil, domainuser.ErrNotFound
		},
	}
	r := gin.New()
	claims := testClaims(id.String(), "a", "USER")
	r.GET("/users/me", injectClaims(claims), handler.NewUserHandler(svc).GetMe)

	w := do(t, r, http.MethodGet, "/users/me", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected error_code USER_NOT_FOUND, got %q", code)
	}
}

func TestGetMyGlobalPermissions_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		listGlobalPermissions: func(_ context.Context, got uuid.UUID) ([]string, error) {
			if got != id {
				t.Fatalf("unexpected id: %v", got)
			}
			return []string{"global_roles.read", "users.read"}, nil
		},
	}
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.GET("/users/me/global-permissions", injectClaims(claims), handler.NewUserHandler(svc).GetMyGlobalPermissions)

	w := do(t, r, http.MethodGet, "/users/me/global-permissions", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetMyGlobalPermissions_Unauthenticated(t *testing.T) {
	r := gin.New()
	r.GET("/users/me/global-permissions", handler.NewUserHandler(&mockUserSvc{}).GetMyGlobalPermissions)

	w := do(t, r, http.MethodGet, "/users/me/global-permissions", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "AUTH_UNAUTHENTICATED" {
		t.Errorf("expected error_code AUTH_UNAUTHENTICATED, got %q", code)
	}
}

func TestGetMyGlobalPermissions_InvalidSubjectClaim(t *testing.T) {
	r := gin.New()
	claims := testClaims("not-a-uuid", "me", "USER")
	r.GET("/users/me/global-permissions", injectClaims(claims), handler.NewUserHandler(&mockUserSvc{}).GetMyGlobalPermissions)

	w := do(t, r, http.MethodGet, "/users/me/global-permissions", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestGetMyGlobalPermissions_ServiceError(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		listGlobalPermissions: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return nil, domainuser.ErrNotFound
		},
	}
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.GET("/users/me/global-permissions", injectClaims(claims), handler.NewUserHandler(svc).GetMyGlobalPermissions)

	w := do(t, r, http.MethodGet, "/users/me/global-permissions", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected error_code USER_NOT_FOUND, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestUserUpdate_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		update: func(_ context.Context, _ uuid.UUID, in domainuser.UpdateInput) (*domainuser.User, error) {
			return &domainuser.User{ID: id, FullName: in.FullName, Role: domainuser.RoleUser}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/users/%s", id),
		jsonBody(t, map[string]string{"full_name": "New Name"}))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUserUpdate_BadID(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPatch, "/users/not-a-uuid",
		jsonBody(t, map[string]string{"full_name": "X"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestUserUpdate_NotFound(t *testing.T) {
	svc := &mockUserSvc{
		update: func(_ context.Context, _ uuid.UUID, _ domainuser.UpdateInput) (*domainuser.User, error) {
			return nil, domainuser.ErrNotFound
		},
	}
	r := newUserRouter(svc)
	id := uuid.New()

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/users/%s", id),
		jsonBody(t, map[string]string{"full_name": "X"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected error_code USER_NOT_FOUND, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestUserDelete_Success(t *testing.T) {
	deleted := false
	svc := &mockUserSvc{
		delete: func(_ context.Context, _ uuid.UUID) error {
			deleted = true
			return nil
		},
	}
	r := newUserRouter(svc)
	id := uuid.New()

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/users/%s", id), nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !deleted {
		t.Error("expected svc.Delete to be called")
	}
}

func TestUserDelete_BadID(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodDelete, "/users/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestUserDelete_NotFound(t *testing.T) {
	svc := &mockUserSvc{
		delete: func(_ context.Context, _ uuid.UUID) error {
			return domainuser.ErrNotFound
		},
	}
	r := newUserRouter(svc)
	id := uuid.New()

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/users/%s", id), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected error_code USER_NOT_FOUND, got %q", code)
	}
}
