package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// fixedTime returns a stable timestamp for building test conversations —
// only its UTC-roundtrip stability through the cursor codec matters here.
func fixedTime() time.Time {
	return time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
}

// ---------------------------------------------------------------------------
// Router helper
// ---------------------------------------------------------------------------

func newConversationRouter(svc agentdom.Service) chi.Router {
	h := handler.NewConversationHandler(svc)
	r := chi.NewRouter()
	r.Route("/projects/{projectId}/conversations", func(r chi.Router) {
		r.Get("/", h.ListConversations)
		r.Get("/{conversationId}/events", h.ListConversationEvents)
	})
	return r
}

// listConversationsResp decodes the standard envelope + list-conversations
// data shape returned by GET /projects/:projectId/conversations.
type listConversationsResp struct {
	Success bool `json:"success"`
	Data    struct {
		Items      []map[string]any `json:"items"`
		PageSize   int              `json:"page_size"`
		NextCursor *string          `json:"next_cursor"`
	} `json:"data"`
	ErrorCode string `json:"error_code"`
}

func doListConversations(t *testing.T, svc agentdom.Service, projectID, query string) (*httptest.ResponseRecorder, listConversationsResp) {
	t.Helper()
	r := newConversationRouter(svc)
	url := "/projects/" + projectID + "/conversations/"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var resp listConversationsResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return rec, resp
}

// ---------------------------------------------------------------------------
// page_size handling
// ---------------------------------------------------------------------------

func TestListConversations_DefaultPageSize(t *testing.T) {
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			return nil, false, nil
		},
	}
	rec, resp := doListConversations(t, svc, uuid.New().String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if resp.Data.PageSize != 20 {
		t.Errorf("expected default page_size=20, got %d", resp.Data.PageSize)
	}
}

func TestListConversations_PageSizeValid(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantLimit int
	}{
		{"absent_defaults", "", 20},
		{"valid_custom_size_kept", "page_size=50", 50},
		{"max_boundary_kept", "page_size=200", 200},
		{"min_boundary_kept", "page_size=1", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotLimit int
			svc := &mockAgentSvc{
				listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error) {
					gotLimit = limit
					return nil, false, nil
				},
			}
			rec, resp := doListConversations(t, svc, uuid.New().String(), tc.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			if gotLimit != tc.wantLimit {
				t.Errorf("expected service called with limit=%d, got %d", tc.wantLimit, gotLimit)
			}
			if resp.Data.PageSize != tc.wantLimit {
				t.Errorf("expected response page_size=%d, got %d", tc.wantLimit, resp.Data.PageSize)
			}
		})
	}
}

// TestListConversations_PageSizeInvalidRejected covers the fix for silent
// page_size substitution: an explicitly supplied out-of-range or non-numeric
// value now fails the request instead of quietly running with a different
// page_size than the caller asked for (which broke offset math for callers
// paginating by page_size).
func TestListConversations_PageSizeInvalidRejected(t *testing.T) {
	cases := []string{
		"page_size=0",
		"page_size=-5",
		"page_size=201",
		"page_size=abc",
	}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			svc := &mockAgentSvc{
				listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error) {
					t.Fatalf("service should not be called for invalid page_size, got limit=%d", limit)
					return nil, false, nil
				},
			}
			rec, _ := doListConversations(t, svc, uuid.New().String(), query)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListConversationEvents: offset/limit handling
// ---------------------------------------------------------------------------

func doListConversationEvents(t *testing.T, svc agentdom.Service, projectID, conversationID, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := newConversationRouter(svc)
	url := "/projects/" + projectID + "/conversations/" + conversationID + "/events"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestListConversationEvents_OffsetLimitValid(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		wantOffset int
		wantLimit  int
	}{
		{"absent_defaults", "", 0, 50},
		{"valid_custom_values_kept", "offset=10&limit=25", 10, 25},
		{"limit_max_boundary_kept", "limit=200", 0, 200},
		{"limit_min_boundary_kept", "limit=1", 0, 1},
		{"offset_zero_kept", "offset=0", 0, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotOffset, gotLimit int
			svc := &mockAgentSvc{
				listConversationEvents: func(_ context.Context, _ uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error) {
					gotOffset, gotLimit = offset, limit
					return nil, 0, nil
				},
			}
			rec := doListConversationEvents(t, svc, uuid.New().String(), uuid.New().String(), tc.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			if gotOffset != tc.wantOffset || gotLimit != tc.wantLimit {
				t.Errorf("expected service called with offset=%d limit=%d, got offset=%d limit=%d",
					tc.wantOffset, tc.wantLimit, gotOffset, gotLimit)
			}
		})
	}
}

