package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
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

// stubProjectReader is a projectSettingsReader returning a fixed project.
type stubProjectReader struct {
	project *projectdom.Project
	err     error
}

func (s stubProjectReader) GetByID(context.Context, uuid.UUID) (*projectdom.Project, error) {
	return s.project, s.err
}

// newJevWalker builds a walker whose consumer reaches jevURL through a plain
// (non-SSRF-guarded) transport, so the Jev call hits a local httptest.Server.
func newJevWalker(project *projectdom.Project, readErr error) *walker {
	return &walker{
		consumer: &AutomationConsumer{
			projectSvc: stubProjectReader{project: project, err: readErr},
			httpClient: &http.Client{Timeout: 5 * time.Second},
		},
		projectID: uuid.New(),
		task:      &taskdom.Task{ID: uuid.New(), Title: "Refund request", Tags: []string{"billing"}},
	}
}

func jevNode(t *testing.T, cfg automationdom.JevConditionConfig) *automationdom.Node {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	return &automationdom.Node{ID: uuid.New(), Type: automationdom.JevConditionNodeType, Config: raw}
}

func TestResolveJevAnswer_ChoiceRoutesToMatchedOption(t *testing.T) {
	var gotReq jev.Request
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		conf := 0.92
		_ = json.NewEncoder(w).Encode(jev.Response{Answers: map[string]jev.Answer{
			"answer": {Type: jev.TypeChoice, Choice: "billing", Confidence: &conf},
		}})
	}))
	defer srv.Close()

	w := newJevWalker(&projectdom.Project{JevAPIKeySecret: "k", JevBaseURL: srv.URL, JevModel: "m"}, nil)
	handle, ans, errMsg := w.resolveJevAnswer(context.Background(), jevNode(t, automationdom.JevConditionConfig{
		AnswerType:   automationdom.JevAnswerChoice,
		Instructions: "Which team?",
		Options:      map[string]string{"billing": "money", "tech": "bugs"},
	}))
	if errMsg != "" || handle != "billing" || ans == nil || ans.Choice != "billing" {
		t.Fatalf("unexpected result: handle=%q ans=%+v err=%q", handle, ans, errMsg)
	}
	if gotAuth != "Bearer k" {
		t.Errorf("expected the project's key as bearer token, got %q", gotAuth)
	}
	if gotReq.Model != "m" {
		t.Errorf("expected the project's model, got %q", gotReq.Model)
	}
	q, ok := gotReq.Questions["answer"]
	if !ok || q.Type != jev.TypeChoice || q.Instructions != "Which team?" {
		t.Errorf("unexpected question sent: %+v", gotReq.Questions)
	}
	state, _ := gotReq.State.(map[string]any)
	if state["title"] != "Refund request" {
		t.Errorf("expected the task title in state, got %v", gotReq.State)
	}
}

func TestResolveJevAnswer_APIErrorRoutesElse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	w := newJevWalker(&projectdom.Project{JevAPIKeySecret: "k", JevBaseURL: srv.URL}, nil)
	handle, ans, errMsg := w.resolveJevAnswer(context.Background(), jevNode(t, automationdom.JevConditionConfig{
		AnswerType: automationdom.JevAnswerNoul, Instructions: "Is it urgent?",
	}))
	if handle != automationdom.ElseHandle || ans != nil || errMsg == "" {
		t.Fatalf("expected else with an error, got handle=%q ans=%+v err=%q", handle, ans, errMsg)
	}
}

func TestResolveJevAnswer_MissingAnswerRoutesElse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jev.Response{Answers: map[string]jev.Answer{}})
	}))
	defer srv.Close()

	w := newJevWalker(&projectdom.Project{JevAPIKeySecret: "k", JevBaseURL: srv.URL}, nil)
	handle, _, errMsg := w.resolveJevAnswer(context.Background(), jevNode(t, automationdom.JevConditionConfig{
		AnswerType: automationdom.JevAnswerNoul, Instructions: "Is it urgent?",
	}))
	if handle != automationdom.ElseHandle || errMsg != "jev returned no answer" {
		t.Fatalf("unexpected result: handle=%q err=%q", handle, errMsg)
	}
}

func TestResolveJevAnswer_ProjectWithoutKeyRoutesElse(t *testing.T) {
	w := newJevWalker(&projectdom.Project{}, nil)
	handle, _, errMsg := w.resolveJevAnswer(context.Background(), jevNode(t, automationdom.JevConditionConfig{
		AnswerType: automationdom.JevAnswerNoul, Instructions: "x",
	}))
	if handle != automationdom.ElseHandle || errMsg == "" {
		t.Fatalf("unexpected result: handle=%q err=%q", handle, errMsg)
	}
}

func TestResolveJevAnswer_ProjectLoadErrorRoutesElse(t *testing.T) {
	w := newJevWalker(nil, errors.New("db down"))
	handle, _, errMsg := w.resolveJevAnswer(context.Background(), jevNode(t, automationdom.JevConditionConfig{
		AnswerType: automationdom.JevAnswerNoul, Instructions: "x",
	}))
	if handle != automationdom.ElseHandle || errMsg == "" {
		t.Fatalf("unexpected result: handle=%q err=%q", handle, errMsg)
	}
}

func TestResolveJevAnswer_BadConfigRoutesElse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("jev must not be called for an invalid node config")
	}))
	defer srv.Close()
	w := newJevWalker(&projectdom.Project{JevAPIKeySecret: "k", JevBaseURL: srv.URL}, nil)

	handle, _, errMsg := w.resolveJevAnswer(context.Background(),
		&automationdom.Node{Type: automationdom.JevConditionNodeType, Config: json.RawMessage(`not json`)})
	if handle != automationdom.ElseHandle || errMsg == "" {
		t.Fatalf("malformed config: handle=%q err=%q", handle, errMsg)
	}

	handle, _, errMsg = w.resolveJevAnswer(context.Background(), jevNode(t, automationdom.JevConditionConfig{AnswerType: "bogus"}))
	if handle != automationdom.ElseHandle || errMsg == "" {
		t.Fatalf("unknown answer type: handle=%q err=%q", handle, errMsg)
	}
}
