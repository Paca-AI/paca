package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

type mockGlobalRoleSvc struct {
	list             func(ctx context.Context) ([]*globalroledom.GlobalRole, error)
	create           func(ctx context.Context, in globalroledom.CreateInput) (*globalroledom.GlobalRole, error)
	update           func(ctx context.Context, id uuid.UUID, in globalroledom.UpdateInput) (*globalroledom.GlobalRole, error)
	delete           func(ctx context.Context, id uuid.UUID) error
	replaceUserRoles func(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) ([]*globalroledom.GlobalRole, error)
	findByID         func(ctx context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error)
	setDefault       func(ctx context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error)
}

func (m *mockGlobalRoleSvc) List(ctx context.Context) ([]*globalroledom.GlobalRole, error) {
	if m.list != nil {
		return m.list(ctx)
	}
	return []*globalroledom.GlobalRole{}, nil
}

func (m *mockGlobalRoleSvc) Create(ctx context.Context, in globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
	if m.create != nil {
		return m.create(ctx, in)
	}
	return nil, errors.New("mock: create not configured")
}

func (m *mockGlobalRoleSvc) Update(ctx context.Context, id uuid.UUID, in globalroledom.UpdateInput) (*globalroledom.GlobalRole, error) {
	if m.update != nil {
		return m.update(ctx, id, in)
	}
	return nil, globalroledom.ErrNotFound
}

func (m *mockGlobalRoleSvc) Delete(ctx context.Context, id uuid.UUID) error {
	if m.delete != nil {
		return m.delete(ctx, id)
	}
	return nil
}

func (m *mockGlobalRoleSvc) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) ([]*globalroledom.GlobalRole, error) {
	if m.replaceUserRoles != nil {
		return m.replaceUserRoles(ctx, userID, roleIDs)
	}
	return []*globalroledom.GlobalRole{}, nil
}

func (m *mockGlobalRoleSvc) FindByID(ctx context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
	if m.findByID != nil {
		return m.findByID(ctx, id)
	}
	return nil, globalroledom.ErrNotFound
}

func (m *mockGlobalRoleSvc) SetDefault(ctx context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
	if m.setDefault != nil {
		return m.setDefault(ctx, id)
	}
	return nil, globalroledom.ErrNotFound
}

func (m *mockGlobalRoleSvc) FindDefault(context.Context) (*globalroledom.GlobalRole, error) {
	return nil, globalroledom.ErrNoDefault
}

func newGlobalRoleRouter(svc globalroledom.Service) chi.Router {
	r := chi.NewRouter()
	h := handler.NewGlobalRoleHandler(svc)
	r.Get("/admin/global-roles", h.List)
	r.Post("/admin/global-roles", h.Create)
	r.Patch("/admin/global-roles/{roleId}", h.Update)
	r.Delete("/admin/global-roles/{roleId}", h.Delete)
	r.Put("/admin/global-roles/{roleId}/set-default", h.SetDefault)
	r.Put("/admin/users/{userId}/global-roles", h.ReplaceUserRoles)
	return r
}

