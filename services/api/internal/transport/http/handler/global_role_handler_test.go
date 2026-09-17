package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

type mockGlobalRoleSvc struct {
	list             func(ctx context.Context) ([]*globalroledom.GlobalRole, error)
	create           func(ctx context.Context, in globalroledom.CreateInput) (*globalroledom.GlobalRole, error)
	update           func(ctx context.Context, id uuid.UUID, in globalroledom.UpdateInput) (*globalroledom.GlobalRole, error)
	delete           func(ctx context.Context, id uuid.UUID) error
	replaceUserRoles func(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) ([]*globalroledom.GlobalRole, error)
	findByID         func(ctx context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error)
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

func newGlobalRoleRouter(svc globalroledom.Service) chi.Router {
	r := chi.NewRouter()
	h := handler.NewGlobalRoleHandler(svc)
	r.Get("/admin/global-roles", h.List)
	r.Post("/admin/global-roles", h.Create)
	r.Patch("/admin/global-roles/{roleId}", h.Update)
	r.Delete("/admin/global-roles/{roleId}", h.Delete)
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

// ---------------------------------------------------------------------------
// God-mode permission guard (GlobalRole Create / Update)
// ---------------------------------------------------------------------------
//
// A stored "*" grants every permission there is to everything the role is
// later assigned to — a user, or a global agent whose API key then carries god
// mode — so writing one into a role definition is a privilege grant, not plain
// role management. An ADMIN holds global_roles.write but not PermissionAll,
// hence the check these tests exercise; see newGlobalRoleAdminRouter's doc
// comment for why the route-level global_roles.write middleware isn't
// replicated here.

// newGlobalRoleAdminRouter wires POST and PATCH /admin/global-roles behind the
// authorizer the guard needs, and injects access claims carrying role — the
// legacy users.role claim authz.LegacyPermissionsForRole resolves into
// permissions, which is where a real ADMIN's global_roles.* grants come from.
// The authorizer's store is nil on purpose: only that claim matters here.
func newGlobalRoleAdminRouter(svc globalroledom.Service, role string) chi.Router {
	h := handler.NewGlobalRoleHandler(svc).WithAuthorizer(authz.NewAuthorizer(nil))
	claims := &domainauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: uuid.New().String()},
		Role:             role,
		Kind:             "access",
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), httpmw.ClaimsContextKey(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Post("/admin/global-roles", h.Create)
	r.Patch("/admin/global-roles/{roleId}", h.Update)
	return r
}

func TestGlobalRoleCreate_AdminGrantingWildcard_Returns403(t *testing.T) {
	// Every payload that still resolves to PermissionAll at request time: the
	// padded keys are the ones the store trims before granting, and each value
	// shape below is one postgres.permissionsFromJSON treats as granted.
	payloads := []map[string]any{
		{"*": true},
		{"*": 1},
		{"*": "true"},
		{"*": "TRUE"},
		{" * ": true},
		{"	*": true},
	}

	for i, permissions := range payloads {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			svc := &mockGlobalRoleSvc{
				create: func(context.Context, globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
					t.Fatal("Create must not be called when the caller can't grant the wildcard")
					return nil, nil
				},
			}
			r := newGlobalRoleAdminRouter(svc, userdom.RoleAdmin)

			w := do(t, r, http.MethodPost, "/admin/global-roles",
				jsonBody(t, map[string]any{"name": "Ops", "permissions": permissions}))
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for %v, got %d: %s", permissions, w.Code, w.Body.String())
			}
		})
	}
}

func TestGlobalRoleCreate_AdminGrantingOrdinaryPermissions_Allowed(t *testing.T) {
	// Controls: ordinary role management is the reason ADMIN holds
	// global_roles.write, and a wildcard key that grants nothing ("*": false)
	// is not a grant the guard should read as one.
	for _, permissions := range []map[string]any{{"users.read": true}, {"*": false}} {
		svc := &mockGlobalRoleSvc{
			create: func(_ context.Context, in globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
				return &globalroledom.GlobalRole{ID: uuid.New(), Name: in.Name, Permissions: in.Permissions}, nil
			},
		}
		r := newGlobalRoleAdminRouter(svc, userdom.RoleAdmin)

		w := do(t, r, http.MethodPost, "/admin/global-roles",
			jsonBody(t, map[string]any{"name": "Ops", "permissions": permissions}))
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 for %v, got %d: %s", permissions, w.Code, w.Body.String())
		}
	}
}

func TestGlobalRoleCreate_SuperAdminGrantingWildcard_Allowed(t *testing.T) {
	svc := &mockGlobalRoleSvc{
		create: func(_ context.Context, in globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: uuid.New(), Name: in.Name, Permissions: in.Permissions}, nil
		},
	}
	r := newGlobalRoleAdminRouter(svc, "SUPER_ADMIN")

	w := do(t, r, http.MethodPost, "/admin/global-roles",
		jsonBody(t, map[string]any{"name": "Ops", "permissions": map[string]any{"*": true}}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for a SUPER_ADMIN caller, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGlobalRoleUpdate_AdminGrantingWildcard_Returns403(t *testing.T) {
	// The edit path matters on its own: adding "*" to a role that already
	// exists is the same grant as creating one with it.
	svc := &mockGlobalRoleSvc{
		update: func(context.Context, uuid.UUID, globalroledom.UpdateInput) (*globalroledom.GlobalRole, error) {
			t.Fatal("Update must not be called when the caller can't grant the wildcard")
			return nil, nil
		},
	}
	r := newGlobalRoleAdminRouter(svc, userdom.RoleAdmin)

	w := do(t, r, http.MethodPatch, "/admin/global-roles/"+uuid.New().String(),
		jsonBody(t, map[string]any{"name": "Editor", "permissions": map[string]any{"docs.read": true, "*": true}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