// TestListConversationEvents_OffsetLimitInvalidRejected covers the same
// silent-substitution fix as TestListConversations_PageSizeInvalidRejected,
// applied to this endpoint's offset/limit pair: an explicitly supplied
// out-of-range or non-numeric value now fails the request instead of quietly
// running with a different limit than the caller asked for (which broke
// offset math for callers advancing offset by their requested limit).
func TestListConversationEvents_OffsetLimitInvalidRejected(t *testing.T) {
	cases := []string{
		"offset=-1",
		"offset=abc",
		"limit=0",
		"limit=-5",
		"limit=201",
		"limit=abc",
	}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			svc := &mockAgentSvc{
				listConversationEvents: func(_ context.Context, _ uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error) {
					t.Fatalf("service should not be called for invalid offset/limit, got offset=%d limit=%d", offset, limit)
					return nil, 0, nil
				},
			}
			rec := doListConversationEvents(t, svc, uuid.New().String(), uuid.New().String(), query)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Filter wiring
// ---------------------------------------------------------------------------

func TestListConversations_FiltersPassedToService(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()

	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			return nil, false, nil
		},
	}
	_, _ = doListConversations(t, svc, projectID.String(),
		"agent_id="+agentID.String()+"&status=running&cursor=some-opaque-cursor")

	if gotFilter.ProjectID == nil || *gotFilter.ProjectID != projectID {
		t.Errorf("expected ProjectID filter %v, got %v", projectID, gotFilter.ProjectID)
	}
	if !slices.Equal(gotFilter.AgentIDs, []uuid.UUID{agentID}) {
		t.Errorf("expected AgentIDs filter [%v], got %v", agentID, gotFilter.AgentIDs)
	}
	if !slices.Equal(gotFilter.Statuses, []string{"running"}) {
		t.Errorf("expected Statuses filter [%q], got %v", "running", gotFilter.Statuses)
	}
	if gotFilter.CursorAfter == nil || *gotFilter.CursorAfter != "some-opaque-cursor" {
		t.Errorf("expected CursorAfter filter %q, got %v", "some-opaque-cursor", gotFilter.CursorAfter)
	}
}

func TestListConversations_InvalidAgentIDIgnored(t *testing.T) {
	// A malformed agent_id query param should be silently dropped rather than
	// erroring the request — only valid UUIDs are added to the filter.
	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			return nil, false, nil
		},
	}
	rec, _ := doListConversations(t, svc, uuid.New().String(), "agent_id=not-a-uuid")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(gotFilter.AgentIDs) != 0 {
		t.Errorf("expected AgentIDs filter to remain empty for malformed input, got %v", gotFilter.AgentIDs)
	}
}

func TestListConversations_MultiValueAgentIDAndStatus(t *testing.T) {
	agent1, agent2 := uuid.New(), uuid.New()
	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			return nil, false, nil
		},
	}
	_, _ = doListConversations(t, svc, uuid.New().String(),
		"agent_id="+agent1.String()+","+agent2.String()+"&status=running,paused")

	if !slices.Equal(gotFilter.AgentIDs, []uuid.UUID{agent1, agent2}) {
		t.Errorf("expected AgentIDs filter [%v, %v], got %v", agent1, agent2, gotFilter.AgentIDs)
	}
	if !slices.Equal(gotFilter.Statuses, []string{"running", "paused"}) {
		t.Errorf("expected Statuses filter [running paused], got %v", gotFilter.Statuses)
	}
}

