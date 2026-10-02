package plugin

import "testing"

func TestValidatePluginSQL(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		mode    sqlMode
		wantErr bool
		verb    string
	}{
		// Allowed.
		{"select", "SELECT id, name FROM items WHERE id = $1", sqlModeQuery, false, "select"},
		{"select leading comment", "/* hi */ select 1", sqlModeQuery, false, "select"},
		{"select trailing semicolon", "SELECT 1;", sqlModeQuery, false, "select"},
		{"select forbidden word in string", "SELECT 'pg_read_file(x); COPY' AS s", sqlModeQuery, false, "select"},
		{"select forbidden word in comment", "SELECT 1 -- pg_read_file\n", sqlModeQuery, false, "select"},
		{"select dollar quoted", "SELECT $q$ ; pg_read_file $q$", sqlModeQuery, false, "select"},
		{"insert returning", "INSERT INTO t(a) VALUES ($1) RETURNING id", sqlModeQuery, false, "insert"},
		{"exec insert upsert", "INSERT INTO t(a) VALUES ($1) ON CONFLICT (a) DO UPDATE SET a = EXCLUDED.a", sqlModeExec, false, "insert"},
		{"exec update", "UPDATE t SET a = 1 WHERE id = $1", sqlModeExec, false, "update"},
		{"exec delete", "DELETE FROM t WHERE id = $1", sqlModeExec, false, "delete"},
		{"exec with cte", "WITH x AS (SELECT 1) DELETE FROM t USING x", sqlModeExec, false, "with"},

		// Statement type.
		{"exec copy program", "COPY (SELECT 1) TO PROGRAM 'id > /tmp/x'", sqlModeExec, true, ""},
		{"exec copy lowercase", "  copy t to program 'id'", sqlModeExec, true, ""},
		{"exec do block", "DO $$ BEGIN PERFORM 1; END $$", sqlModeExec, true, ""},
		{"exec set", "SET ROLE postgres", sqlModeExec, true, ""},
		{"exec call", "CALL p()", sqlModeExec, true, ""},
		{"exec create", "CREATE TABLE x(a int)", sqlModeExec, true, ""},
		{"exec drop", "DROP TABLE x", sqlModeExec, true, ""},
		{"exec select", "SELECT 1", sqlModeExec, true, ""},
		{"comment hides copy", "/* x */ COPY t TO PROGRAM 'id'", sqlModeExec, true, ""},
		{"query copy", "COPY t TO PROGRAM 'id'", sqlModeQuery, true, ""},
		{"query dml without returning", "DELETE FROM t", sqlModeQuery, true, ""},
		{"query with", "WITH x AS (SELECT 1) SELECT * FROM x", sqlModeQuery, true, ""},
		{"select into", "SELECT 1 INTO newtbl", sqlModeQuery, true, ""},
		{"empty", "  -- nothing", sqlModeExec, true, ""},

		// Multi-statement.
		{"stacked", "SELECT 1; COPY t TO PROGRAM 'id'", sqlModeQuery, true, ""},
		{"stacked exec", "DELETE FROM t; DROP TABLE t", sqlModeExec, true, ""},

		// Forbidden functions.
		{"pg_read_file", "SELECT pg_read_file('/etc/passwd')", sqlModeQuery, true, ""},
		{"pg_catalog qualified", "SELECT pg_catalog.pg_read_file('/etc/passwd')", sqlModeQuery, true, ""},
		{"quoted", `SELECT "pg_read_file"('/etc/passwd')`, sqlModeQuery, true, ""},
		{"mixed case", "SELECT PG_Read_Binary_File('/etc/passwd')", sqlModeQuery, true, ""},
		{"pg_ls_dir", "SELECT pg_ls_dir('/')", sqlModeQuery, true, ""},
		{"lo_import", "SELECT lo_import('/etc/passwd')", sqlModeQuery, true, ""},
		{"query_to_xml", "SELECT query_to_xml('select password_hash from users', true, false, '')", sqlModeQuery, true, ""},
		{"set_config", "SELECT set_config('role', 'postgres', false)", sqlModeQuery, true, ""},
		{"dblink", "SELECT * FROM dblink('x', 'select 1') AS t(a int)", sqlModeQuery, true, ""},
		{"in cte", "WITH x AS (SELECT pg_read_file('/etc/passwd')) DELETE FROM t", sqlModeExec, true, ""},
		{"in returning", "INSERT INTO t(a) VALUES (1) RETURNING pg_read_file('/etc/passwd')", sqlModeQuery, true, ""},

		// Obfuscation.
		{"unicode escape ident", `SELECT U&"pg\005fread_file"('/etc/passwd')`, sqlModeQuery, true, ""},
		{"unterminated string", "SELECT 'abc", sqlModeQuery, true, ""},
		{"unterminated comment", "SELECT 1 /* abc", sqlModeQuery, true, ""},
		{"unterminated dollar", "SELECT $x$ abc", sqlModeQuery, true, ""},
		{"e-string backslash quote", `SELECT E'\'' ; COPY t TO PROGRAM 'id'`, sqlModeQuery, true, ""},
		{"nested comment", "SELECT 1 /* a /* b */ ; COPY */ ; DROP TABLE t", sqlModeQuery, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verb, err := validatePluginSQL(tt.sql, tt.mode)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePluginSQL(%q) error = %v, wantErr %v", tt.sql, err, tt.wantErr)
			}
			if err == nil && verb != tt.verb {
				t.Fatalf("verb = %q, want %q", verb, tt.verb)
			}
		})
	}
}
