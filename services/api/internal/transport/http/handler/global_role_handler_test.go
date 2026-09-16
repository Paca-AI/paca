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

// --- Privilege-escalation guard (ADMIN must not self-promote to SUPER_ADMIN) ---

// staticGlobalRolePermStore is a minimal authz.PermissionStore for the
// escalation-guard tests: every user resolves to the configured global perms.
type staticGlobalRolePermStore struct {
	globalPerms []authz.Permission
}

func (s *staticGlobalRolePermStore) ListGlobalPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return append([]authz.Permission(nil), s.globalPerms...), nil
}

func (s *staticGlobalRolePermStore) ListProjectPermissions(context.Context, uuid.UUID, uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}

// adminGlobalPerms returns the real built-in ADMIN role's grants — global
// role management included, but NOT the PermissionAll wildcard — so the
// guard tests exercise the exact production boundary.
func adminGlobalPerms() []authz.Permission {
	for _, def := range authz.DefaultGlobalRoles() {
		if def.Name == "ADMIN" {
			return def.Permissions
		}
	}
	return nil
}

func globalRoleClaims(role string) *domainauth.Claims {
	return &domainauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: uuid.NewString()},
		Role:             role,
		Kind:             "access",
	}
}

// newGlobalRoleRouterWithAuthz builds the global-role router with the
// privilege-escalation guard enabled: every request carries claims and the
// authorizer resolves permissions from the static store.
func newGlobalRoleRouterWithAuthz(svc globalroledom.Service, claims *domainauth.Claims, perms []authz.Permission) chi.Router {
	r := chi.NewRouter()
	r.Use(injectClaims(claims))
	h := handler.NewGlobalRoleHandler(svc, authz.NewAuthorizer(&staticGlobalRolePermStore{globalPerms: perms}))
	r.Get("/admin/global-roles", h.List)
	r.Post("/admin/global-roles", h.Create)
	r.Patch("/admin/global-roles/{roleId}", h.Update)
	r.Delete("/admin/global-roles/{roleId}", h.Delete)
	r.Put("/admin/users/{userId}/global-roles", h.ReplaceUserRoles)
	return r
}

