package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/jev"
)

func TestJevQuestionForNode(t *testing.T) {
	choiceCfg, _ := json.Marshal(automationdom.JevChoiceConfig{
		Instructions: "Which team?",
		Criteria:     map[string]string{"billing": "money stuff", "tech": "bugs"},
	})
	q, err := jevQuestionForNode(&automationdom.Node{Type: automationdom.JevChoiceNodeType, Config: choiceCfg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Type != jev.TypeChoice || q.Instructions != "Which team?" {
		t.Errorf("unexpected question: %+v", q)
	}
	criteria, ok := q.Criteria.(map[string]any)
	if !ok || len(criteria) != 2 {
		t.Errorf("unexpected criteria: %+v", q.Criteria)
	}

	scoreCfg, _ := json.Marshal(automationdom.JevScoreConfig{
		Instructions: "How severe?",
		Criteria:     []string{"low", "medium", "high"},
	})
	q, err = jevQuestionForNode(&automationdom.Node{Type: automationdom.JevScoreNodeType, Config: scoreCfg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Type != jev.TypeScore {
		t.Errorf("expected score type, got %v", q.Type)
	}
	levels, ok := q.Criteria.([]any)
	if !ok || len(levels) != 3 {
		t.Errorf("unexpected criteria: %+v", q.Criteria)
	}

	noulCfg, _ := json.Marshal(automationdom.JevNoulConfig{Instructions: "Is this urgent?"})
	q, err = jevQuestionForNode(&automationdom.Node{Type: automationdom.JevNoulNodeType, Config: noulCfg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Type != jev.TypeNoul {
		t.Errorf("expected noul type, got %v", q.Type)
	}

	if _, err := jevQuestionForNode(&automationdom.Node{Type: "not_a_jev_type"}); err == nil {
		t.Error("expected error for an unknown node type")
	}
}

func TestMatchedHandleForAnswer_Choice(t *testing.T) {
	cfg, _ := json.Marshal(automationdom.JevChoiceConfig{Criteria: map[string]string{"a": "x"}, ConfidenceThreshold: 0.7})

	high := 0.9
	if got := matchedHandleForAnswer(automationdom.JevChoiceNodeType, cfg, jev.Answer{Type: jev.TypeChoice, Choice: "a", Confidence: &high}); got != "a" {
		t.Errorf("expected handle 'a' at high confidence, got %q", got)
	}

	low := 0.5
	if got := matchedHandleForAnswer(automationdom.JevChoiceNodeType, cfg, jev.Answer{Type: jev.TypeChoice, Choice: "a", Confidence: &low}); got != automationdom.ElseHandle {
		t.Errorf("expected else at low confidence, got %q", got)
	}

	if got := matchedHandleForAnswer(automationdom.JevChoiceNodeType, cfg, jev.Answer{Type: jev.TypeChoice, Choice: "a"}); got != automationdom.ElseHandle {
		t.Errorf("expected else with nil confidence, got %q", got)
	}
}

func TestMatchedHandleForAnswer_Score(t *testing.T) {
	cfg, _ := json.Marshal(automationdom.JevScoreConfig{Criteria: []string{"low", "medium", "high", "critical"}})
	high := 0.95

	cases := []struct {
		score float64
		want  string
	}{
		{0, "0"}, {0.4, "0"}, {1.6, "2"}, {3, "3"}, {99, "3"}, {-5, "0"},
	}
	for _, c := range cases {
		got := matchedHandleForAnswer(automationdom.JevScoreNodeType, cfg, jev.Answer{Type: jev.TypeScore, Score: c.score, Confidence: &high})
		if got != c.want {
			t.Errorf("score %v: got handle %q, want %q", c.score, got, c.want)
		}
	}
}

func TestMatchedHandleForAnswer_Noul(t *testing.T) {
	cfg, _ := json.Marshal(automationdom.JevNoulConfig{})

	if got := matchedHandleForAnswer(automationdom.JevNoulNodeType, cfg, jev.Answer{Type: jev.TypeNoul, Noul: 0.8}); got != automationdom.PluginConditionTrueHandle {
		t.Errorf("expected true handle for a confident yes, got %q", got)
	}
	if got := matchedHandleForAnswer(automationdom.JevNoulNodeType, cfg, jev.Answer{Type: jev.TypeNoul, Noul: 0.3}); got != automationdom.ElseHandle {
		t.Errorf("expected else for a no, got %q", got)
	}

	// A custom (higher) threshold makes a middling answer fall to else.
	strictCfg, _ := json.Marshal(automationdom.JevNoulConfig{TrueThreshold: 0.9})
	if got := matchedHandleForAnswer(automationdom.JevNoulNodeType, strictCfg, jev.Answer{Type: jev.TypeNoul, Noul: 0.7}); got != automationdom.ElseHandle {
		t.Errorf("expected else below a custom stricter threshold, got %q", got)
	}
}

func TestResolveJevAnswer_UnconfiguredClientRoutesElse(t *testing.T) {
	w := &walker{
		consumer: &AutomationConsumer{projectSvc: nil},
		task:     &taskdom.Task{ID: uuid.New(), Title: "t"},
	}
	cfg, _ := json.Marshal(automationdom.JevNoulConfig{})
	handle, answer, errMsg := w.resolveJevAnswer(context.Background(), &automationdom.Node{Type: automationdom.JevNoulNodeType, Config: cfg})
	if handle != automationdom.ElseHandle {
		t.Errorf("expected else handle when jev is unconfigured, got %q", handle)
	}
	if answer != nil {
		t.Errorf("expected nil answer, got %+v", answer)
	}
	if errMsg == "" {
		t.Error("expected a non-empty error message explaining why")
	}
}

func TestJevConditionState_UsesTaskFields(t *testing.T) {
	w := &walker{task: &taskdom.Task{
		Title:       "Fix the bug",
		Description: json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"steps to repro"}]}]`),
		Importance:  35,
		Tags:        []string{"bug"},
	}}
	state := w.jevConditionState()
	if state["title"] != "Fix the bug" {
		t.Errorf("unexpected title: %v", state["title"])
	}
	if state["description"] != "steps to repro" {
		t.Errorf("unexpected description: %v", state["description"])
	}
	if state["importance"] != 35 {
		t.Errorf("unexpected importance: %v", state["importance"])
	}
}
