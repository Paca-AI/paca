package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

type fakeTrendSource struct {
	projectIDs  []uuid.UUID
	first, last time.Time
}

func series(first, last time.Time, base int64) []int64 {
	var out []int64
	for d := first; !d.After(last); d = d.Add(24 * time.Hour) {
		out = append(out, base+int64(len(out)))
	}
	return out
}

func (f *fakeTrendSource) ProjectCountTrend(_ context.Context, ids []uuid.UUID, first, last time.Time) ([]int64, error) {
	f.projectIDs, f.first, f.last = ids, first, last
	return series(first, last, 1), nil
}

func (f *fakeTrendSource) OpenTaskTrend(_ context.Context, _ []uuid.UUID, first, last time.Time) ([]int64, error) {
	return series(first, last, 10), nil
}

func (f *fakeTrendSource) UserCountTrend(_ context.Context, first, last time.Time) ([]int64, error) {
	return series(first, last, 100), nil
}

type trendBody struct {
	Days        int      `json:"days"`
	Dates       []string `json:"dates"`
	Projects    []int64  `json:"projects"`
	OpenTasks   []int64  `json:"open_tasks"`
	TeamMembers []int64  `json:"team_members"`
}

func trendRouter(opts ...handler.ProjectHandlerOption) (http.Handler, []uuid.UUID) {
	projects := []uuid.UUID{uuid.New(), uuid.New()}
	svc := &mockProjectSvc{
		list: func(_ context.Context, _, _ int) ([]*projectdom.Project, int64, error) {
			return []*projectdom.Project{{ID: projects[0]}, {ID: projects[1]}}, 2, nil
		},
	}
	clock := handler.WithProjectClock(func() time.Time {
		return time.Date(2026, 11, 25, 15, 30, 0, 0, time.UTC)
	})
	h := handler.NewProjectHandler(svc, adminAuthorizer(), append([]handler.ProjectHandlerOption{clock}, opts...)...)
	r := chi.NewRouter()
	r.Use(adminClaimsMiddleware())
	r.Get("/projects/workspace-trend", h.GetWorkspaceTrend)
	return r, projects
}

func decodeTrend(t *testing.T, body []byte) trendBody {
	t.Helper()
	var env struct {
		Data trendBody `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return env.Data
}

func TestGetWorkspaceTrend_ReturnsDailySeries(t *testing.T) {
	src := &fakeTrendSource{}
	r, projects := trendRouter(handler.WithProjectWorkspaceTrend(src))

	w := do(t, r, http.MethodGet, "/projects/workspace-trend?days=7", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Seven UTC days ending today: the day ends are the midnights after
	// Nov 19 .. Nov 25.
	wantFirst := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	wantLast := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	if !src.first.Equal(wantFirst) || !src.last.Equal(wantLast) {
		t.Fatalf("window = %s .. %s, want %s .. %s", src.first, src.last, wantFirst, wantLast)
	}
	if !slices.Equal(src.projectIDs, projects) {
		t.Fatalf("project ids = %v, want %v", src.projectIDs, projects)
	}

	got := decodeTrend(t, w.Body.Bytes())
	if got.Days != 7 {
		t.Fatalf("days = %d, want 7", got.Days)
	}
	wantDates := []string{"2026-11-19", "2026-11-20", "2026-11-21", "2026-11-22", "2026-11-23", "2026-11-24", "2026-11-25"}
	if !slices.Equal(got.Dates, wantDates) {
		t.Fatalf("dates = %v, want %v", got.Dates, wantDates)
	}
	if !slices.Equal(got.Projects, []int64{1, 2, 3, 4, 5, 6, 7}) ||
		!slices.Equal(got.OpenTasks, []int64{10, 11, 12, 13, 14, 15, 16}) ||
		!slices.Equal(got.TeamMembers, []int64{100, 101, 102, 103, 104, 105, 106}) {
		t.Fatalf("unexpected series: %+v", got)
	}
}

func TestGetWorkspaceTrend_DefaultsToThirtyDays(t *testing.T) {
	r, _ := trendRouter(handler.WithProjectWorkspaceTrend(&fakeTrendSource{}))

	w := do(t, r, http.MethodGet, "/projects/workspace-trend", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeTrend(t, w.Body.Bytes())
	if got.Days != 30 || len(got.Dates) != 30 || len(got.OpenTasks) != 30 {
		t.Fatalf("expected 30 days of data, got %+v", got)
	}
	if got.Dates[29] != "2026-11-25" {
		t.Fatalf("last date = %s, want today", got.Dates[29])
	}
}

func TestGetWorkspaceTrend_RejectsBadDays(t *testing.T) {
	r, _ := trendRouter(handler.WithProjectWorkspaceTrend(&fakeTrendSource{}))
	for _, q := range []string{"abc", "6", "91", "-1"} {
		w := do(t, r, http.MethodGet, "/projects/workspace-trend?days="+q, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("days=%s: expected 400, got %d", q, w.Code)
		}
	}
}

func TestGetWorkspaceTrend_WithoutSourceReturnsEmptySeries(t *testing.T) {
	r, _ := trendRouter()

	w := do(t, r, http.MethodGet, "/projects/workspace-trend?days=7", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeTrend(t, w.Body.Bytes())
	if len(got.Dates) != 7 {
		t.Fatalf("dates = %v, want 7", got.Dates)
	}
	if got.Projects == nil || len(got.Projects) != 0 || len(got.OpenTasks) != 0 || len(got.TeamMembers) != 0 {
		t.Fatalf("expected empty series, got %+v", got)
	}
}
