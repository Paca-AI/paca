package automationdom

import "testing"

func TestNodeRequiresTask(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		typ  string
		want bool
	}{
		{"built-in condition", KindCondition, ConditionNodeType, true},
		{"plugin condition", KindCondition, "com.acme.some_condition", true},
		{"call_api action does not require a task", KindAction, string(ActionCallAPI), false},
		{"trigger_ai_agent action does not require a task", KindAction, string(ActionTriggerAIAgent), false},
		{"update_task action requires a task", KindAction, string(ActionUpdateTask), true},
		{"plugin action conservatively requires a task", KindAction, "com.acme.some_action", true},
		{"trigger never requires a task itself", KindTrigger, string(TriggerCron), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NodeRequiresTask(tt.kind, tt.typ); got != tt.want {
				t.Errorf("NodeRequiresTask(%v, %q) = %v, want %v", tt.kind, tt.typ, got, tt.want)
			}
		})
	}
}
