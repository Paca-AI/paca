package projectdom

import "testing"

func TestComposeJevDescription_HumanWithDescription(t *testing.T) {
	m := ProjectMember{MemberType: "human", FullName: "Alice", Description: "  Frontend lead  "}
	if got := m.ComposeJevDescription(); got != "Frontend lead" {
		t.Errorf("expected trimmed description, got %q", got)
	}
}

func TestComposeJevDescription_HumanWithoutDescriptionFallsBackToDisplayName(t *testing.T) {
	m := ProjectMember{MemberType: "human", FullName: "Alice"}
	if got := m.ComposeJevDescription(); got != "Alice" {
		t.Errorf("expected fallback to display name, got %q", got)
	}
}

func TestComposeJevDescription_AgentWithDescriptionAndProvider(t *testing.T) {
	m := ProjectMember{
		MemberType:       "agent",
		AgentName:        "Bot",
		AgentDescription: "Handles backend bugs",
		AgentLLMProvider: "anthropic",
	}
	want := "Handles backend bugs\nProvider: anthropic"
	if got := m.ComposeJevDescription(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComposeJevDescription_AgentACPProviderOverridesLLMProvider(t *testing.T) {
	acp := "claude-code"
	m := ProjectMember{
		MemberType:       "agent",
		AgentName:        "Bot",
		AgentLLMProvider: "",
		AgentACPProvider: &acp,
	}
	if got := m.ComposeJevDescription(); got != "Provider: claude-code" {
		t.Errorf("got %q", got)
	}
}

func TestComposeJevDescription_AgentWithNeitherFallsBackToDisplayName(t *testing.T) {
	m := ProjectMember{MemberType: "agent", AgentName: "Bot"}
	if got := m.ComposeJevDescription(); got != "Bot" {
		t.Errorf("expected fallback to display name, got %q", got)
	}
}

func TestParseJevSettings_Defaults(t *testing.T) {
	got := ParseJevSettings(map[string]any{})
	if !got.AutofillEnabled {
		t.Error("expected AutofillEnabled to default true")
	}
	if got.AutoAssignScope != JevAutoAssignScopeAll {
		t.Errorf("expected default scope=all, got %q", got.AutoAssignScope)
	}
	if len(got.AutofillExcludedFields) != 0 {
		t.Errorf("expected no excluded fields by default, got %v", got.AutofillExcludedFields)
	}
}

func TestParseJevSettings_ExplicitValues(t *testing.T) {
	got := ParseJevSettings(map[string]any{
		"jev": map[string]any{
			"autofill_enabled":         false,
			"autofill_excluded_fields": []any{"importance", "custom:severity"},
			"auto_assign_scope":        "human",
		},
	})
	if got.AutofillEnabled {
		t.Error("expected AutofillEnabled=false")
	}
	if got.AutoAssignScope != JevAutoAssignScopeHuman {
		t.Errorf("expected scope=human, got %q", got.AutoAssignScope)
	}
	if !got.Excludes("importance") || !got.Excludes("custom:severity") {
		t.Errorf("expected both fields excluded, got %v", got.AutofillExcludedFields)
	}
	if got.Excludes("task_type_id") {
		t.Error("task_type_id was not in the excluded list")
	}
}

func TestParseJevSettings_MalformedShapeFallsBackToDefaults(t *testing.T) {
	got := ParseJevSettings(map[string]any{"jev": "not an object"})
	if !got.AutofillEnabled || got.AutoAssignScope != JevAutoAssignScopeAll {
		t.Errorf("expected defaults for a malformed jev value, got %+v", got)
	}

	got = ParseJevSettings(map[string]any{"jev": map[string]any{"auto_assign_scope": "bogus"}})
	if got.AutoAssignScope != JevAutoAssignScopeAll {
		t.Errorf("expected an invalid scope value to fall back to the default, got %q", got.AutoAssignScope)
	}
}
