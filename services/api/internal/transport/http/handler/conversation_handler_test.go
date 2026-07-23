package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
	if gotFilter.AgentID == nil || *gotFilter.AgentID != agentID {
		t.Errorf("expected AgentID filter %v, got %v", agentID, gotFilter.AgentID)
	}
	if gotFilter.Status == nil || *gotFilter.Status != "running" {
		t.Errorf("expected Status filter %q, got %v", "running", gotFilter.Status)
	}
	if gotFilter.CursorAfter == nil || *gotFilter.CursorAfter != "some-opaque-cursor" {
		t.Errorf("expected CursorAfter filter %q, got %v", "some-opaque-cursor", gotFilter.CursorAfter)
	}
}

func TestListConversations_InvalidAgentIDIgnored(t *testing.T) {
	// A malformed agent_id query param should be silently dropped rather than
	// erroring the request — only a valid UUID sets the filter.
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
	if gotFilter.AgentID != nil {
		t.Errorf("expected AgentID filter to remain nil for malformed input, got %v", *gotFilter.AgentID)
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
