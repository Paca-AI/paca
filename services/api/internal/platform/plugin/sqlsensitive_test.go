package plugin

import (
	"strings"
	"testing"
)

// checkRead runs the full sensitive-read admission path for a query as the
// given caller, returning the aliases that must be redacted.
func checkRead(t *testing.T, caller string, requested []string, sql string) ([]string, error) {
	t.Helper()
	rt := &Runtime{}
	tables, cols := rt.sensitiveReadTables(callerPlugin(caller, requested...), sql)
	if len(tables) == 0 {
		return nil, nil
	}
	return checkSensitiveReadShape(sql, cols, tables)
}

// TestCheckSensitiveReadShape_RejectsBypasses covers GHSA-x5v3-9j39-fq73:
// every query below would let a sensitive value reach the plugin under an
// output column name redactColumns doesn't recognise.
func TestCheckSensitiveReadShape_RejectsBypasses(t *testing.T) {
	bad := []string{
		"SELECT concat(password_hash) FROM users",
		"SELECT password_hash || '' FROM users",
		"SELECT password_hash::text FROM users",
		"SELECT lower(password_hash) FROM users",
		"SELECT max(password_hash) FROM users",
		"SELECT password_hash ph FROM users",
		"SELECT users.password_hash || 'x' FROM users",
		"SELECT \"password_hash\" || '' FROM public.users",
		"SELECT (SELECT password_hash FROM users LIMIT 1)",
		"SELECT x FROM (SELECT password_hash AS x FROM users) t",
		"SELECT * FROM (SELECT * FROM users) t",
		"WITH t AS (SELECT password_hash AS x FROM users) SELECT x FROM t",
		"SELECT 1 AS a UNION SELECT password_hash FROM users",
		"SELECT to_jsonb(users) FROM users",
		"SELECT to_jsonb(u) FROM users u",
		"SELECT row_to_json(u) FROM users AS u",
		"SELECT to_jsonb(u.*) FROM users u",
		"SELECT (users.*) FROM users",
		"SELECT u FROM users u",
		"SELECT users::text FROM users",
		"SELECT to_jsonb(u) FROM projects p, users u",
		"SELECT id FROM users WHERE password_hash LIKE 'a%'",
		"SELECT id FROM users ORDER BY password_hash",
		"SELECT id FROM projects WHERE owner IN (SELECT password_hash FROM users)",
		"SELECT id FROM projects WHERE owner IN (SELECT to_jsonb(u) FROM users u)",
		"SELECT p.id FROM projects p JOIN users u USING (password_hash)",
		"SELECT concat(key_hash) FROM api_keys",
		"SELECT to_jsonb(k) FROM api_keys k",
		"SELECT concat(llm_api_key_secret) FROM agents",
		"SELECT concat(env) FROM agent_mcp_servers",
		"SELECT concat(encrypted_value) FROM agent_environment_variables",
		"SELECT concat(password_hash) FROM (users)",
		"SELECT concat(password_hash) FROM \"users\"",
		"SELECT concat(password_hash) FROM public . users",
		"SELECT id, count(*) FROM users GROUP BY password_hash",
		"SELECT * FROM users WHERE id IN (SELECT * FROM users)",
	}
	for _, sql := range bad {
		if _, err := checkRead(t, "com.paca.evil", nil, sql); err == nil {
			t.Errorf("expected rejection for %q", sql)
		}
	}
}

// TestCheckSensitiveReadShape_AllowsRedactableQueries pins that ordinary
// reads of sensitive tables keep working (and keep being redacted by name).
func TestCheckSensitiveReadShape_AllowsRedactableQueries(t *testing.T) {
	good := []string{
		"SELECT id, username FROM users",
		"SELECT * FROM users",
		"SELECT u.* FROM users u",
		"SELECT password_hash FROM users",
		"SELECT u.password_hash, u.id FROM users u WHERE u.id = $1",
		"SELECT DISTINCT password_hash FROM users",
		"SELECT password_hash FROM users;",
		"SELECT count(*) FROM users",
		"SELECT id * 2 FROM users",
		"SELECT p.id FROM projects p JOIN users u ON u.id = p.owner_id WHERE u.username = 'x'",
		"SELECT id FROM projects WHERE owner_id IN (SELECT id FROM users WHERE username = 'x')",
		"SELECT id FROM projects p WHERE EXISTS (SELECT 1 FROM users u WHERE u.id = p.owner_id)",
		"SELECT p.*, u.username FROM projects p, users u WHERE u.id = p.owner_id",
		"SELECT id FROM tasks",
	}
	for _, sql := range good {
		if _, err := checkRead(t, "com.paca.good", nil, sql); err != nil {
			t.Errorf("unexpected rejection for %q: %v", sql, err)
		}
	}
}

func TestCheckSensitiveReadShape_ReturnsAliases(t *testing.T) {
	aliases, err := checkRead(t, "com.paca.a", nil, "SELECT password_hash AS ph, u.password_hash AS \"h2\" FROM users u")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(aliases, ",") != "ph,h2" {
		t.Fatalf("aliases = %v", aliases)
	}
}

func TestCheckSensitiveReadShape_RequestedFieldIsExempt(t *testing.T) {
	sql := "SELECT concat(password_hash) FROM users"
	if _, err := checkRead(t, "com.paca.auth", []string{"users.password_hash"}, sql); err != nil {
		t.Fatalf("a plugin that requested users.password_hash must not be restricted: %v", err)
	}
}

func TestValidatePluginSQL_RejectsPlannerStatistics(t *testing.T) {
	for _, sql := range []string{
		"SELECT most_common_vals FROM pg_stats WHERE tablename = 'users'",
		"SELECT * FROM pg_catalog.pg_statistic",
	} {
		if _, err := validatePluginSQL(sql, sqlModeQuery); err == nil {
			t.Errorf("expected rejection for %q", sql)
		}
	}
}
