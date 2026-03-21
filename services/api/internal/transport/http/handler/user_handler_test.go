package handler_test

import (
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
	getByID func(ctx context.Context, id uuid.UUID) (*domainuser.User, error)
	create  func(ctx context.Context, in domainuser.CreateInput) (*domainuser.User, error)
	update  func(ctx context.Context, id uuid.UUID, in domainuser.UpdateInput) (*domainuser.User, error)
	delete  func(ctx context.Context, id uuid.UUID) error
}

func (m *mockUserSvc) GetByID(ctx context.Context, id uuid.UUID) (*domainuser.User, error) {
	if m.getByID != nil {
		return m.getByID(ctx, id)
	}
	return nil, domainuser.ErrNotFound
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
			return &domainuser.User{ID: id, Email: in.Email, Name: in.Name, Role: domainuser.RoleUser}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPost, "/users",
		jsonBody(t, map[string]string{"email": "new@example.com", "password": "pass1234", "name": "Alice"}))
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUserCreate_BadJSON(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPost, "/users", jsonBody(t, map[string]string{"email": "bad"}))
	if w.Code == http.StatusCreated {
		t.Errorf("expected validation error, got 201")
	}
}

func TestUserCreate_EmailTaken(t *testing.T) {
	svc := &mockUserSvc{
		create: func(_ context.Context, _ domainuser.CreateInput) (*domainuser.User, error) {
			return nil, domainuser.ErrEmailTaken
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPost, "/users",
		jsonBody(t, map[string]string{"email": "dup@example.com", "password": "pass1234", "name": "Bob"}))
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GetMe
// ---------------------------------------------------------------------------

func TestGetMe_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*domainuser.User, error) {
			return &domainuser.User{ID: id, Email: "me@example.com", Role: domainuser.RoleUser}, nil
		},
	}
	r := gin.New()
	claims := testClaims(id.String(), "me@example.com", "USER")
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
		t.Errorf("expected 401, got %d", w.Code)
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
	claims := testClaims(id.String(), "a@b.com", "USER")
	r.GET("/users/me", injectClaims(claims), handler.NewUserHandler(svc).GetMe)

	w := do(t, r, http.MethodGet, "/users/me", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestUserUpdate_Success(t *testing.T) {
	id := uuid.New()
	svc := &mockUserSvc{
		update: func(_ context.Context, _ uuid.UUID, in domainuser.UpdateInput) (*domainuser.User, error) {
			return &domainuser.User{ID: id, Name: in.Name, Role: domainuser.RoleUser}, nil
		},
	}
	r := newUserRouter(svc)

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/users/%s", id),
		jsonBody(t, map[string]string{"name": "New Name"}))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUserUpdate_BadID(t *testing.T) {
	r := newUserRouter(&mockUserSvc{})

	w := do(t, r, http.MethodPatch, "/users/not-a-uuid",
		jsonBody(t, map[string]string{"name": "X"}))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
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
		jsonBody(t, map[string]string{"name": "X"}))
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
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
		t.Errorf("expected 400, got %d", w.Code)
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
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
