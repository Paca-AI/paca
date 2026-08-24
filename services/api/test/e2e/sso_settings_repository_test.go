package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
)

func TestWorkspaceSettingsOIDCRoundTrip(t *testing.T) {
	env := newE2EEnv(t)
	adminIDs, _, _ := seedSSOOnlyFixture(t, env, 1)
	repo := pgRepo.NewSettingsRepository(env.db)
	actor := adminIDs[0]
	now := time.Now().UTC().Truncate(time.Microsecond)

	want := &settingsdom.WorkspaceSettings{
		OIDCConfigured:      true,
		OIDCEnabled:         true,
		OIDCIssuerURL:       testStringPtr("https://id.example.com/realms/company/"),
		OIDCClientID:        testStringPtr("paca"),
		OIDCClientSecretEnc: testStringPtr("ciphertext-not-plaintext"),
		OIDCScopes:          testStringPtr("openid,profile,email"),
		OIDCRedirectURL:     testStringPtr("https://paca.example.com/api/v1/auth/oidc/callback"),
		OIDCDisplayName:     testStringPtr("Company SSO"),
		OIDCUsernameClaim:   testStringPtr("preferred_username"),
		LocalLoginEnabled:   false,
		UpdatedAt:           now,
		UpdatedBy:           &actor,
	}

	if _, err := repo.WithLock(t.Context(), func(row *settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error) {
		row.OIDCConfigured = want.OIDCConfigured
		row.OIDCEnabled = want.OIDCEnabled
		row.OIDCIssuerURL = want.OIDCIssuerURL
		row.OIDCClientID = want.OIDCClientID
		row.OIDCClientSecretEnc = want.OIDCClientSecretEnc
		row.OIDCScopes = want.OIDCScopes
		row.OIDCRedirectURL = want.OIDCRedirectURL
		row.OIDCDisplayName = want.OIDCDisplayName
		row.OIDCUsernameClaim = want.OIDCUsernameClaim
		row.LocalLoginEnabled = want.LocalLoginEnabled
		row.UpdatedAt = want.UpdatedAt
		row.UpdatedBy = want.UpdatedBy
		return row, nil
	}); err != nil {
		t.Fatalf("WithLock: %v", err)
	}

	got, err := repo.Get(t.Context())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.OIDCConfigured || !got.OIDCEnabled || got.LocalLoginEnabled {
		t.Fatalf("unexpected switches after round trip: %+v", got)
	}
	assertTestStringPtr(t, "issuer", got.OIDCIssuerURL, *want.OIDCIssuerURL)
	assertTestStringPtr(t, "client ID", got.OIDCClientID, *want.OIDCClientID)
	assertTestStringPtr(t, "encrypted client secret", got.OIDCClientSecretEnc, *want.OIDCClientSecretEnc)
	assertTestStringPtr(t, "scopes", got.OIDCScopes, *want.OIDCScopes)
	assertTestStringPtr(t, "redirect URL", got.OIDCRedirectURL, *want.OIDCRedirectURL)
	assertTestStringPtr(t, "display name", got.OIDCDisplayName, *want.OIDCDisplayName)
	assertTestStringPtr(t, "username claim", got.OIDCUsernameClaim, *want.OIDCUsernameClaim)
	if got.UpdatedBy == nil || *got.UpdatedBy != actor {
		t.Fatalf("updated_by = %v, want %s", got.UpdatedBy, actor)
	}

	var storedSecret string
	if err := env.db.GetContext(t.Context(), &storedSecret, `SELECT oidc_client_secret_enc FROM workspace_settings WHERE id = true`); err != nil {
		t.Fatalf("query stored secret: %v", err)
	}
	if storedSecret != *want.OIDCClientSecretEnc {
		t.Fatalf("stored client secret = %q, want ciphertext unchanged", storedSecret)
	}
}

func TestSSOOnlyInvariantRejectsActivationWithoutAdministrator(t *testing.T) {
	env := newE2EEnv(t)
	repo := pgRepo.NewSettingsRepository(env.db)

	_, err := repo.WithLock(t.Context(), func(row *settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error) {
		row.OIDCConfigured = true
		row.OIDCEnabled = true
		row.OIDCIssuerURL = testStringPtr("https://id.example.com/realms/company/")
		row.LocalLoginEnabled = false
		row.UpdatedAt = time.Now().UTC()
		return row, nil
	})
	if !errors.Is(err, settingsdom.ErrSSOAdminRequired) {
		t.Fatalf("activate SSO-only without administrator: got %v, want ErrSSOAdminRequired", err)
	}
}

