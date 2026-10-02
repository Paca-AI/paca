package plugin

import (
	"fmt"
	"strings"
)

// sqlMode selects which statement shapes validatePluginSQL admits.
type sqlMode int

const (
	// sqlModeQuery is paca.db_query / db_query2: SELECT, or INSERT/UPDATE/DELETE
	// carrying a RETURNING clause.
	sqlModeQuery sqlMode = iota
	// sqlModeExec is paca.db_exec: INSERT/UPDATE/DELETE, optionally behind a WITH.
	sqlModeExec
)

// forbiddenSQLIdentifierPrefixes lists function/identifier name prefixes a
// plugin may never reference, in any position and whether or not quoted or
// schema-qualified. They cover server-side file and program access, large
// objects, cross-database calls, server control, session-setting changes, and
// functions that execute a SQL string themselves (which would sidestep the
// table-based checks in checkWriteAllowed / sensitiveColumnsForQuery).
var forbiddenSQLIdentifierPrefixes = []string{
	"pg_read_", "pg_ls_", "pg_file_", "pg_stat_file", "pg_logdir_ls",
	"pg_terminate_", "pg_cancel_", "pg_reload_", "pg_rotate_", "pg_switch_",
	"pg_promote", "pg_create_", "pg_drop_", "pg_replication_", "pg_logical_",
	"pg_backup_", "pg_start_backup", "pg_stop_backup", "pg_import_",
	"pg_export_", "pg_wal_", "pg_current_logfile", "pg_sleep",
	"pg_execute_server_program",
	"lo_", "loread", "lowrite",
	"dblink",
	"set_config",
	"query_to_", "cursor_to_", "table_to_", "schema_to_", "database_to_",
}

// validatePluginSQL is the statement-type admission gate for plugin-authored
// SQL. It is an allowlist, not a blacklist: the SQL must be a single statement
// whose leading keyword is permitted for mode, and must not reference any
// identifier in forbiddenSQLIdentifierPrefixes. It returns the statement's
// lowercased leading keyword. Comments, string literals and
// dollar-quoted bodies are skipped so their contents can neither hide a
// forbidden token nor trigger a false positive.
//
// Like the table-reference checks, this is a lexical guard, not a SQL parser,
// and is defense in depth: the real boundary for the sandbox is running plugin
// SQL under a database role without superuser or file/program privileges.
func validatePluginSQL(sqlStr string, mode sqlMode) (string, error) {
	toks, err := lexSQL(sqlStr)
	if err != nil {
		return "", err
	}

	// A single trailing semicolon is harmless; anything beyond that is a second
	// statement (the driver runs a parameterless string via the simple protocol,
	// which accepts several).
	if len(toks) > 0 && toks[len(toks)-1].kind == sqlTokSemicolon {
		toks = toks[:len(toks)-1]
	}
	if len(toks) == 0 {
		return "", fmt.Errorf("empty statement")
	}
	for _, t := range toks {
		if t.kind == sqlTokSemicolon {
			return "", fmt.Errorf("multiple statements are not allowed")
		}
	}

	first := toks[0]
	if first.kind != sqlTokWord {
		return "", fmt.Errorf("statement must start with a keyword")
	}

	has := func(word string) bool {
		for _, t := range toks {
			if t.kind == sqlTokWord && t.text == word {
				return true
			}
		}
		return false
	}

	switch mode {
	case sqlModeQuery:
		switch first.text {
		case "select":
			// SELECT ... INTO creates a table.
			if has("into") {
				return "", fmt.Errorf("SELECT INTO is not allowed")
			}
		case "insert", "update", "delete":
			if !has("returning") {
				return "", fmt.Errorf("only SELECT and DML with RETURNING statements are allowed")
			}
		default:
			return "", fmt.Errorf("only SELECT and DML with RETURNING statements are allowed")
		}
	case sqlModeExec:
		switch first.text {
		case "insert", "update", "delete", "with":
		default:
			return "", fmt.Errorf("only INSERT, UPDATE and DELETE statements are allowed")
		}
	}

	for _, t := range toks {
		if t.kind != sqlTokWord && t.kind != sqlTokQuotedIdent {
			continue
		}
		for _, p := range forbiddenSQLIdentifierPrefixes {
			if strings.HasPrefix(t.text, p) {
				return "", fmt.Errorf("use of %q is not allowed", t.text)
			}
		}
	}
	return first.text, nil
}

type sqlTokKind int

const (
	sqlTokWord        sqlTokKind = iota // unquoted identifier or keyword, lowercased
	sqlTokQuotedIdent                   // "double quoted" identifier, lowercased
	sqlTokSemicolon
	sqlTokOther // literals, operators, punctuation, parameters
)

