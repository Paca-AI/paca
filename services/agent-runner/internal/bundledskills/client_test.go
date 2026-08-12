package bundledskills

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Load_FetchesAndCaches(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.URL.Query().Get("target"); got != "agent" {
			t.Errorf("target query param = %q, want %q", got, "agent")
		}
		fmt.Fprint(w, `{"success":true,"data":{"skills":[
			{"name":"paca","path":"paca.md","content":"# paca routing"},
			{"name":"paca-do","path":"paca-do/SKILL.md","content":"# paca-do clone/push workflow"}
		]}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	skills, err := c.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("len(skills) = %d, want 2", len(skills))
	}
	if skills[0].SkillName != "paca" || !skills[0].IsEnabled {
		t.Errorf("skills[0] = %+v, want name=paca IsEnabled=true", skills[0])
	}
	if skills[1].SkillName != "paca-do" || skills[1].SkillContent != "# paca-do clone/push workflow" {
		t.Errorf("skills[1] = %+v, want name=paca-do with clone/push content", skills[1])
	}

	if _, err := c.Load(context.Background()); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1 (second Load should hit cache)", requests)
	}
}

func TestClient_Load_FailureNotCached(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"skills":[{"name":"paca","path":"paca.md","content":"x"}]}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	if _, err := c.Load(context.Background()); err == nil {
		t.Fatal("first Load: expected error, got nil")
	}

	skills, err := c.Load(context.Background())
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if requests != 2 {
		t.Errorf("requests = %d, want 2 (failed fetch must not be cached)", requests)
	}
	if len(skills) != 1 {
		t.Fatalf("len(skills) = %d, want 1", len(skills))
	}
}