func TestSSOOnlyInvariantRejectsRemovingFinalAdministrator(t *testing.T) {
	env := newE2EEnv(t)
	adminID, adminRoleID, userRoleID := seedSSOOnlyFixture(t, env, 1)

	if err := env.roleRepo.ReplaceUserRoles(t.Context(), adminID[0], []uuid.UUID{userRoleID}); !errors.Is(err, settingsdom.ErrSSOAdminRequired) {
		t.Fatalf("demote final SSO administrator: got %v, want ErrSSOAdminRequired", err)
	}
	admin, err := env.userRepo.FindByID(t.Context(), adminID[0])
	if err != nil {
		t.Fatalf("find SSO administrator: %v", err)
	}
	admin.RoleID = userRoleID
	admin.Role = "USER"
	admin.UpdatedAt = time.Now().UTC()
	if err := env.userRepo.Update(t.Context(), admin); !errors.Is(err, settingsdom.ErrSSOAdminRequired) {
		t.Fatalf("update final SSO administrator role: got %v, want ErrSSOAdminRequired", err)
	}
	if err := env.userRepo.Delete(t.Context(), adminID[0]); !errors.Is(err, settingsdom.ErrSSOAdminRequired) {
		t.Fatalf("delete final SSO administrator: got %v, want ErrSSOAdminRequired", err)
	}
	role, err := env.roleRepo.FindByID(t.Context(), adminRoleID)
	if err != nil {
		t.Fatalf("find administrator role: %v", err)
	}
	role.Name = "RENAMED_ADMIN"
	role.UpdatedAt = time.Now().UTC()
	if err := env.roleRepo.Update(t.Context(), role); !errors.Is(err, settingsdom.ErrSSOAdminRequired) {
		t.Fatalf("rename final administrator role: got %v, want ErrSSOAdminRequired", err)
	}
}

func TestSSOOnlyInvariantSerializesConcurrentAdministratorDemotions(t *testing.T) {
	env := newE2EEnv(t)
	adminIDs, _, userRoleID := seedSSOOnlyFixture(t, env, 2)

	start := make(chan struct{})
	errs := make(chan error, len(adminIDs))
	var wg sync.WaitGroup
	for _, adminID := range adminIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- env.roleRepo.ReplaceUserRoles(context.Background(), adminID, []uuid.UUID{userRoleID})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	failures := 0
	for err := range errs {
		if err != nil {
			failures++
		}
	}
	if failures == 0 {
		t.Fatal("both concurrent demotions succeeded")
	}

	var eligible int
	if err := env.db.GetContext(t.Context(), &eligible, `
		SELECT COUNT(*)
		FROM user_external_identities ei
		JOIN users u ON u.id = ei.user_id
		JOIN global_roles gr ON gr.id = u.role_id
		WHERE ei.provider = 'oidc'
		  AND ei.issuer = 'https://id.example.com/realms/company/'
		  AND gr.name IN ('ADMIN', 'SUPER_ADMIN')
		  AND u.deleted_at IS NULL`); err != nil {
		t.Fatalf("count eligible administrators: %v", err)
	}
	if eligible < 1 {
		t.Fatalf("eligible SSO administrators = %d, want at least one", eligible)
	}
}

func seedSSOOnlyFixture(t *testing.T, env *e2eEnv, adminCount int) ([]uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	adminRole, err := env.roleRepo.FindByName(t.Context(), "ADMIN")
	if err != nil {
		t.Fatalf("find ADMIN role: %v", err)
	}
	userRole, err := env.roleRepo.FindByName(t.Context(), "USER")
	if err != nil {
		t.Fatalf("find USER role: %v", err)
	}
	adminRoleID := adminRole.ID
	userRoleID := userRole.ID

	adminIDs := make([]uuid.UUID, 0, adminCount)
	for i := range adminCount {
		adminID := uuid.New()
		adminIDs = append(adminIDs, adminID)
		if _, err := env.db.ExecContext(t.Context(), `
			INSERT INTO users (id, username, password_hash, full_name, role_id)
			VALUES ($1, $2, '!', $3, $4)`, adminID, fmt.Sprintf("sso-admin-%d", i), fmt.Sprintf("SSO Admin %d", i), adminRoleID); err != nil {
			t.Fatalf("seed administrator %d: %v", i, err)
		}
		if _, err := env.db.ExecContext(t.Context(), `
			INSERT INTO user_external_identities (user_id, issuer, subject)
			VALUES ($1, 'https://id.example.com/realms/company/', $2)`, adminID, fmt.Sprintf("admin-subject-%d", i)); err != nil {
			t.Fatalf("seed external identity %d: %v", i, err)
		}
	}

	repo := pgRepo.NewSettingsRepository(env.db)
	if _, err := repo.WithLock(t.Context(), func(row *settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error) {
		row.OIDCConfigured = true
		row.OIDCEnabled = true
		row.OIDCIssuerURL = testStringPtr("https://id.example.com/realms/company/")
		row.LocalLoginEnabled = false
		row.UpdatedAt = time.Now().UTC()
		return row, nil
	}); err != nil {
		t.Fatalf("activate SSO-only fixture: %v", err)
	}
	return adminIDs, adminRoleID, userRoleID
}

func testStringPtr(v string) *string { return &v }

func assertTestStringPtr(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %q", field, got, want)
	}
}
