package e2e_test

import (
	"net/http"
	"testing"
)

// TestSSOOnlyMode covers SSO-only mode end to end on a real database: it
// cannot be turned on without an enabled provider; once on, password sign-in
// and every way of giving an account a password are refused to all but the
// default super admin, while SSO sign-in keeps working; turning it off
// restores password sign-in.
func TestSSOOnlyMode(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	idp := newE2EIDP(t)

	const password = "supersecret"
	// The default super admin: the account seeded from the admin config.
	seedUser(t, env, "admin", password, "Admin")
	assignPlatformRole(t, env, "admin", "SUPER_ADMIN")
	admin, err := env.userRepo.FindByUsername(env.ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.userRepo.MarkBootstrapAdmin(env.ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	// Another SUPER_ADMIN, which is not exempt.
	seedUser(t, env, "root", password, "Root")
	assignPlatformRole(t, env, "root", "SUPER_ADMIN")
	seedUser(t, env, "jane", password, "Jane Doe")
	jane, err := env.userRepo.FindByUsername(env.ctx, "jane")
	if err != nil {
		t.Fatal(err)
	}
	email := "jane@corp.example"
	jane.Email = &email
	if err := env.userRepo.Update(env.ctx, jane); err != nil {
		t.Fatal(err)
	}

	adminClient := newLoggedInClient(t, env, "admin", password)
	// Signed in before the mode is turned on.
	rootClient := newLoggedInClient(t, env, "root", password)

	setSSOOnly := func(t *testing.T, on bool) (int, envelope) {
		t.Helper()
		return doJSON(t, env, adminClient, http.MethodPut, "/api/v1/admin/auth-security", map[string]any{
			"privileged_roles": []string{"*"}, "require_mfa": false, "ip_allow_list": []string{}, "sso_only": on,
		})
	}
	passwordLogin := func(t *testing.T, username, pw string) (int, envelope) {
		t.Helper()
		return doJSON(t, env, &http.Client{}, http.MethodPost, "/api/v1/auth/login", map[string]any{
			"username": username, "password": pw,
		})
	}
	loginOptions := func(t *testing.T) bool {
		t.Helper()
		status, out := doJSON(t, env, &http.Client{}, http.MethodGet, "/api/v1/auth/login-options", nil)
		if status != http.StatusOK {
			t.Fatalf("GET /auth/login-options: want 200, got %d", status)
		}
		on, _ := assertDataMap(t, out)["sso_only"].(bool)
		return on
	}

	t.Run("needs_an_enabled_provider", func(t *testing.T) {
		status, out := setSSOOnly(t, true)
		if status != http.StatusBadRequest || out.ErrorCode != "SSO_ONLY_NO_PROVIDER" {
			t.Fatalf("want 400 SSO_ONLY_NO_PROVIDER, got %d %s", status, out.ErrorCode)
		}
		if loginOptions(t) {
			t.Fatal("a refused policy must not be saved")
		}
	})

	t.Run("turns_on_with_an_enabled_provider", func(t *testing.T) {
		status, out := doJSON(t, env, adminClient, http.MethodPost, "/api/v1/admin/sso/providers", map[string]any{
			"slug": "corp", "display_name": "Corp SSO", "issuer_url": idp.srv.URL,
			"client_id": "paca-e2e", "client_secret": "s3cret",
			"enabled": true, "link_by_email": true,
		})
		if status != http.StatusCreated {
			t.Fatalf("create provider: want 201, got %d (%s)", status, out.ErrorCode)
		}
		if status, out := setSSOOnly(t, true); status != http.StatusOK {
			t.Fatalf("want 200, got %d (%s)", status, out.ErrorCode)
		}
		if !loginOptions(t) {
			t.Fatal("the login page must learn the instance is SSO-only")
		}
	})

	t.Run("password_sign_in_is_refused_except_for_the_default_super_admin", func(t *testing.T) {
		for _, username := range []string{"root", "jane"} {
			status, out := passwordLogin(t, username, password)
			if status != http.StatusForbidden || out.ErrorCode != "AUTH_PASSWORD_LOGIN_DISABLED" {
				t.Errorf("%s: want 403 AUTH_PASSWORD_LOGIN_DISABLED, got %d %s", username, status, out.ErrorCode)
			}
		}
		// A wrong password still reads as one, so the mode reveals nothing
		// about which passwords are right.
		if status, out := passwordLogin(t, "jane", "wrong-password"); status != http.StatusUnauthorized || out.ErrorCode != "AUTH_INVALID_CREDENTIALS" {
			t.Errorf("wrong password: want 401 AUTH_INVALID_CREDENTIALS, got %d %s", status, out.ErrorCode)
		}
		if status, out := passwordLogin(t, "admin", password); status != http.StatusOK {
			t.Fatalf("default super admin: want 200, got %d %s", status, out.ErrorCode)
		}
	})

	t.Run("no_password_can_be_set_reset_or_changed", func(t *testing.T) {
		status, out := doJSON(t, env, adminClient, http.MethodPatch, "/api/v1/admin/users/"+jane.ID.String()+"/password",
			map[string]any{"new_password": "brand-new-password"})
		if status != http.StatusForbidden || out.ErrorCode != "AUTH_PASSWORD_LOGIN_DISABLED" {
			t.Errorf("admin reset: want 403 AUTH_PASSWORD_LOGIN_DISABLED, got %d %s", status, out.ErrorCode)
		}
		status, out = doJSON(t, env, rootClient, http.MethodPatch, "/api/v1/users/me/password",
			map[string]any{"current_password": password, "new_password": "brand-new-password"})
		if status != http.StatusForbidden || out.ErrorCode != "AUTH_PASSWORD_LOGIN_DISABLED" {
			t.Errorf("own change: want 403 AUTH_PASSWORD_LOGIN_DISABLED, got %d %s", status, out.ErrorCode)
		}
		// The default super admin can still change its own password.
		status, out = doJSON(t, env, adminClient, http.MethodPatch, "/api/v1/users/me/password",
			map[string]any{"current_password": password, "new_password": password + "2"})
		if status >= 300 {
			t.Fatalf("default super admin change: got %d %s", status, out.ErrorCode)
		}
		status, out = doJSON(t, env, adminClient, http.MethodPatch, "/api/v1/users/me/password",
			map[string]any{"current_password": password + "2", "new_password": password})
		if status >= 300 {
			t.Fatalf("default super admin change back: got %d %s", status, out.ErrorCode)
		}
	})

	t.Run("sso_sign_in_works_and_drops_an_unmeetable_password_change", func(t *testing.T) {
		// As if an admin had created jane with a temporary password.
		if _, err := env.db.ExecContext(env.ctx, `UPDATE users SET must_change_password = TRUE WHERE id = $1`, jane.ID); err != nil {
			t.Fatal(err)
		}
		idp.signInAs(map[string]any{"sub": "u-jane", "email": email, "email_verified": true})
		browser := ssoBrowser(t, env)
		if landed := ssoSignIn(t, env, browser, "corp", ""); landed != "/home" {
			t.Fatalf("landed on %q, want /home", landed)
		}
		status, out := doJSON(t, env, browser, http.MethodGet, "/api/v1/users/me", nil)
		if status != http.StatusOK {
			t.Fatalf("GET /users/me: want 200, got %d %s", status, out.ErrorCode)
		}
		if me := assertDataMap(t, out); me["id"] != jane.ID.String() || me["must_change_password"] != false {
			t.Fatalf("signed in as %v (must_change_password %v), want jane without a pending change", me["id"], me["must_change_password"])
		}
	})

	t.Run("turning_it_off_restores_password_sign_in", func(t *testing.T) {
		if status, out := setSSOOnly(t, false); status != http.StatusOK {
			t.Fatalf("want 200, got %d (%s)", status, out.ErrorCode)
		}
		if status, out := passwordLogin(t, "root", password); status != http.StatusOK {
			t.Fatalf("root: want 200, got %d %s", status, out.ErrorCode)
		}
	})
}