func TestListConversations_TriggerTypeFilterPassedToService(t *testing.T) {
	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			return nil, false, nil
		},
	}
	_, _ = doListConversations(t, svc, uuid.New().String(), "trigger_type=task_assigned,chat_message")

	if !slices.Equal(gotFilter.TriggerTypes, []string{"task_assigned", "chat_message"}) {
		t.Errorf("expected TriggerTypes filter [task_assigned chat_message], got %v", gotFilter.TriggerTypes)
	}
}

func TestListConversations_SearchFilterPassedToService(t *testing.T) {
	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			return nil, false, nil
		},
	}
	_, _ = doListConversations(t, svc, uuid.New().String(), "search=%20fix+the+bug%20")

	if gotFilter.Search == nil || *gotFilter.Search != "fix the bug" {
		t.Errorf("expected trimmed Search filter %q, got %v", "fix the bug", gotFilter.Search)
	}
}

func TestListConversations_SearchAbsentWhenBlank(t *testing.T) {
	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			return nil, false, nil
		},
	}
	_, _ = doListConversations(t, svc, uuid.New().String(), "search=%20%20")

	if gotFilter.Search != nil {
		t.Errorf("expected nil Search filter for blank input, got %q", *gotFilter.Search)
	}
}

func TestListConversations_DateRangeFilterPassedToService(t *testing.T) {
	// A bare "YYYY-MM-DD" date is treated as a whole UTC day, inclusive on
	// both ends — so created_before=2026-01-31 becomes an exclusive bound of
	// UTC midnight on 2026-02-01. See parseCreatedAfterBound/
	// parseCreatedBeforeBound.
	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			return nil, false, nil
		},
	}
	_, _ = doListConversations(t, svc, uuid.New().String(), "created_after=2026-01-01&created_before=2026-01-31")

	wantAfter := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if gotFilter.CreatedAfter == nil || !gotFilter.CreatedAfter.Equal(wantAfter) {
		t.Errorf("expected CreatedAfter filter %v, got %v", wantAfter, gotFilter.CreatedAfter)
	}
	if gotFilter.CreatedBefore == nil || !gotFilter.CreatedBefore.Equal(wantBefore) {
		t.Errorf("expected CreatedBefore filter %v, got %v", wantBefore, gotFilter.CreatedBefore)
	}
}

func TestListConversations_DateRangeFilterAcceptsRFC3339Timestamp(t *testing.T) {
	// The frontend sends precise RFC3339 instants (computed from the user's
	// local calendar day) rather than bare dates, to avoid the UTC-day
	// mismatch a bare date would have for non-UTC users. Those must be used
	// exactly as given, with no additional whole-day adjustment.
	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			return nil, false, nil
		},
	}
	_, _ = doListConversations(t, svc, uuid.New().String(),
		"created_after="+url.QueryEscape("2026-01-01T08:00:00Z")+"&created_before="+url.QueryEscape("2026-01-31T08:00:00Z"))

	wantAfter := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 1, 31, 8, 0, 0, 0, time.UTC)
	if gotFilter.CreatedAfter == nil || !gotFilter.CreatedAfter.Equal(wantAfter) {
		t.Errorf("expected CreatedAfter filter %v, got %v", wantAfter, gotFilter.CreatedAfter)
	}
	if gotFilter.CreatedBefore == nil || !gotFilter.CreatedBefore.Equal(wantBefore) {
		t.Errorf("expected CreatedBefore filter %v, got %v", wantBefore, gotFilter.CreatedBefore)
	}
}

func TestListConversations_InvalidDateRangeRejected(t *testing.T) {
	cases := []string{
		"created_after=not-a-date",
		"created_after=2026-13-40",
		"created_before=not-a-date",
	}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			svc := &mockAgentSvc{
				listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
					t.Fatal("service should not be called for an invalid date filter")
					return nil, false, nil
				},
			}
			rec, _ := doListConversations(t, svc, uuid.New().String(), query)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListConversations_InvalidStatusRejected(t *testing.T) {
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			t.Fatal("service should not be called for an invalid status filter")
			return nil, false, nil
		},
	}
	rec, _ := doListConversations(t, svc, uuid.New().String(), "status=running,not-a-real-status")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListConversations_InvalidTriggerTypeRejected(t *testing.T) {
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			t.Fatal("service should not be called for an invalid trigger_type filter")
			return nil, false, nil
		},
	}
	rec, _ := doListConversations(t, svc, uuid.New().String(), "trigger_type=not-a-real-type")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListConversations_NoCursorParam_FilterCursorNil(t *testing.T) {
	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			return nil, false, nil
		},
	}
	_, _ = doListConversations(t, svc, uuid.New().String(), "")
	if gotFilter.CursorAfter != nil {
		t.Errorf("expected nil CursorAfter when no cursor query param is set, got %q", *gotFilter.CursorAfter)
	}
}

