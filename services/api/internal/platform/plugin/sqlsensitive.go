package plugin

import (
	"fmt"
	"strings"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// checkSensitiveReadShape is the admission gate for a SELECT that reaches a
// table with sensitive columns the caller hasn't requested. Read redaction
// (redactColumns) only masks result columns by output name, so any query that
// lets a sensitive value surface under a different name — wrapped in an
// expression (concat(password_hash)), packed into a whole-row value
// (to_jsonb(users), SELECT u FROM users u), pulled through a derived table, CTE
// or set operation — would slip past it. Rather than trying to track the value
// through arbitrary SQL, such queries are rejected: when the statement touches
// a sensitive table, a sensitive column may only appear as a plain select-list
// item of the top-level query, so its output name is its own (or an explicit
// AS alias, which is returned so the caller can redact it too).
//
// Concretely, for a statement touching one of tables:
//   - a column in cols may only be a bare select item (optionally qualified,
//     optionally followed by AS alias) of the top-level SELECT; any other use
//     (expressions, casts, WHERE/ORDER BY/GROUP BY, JOIN ... USING) is rejected,
//     which also closes boolean/ordering side channels;
//   - "*" / "t.*" may only be a bare select item of the top-level SELECT
//     (count(*) and multiplication are unaffected);
//   - a sensitive table's name or alias may not be used as a whole-row value;
//   - WITH, set operations, and subqueries other than IN/EXISTS/ANY/ALL/SOME
//     operands are rejected, so a sensitive value can't be relabelled by an
//     inner query.
//
// Like the rest of the plugin SQL guards this is lexical, not a parser, and
// errs on the side of rejecting. It returns the AS aliases given to sensitive
// columns.
func checkSensitiveReadShape(sqlStr string, cols, tables map[string]struct{}) ([]string, error) {
	toks, err := lexSQL(sqlStr)
	if err != nil {
		return nil, err
	}
	if len(toks) > 0 && toks[len(toks)-1].kind == sqlTokSemicolon {
		toks = toks[:len(toks)-1]
	}
	n := len(toks)

	isIdent := func(i int) bool {
		return i >= 0 && i < n && (toks[i].kind == sqlTokWord || toks[i].kind == sqlTokQuotedIdent)
	}
	isWord := func(i int, w string) bool {
		return i >= 0 && i < n && toks[i].kind == sqlTokWord && toks[i].text == w
	}
	isPunct := func(i int, p string) bool {
		return i >= 0 && i < n && toks[i].kind == sqlTokOther && toks[i].text == p
	}

	// Pass 1: depth and clause of every token. clause is "select", "from" or
	// "other" for the innermost enclosing query block / parenthesis.
	depth := make([]int, n)
	clause := make([]string, n)
	stack := []string{"other"}
	for i, t := range toks {
		if isPunct(i, ")") && len(stack) > 1 {
			stack = stack[:len(stack)-1]
		}
		depth[i] = len(stack) - 1
		clause[i] = stack[len(stack)-1]
		switch {
		case isPunct(i, "("):
			stack = append(stack, "other")
		case t.kind == sqlTokWord:
			switch t.text {
			case "select":
				stack[len(stack)-1] = "select"
			case "from", "join":
				stack[len(stack)-1] = "from"
			case "where", "group", "order", "having", "limit", "offset", "window", "fetch", "for", "returning":
				stack[len(stack)-1] = "other"
			}
		}
	}

	// Pass 2: structural bans.
	for _, t := range toks {
		if t.kind != sqlTokWord {
			continue
		}
		switch t.text {
		case "with", "union", "intersect", "except":
			return nil, fmt.Errorf("%q is not allowed in a query that reads sensitive columns", t.text)
		}
	}
	for i := range toks {
		if !isPunct(i, "(") {
			continue
		}
		if isWord(i+1, "select") || isWord(i+1, "values") || isWord(i+1, "table") {
			if !(isWord(i-1, "in") || isWord(i-1, "exists") || isWord(i-1, "any") || isWord(i-1, "all") || isWord(i-1, "some")) {
				return nil, fmt.Errorf("subqueries are only allowed as IN/EXISTS/ANY/ALL/SOME operands in a query that reads sensitive columns")
			}
		}
	}

	// itemStart reports whether token j begins a select-list item of the
	// top-level SELECT.
	itemStart := func(j int) bool {
		if j < 0 || depth[j] != 0 || clause[j] != "select" {
			return false
		}
		if j == 0 {
			return false
		}
		return isWord(j-1, "select") || isWord(j-1, "distinct") || isWord(j-1, "all") || isPunct(j-1, ",")
	}
	// qualifierStart walks back over "a.b." qualifiers preceding token i.
	qualifierStart := func(i int) int {
		j := i
		for isPunct(j-1, ".") && isIdent(j-2) {
			j -= 2
		}
		return j
	}

	// Pass 3: whole-row references. Find where each sensitive table appears in a
	// FROM list and any alias it is given.
	names := make(map[string]struct{}, len(tables))
	for tbl := range tables {
		names[tbl] = struct{}{}
	}
	tablePos := make([]bool, n)
	aliasDef := make([]bool, n)
	nonAlias := map[string]struct{}{
		"where": {}, "join": {}, "inner": {}, "left": {}, "right": {}, "full": {}, "outer": {},
		"cross": {}, "natural": {}, "on": {}, "using": {}, "group": {}, "order": {}, "limit": {},
		"offset": {}, "having": {}, "window": {}, "for": {}, "fetch": {}, "returning": {},
		"tablesample": {}, "lateral": {},
	}
	for i := range toks {
		if !isIdent(i) {
			continue
		}
		if _, ok := names[toks[i].text]; !ok {
			continue
		}
		pos := isPunct(i-1, ".") ||
			isWord(i-1, "from") || isWord(i-1, "join") || isWord(i-1, "only") || isWord(i-1, "table") ||
			(isPunct(i-1, ",") && clause[i] == "from")
		if !pos {
			continue
		}
		tablePos[i] = true
		j := i + 1
		if isWord(j, "as") {
			j++
		}
		if isIdent(j) {
			if _, stop := nonAlias[toks[j].text]; toks[j].kind == sqlTokQuotedIdent || !stop {
				aliasDef[j] = true
				names[toks[j].text] = struct{}{}
			}
		}
	}
	for i := range toks {
		if !isIdent(i) {
			continue
		}
		if _, ok := names[toks[i].text]; !ok {
			continue
		}
		if tablePos[i] || aliasDef[i] || isPunct(i+1, ".") || isPunct(i-1, ".") {
			continue
		}
		return nil, fmt.Errorf("%q may not be referenced as a whole-row value in a query that reads sensitive columns", toks[i].text)
	}

	// Pass 4: sensitive columns and wildcards may only be bare top-level select
	// items.
	var aliases []string
	for i, t := range toks {
		switch {
		case isIdent(i):
			if _, ok := cols[t.text]; !ok {
				continue
			}
			if !itemStart(qualifierStart(i)) {
				return nil, fmt.Errorf("sensitive column %q may only be selected directly, not used in an expression, filter or subquery", t.text)
			}
			switch {
			case i+1 >= n, isPunct(i+1, ","), isWord(i+1, "from"):
			case isWord(i+1, "as") && isIdent(i+2):
				aliases = append(aliases, toks[i+2].text)
			default:
				return nil, fmt.Errorf("sensitive column %q may only be selected directly (use AS to rename it)", t.text)
			}
		case isPunct(i, "*"):
			if isPunct(i-1, "(") && isWord(i-2, "count") && isPunct(i+1, ")") {
				continue
			}
			wildcard := isPunct(i-1, ".") || isWord(i-1, "select") || isWord(i-1, "distinct") ||
				isWord(i-1, "all") || (isPunct(i-1, ",") && clause[i] == "select")
			if !wildcard {
				continue // multiplication
			}
			if !itemStart(qualifierStart(i)) {
				return nil, fmt.Errorf("\"*\" may only be selected directly in a query that reads sensitive columns")
			}
			if !(i+1 >= n || isPunct(i+1, ",") || isWord(i+1, "from")) {
				return nil, fmt.Errorf("\"*\" may only be selected directly in a query that reads sensitive columns")
			}
		}
	}
	return aliases, nil
}

// sensitiveReadTables returns the lowercased names of the tables the query
// reaches that have at least one sensitive column the caller hasn't requested,
// and those columns. Besides the regex-derived table references it also
// matches core sensitive table names appearing anywhere as a token, so an
// unusual table-reference spelling can't hide one.
func (r *Runtime) sensitiveReadTables(caller plugindom.Plugin, sqlStr string) (tables, cols map[string]struct{}) {
	tables = make(map[string]struct{})
	cols = make(map[string]struct{})
	callerSchema := schemaName(caller.Name)
	add := func(owner string, ref tableRef, cs []string) {
		for _, c := range cs {
			if isRequestedSensitiveField(caller, owner, ref.table, c) {
				continue
			}
			tables[ref.table] = struct{}{}
			cols[strings.ToLower(c)] = struct{}{}
		}
	}
	for _, ref := range referencedTables(sqlStr) {
		owner, cs := r.sensitiveTableColumns(callerSchema, ref)
		add(owner, ref, cs)
	}
	if toks, err := lexSQL(sqlStr); err == nil {
		for _, t := range toks {
			if t.kind != sqlTokWord && t.kind != sqlTokQuotedIdent {
				continue
			}
			if cs, ok := coreSensitiveFields[t.text]; ok {
				add("", tableRef{table: t.text}, cs)
			}
		}
	}
	return tables, cols
}
