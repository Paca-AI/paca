package vartemplate

import "testing"

func TestRender_SubstitutesKnownKeys(t *testing.T) {
	got := Render("Hello {{task.title}}, in {{sprint.name}}", map[string]string{
		"task.title":  "Fix the bug",
		"sprint.name": "Sprint 4",
	})
	want := "Hello Fix the bug, in Sprint 4"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRender_UnknownKeyLeftVerbatim(t *testing.T) {
	got := Render("Value: {{task.unknown_field}}", map[string]string{"task.title": "x"})
	want := "Value: {{task.unknown_field}}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRender_ToleratesWhitespaceInsidePlaceholder(t *testing.T) {
	got := Render("{{ task.title }}", map[string]string{"task.title": "Fix the bug"})
	if got != "Fix the bug" {
		t.Fatalf("got %q, want %q", got, "Fix the bug")
	}
}

func TestRender_NoPlaceholdersReturnsInputUnchanged(t *testing.T) {
	got := Render("plain text, no vars", map[string]string{"task.title": "x"})
	if got != "plain text, no vars" {
		t.Fatalf("got %q", got)
	}
}

func TestRender_EmptyTemplateOrVars(t *testing.T) {
	if got := Render("", map[string]string{"a": "b"}); got != "" {
		t.Fatalf("expected empty template to return empty, got %q", got)
	}
	if got := Render("{{a}}", nil); got != "{{a}}" {
		t.Fatalf("expected a nil vars map to leave the template untouched, got %q", got)
	}
}

func TestRender_SamePlaceholderMultipleTimes(t *testing.T) {
	got := Render("{{task.title}} - {{task.title}}", map[string]string{"task.title": "x"})
	if got != "x - x" {
		t.Fatalf("got %q", got)
	}
}