// ---------------------------------------------------------------------------
// next_cursor response wiring
// ---------------------------------------------------------------------------

func TestListConversations_NextCursorPresentWhenHasMore(t *testing.T) {
	last := &agentdom.AgentConversation{ID: uuid.New(), CreatedAt: fixedTime()}
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			return []*agentdom.AgentConversation{{ID: uuid.New(), CreatedAt: fixedTime()}, last}, true, nil
		},
	}
	_, resp := doListConversations(t, svc, uuid.New().String(), "")
	if resp.Data.NextCursor == nil || *resp.Data.NextCursor == "" {
		t.Fatal("expected non-empty next_cursor when hasMore=true")
	}
	wantCursor := agentdom.EncodeConversationCursor(last)
	if *resp.Data.NextCursor != wantCursor {
		t.Errorf("expected next_cursor encoded from the last item, got %q want %q", *resp.Data.NextCursor, wantCursor)
	}
}

func TestListConversations_NextCursorAbsentWhenNoMore(t *testing.T) {
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			return []*agentdom.AgentConversation{{ID: uuid.New(), CreatedAt: fixedTime()}}, false, nil
		},
	}
	_, resp := doListConversations(t, svc, uuid.New().String(), "")
	if resp.Data.NextCursor != nil {
		t.Errorf("expected nil next_cursor when hasMore=false, got %q", *resp.Data.NextCursor)
	}
}

func TestListConversations_NextCursorAbsentWhenEmptyPage(t *testing.T) {
	// hasMore=true with zero items shouldn't happen in practice, but the
	// handler must not panic indexing into an empty slice.
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			return nil, true, nil
		},
	}
	rec, resp := doListConversations(t, svc, uuid.New().String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if resp.Data.NextCursor != nil {
		t.Errorf("expected nil next_cursor for an empty page, got %q", *resp.Data.NextCursor)
	}
}

func TestListConversations_ItemsMarshaledInOrder(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			return []*agentdom.AgentConversation{
				{ID: id1, Status: "running", CreatedAt: fixedTime()},
				{ID: id2, Status: "queued", CreatedAt: fixedTime()},
			}, false, nil
		},
	}
	_, resp := doListConversations(t, svc, uuid.New().String(), "")
	if len(resp.Data.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Data.Items))
	}
	if resp.Data.Items[0]["id"] != id1.String() || resp.Data.Items[1]["id"] != id2.String() {
		t.Errorf("expected items in service-returned order [%s, %s], got %v", id1, id2, resp.Data.Items)
	}
}

// ---------------------------------------------------------------------------
// Error propagation
// ---------------------------------------------------------------------------

func TestListConversations_InvalidCursorReturnsBadRequest(t *testing.T) {
	svc := &mockAgentSvc{
		listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			return nil, false, agentdom.ErrConversationInvalidCursor
		},
	}
	rec, resp := doListConversations(t, svc, uuid.New().String(), "cursor=not-a-valid-cursor")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid cursor, got %d", rec.Code)
	}
	if resp.Success {
		t.Error("expected success=false in error envelope")
	}
	if resp.ErrorCode == "" {
		t.Error("expected non-empty error_code in error envelope")
	}
}

