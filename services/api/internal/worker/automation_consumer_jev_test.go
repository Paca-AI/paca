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

func TestJevQuestionForConfig(t *testing.T) {
	q, err := jevQuestionForConfig(automationdom.JevConditionConfig{
		AnswerType:   automationdom.JevAnswerChoice,
		Instructions: "Which team?",
		Options:      map[string]string{"billing": "money stuff", "tech": "bugs"},
	})
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

	q, err = jevQuestionForConfig(automationdom.JevConditionConfig{
		AnswerType:   automationdom.JevAnswerScore,
		Instructions: "How severe?",
		Levels:       []string{"low", "medium", "high"},
	})
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

	q, err = jevQuestionForConfig(automationdom.JevConditionConfig{AnswerType: automationdom.JevAnswerNoul, Instructions: "Is this urgent?"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Type != jev.TypeNoul {
		t.Errorf("expected noul type, got %v", q.Type)
	}

	if _, err := jevQuestionForConfig(automationdom.JevConditionConfig{AnswerType: "not_an_answer_type"}); err == nil {
		t.Error("expected error for an unknown answer type")
	}
}

func TestMatchedHandleForAnswer_Choice(t *testing.T) {
	cfg := automationdom.JevConditionConfig{AnswerType: automationdom.JevAnswerChoice, Options: map[string]string{"a": "x"}, ConfidenceThreshold: 0.7}

	high := 0.9
	if got := matchedHandleForAnswer(cfg, jev.Answer{Type: jev.TypeChoice, Choice: "a", Confidence: &high}); got != "a" {
		t.Errorf("expected handle 'a' at high confidence, got %q", got)
	}

	low := 0.5
	if got := matchedHandleForAnswer(cfg, jev.Answer{Type: jev.TypeChoice, Choice: "a", Confidence: &low}); got != automationdom.ElseHandle {
		t.Errorf("expected else at low confidence, got %q", got)
	}

	if got := matchedHandleForAnswer(cfg, jev.Answer{Type: jev.TypeChoice, Choice: "a"}); got != automationdom.ElseHandle {
		t.Errorf("expected else with nil confidence, got %q", got)
	}

	if got := matchedHandleForAnswer(cfg, jev.Answer{Type: jev.TypeChoice, Choice: "undeclared", Confidence: &high}); got != automationdom.ElseHandle {
		t.Errorf("expected else for a choice that isn't one of the node's options, got %q", got)
	}
}

func TestMatchedHandleForAnswer_Score(t *testing.T) {
	cfg := automationdom.JevConditionConfig{AnswerType: automationdom.JevAnswerScore, Levels: []string{"low", "medium", "high", "critical"}}
	high := 0.95

	cases := []struct {
		score float64
		want  string
	}{
		{0, "0"}, {0.4, "0"}, {1.6, "2"}, {3, "3"}, {99, "3"}, {-5, "0"},
	}
	for _, c := range cases {
		got := matchedHandleForAnswer(cfg, jev.Answer{Type: jev.TypeScore, Score: c.score, Confidence: &high})
		if got != c.want {
			t.Errorf("score %v: got handle %q, want %q", c.score, got, c.want)
		}
	}
}

func TestMatchedHandleForAnswer_Noul(t *testing.T) {
	cfg := automationdom.JevConditionConfig{AnswerType: automationdom.JevAnswerNoul}

	if got := matchedHandleForAnswer(cfg, jev.Answer{Type: jev.TypeNoul, Noul: 0.8}); got != automationdom.PluginConditionTrueHandle {
		t.Errorf("expected true handle for a confident yes, got %q", got)
	}
	if got := matchedHandleForAnswer(cfg, jev.Answer{Type: jev.TypeNoul, Noul: 0.3}); got != automationdom.ElseHandle {
		t.Errorf("expected else for a no, got %q", got)
	}

	// A custom (higher) threshold makes a middling answer fall to else.
	strictCfg := automationdom.JevConditionConfig{AnswerType: automationdom.JevAnswerNoul, TrueThreshold: 0.9}
	if got := matchedHandleForAnswer(strictCfg, jev.Answer{Type: jev.TypeNoul, Noul: 0.7}); got != automationdom.ElseHandle {
		t.Errorf("expected else below a custom stricter threshold, got %q", got)
	}
}

func TestResolveJevAnswer_UnconfiguredClientRoutesElse(t *testing.T) {
	w := &walker{
		consumer: &AutomationConsumer{projectSvc: nil},
		task:     &taskdom.Task{ID: uuid.New(), Title: "t"},
	}
	cfg, _ := json.Marshal(automationdom.JevConditionConfig{AnswerType: automationdom.JevAnswerNoul})
	handle, answer, errMsg := w.resolveJevAnswer(context.Background(), &automationdom.Node{Type: automationdom.JevConditionNodeType, Config: cfg})
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
