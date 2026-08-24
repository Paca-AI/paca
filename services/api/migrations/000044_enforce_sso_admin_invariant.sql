-- Keep SSO-only installations administrable. The invariant is enforced in
-- PostgreSQL so every API replica and every mutation path observes it.

BEGIN;

CREATE OR REPLACE FUNCTION enforce_sso_admin_invariant()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    sso_enabled BOOLEAN;
    password_login_enabled BOOLEAN;
    configured_issuer TEXT;
BEGIN
    -- This singleton row is the serialization point for configuration, role,
    -- user, and identity changes that could remove the final SSO administrator.
    SELECT oidc_enabled, local_login_enabled, oidc_issuer_url
      INTO sso_enabled, password_login_enabled, configured_issuer
      FROM workspace_settings
     WHERE id = TRUE
       FOR UPDATE;

    IF NOT sso_enabled OR password_login_enabled THEN
        RETURN NULL;
    END IF;

    -- Lock the qualifying binding, user, and role. Along with the settings-row
    -- lock above, this turns concurrent demotions/deletions into one ordered
    -- decision instead of a write-skew race.
    PERFORM u.id
      FROM user_external_identities ei
      JOIN users u ON u.id = ei.user_id
      JOIN global_roles gr ON gr.id = u.role_id
     WHERE ei.provider = 'oidc'
       AND ei.issuer = configured_issuer
       AND gr.name IN ('ADMIN', 'SUPER_ADMIN')
       AND u.deleted_at IS NULL
     ORDER BY u.id
     LIMIT 1
       FOR UPDATE OF ei, u, gr;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'sso-only mode requires an administrator bound to the configured issuer'
            USING ERRCODE = '23514', CONSTRAINT = 'paca_sso_admin_required';
    END IF;

    RETURN NULL;
END;
$$;

-- Relevant writes must take the singleton settings-row lock before they take
-- user, role, or identity row locks. Without this common lock order, two
-- concurrent demotions can deadlock when the deferred check tries to lock the
-- other transaction's remaining administrator.
CREATE OR REPLACE FUNCTION serialize_sso_admin_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM id
      FROM workspace_settings
     WHERE id = TRUE
       FOR UPDATE;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS serialize_sso_admin_on_settings ON workspace_settings;
CREATE TRIGGER serialize_sso_admin_on_settings
BEFORE UPDATE OF oidc_enabled, oidc_issuer_url, local_login_enabled
ON workspace_settings
FOR EACH STATEMENT EXECUTE FUNCTION serialize_sso_admin_mutation();

DROP TRIGGER IF EXISTS serialize_sso_admin_on_users ON users;
CREATE TRIGGER serialize_sso_admin_on_users
BEFORE UPDATE OF role_id, deleted_at OR DELETE
ON users
FOR EACH STATEMENT EXECUTE FUNCTION serialize_sso_admin_mutation();

DROP TRIGGER IF EXISTS serialize_sso_admin_on_roles ON global_roles;
CREATE TRIGGER serialize_sso_admin_on_roles
BEFORE UPDATE OF name OR DELETE
ON global_roles
FOR EACH STATEMENT EXECUTE FUNCTION serialize_sso_admin_mutation();

DROP TRIGGER IF EXISTS serialize_sso_admin_on_identities ON user_external_identities;
CREATE TRIGGER serialize_sso_admin_on_identities
BEFORE UPDATE OF user_id, provider, issuer OR DELETE
ON user_external_identities
FOR EACH STATEMENT EXECUTE FUNCTION serialize_sso_admin_mutation();

DROP TRIGGER IF EXISTS enforce_sso_admin_on_settings ON workspace_settings;
CREATE CONSTRAINT TRIGGER enforce_sso_admin_on_settings
AFTER UPDATE OF oidc_enabled, oidc_issuer_url, local_login_enabled
ON workspace_settings
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_sso_admin_invariant();

DROP TRIGGER IF EXISTS enforce_sso_admin_on_users ON users;
CREATE CONSTRAINT TRIGGER enforce_sso_admin_on_users
AFTER UPDATE OF role_id, deleted_at OR DELETE
ON users
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_sso_admin_invariant();

DROP TRIGGER IF EXISTS enforce_sso_admin_on_roles ON global_roles;
CREATE CONSTRAINT TRIGGER enforce_sso_admin_on_roles
AFTER UPDATE OF name OR DELETE
ON global_roles
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_sso_admin_invariant();

DROP TRIGGER IF EXISTS enforce_sso_admin_on_identities ON user_external_identities;
CREATE CONSTRAINT TRIGGER enforce_sso_admin_on_identities
AFTER UPDATE OF user_id, provider, issuer OR DELETE
ON user_external_identities
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_sso_admin_invariant();

COMMIT;