func TestListConversations_InvalidProjectIDReturnsBadRequest(t *testing.T) {
	svc := &mockAgentSvc{}
	rec, _ := doListConversations(t, svc, "not-a-uuid", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed project id, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// ListGlobalConversations (GET /agents/conversations)
// ---------------------------------------------------------------------------

func newGlobalConversationsListRouter(svc agentdom.Service, subject string) chi.Router {
	h := handler.NewConversationHandler(svc)
	r := chi.NewRouter()
	r.Use(claimsMiddleware(subject))
	r.Get("/agents/conversations", h.ListGlobalConversations)
	return r
}

// The caller's own user id must always be forced as ActorUserID, and
// GlobalOnly/ProjectID must never be left for the client to override via
// query params — this is the privacy boundary that keeps one user's global
// chats from leaking to another.
func TestListGlobalConversations_ScopesToCallerAndGlobalOnly(t *testing.T) {
	callerID := uuid.New()
	var gotActorUserID uuid.UUID
	var gotFilter agentdom.ListConversationsFilter
	svc := &mockAgentSvc{
		listGlobalConversations: func(_ context.Context, actorUserID uuid.UUID, filter agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			gotActorUserID = actorUserID
			gotFilter = filter
			return []*agentdom.AgentConversation{{ID: uuid.New()}}, false, nil
		},
	}
	r := newGlobalConversationsListRouter(svc, callerID.String())
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/conversations", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotActorUserID != callerID {
		t.Errorf("expected actorUserID %s forced from claims, got %s", callerID, gotActorUserID)
	}
	// The handler itself never sets GlobalOnly/ProjectID — that's the
	// service's job (see Service.ListGlobalConversations) so there's no
	// query param a client could smuggle those through with.
	if gotFilter.ProjectID != nil {
		t.Errorf("expected handler to leave ProjectID unset, got %v", gotFilter.ProjectID)
	}
}

func TestListGlobalConversations_Unauthenticated401s(t *testing.T) {
	svc := &mockAgentSvc{}
	h := handler.NewConversationHandler(svc)
	r := chi.NewRouter()
	r.Get("/agents/conversations", h.ListGlobalConversations)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/conversations", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with no claims in context, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GetGlobalConversationEvents (GET /agents/conversations/:conversationId/events)
// ---------------------------------------------------------------------------

func newGlobalConversationRouter(svc agentdom.Service) chi.Router {
	h := handler.NewConversationHandler(svc)
	r := chi.NewRouter()
	r.Route("/agents/conversations/{conversationId}", func(r chi.Router) {
		r.Get("/events", h.GetGlobalConversationEvents)
	})
	return r
}

func TestGetGlobalConversationEvents_ReturnsEvents(t *testing.T) {
	convID := uuid.New()
	svc := &mockAgentSvc{
		getGlobalConversation: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id != convID {
				t.Fatalf("unexpected conversation id %s", id)
			}
			return &agentdom.AgentConversation{ID: convID}, nil
		},
		listConversationEvents: func(_ context.Context, id uuid.UUID, _, _ int) ([]*agentdom.AgentConversationEvent, int64, error) {
			if id != convID {
				t.Fatalf("unexpected conversation id %s", id)
			}
			return []*agentdom.AgentConversationEvent{{ID: uuid.New(), ConversationID: convID}}, 1, nil
		},
	}
	r := newGlobalConversationRouter(svc)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/conversations/"+convID.String()+"/events", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data.Items) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resp.Data.Items))
	}
}

// A conversation id that doesn't resolve to a global (project_id IS NULL)
// conversation must 404 via GetGlobalConversation's own scope check —
// ListConversationEvents must never be reached in that case, since it has
// no ownership check of its own to fall back on.
func TestGetGlobalConversationEvents_UnknownConversationNotFound(t *testing.T) {
	eventsCalled := false
	svc := &mockAgentSvc{
		getGlobalConversation: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return nil, agentdom.ErrConversationNotFound
		},
		listConversationEvents: func(_ context.Context, _ uuid.UUID, _, _ int) ([]*agentdom.AgentConversationEvent, int64, error) {
			eventsCalled = true
			return nil, 0, nil
		},
	}
	r := newGlobalConversationRouter(svc)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/conversations/"+uuid.New().String()+"/events", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	if eventsCalled {
		t.Error("ListConversationEvents must not be called when the conversation isn't a valid global conversation")
	}
}
