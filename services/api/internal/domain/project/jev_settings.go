package projectdom

// JevAutoAssignScopeAll/JevAutoAssignScopeHuman are the allowed values of
// JevSettings.AutoAssignScope.
const (
	JevAutoAssignScopeAll   = "all"
	JevAutoAssignScopeHuman = "human"
)

// JevSettings is this project's configuration for Jev-powered features
// (task field auto-fill, Auto-assign) — stored under the "jev" key of the
// existing, otherwise-unvalidated Project.Settings map. Unlike the rest of
// that map, this sub-object is validated (see ParseJevSettings) since it
// drives real behavior, not just display.
//
// Entirely inert unless the instance itself has Jev configured (see
// config.JevConfig.Effective) — these settings only choose which
// Jev-dependent features a project that CAN use Jev actually does use.
type JevSettings struct {
	// AutofillEnabled: whether worker.TaskAutofillConsumer fills in blank
	// task fields (importance, task type, and typed custom fields) after
	// creation. Defaults to true — once an instance operator has turned Jev
	// on at all, projects benefit by default, same as most opt-out (not
	// opt-in) feature flags in this codebase.
	AutofillEnabled bool
	// AutofillExcludedFields lists field keys ("importance", "task_type_id",
	// "custom:<fieldKey>") that autofill should never touch on this
	// project, even though they're otherwise eligible.
	AutofillExcludedFields []string
	// AutoAssignScope is which members Jev may choose from:
	// JevAutoAssignScopeAll (default) or JevAutoAssignScopeHuman. Auto-assign
	// itself has no separate enable toggle — a task with
	// assignment_mode='auto' always gets resolved by Jev whenever the
	// project has Jev configured; this only narrows the candidate pool.
	AutoAssignScope string
}

// DefaultJevSettings returns the settings a project has if it has never set
// the "jev" key at all.
func DefaultJevSettings() JevSettings {
	return JevSettings{
		AutofillEnabled: true,
		AutoAssignScope: JevAutoAssignScopeAll,
	}
}

// ParseJevSettings extracts and validates the "jev" sub-object of a
// project's Settings map, falling back to DefaultJevSettings for any
// missing or malformed field rather than erroring — Settings is
// client-writable free-form JSON (see Project.Settings's doc comment), so a
// stale or hand-edited shape should degrade to defaults, not break every
// read of the project.
func ParseJevSettings(settings map[string]any) JevSettings {
	out := DefaultJevSettings()
	raw, ok := settings["jev"]
	if !ok {
		return out
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	if v, ok := m["autofill_enabled"].(bool); ok {
		out.AutofillEnabled = v
	}
	if v, ok := m["auto_assign_scope"].(string); ok && (v == JevAutoAssignScopeAll || v == JevAutoAssignScopeHuman) {
		out.AutoAssignScope = v
	}
	if arr, ok := m["autofill_excluded_fields"].([]any); ok {
		fields := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				fields = append(fields, s)
			}
		}
		out.AutofillExcludedFields = fields
	}
	return out
}

// Excludes reports whether fieldKey is in AutofillExcludedFields.
func (s JevSettings) Excludes(fieldKey string) bool {
	for _, k := range s.AutofillExcludedFields {
		if k == fieldKey {
			return true
		}
	}
	return false
}