func TestGlobalRoleList_Success(t *testing.T) {
	roleID := uuid.New()
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{
		list: func(_ context.Context) ([]*globalroledom.GlobalRole, error) {
			return []*globalroledom.GlobalRole{{ID: roleID, Name: "SUPER_ADMIN"}}, nil
		},
	})

	w := do(t, r, http.MethodGet, "/admin/global-roles", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGlobalRoleCreate_Conflict(t *testing.T) {
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{
		create: func(_ context.Context, _ globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
			return nil, globalroledom.ErrNameTaken
		},
	})

	w := do(t, r, http.MethodPost, "/admin/global-roles",
		jsonBody(t, map[string]any{"name": "SUPER_ADMIN", "permissions": map[string]any{"x": true}}))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "GLOBAL_ROLE_NAME_TAKEN" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestGlobalRoleUpdate_BadID(t *testing.T) {
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{})

	w := do(t, r, http.MethodPatch, "/admin/global-roles/not-a-uuid",
		jsonBody(t, map[string]any{"name": "ADMIN", "permissions": map[string]any{}}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGlobalRoleDelete_NotFound(t *testing.T) {
	id := uuid.New()
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{
		delete: func(_ context.Context, _ uuid.UUID) error { return globalroledom.ErrNotFound },
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/admin/global-roles/%s", id), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "GLOBAL_ROLE_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestGlobalRoleDelete_DefaultRoleIsRefused(t *testing.T) {
	id := uuid.New()
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{
		delete: func(_ context.Context, _ uuid.UUID) error { return globalroledom.ErrIsDefault },
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/admin/global-roles/%s", id), nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != "GLOBAL_ROLE_IS_DEFAULT" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestGlobalRoleSetDefault_ReturnsTheRoleAsDefault(t *testing.T) {
	id := uuid.New()
	var got uuid.UUID
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{
		setDefault: func(_ context.Context, roleID uuid.UUID) (*globalroledom.GlobalRole, error) {
			got = roleID
			return &globalroledom.GlobalRole{ID: roleID, Name: "MEMBER", IsDefault: true}, nil
		},
	})

	w := do(t, r, http.MethodPut, fmt.Sprintf("/admin/global-roles/%s/set-default", id), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got != id {
		t.Fatalf("the service was asked to default %s, want %s", got, id)
	}
	var body struct {
		Data struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsDefault bool   `json:"is_default"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.ID != id.String() || body.Data.Name != "MEMBER" || !body.Data.IsDefault {
		t.Fatalf("unexpected response: %+v", body.Data)
	}
}

func TestGlobalRoleSetDefault_NotFound(t *testing.T) {
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{
		setDefault: func(context.Context, uuid.UUID) (*globalroledom.GlobalRole, error) {
			return nil, globalroledom.ErrNotFound
		},
	})

	w := do(t, r, http.MethodPut, fmt.Sprintf("/admin/global-roles/%s/set-default", uuid.New()), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "GLOBAL_ROLE_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestGlobalRoleSetDefault_InvalidID(t *testing.T) {
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{})

	w := do(t, r, http.MethodPut, "/admin/global-roles/not-a-uuid/set-default", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGlobalRoleList_ReportsWhichRoleIsTheDefault(t *testing.T) {
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{
		list: func(context.Context) ([]*globalroledom.GlobalRole, error) {
			return []*globalroledom.GlobalRole{
				{ID: uuid.New(), Name: "ADMIN"},
				{ID: uuid.New(), Name: "USER", IsDefault: true},
			}, nil
		},
	})

	w := do(t, r, http.MethodGet, "/admin/global-roles", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body struct {
		Data []struct {
			Name      string `json:"name"`
			IsDefault bool   `json:"is_default"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 2 || body.Data[0].IsDefault || !body.Data[1].IsDefault || body.Data[1].Name != "USER" {
		t.Fatalf("expected only USER to be the default, got %+v", body.Data)
	}
}

func TestReplaceUserGlobalRoles_UserNotFound(t *testing.T) {
	userID := uuid.New()
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{
		replaceUserRoles: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]*globalroledom.GlobalRole, error) {
			return nil, userdom.ErrNotFound
		},
	})

	w := do(t, r, http.MethodPut, fmt.Sprintf("/admin/users/%s/global-roles", userID),
		jsonBody(t, map[string]any{"role_ids": []string{uuid.NewString()}}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "USER_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestGlobalRoleCreate_EmptyName_Returns400(t *testing.T) {
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{})

	w := do(t, r, http.MethodPost, "/admin/global-roles",
		jsonBody(t, map[string]any{"name": "", "permissions": map[string]any{}}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGlobalRoleUpdate_EmptyName_Returns400(t *testing.T) {
	id := uuid.New()
	r := newGlobalRoleRouter(&mockGlobalRoleSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/global-roles/%s", id),
		jsonBody(t, map[string]any{"name": "", "permissions": map[string]any{}}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d: %s", w.Code, w.Body.String())
	}
}