type sqlToken struct {
	kind sqlTokKind
	text string
}

func isSQLIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isSQLIdentPart(c byte) bool {
	return isSQLIdentStart(c) || (c >= '0' && c <= '9') || c == '$'
}

// lexSQL splits sqlStr into coarse tokens, dropping comments (including
// PostgreSQL's nested block comments) and collapsing every string literal
// (plain, E”, dollar-quoted) into a single sqlTokOther. It errors on
// unterminated constructs and on Unicode-escaped literals/identifiers (U&"..."),
// which could encode a forbidden name in a form this guard cannot read.
func lexSQL(s string) ([]sqlToken, error) {
	var toks []sqlToken
	n := len(s)
	for i := 0; i < n; {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++

		case c == '-' && i+1 < n && s[i+1] == '-':
			for i < n && s[i] != '\n' {
				i++
			}

		case c == '/' && i+1 < n && s[i+1] == '*':
			depth := 1
			i += 2
			for i < n && depth > 0 {
				switch {
				case s[i] == '/' && i+1 < n && s[i+1] == '*':
					depth++
					i += 2
				case s[i] == '*' && i+1 < n && s[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
			if depth > 0 {
				return nil, fmt.Errorf("unterminated block comment")
			}

		case c == '\'':
			end, err := skipQuoted(s, i, '\'', false)
			if err != nil {
				return nil, err
			}
			toks = append(toks, sqlToken{kind: sqlTokOther})
			i = end

		case c == '"':
			end, err := skipQuoted(s, i, '"', false)
			if err != nil {
				return nil, err
			}
			body := strings.ReplaceAll(s[i+1:end-1], `""`, `"`)
			toks = append(toks, sqlToken{kind: sqlTokQuotedIdent, text: strings.ToLower(body)})
			i = end

		case c == '$':
			// $1 parameter, or $tag$ ... $tag$ dollar-quoted string.
			j := i + 1
			if j < n && s[j] >= '0' && s[j] <= '9' {
				for j < n && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				toks = append(toks, sqlToken{kind: sqlTokOther})
				i = j
				break
			}
			for j < n && isSQLIdentPart(s[j]) && s[j] != '$' {
				j++
			}
			if j < n && s[j] == '$' {
				tag := s[i : j+1]
				idx := strings.Index(s[j+1:], tag)
				if idx < 0 {
					return nil, fmt.Errorf("unterminated dollar-quoted string")
				}
				toks = append(toks, sqlToken{kind: sqlTokOther})
				i = j + 1 + idx + len(tag)
				break
			}
			toks = append(toks, sqlToken{kind: sqlTokOther})
			i++

		case isSQLIdentStart(c):
			j := i + 1
			for j < n && isSQLIdentPart(s[j]) {
				j++
			}
			word := strings.ToLower(s[i:j])
			// String-literal prefixes: E'..' honors backslash escapes; U&'..' /
			// U&"..." are rejected outright.
			if j < n && s[j] == '\'' && len(word) == 1 {
				switch word {
				case "e":
					end, err := skipQuoted(s, j, '\'', true)
					if err != nil {
						return nil, err
					}
					toks = append(toks, sqlToken{kind: sqlTokOther})
					i = end
					continue
				case "b", "x", "n":
					// Prefixed literal: the quote is lexed on the next iteration.
					i = j
					continue
				}
			}
			if word == "u" && j+1 < n && s[j] == '&' && (s[j+1] == '\'' || s[j+1] == '"') {
				return nil, fmt.Errorf("unicode escape strings and identifiers are not allowed")
			}
			toks = append(toks, sqlToken{kind: sqlTokWord, text: word})
			i = j

		case c == ';':
			toks = append(toks, sqlToken{kind: sqlTokSemicolon})
			i++

		default:
			toks = append(toks, sqlToken{kind: sqlTokOther})
			i++
		}
	}
	return toks, nil
}

// skipQuoted returns the index just past the closing quote of the quoted run
// starting at s[start] (which must be the opening quote). A doubled quote is an
// escaped quote; with backslash set, \x also escapes the next byte (E” strings).
func skipQuoted(s string, start int, quote byte, backslash bool) (int, error) {
	for i := start + 1; i < len(s); i++ {
		switch {
		case backslash && s[i] == '\\':
			i++
		case s[i] == quote:
			if i+1 < len(s) && s[i+1] == quote {
				i++
				continue
			}
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated quoted string or identifier")
}
