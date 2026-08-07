// Package vartemplate implements minimal, non-nested {{variable}}
// placeholder substitution for automation node config fields — e.g.
// trigger_ai_agent's Message, call_api's URL/Body/Headers, update_task's
// Title, update_sprint's Name/Goal. Deliberately not a general expression
// language: no arithmetic, no logic, no function calls — just "does this
// key exist in the current walk's context, and if so substitute it."
package vartemplate

import "regexp"

// placeholderPattern matches {{key}}, optionally padded with whitespace
// (e.g. {{ task.title }}). key may contain letters, digits, underscores,
// and dots (for the "namespace.field" shape every variable uses today —
// task.title, sprint.name, automation.name).
var placeholderPattern = regexp.MustCompile(`\{\{\s*([\w.]+)\s*\}\}`)

// Render replaces every {{key}} in tmpl with vars[key]. A placeholder whose
// key isn't in vars is left verbatim (including its braces) rather than
// blanked out — a template referencing {{task.sprint_name}} when there's no
// sprint in context should read as obviously unresolved in the output, not
// silently vanish into an empty string that looks intentional.
func Render(tmpl string, vars map[string]string) string {
	if tmpl == "" || len(vars) == 0 {
		return tmpl
	}
	return placeholderPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := placeholderPattern.FindStringSubmatch(match)[1]
		if v, ok := vars[key]; ok {
			return v
		}
		return match
	})
}
