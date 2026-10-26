package worker

import "testing"

func TestIsAskQuestionResult(t *testing.T) {
	result := `{"content":[{"type":"content","content":{"text":"Question shown to the user. Stop now and wait.\n\n` + "```paca-ask" + `\n{}\n` + "```" + `","type":"text"}}]}`
	cases := []struct {
		name, eventType, payload string
		want                     bool
	}{
		{"ask result", "tool_call_update", result, true},
		{"wrong event type", "tool_call", result, false},
		{"other tool result", "tool_call_update", `{"content":"ok"}`, false},
		{"prefix without block", "tool_call_update", "Question shown to the user", false},
	}
	for _, tc := range cases {
		if got := isAskQuestionResult(tc.eventType, tc.payload); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
