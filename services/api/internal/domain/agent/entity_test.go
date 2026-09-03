package agentdom

import "testing"

func TestValidateContextItems_Valid(t *testing.T) {
	items := []ContextItemRef{
		{Type: ContextItemTask, ID: "t1", Title: "Fix login bug"},
		{Type: ContextItemDoc, ID: "d1", Title: "Runbook"},
		{Type: ContextItemConversation, ID: "c1", Title: "Earlier thread"},
		{Type: ContextItemAutomation, ID: "a1", Title: "Nightly sync"},
	}
	if err := ValidateContextItems(items); err != nil {
		t.Errorf("expected valid items to pass, got: %v", err)
	}
}

func TestValidateContextItems_Empty(t *testing.T) {
	if err := ValidateContextItems(nil); err != nil {
		t.Errorf("expected nil/empty items to pass, got: %v", err)
	}
}

func TestValidateContextItems_TooMany(t *testing.T) {
	items := make([]ContextItemRef, MaxContextItems+1)
	for i := range items {
		items[i] = ContextItemRef{Type: ContextItemTask, ID: "t", Title: "x"}
	}
	if err := ValidateContextItems(items); err == nil {
		t.Error("expected an error for more than MaxContextItems items")
	}
}

func TestValidateContextItems_AtLimit(t *testing.T) {
	items := make([]ContextItemRef, MaxContextItems)
	for i := range items {
		items[i] = ContextItemRef{Type: ContextItemTask, ID: "t", Title: "x"}
	}
	if err := ValidateContextItems(items); err != nil {
		t.Errorf("expected exactly MaxContextItems items to pass, got: %v", err)
	}
}

func TestValidateContextItems_UnknownType(t *testing.T) {
	items := []ContextItemRef{{Type: "not-a-real-type", ID: "x1", Title: "x"}}
	if err := ValidateContextItems(items); err == nil {
		t.Error("expected an error for an unrecognized Type")
	}
}

func TestValidateContextItems_EmptyID(t *testing.T) {
	items := []ContextItemRef{{Type: ContextItemTask, ID: "", Title: "x"}}
	if err := ValidateContextItems(items); err == nil {
		t.Error("expected an error for a blank ID")
	}
}

func TestValidateContextItems_TitleTooLong(t *testing.T) {
	title := make([]byte, MaxContextItemTitleLength+1)
	for i := range title {
		title[i] = 'a'
	}
	items := []ContextItemRef{{Type: ContextItemTask, ID: "t1", Title: string(title)}}
	if err := ValidateContextItems(items); err == nil {
		t.Error("expected an error for a title exceeding MaxContextItemTitleLength")
	}
}

func TestValidateContextItems_TitleAtLimit(t *testing.T) {
	title := make([]byte, MaxContextItemTitleLength)
	for i := range title {
		title[i] = 'a'
	}
	items := []ContextItemRef{{Type: ContextItemTask, ID: "t1", Title: string(title)}}
	if err := ValidateContextItems(items); err != nil {
		t.Errorf("expected a title at exactly MaxContextItemTitleLength to pass, got: %v", err)
	}
}