func TestGlobalRoleCreate_GodRolePermissions_ForbiddenForAdmin(t *testing.T) {
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	w := do(t, r, http.MethodPost, "/admin/global-roles",
		jsonBody(t, map[string]any{"name": "GOD", "permissions": map[string]any{"*": true}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != "FORBIDDEN" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestGlobalRoleCreate_GodRolePermissions_AllowedForSuperAdmin(t *testing.T) {
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		create: func(_ context.Context, _ globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: uuid.New(), Name: "GOD", Permissions: map[string]any{"*": true}}, nil
		},
	}, globalRoleClaims("SUPER_ADMIN"), nil)

	w := do(t, r, http.MethodPost, "/admin/global-roles",
		jsonBody(t, map[string]any{"name": "GOD", "permissions": map[string]any{"*": true}}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGlobalRoleUpdate_AddGodPermission_ForbiddenForAdmin(t *testing.T) {
	roleID := uuid.New()
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "CUSTOM", Permissions: map[string]any{"users.read": true}}, nil
		},
	}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/global-roles/%s", roleID),
		jsonBody(t, map[string]any{"name": "CUSTOM", "permissions": map[string]any{"*": true}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGlobalRoleUpdate_SuperAdminRoleDefinition_ForbiddenForAdmin(t *testing.T) {
	roleID := uuid.New()
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "SUPER_ADMIN", Permissions: map[string]any{"*": true}}, nil
		},
	}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	// Even a mutation that doesn't itself add "*" is reserved: an ADMIN must
	// not be able to reshape the SUPER_ADMIN role definition.
	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/global-roles/%s", roleID),
		jsonBody(t, map[string]any{"name": "SUPER_ADMIN", "permissions": map[string]any{"users.read": true}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGlobalRoleUpdate_NormalRole_AllowedForAdmin(t *testing.T) {
	roleID := uuid.New()
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "CUSTOM", Permissions: map[string]any{"users.read": true}}, nil
		},
		update: func(_ context.Context, _ uuid.UUID, _ globalroledom.UpdateInput) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: roleID, Name: "CUSTOM", Permissions: map[string]any{"users.read": true, "users.write": true}}, nil
		},
	}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/global-roles/%s", roleID),
		jsonBody(t, map[string]any{"name": "CUSTOM", "permissions": map[string]any{"users.read": true, "users.write": true}}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGlobalRoleDelete_SuperAdminRole_ForbiddenForAdmin(t *testing.T) {
	roleID := uuid.New()
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "SUPER_ADMIN", Permissions: map[string]any{"*": true}}, nil
		},
	}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/admin/global-roles/%s", roleID), nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReplaceUserGlobalRoles_GodRole_ForbiddenForAdmin(t *testing.T) {
	superAdminID := uuid.New()
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "SUPER_ADMIN", Permissions: map[string]any{"*": true}}, nil
		},
	}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	w := do(t, r, http.MethodPut, fmt.Sprintf("/admin/users/%s/global-roles", uuid.NewString()),
		jsonBody(t, map[string]any{"role_ids": []string{superAdminID.String()}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReplaceUserGlobalRoles_GodRole_AllowedForSuperAdmin(t *testing.T) {
	superAdminID := uuid.New()
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "SUPER_ADMIN", Permissions: map[string]any{"*": true}}, nil
		},
		replaceUserRoles: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]*globalroledom.GlobalRole, error) {
			return []*globalroledom.GlobalRole{{ID: superAdminID, Name: "SUPER_ADMIN", Permissions: map[string]any{"*": true}}}, nil
		},
	}, globalRoleClaims("SUPER_ADMIN"), nil)

	w := do(t, r, http.MethodPut, fmt.Sprintf("/admin/users/%s/global-roles", uuid.NewString()),
		jsonBody(t, map[string]any{"role_ids": []string{superAdminID.String()}}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReplaceUserGlobalRoles_NormalRole_AllowedForAdmin(t *testing.T) {
	userRoleID := uuid.New()
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "USER", Permissions: map[string]any{"users.read": true}}, nil
		},
		replaceUserRoles: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]*globalroledom.GlobalRole, error) {
			return []*globalroledom.GlobalRole{{ID: userRoleID, Name: "USER", Permissions: map[string]any{"users.read": true}}}, nil
		},
	}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	w := do(t, r, http.MethodPut, fmt.Sprintf("/admin/users/%s/global-roles", uuid.NewString()),
		jsonBody(t, map[string]any{"role_ids": []string{userRoleID.String()}}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Regression: the guard must read permission keys exactly like the store ---
//
// postgres.permissionsFromJSON trims keys before granting, so a whitespace-
// padded "*" (" *", "*	", "\u00a0*") resolves to PermissionAll at request
// time. A guard comparing raw keys would let it through — the bypass the
// parser-sharing change closes. These tests pin the guard to the resolver.

func TestGlobalRoleCreate_PaddedWildcardKey_ForbiddenForAdmin(t *testing.T) {
	for _, key := range []string{" *", "* ", "	*", "\u00a0*"} {
		t.Run(fmt.Sprintf("%q", key), func(t *testing.T) {
			r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{}, globalRoleClaims("ADMIN"), adminGlobalPerms())

			w := do(t, r, http.MethodPost, "/admin/global-roles",
				jsonBody(t, map[string]any{"name": "GOD_PAD", "permissions": map[string]any{key: true}}))
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for padded wildcard %q, got %d: %s", key, w.Code, w.Body.String())
			}
		})
	}
}

func TestGlobalRoleCreate_PaddedReservedName_ForbiddenForAdmin(t *testing.T) {
	// The service trims names before persisting, so " SUPER_ADMIN " must be
	// treated as the reserved role too.
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	w := do(t, r, http.MethodPost, "/admin/global-roles",
		jsonBody(t, map[string]any{"name": " SUPER_ADMIN ", "permissions": map[string]any{"users.read": true}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a padded reserved name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGlobalRoleUpdate_AddPaddedWildcardKey_ForbiddenForAdmin(t *testing.T) {
	roleID := uuid.New()
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "CUSTOM", Permissions: map[string]any{"users.read": true}}, nil
		},
	}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/global-roles/%s", roleID),
		jsonBody(t, map[string]any{"name": "CUSTOM", "permissions": map[string]any{"* ": true}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a padded wildcard, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReplaceUserGlobalRoles_PaddedWildcardRole_ForbiddenForAdmin(t *testing.T) {
	roleID := uuid.New()
	r := newGlobalRoleRouterWithAuthz(&mockGlobalRoleSvc{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "GOD_PAD", Permissions: map[string]any{" * ": true}}, nil
		},
	}, globalRoleClaims("ADMIN"), adminGlobalPerms())

	w := do(t, r, http.MethodPut, fmt.Sprintf("/admin/users/%s/global-roles", uuid.NewString()),
		jsonBody(t, map[string]any{"role_ids": []string{roleID.String()}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 assigning a role with a padded wildcard, got %d: %s", w.Code, w.Body.String())
	}
}
