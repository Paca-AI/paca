package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	list                  func(ctx context.Context, page, pageSize int) ([]*domainuser.User, int64, error)
	listGlobalPermissions func(ctx context.Context, id uuid.UUID) ([]string, error)
	create                func(ctx context.Context, in domainuser.CreateInput) (*domainuser.User, error)
	updateProfile         func(ctx context.Context, id uuid.UUID, in domainuser.UpdateProfileInput) (*domainuser.User, error)
	adminUpdate           func(ctx context.Context, id uuid.UUID, in domainuser.AdminUpdateInput) (*domainuser.User, error)
	resetPassword         func(ctx context.Context, id uuid.UUID, newPassword string) error
	changeMyPassword      func(ctx context.Context, id uuid.UUID, currentPassword, newPassword string) error
	delete                func(ctx context.Context, id uuid.UUID) error
}

func (m *mockUserSvc) GetByID(ctx context.Context, id uuid.UUID) (*domainuser.User, error) {
	if m.getByID != nil {
		return m.getByID(ctx, id)
	}
	return nil, domainuser.ErrNotFound
}
func (m *mockUserSvc) List(ctx context.Context, page, pageSize int) ([]*domainuser.User, int64, error) {
	if m.list != nil {
		return m.list(ctx, page, pageSize)
	}
	return nil, 0, nil
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
func (m *mockUserSvc) UpdateProfile(ctx context.Context, id uuid.UUID, in domainuser.UpdateProfileInput) (*domainuser.User, error) {
	if m.updateProfile != nil {
		return m.updateProfile(ctx, id, in)
	}
	return nil, domainuser.ErrNotFound
}
func (m *mockUserSvc) AdminUpdate(ctx context.Context, id uuid.UUID, in domainuser.AdminUpdateInput) (*domainuser.User, error) {
	if m.adminUpdate != nil {
		return m.adminUpdate(ctx, id, in)
	}
	return nil, domainuser.ErrNotFound
}
func (m *mockUserSvc) Delete(ctx context.Context, id uuid.UUID) error {
	if m.delete != nil {
		return m.delete(ctx, id)
	}
	return nil
}
func (m *mockUserSvc) ResetPassword(ctx context.Context, id uuid.UUID, newPassword string) error {
	if m.resetPassword != nil {
		return m.resetPassword(ctx, id, newPassword)
	}
	return nil
}
func (m *mockUserSvc) ChangeMyPassword(ctx context.Context, id uuid.UUID, currentPassword, newPassword string) error {
	if m.changeMyPassword != nil {
		return m.changeMyPassword(ctx, id, currentPassword, newPassword)
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
	// self-service routes
	r.GET("/users/me", h.GetMe)
	r.PATCH("/users/me", h.UpdateMe)
	r.GET("/users/me/global-permissions", h.GetMyGlobalPermissions)
	// admin routes
	r.GET("/admin/users", h.ListUsers)
	r.POST("/admin/users", h.CreateUser)
	r.GET("/admin/users/:userId", h.GetUserByID)
	r.PATCH("/admin/users/:userId", h.AdminUpdateUser)
	r.PATCH("/admin/users/:userId/password", h.ResetPassword)
	r.DELETE("/admin/users/:userId", h.DeleteUser)
	return r
}

// ---------------------------------------------------------------------------
// CreateUser (admin)
// ---------------------------------------------------------------------------

func TestCreateUser_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		create: func(_ context.Context, in domainuser.CreateInput) (*domainuser.User, error) {
			return &domainuser.User{ID: id, Username: in.Username, FullName: in.FullName, Role: domainuser.RoleUser}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPost, "/admin/users",
		jsonBody(t, map[string]string{"username": "alice", "password": "pass1234", "full_name": "Alice"}))
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateUser_WithRole(t *testing.T) {
	id := uuid.New()
	var capturedRole string
	svc := &mockUserSvc{
		create: func(_ context.Context, in domainuser.CreateInput) (*domainuser.User, error) {
			capturedRole = in.Role
			return &domainuser.User{ID: id, Username: in.Username, FullName: in.FullName, Role: in.Role}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPost, "/admin/users",
		jsonBody(t, map[string]string{"username": "bob", "password": "pass1234", "full_name": "Bob", "role": "ADMIN"}))
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if capturedRole != "ADMIN" {
		t.Errorf("expected role ADMIN, got %q", capturedRole)
	}
}

func TestCreateUser_MalformedJSON(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPost, "/admin/users", bytes.NewBufferString("{bad body"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestCreateUser_UsernameTaken(t *testing.T) {
	svc := &mockUserSvc{
		create: func(_ context.Context, _ domainuser.CreateInput) (*domainuser.User, error) {
			return nil, domainuser.ErrUsernameTaken
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPost, "/admin/users",
		jsonBody(t, map[string]string{"username": "bob", "password": "pass1234", "full_name": "Bob"}))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_USERNAME_TAKEN" {
		t.Errorf("expected error_code USER_USERNAME_TAKEN, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// ListUsers (admin)
// ---------------------------------------------------------------------------

func TestListUsers_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		list: func(_ context.Context, page, pageSize int) ([]*domainuser.User, int64, error) {
			return []*domainuser.User{
				{ID: id, Username: "alice", FullName: "Alice", Role: domainuser.RoleUser},
			}, 1, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodGet, "/admin/users?page=1&page_size=10", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListUsers_ServiceError(t *testing.T) {
	svc := &mockUserSvc{
		list: func(_ context.Context, _, _ int) ([]*domainuser.User, int64, error) {
			return nil, 0, errors.New("db error")
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodGet, "/admin/users", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GetUserByID (admin)
// ---------------------------------------------------------------------------

func TestGetUserByID_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		getByID: func(_ context.Context, got uuid.UUID) (*domainuser.User, error) {
			if got != id {
				t.Fatalf("unexpected id: %v", got)
			}
			return &domainuser.User{ID: id, Username: "alice", Role: domainuser.RoleUser}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodGet, fmt.Sprintf("/admin/users/%s", id), nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetUserByID_BadID(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodGet, "/admin/users/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestGetUserByID_NotFound(t *testing.T) {
	svc := &mockUserSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*domainuser.User, error) {
			return nil, domainuser.ErrNotFound
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodGet, fmt.Sprintf("/admin/users/%s", uuid.New()), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected error_code USER_NOT_FOUND, got %q", code)
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

// ---------------------------------------------------------------------------
// UpdateMe (self-service)
// ---------------------------------------------------------------------------

func TestUpdateMe_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		updateProfile: func(_ context.Context, got uuid.UUID, in domainuser.UpdateProfileInput) (*domainuser.User, error) {
			if got != id {
				t.Fatalf("unexpected id: %v", got)
			}
			return &domainuser.User{ID: id, FullName: in.FullName, Role: domainuser.RoleUser}, nil
		},
	}
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.PATCH("/users/me", injectClaims(claims), handler.NewUserHandler(svc).UpdateMe)

	w := do(t, r, http.MethodPatch, "/users/me",
		jsonBody(t, map[string]string{"full_name": "New Name"}))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMe_Unauthenticated(t *testing.T) {
	r := gin.New()
	r.PATCH("/users/me", handler.NewUserHandler(&mockUserSvc{}).UpdateMe)

	w := do(t, r, http.MethodPatch, "/users/me",
		jsonBody(t, map[string]string{"full_name": "Name"}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// GetMyGlobalPermissions
// ---------------------------------------------------------------------------

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
// AdminUpdateUser (admin)
// ---------------------------------------------------------------------------

func TestAdminUpdateUser_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		adminUpdate: func(_ context.Context, got uuid.UUID, in domainuser.AdminUpdateInput) (*domainuser.User, error) {
			if got != id {
				t.Fatalf("unexpected id: %v", got)
			}
			return &domainuser.User{ID: id, FullName: in.FullName, Role: domainuser.RoleUser}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/users/%s", id),
		jsonBody(t, map[string]string{"full_name": "New Name"}))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminUpdateUser_BadID(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPatch, "/admin/users/not-a-uuid",
		jsonBody(t, map[string]string{"full_name": "X"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestAdminUpdateUser_NotFound(t *testing.T) {
	svc := &mockUserSvc{
		adminUpdate: func(_ context.Context, _ uuid.UUID, _ domainuser.AdminUpdateInput) (*domainuser.User, error) {
			return nil, domainuser.ErrNotFound
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/users/%s", uuid.New()),
		jsonBody(t, map[string]string{"full_name": "X"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected error_code USER_NOT_FOUND, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// DeleteUser (admin)
// ---------------------------------------------------------------------------

func TestDeleteUser_Success(t *testing.T) {
	deleted := false
	svc := &mockUserSvc{
		delete: func(_ context.Context, _ uuid.UUID) error {
			deleted = true
			return nil
		},
	}
	r := newUserRouter(svc)
	id := uuid.New()

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/admin/users/%s", id), nil)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if !deleted {
		t.Error("expected svc.Delete to be called")
	}
}

func TestDeleteUser_BadID(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodDelete, "/admin/users/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestDeleteUser_NotFound(t *testing.T) {
	svc := &mockUserSvc{
		delete: func(_ context.Context, _ uuid.UUID) error {
			return domainuser.ErrNotFound
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/admin/users/%s", uuid.New()), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected error_code USER_NOT_FOUND, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// ResetPassword (admin)
// ---------------------------------------------------------------------------

func TestResetPassword_Success(t *testing.T) {
	id := uuid.New()
	resetCalled := false
	svc := &mockUserSvc{
		resetPassword: func(_ context.Context, got uuid.UUID, pw string) error {
			if got != id {
				t.Fatalf("unexpected id: %v", got)
			}
			if pw != "newpassword123" {
				t.Fatalf("unexpected password: %q", pw)
			}
			resetCalled = true
			return nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/users/%s/password", id),
		jsonBody(t, map[string]string{"new_password": "newpassword123"}))
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if !resetCalled {
		t.Error("expected svc.ResetPassword to be called")
	}
}

func TestResetPassword_UserNotFound(t *testing.T) {
	svc := &mockUserSvc{
		resetPassword: func(_ context.Context, _ uuid.UUID, _ string) error {
			return domainuser.ErrNotFound
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/users/%s/password", uuid.New()),
		jsonBody(t, map[string]string{"new_password": "newpassword123"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected error_code USER_NOT_FOUND, got %q", code)
	}
}

func TestResetPassword_BadID(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPatch, "/admin/users/not-a-uuid/password",
		jsonBody(t, map[string]string{"new_password": "newpassword123"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestResetPassword_MalformedJSON(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/users/%s/password", uuid.New()),
		bytes.NewBufferString("{bad json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestResetPassword_PasswordTooShort(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/users/%s/password", uuid.New()),
		jsonBody(t, map[string]string{"new_password": "short"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too-short password, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// ChangeMyPassword (self-service)
// ---------------------------------------------------------------------------

func TestChangeMyPassword_Success(t *testing.T) {
	id := uuid.New()
	called := false
	svc := &mockUserSvc{
		changeMyPassword: func(_ context.Context, got uuid.UUID, current, next string) error {
			if got != id {
				t.Fatalf("unexpected id: %v", got)
			}
			if current != "oldpass123" {
				t.Fatalf("unexpected current password: %q", current)
			}
			if next != "newpass456" {
				t.Fatalf("unexpected new password: %q", next)
			}
			called = true
			return nil
		},
	}
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.PATCH("/users/me/password", injectClaims(claims), handler.NewUserHandler(svc).ChangeMyPassword)

	w := do(t, r, http.MethodPatch, "/users/me/password",
		jsonBody(t, map[string]string{"current_password": "oldpass123", "new_password": "newpass456"}))
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Error("expected svc.ChangeMyPassword to be called")
	}
}

func TestChangeMyPassword_Unauthenticated(t *testing.T) {
	r := gin.New()
	r.PATCH("/users/me/password", handler.NewUserHandler(&mockUserSvc{}).ChangeMyPassword)

	w := do(t, r, http.MethodPatch, "/users/me/password",
		jsonBody(t, map[string]string{"current_password": "old12345", "new_password": "new12345"}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "AUTH_UNAUTHENTICATED" {
		t.Errorf("expected error_code AUTH_UNAUTHENTICATED, got %q", code)
	}
}

func TestChangeMyPassword_MalformedJSON(t *testing.T) {
	id := uuid.New()
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.PATCH("/users/me/password", injectClaims(claims), handler.NewUserHandler(&mockUserSvc{}).ChangeMyPassword)

	w := do(t, r, http.MethodPatch, "/users/me/password", bytes.NewBufferString("{bad json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestChangeMyPassword_NewPasswordTooShort(t *testing.T) {
	id := uuid.New()
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.PATCH("/users/me/password", injectClaims(claims), handler.NewUserHandler(&mockUserSvc{}).ChangeMyPassword)

	w := do(t, r, http.MethodPatch, "/users/me/password",
		jsonBody(t, map[string]string{"current_password": "old12345", "new_password": "short"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too-short new password, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestChangeMyPassword_WrongCurrentPassword(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		changeMyPassword: func(_ context.Context, _ uuid.UUID, _, _ string) error {
			return domainuser.ErrInvalidCurrentPassword
		},
	}
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.PATCH("/users/me/password", injectClaims(claims), handler.NewUserHandler(svc).ChangeMyPassword)

	w := do(t, r, http.MethodPatch, "/users/me/password",
		jsonBody(t, map[string]string{"current_password": "wrongpass", "new_password": "newpass456"}))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != "USER_INVALID_CURRENT_PASSWORD" {
		t.Errorf("expected error_code USER_INVALID_CURRENT_PASSWORD, got %q", code)
	}
}

func TestChangeMyPassword_InvalidSubjectClaim(t *testing.T) {
	r := gin.New()
	claims := testClaims("not-a-uuid", "me", "USER")
	r.PATCH("/users/me/password", injectClaims(claims), handler.NewUserHandler(&mockUserSvc{}).ChangeMyPassword)

	w := do(t, r, http.MethodPatch, "/users/me/password",
		jsonBody(t, map[string]string{"current_password": "old12345", "new_password": "new12345"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// UpdateMe — additional cases
// ---------------------------------------------------------------------------

func TestUpdateMe_MalformedJSON(t *testing.T) {
	id := uuid.New()
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.PATCH("/users/me", injectClaims(claims), handler.NewUserHandler(&mockUserSvc{}).UpdateMe)

	w := do(t, r, http.MethodPatch, "/users/me", bytes.NewBufferString("{bad json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestUpdateMe_NotFound(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		updateProfile: func(_ context.Context, _ uuid.UUID, _ domainuser.UpdateProfileInput) (*domainuser.User, error) {
			return nil, domainuser.ErrNotFound
		},
	}
	r := gin.New()
	claims := testClaims(id.String(), "me", "USER")
	r.PATCH("/users/me", injectClaims(claims), handler.NewUserHandler(svc).UpdateMe)

	w := do(t, r, http.MethodPatch, "/users/me",
		jsonBody(t, map[string]string{"full_name": "New Name"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected error_code USER_NOT_FOUND, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// CreateUser — additional validation cases
// ---------------------------------------------------------------------------

func TestCreateUser_MissingUsername(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPost, "/admin/users",
		jsonBody(t, map[string]string{"password": "pass1234", "full_name": "No Username"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestCreateUser_PasswordTooShort(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPost, "/admin/users",
		jsonBody(t, map[string]string{"username": "alice", "password": "short", "full_name": "Alice"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for too-short password, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// ListUsers — response shape
// ---------------------------------------------------------------------------

func TestListUsers_ResponseShape(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		list: func(_ context.Context, page, pageSize int) ([]*domainuser.User, int64, error) {
			return []*domainuser.User{
				{ID: id, Username: "alice", FullName: "Alice", Role: domainuser.RoleUser},
			}, 1, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodGet, "/admin/users?page=2&page_size=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Items    []any `json:"items"`
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !env.Success {
		t.Fatal("expected success=true")
	}
	if env.Data.Total != 1 {
		t.Errorf("expected total=1, got %d", env.Data.Total)
	}
	if env.Data.Page != 2 {
		t.Errorf("expected page=2, got %d", env.Data.Page)
	}
	if env.Data.PageSize != 5 {
		t.Errorf("expected page_size=5, got %d", env.Data.PageSize)
	}
	if len(env.Data.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(env.Data.Items))
	}
}

// ---------------------------------------------------------------------------
// AdminUpdateUser — additional cases
// ---------------------------------------------------------------------------

func TestAdminUpdateUser_MalformedJSON(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/users/%s", uuid.New()),
		bytes.NewBufferString("{bad json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "BAD_REQUEST" {
		t.Errorf("expected error_code BAD_REQUEST, got %q", code)
	}
}

func TestAdminUpdateUser_RoleChange(t *testing.T) {
	id := uuid.New()
	var capturedRole string
	svc := &mockUserSvc{
		adminUpdate: func(_ context.Context, _ uuid.UUID, in domainuser.AdminUpdateInput) (*domainuser.User, error) {
			capturedRole = in.Role
			return &domainuser.User{ID: id, FullName: in.FullName, Role: in.Role}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/users/%s", id),
		jsonBody(t, map[string]string{"role": "ADMIN"}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if capturedRole != "ADMIN" {
		t.Errorf("expected role=ADMIN forwarded, got %q", capturedRole)
	}
}

// ---------------------------------------------------------------------------
// UserResponse — must_change_password field
// ---------------------------------------------------------------------------

func TestCreateUser_ResponseIncludesMustChangePassword(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		create: func(_ context.Context, _ domainuser.CreateInput) (*domainuser.User, error) {
			return &domainuser.User{ID: id, Username: "alice", Role: domainuser.RoleUser, MustChangePassword: true}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPost, "/admin/users",
		jsonBody(t, map[string]string{"username": "alice", "password": "pass1234", "full_name": "Alice"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			MustChangePassword bool `json:"must_change_password"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !env.Data.MustChangePassword {
		t.Error("expected must_change_password=true in response")
	}
}
