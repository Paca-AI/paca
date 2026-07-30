package e2e_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// seedConversationFixture creates a logged-in project-admin user, a project,
// and an LLM agent in that project, returning everything a test needs to
// seed conversations directly via the repo and list them over HTTP.
func seedConversationFixture(t *testing.T, env *e2eEnv) (client *http.Client, token, projID string, agentID, memberID uuid.UUID) {
	t.Helper()
	username := "conv-pag-user-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "convpagpass1")
	client, token = taskMemberLogin(t, env, username, "convpagpass1")
	// The project creator is granted the project's built-in "Admin" role,
	// which includes agents.read/write — see seedACPUser in
	// acp_agent_management_test.go for the same reasoning.
	projID = createProjectForTasksViaAPI(t, env, client, token)
	editorRoleID := projectRoleIDByName(t, env, client, token, projID, "Editor")

	status, resp := createAgentRequest(t, env, client, token, projID,
		llmAgentBody(editorRoleID, "conv-pag-agent-"+uuid.NewString(), nil))
	if status != http.StatusCreated {
		t.Fatalf("create agent: expected 201, got %d: %+v", status, resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("create agent: expected data object, got %T", resp.Data)
	}
	agentIDStr, _ := data["id"].(string)
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		t.Fatalf("parse created agent id %q: %v", agentIDStr, err)
	}

	user, err := env.userRepo.FindByUsername(env.ctx, username)
	if err != nil {
		t.Fatalf("find seeded user %q: %v", username, err)
	}
	member, err := env.projectRepo.FindMember(env.ctx, uuid.MustParse(projID), user.ID)
	if err != nil {
		t.Fatalf("find project member for user %q: %v", username, err)
	}

	return client, token, projID, agentID, member.ID
}

// createConversationAt inserts a conversation directly via the repository —
// there is no lightweight HTTP path to create one, since a real conversation
// spins up a container through the agent-trigger flow — and returns its ID.
// createdAt is set explicitly so tests can control keyset-pagination order
// deterministically instead of relying on wall-clock timing.
func createConversationAt(t *testing.T, env *e2eEnv, projID string, agentID, memberID uuid.UUID, status string, createdAt time.Time) string {
	t.Helper()
	return createConversationWithTriggerAt(t, env, projID, agentID, memberID, status, "chat_message", createdAt)
}

// createConversationWithTriggerAt is createConversationAt with an explicit
// trigger_type, for tests exercising the trigger_type ("type") filter.
func createConversationWithTriggerAt(t *testing.T, env *e2eEnv, projID string, agentID, memberID uuid.UUID, status, triggerType string, createdAt time.Time) string {
	t.Helper()
	conv := &agentdom.AgentConversation{
		ID:                  uuid.New(),
		AgentID:             agentID,
		ProjectID:           uuid.MustParse(projID),
		TriggerType:         triggerType,
		TriggeredByMemberID: &memberID,
		Status:              status,
		CreatedAt:           createdAt,
		UpdatedAt:           createdAt,
	}
	if err := env.agentRepo.CreateConversation(env.ctx, conv); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	return conv.ID.String()
}

// listConversationsPage issues GET /projects/:id/conversations with the given
// query params and asserts a 200 response, returning the decoded data map.
func listConversationsPage(t *testing.T, env *e2eEnv, client *http.Client, token, projID string, q url.Values) map[string]any {
	t.Helper()
	reqURL := fmt.Sprintf("%s/api/v1/projects/%s/conversations", env.base, projID)
	if len(q) > 0 {
		reqURL += "?" + q.Encode()
	}
	req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	return assertDataMap(t, env2)
}

// ---------------------------------------------------------------------------
// TestE2EListConversationPagination_CursorBased
// ---------------------------------------------------------------------------

func TestE2EListConversationPagination_CursorBased(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, agentID, memberID := seedConversationFixture(t, env)

	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	var allConvIDs []string
	for i := 0; i < 5; i++ {
		id := createConversationAt(t, env, projID, agentID, memberID, "finished", base.Add(time.Duration(i)*time.Minute))
		allConvIDs = append(allConvIDs, id)
	}

	t.Run("first_page_returns_page_size_items_with_next_cursor", func(t *testing.T) {
		data := listConversationsPage(t, env, client, token, projID, url.Values{"page_size": {"3"}})
		items, _ := data["items"].([]any)
		if len(items) != 3 {
			t.Errorf("expected 3 items on first page, got %d", len(items))
		}
		if nextCursorStr(data) == "" {
			t.Error("expected next_cursor to be set when more conversations exist beyond first page")
		}
		if ps, _ := data["page_size"].(float64); ps != 3 {
			t.Errorf("expected page_size=3 echoed back, got %v", ps)
		}
	})

	t.Run("first_page_is_newest_first", func(t *testing.T) {
		data := listConversationsPage(t, env, client, token, projID, url.Values{"page_size": {"3"}})
		ids := itemIDs(data)
		// Conversations are ordered created_at DESC, so the newest 3 (indices
		// 4,3,2 of allConvIDs) must come back first, in that order.
		want := []string{allConvIDs[4], allConvIDs[3], allConvIDs[2]}
		for i, w := range want {
			if ids[i] != w {
				t.Errorf("position %d: expected %s, got %s (full: %v)", i, w, ids[i], ids)
			}
		}
	})

	t.Run("second_page_via_cursor_has_remaining_items_and_no_cursor", func(t *testing.T) {
		firstPage := listConversationsPage(t, env, client, token, projID, url.Values{"page_size": {"3"}})
		cursor := nextCursorStr(firstPage)
		if cursor == "" {
			t.Fatal("expected non-empty next_cursor from first page")
		}

		secondPage := listConversationsPage(t, env, client, token, projID, url.Values{
			"page_size": {"3"},
			"cursor":    {cursor},
		})
		items, _ := secondPage["items"].([]any)
		if len(items) != 2 {
			t.Errorf("expected 2 remaining items on second page (5 total, 3 on first), got %d", len(items))
		}
		if nextCursorStr(secondPage) != "" {
			t.Error("expected next_cursor to be absent on the last page")
		}
		ids := itemIDs(secondPage)
		want := []string{allConvIDs[1], allConvIDs[0]}
		for i, w := range want {
			if ids[i] != w {
				t.Errorf("second page position %d: expected %s, got %s (full: %v)", i, w, ids[i], ids)
			}
		}
	})

	t.Run("no_next_cursor_when_all_conversations_fit_in_one_page", func(t *testing.T) {
		data := listConversationsPage(t, env, client, token, projID, url.Values{"page_size": {"10"}})
		items, _ := data["items"].([]any)
		if len(items) != 5 {
			t.Errorf("expected 5 items when page_size exceeds conversation count, got %d", len(items))
		}
		if nextCursorStr(data) != "" {
			t.Error("expected no next_cursor when all conversations fit in one page")
		}
	})

	t.Run("full_traversal_returns_all_conversations_without_duplicates", func(t *testing.T) {
		seen := make(map[string]int)
		cursor := ""
		for {
			q := url.Values{"page_size": {"2"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			data := listConversationsPage(t, env, client, token, projID, q)
			for _, id := range itemIDs(data) {
				seen[id]++
			}
			cursor = nextCursorStr(data)
			if cursor == "" {
				break
			}
		}
		if len(seen) != 5 {
			t.Errorf("expected 5 unique conversations after full traversal, got %d", len(seen))
		}
		for _, id := range allConvIDs {
			if seen[id] == 0 {
				t.Errorf("conversation %q was not returned during full traversal", id)
			}
			if seen[id] > 1 {
				t.Errorf("conversation %q was returned %d times (duplicate)", id, seen[id])
			}
		}
	})

	t.Run("page_size_zero_is_rejected", func(t *testing.T) {
		q := url.Values{"page_size": {"0"}}
		reqURL := fmt.Sprintf("%s/api/v1/projects/%s/conversations?%s", env.base, projID, q.Encode())
		req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "BAD_REQUEST")
	})

	t.Run("page_size_over_max_is_rejected", func(t *testing.T) {
		q := url.Values{"page_size": {"201"}}
		reqURL := fmt.Sprintf("%s/api/v1/projects/%s/conversations?%s", env.base, projID, q.Encode())
		req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "BAD_REQUEST")
	})

	t.Run("invalid_cursor_returns_error", func(t *testing.T) {
		q := url.Values{"cursor": {"not-a-valid-base64-cursor!!"}}
		reqURL := fmt.Sprintf("%s/api/v1/projects/%s/conversations?%s", env.base, projID, q.Encode())
		req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("expected a non-200 error response for invalid cursor, got 200")
		}
		assertErrorCode(t, resp, "AGENT_CONVERSATION_INVALID_CURSOR")
	})
}

// ---------------------------------------------------------------------------
// TestE2EListConversationPagination_StatusFilter
// Verifies cursor pagination works correctly when combined with the status
// filter — only conversations matching the requested status are paginated.
// ---------------------------------------------------------------------------

func TestE2EListConversationPagination_StatusFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, agentID, memberID := seedConversationFixture(t, env)

	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	var runningIDs []string
	for i := 0; i < 4; i++ {
		id := createConversationAt(t, env, projID, agentID, memberID, "running", base.Add(time.Duration(i)*time.Minute))
		runningIDs = append(runningIDs, id)
	}
	// Noise: conversations in a different status must not appear in filtered results.
	for i := 0; i < 2; i++ {
		createConversationAt(t, env, projID, agentID, memberID, "finished", base.Add(time.Duration(10+i)*time.Minute))
	}

	t.Run("first_page_has_cursor_when_more_exist", func(t *testing.T) {
		data := listConversationsPage(t, env, client, token, projID, url.Values{
			"status":    {"running"},
			"page_size": {"2"},
		})
		items, _ := data["items"].([]any)
		if len(items) != 2 {
			t.Errorf("expected 2 running conversations on first page, got %d", len(items))
		}
		if nextCursorStr(data) == "" {
			t.Error("expected next_cursor when more running conversations exist beyond first page")
		}
	})

	t.Run("full_traversal_returns_only_matching_status_without_duplicates", func(t *testing.T) {
		seen := make(map[string]int)
		cursor := ""
		for {
			q := url.Values{"status": {"running"}, "page_size": {"2"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			data := listConversationsPage(t, env, client, token, projID, q)
			for _, id := range itemIDs(data) {
				seen[id]++
			}
			cursor = nextCursorStr(data)
			if cursor == "" {
				break
			}
		}
		if len(seen) != 4 {
			t.Errorf("expected 4 running conversations from full traversal, got %d", len(seen))
		}
		for _, id := range runningIDs {
			if seen[id] != 1 {
				t.Errorf("running conversation %q appeared %d times (expected exactly once)", id, seen[id])
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestE2EListConversationPagination_AgentIDFilter
// Verifies cursor pagination works correctly when combined with the
// agent_id filter — only conversations from the requested agent appear.
// ---------------------------------------------------------------------------

func TestE2EListConversationPagination_AgentIDFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, agentA, memberID := seedConversationFixture(t, env)

	editorRoleID := projectRoleIDByName(t, env, client, token, projID, "Editor")
	status, resp := createAgentRequest(t, env, client, token, projID,
		llmAgentBody(editorRoleID, "conv-pag-agent-b-"+uuid.NewString(), nil))
	if status != http.StatusCreated {
		t.Fatalf("create second agent: expected 201, got %d: %+v", status, resp)
	}
	data, _ := resp.Data.(map[string]any)
	agentBIDStr, _ := data["id"].(string)
	agentB, err := uuid.Parse(agentBIDStr)
	if err != nil {
		t.Fatalf("parse second agent id: %v", err)
	}

	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	var agentAConvIDs []string
	for i := 0; i < 3; i++ {
		id := createConversationAt(t, env, projID, agentA, memberID, "finished", base.Add(time.Duration(i)*time.Minute))
		agentAConvIDs = append(agentAConvIDs, id)
	}
	for i := 0; i < 3; i++ {
		createConversationAt(t, env, projID, agentB, memberID, "finished", base.Add(time.Duration(10+i)*time.Minute))
	}

	data2 := listConversationsPage(t, env, client, token, projID, url.Values{
		"agent_id":  {agentA.String()},
		"page_size": {"10"},
	})
	ids := itemIDs(data2)
	if len(ids) != 3 {
		t.Fatalf("expected 3 conversations for agent A, got %d: %v", len(ids), ids)
	}
	for _, id := range agentAConvIDs {
		found := false
		for _, got := range ids {
			if got == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected agent A conversation %q in filtered results", id)
		}
	}
}

// ---------------------------------------------------------------------------
// TestE2EListConversationPagination_TriggerTypeFilter
// Verifies cursor pagination works correctly when combined with the
// trigger_type ("type") filter.
// ---------------------------------------------------------------------------

func TestE2EListConversationPagination_TriggerTypeFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, agentID, memberID := seedConversationFixture(t, env)

	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	var taskAssignedIDs []string
	for i := 0; i < 4; i++ {
		id := createConversationWithTriggerAt(t, env, projID, agentID, memberID, "finished", "task_assigned", base.Add(time.Duration(i)*time.Minute))
		taskAssignedIDs = append(taskAssignedIDs, id)
	}
	// Noise: conversations of a different type must not appear in filtered results.
	for i := 0; i < 2; i++ {
		createConversationWithTriggerAt(t, env, projID, agentID, memberID, "finished", "chat_message", base.Add(time.Duration(10+i)*time.Minute))
	}

	t.Run("first_page_has_cursor_when_more_exist", func(t *testing.T) {
		data := listConversationsPage(t, env, client, token, projID, url.Values{
			"trigger_type": {"task_assigned"},
			"page_size":    {"2"},
		})
		items, _ := data["items"].([]any)
		if len(items) != 2 {
			t.Errorf("expected 2 task_assigned conversations on first page, got %d", len(items))
		}
		if nextCursorStr(data) == "" {
			t.Error("expected next_cursor when more task_assigned conversations exist beyond first page")
		}
	})

	t.Run("full_traversal_returns_only_matching_type_without_duplicates", func(t *testing.T) {
		seen := make(map[string]int)
		cursor := ""
		for {
			q := url.Values{"trigger_type": {"task_assigned"}, "page_size": {"2"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			data := listConversationsPage(t, env, client, token, projID, q)
			for _, id := range itemIDs(data) {
				seen[id]++
			}
			cursor = nextCursorStr(data)
			if cursor == "" {
				break
			}
		}
		if len(seen) != 4 {
			t.Errorf("expected 4 task_assigned conversations from full traversal, got %d", len(seen))
		}
		for _, id := range taskAssignedIDs {
			if seen[id] != 1 {
				t.Errorf("task_assigned conversation %q appeared %d times (expected exactly once)", id, seen[id])
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestE2EListConversationPagination_MultiValueFilters
// Verifies agent_id and status accept comma-separated lists, combined via IN
// clauses (agent IN (...) AND status IN (...)), and still page correctly.
// ---------------------------------------------------------------------------

func TestE2EListConversationPagination_MultiValueFilters(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, agentA, memberID := seedConversationFixture(t, env)

	editorRoleID := projectRoleIDByName(t, env, client, token, projID, "Editor")
	status, resp := createAgentRequest(t, env, client, token, projID,
		llmAgentBody(editorRoleID, "conv-pag-agent-c-"+uuid.NewString(), nil))
	if status != http.StatusCreated {
		t.Fatalf("create second agent: expected 201, got %d: %+v", status, resp)
	}
	data, _ := resp.Data.(map[string]any)
	agentBIDStr, _ := data["id"].(string)
	agentB, err := uuid.Parse(agentBIDStr)
	if err != nil {
		t.Fatalf("parse second agent id: %v", err)
	}

	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	var wantIDs []string
	wantIDs = append(wantIDs, createConversationAt(t, env, projID, agentA, memberID, "running", base))
	wantIDs = append(wantIDs, createConversationAt(t, env, projID, agentB, memberID, "paused", base.Add(time.Minute)))
	// Noise: right agents wrong status, right status wrong agent, and a third agent entirely.
	createConversationAt(t, env, projID, agentA, memberID, "finished", base.Add(2*time.Minute))
	createConversationAt(t, env, projID, agentB, memberID, "finished", base.Add(3*time.Minute))

	q := url.Values{
		"agent_id":  {agentA.String() + "," + agentB.String()},
		"status":    {"running,paused"},
		"page_size": {"10"},
	}
	got := itemIDs(listConversationsPage(t, env, client, token, projID, q))
	if len(got) != 2 {
		t.Fatalf("expected 2 conversations matching agent IN (A,B) AND status IN (running,paused), got %d: %v", len(got), got)
	}
	for _, id := range wantIDs {
		if !slices.Contains(got, id) {
			t.Errorf("expected conversation %q in multi-value filtered results, got %v", id, got)
		}
	}
}

// ---------------------------------------------------------------------------
// TestE2EListConversationPagination_DateRangeFilter
// Verifies created_after/created_before filter conversations by created_at,
// with an inclusive whole-day upper bound (created_before=2026-03-15 must
// include a conversation created at 23:59:59 that day but exclude one created
// at 00:00:00 the next day) — see the ::date + INTERVAL '1 day' comparison in
// ListConversations.
// ---------------------------------------------------------------------------

func TestE2EListConversationPagination_DateRangeFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, agentID, memberID := seedConversationFixture(t, env)

	tooEarly := createConversationAt(t, env, projID, agentID, memberID, "finished", time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC))
	inRangeStart := createConversationAt(t, env, projID, agentID, memberID, "finished", time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC))
	inRangeEnd := createConversationAt(t, env, projID, agentID, memberID, "finished", time.Date(2026, 3, 15, 23, 59, 59, 0, time.UTC))
	tooLate := createConversationAt(t, env, projID, agentID, memberID, "finished", time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC))

	got := itemIDs(listConversationsPage(t, env, client, token, projID, url.Values{
		"created_after":  {"2026-03-11"},
		"created_before": {"2026-03-15"},
		"page_size":      {"10"},
	}))

	if !slices.Contains(got, inRangeStart) || !slices.Contains(got, inRangeEnd) {
		t.Errorf("expected both boundary-inclusive conversations in range, got %v", got)
	}
	if slices.Contains(got, tooEarly) {
		t.Errorf("expected conversation before created_after to be excluded, got %v", got)
	}
	if slices.Contains(got, tooLate) {
		t.Errorf("expected conversation on the day after created_before to be excluded (whole-day inclusive upper bound), got %v", got)
	}
}

// TestE2EListConversationPagination_DateRangeFilterRFC3339 verifies that a
// full RFC3339 timestamp (as the frontend sends, computed from the user's
// local calendar day) is used as the exact instant boundary rather than
// being reinterpreted as a UTC calendar day — see parseCreatedAfterBound/
// parseCreatedBeforeBound in the handler.
func TestE2EListConversationPagination_DateRangeFilterRFC3339(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, agentID, memberID := seedConversationFixture(t, env)

	before := createConversationAt(t, env, projID, agentID, memberID, "finished", time.Date(2026, 3, 15, 7, 59, 59, 0, time.UTC))
	atBound := createConversationAt(t, env, projID, agentID, memberID, "finished", time.Date(2026, 3, 15, 8, 0, 0, 0, time.UTC))
	after := createConversationAt(t, env, projID, agentID, memberID, "finished", time.Date(2026, 3, 15, 8, 0, 1, 0, time.UTC))

	got := itemIDs(listConversationsPage(t, env, client, token, projID, url.Values{
		"created_after": {"2026-03-15T08:00:00Z"},
		"page_size":     {"10"},
	}))

	if !slices.Contains(got, atBound) || !slices.Contains(got, after) {
		t.Errorf("expected conversations at/after the exact instant boundary, got %v", got)
	}
	if slices.Contains(got, before) {
		t.Errorf("expected conversation just before the exact instant boundary to be excluded, got %v", got)
	}
}

func TestE2EListConversations_InvalidDateFilterRejected(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, _, _ := seedConversationFixture(t, env)

	reqURL := fmt.Sprintf("%s/api/v1/projects/%s/conversations?created_after=not-a-date", env.base, projID)
	req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusBadRequest)
	assertErrorCode(t, resp, "BAD_REQUEST")
}

// ---------------------------------------------------------------------------
// TestE2EListConversationPagination_SearchFilter
// Verifies the search filter matches conversations by text extracted from
// their events' JSONB payloads, and that it composes correctly with cursor
// pagination (a search matching more than one page of conversations still
// traverses cleanly with no duplicates/gaps).
// ---------------------------------------------------------------------------

func TestE2EListConversationPagination_SearchFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, agentID, memberID := seedConversationFixture(t, env)

	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

	var loginBugIDs []string
	for i := 0; i < 3; i++ {
		id := createConversationAt(t, env, projID, agentID, memberID, "finished", base.Add(time.Duration(i)*time.Minute))
		createConversationEventWithPayload(t, env, id, 0, "MessageEvent", map[string]any{
			"content": "please investigate the login bug on the auth page",
		})
		loginBugIDs = append(loginBugIDs, id)
	}

	// Noise: an unrelated conversation whose event text shouldn't match, and
	// one with no events at all (must not false-positive via a NULL EXISTS).
	unrelatedID := createConversationAt(t, env, projID, agentID, memberID, "finished", base.Add(10*time.Minute))
	createConversationEventWithPayload(t, env, unrelatedID, 0, "MessageEvent", map[string]any{
		"content": "please update the dependency versions in package.json",
	})
	createConversationAt(t, env, projID, agentID, memberID, "finished", base.Add(11*time.Minute))

	t.Run("matches_conversations_by_event_text", func(t *testing.T) {
		got := itemIDs(listConversationsPage(t, env, client, token, projID, url.Values{
			"search":    {"login bug"},
			"page_size": {"10"},
		}))
		if len(got) != 3 {
			t.Fatalf("expected 3 conversations matching search, got %d: %v", len(got), got)
		}
		for _, id := range loginBugIDs {
			if !slices.Contains(got, id) {
				t.Errorf("expected conversation %q in search results", id)
			}
		}
		if slices.Contains(got, unrelatedID) {
			t.Errorf("expected unrelated conversation to be excluded from search results")
		}
	})

	t.Run("full_traversal_with_search_active_has_no_duplicates", func(t *testing.T) {
		seen := make(map[string]int)
		cursor := ""
		for {
			q := url.Values{"search": {"login"}, "page_size": {"2"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			data := listConversationsPage(t, env, client, token, projID, q)
			for _, id := range itemIDs(data) {
				seen[id]++
			}
			cursor = nextCursorStr(data)
			if cursor == "" {
				break
			}
		}
		if len(seen) != 3 {
			t.Errorf("expected 3 unique conversations from full traversal with search active, got %d", len(seen))
		}
		for _, id := range loginBugIDs {
			if seen[id] != 1 {
				t.Errorf("conversation %q appeared %d times during search traversal (expected exactly once)", id, seen[id])
			}
		}
	})

	t.Run("no_matches_returns_empty_page_and_no_cursor", func(t *testing.T) {
		data := listConversationsPage(t, env, client, token, projID, url.Values{"search": {"nonexistent-term-xyz"}})
		items, _ := data["items"].([]any)
		if len(items) != 0 {
			t.Errorf("expected 0 items for a search term with no matches, got %d", len(items))
		}
		if nextCursorStr(data) != "" {
			t.Error("expected no next_cursor when search matches nothing")
		}
	})
}

// ---------------------------------------------------------------------------
// TestE2EListConversationEvents_OffsetLimitValidation
// Mirrors the page_size validation coverage above, applied to this sibling
// endpoint's offset/limit pair: an explicitly supplied out-of-range or
// non-numeric value must be rejected rather than silently substituted, for
// the same reason (a caller advancing offset by its requested limit would
// otherwise skip or duplicate rows against its own math with no signal).
// The conversation ID need not exist — invalid offset/limit is rejected
// before the service layer is ever reached.
// ---------------------------------------------------------------------------

func TestE2EListConversationEvents_OffsetLimitValidation(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, _, _ := seedConversationFixture(t, env)
	convID := uuid.NewString()

	eventsURL := func(q url.Values) string {
		u := fmt.Sprintf("%s/api/v1/projects/%s/conversations/%s/events", env.base, projID, convID)
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		return u
	}

	t.Run("absent_offset_and_limit_default", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, eventsURL(nil), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	invalidCases := []url.Values{
		{"offset": {"-1"}},
		{"offset": {"abc"}},
		{"limit": {"0"}},
		{"limit": {"-5"}},
		{"limit": {"201"}},
		{"limit": {"abc"}},
	}
	for _, q := range invalidCases {
		t.Run(q.Encode(), func(t *testing.T) {
			req := mustRequest(env.ctx, t, http.MethodGet, eventsURL(q), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp := mustDo(t, client, req)
			defer func() { _ = resp.Body.Close() }()
			assertStatus(t, resp, http.StatusBadRequest)
			assertErrorCode(t, resp, "BAD_REQUEST")
		})
	}
}

// ---------------------------------------------------------------------------
// TestE2EListConversationPagination_EmptyProject
// ---------------------------------------------------------------------------

func TestE2EListConversationPagination_EmptyProject(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "conv-empty-user-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "convemptypass1")
	client, token := taskMemberLogin(t, env, username, "convemptypass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	data := listConversationsPage(t, env, client, token, projID, nil)
	items, _ := data["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items for a project with no conversations, got %d", len(items))
	}
	if nextCursorStr(data) != "" {
		t.Error("expected no next_cursor for an empty result set")
	}
}

// ---------------------------------------------------------------------------
// TestE2EConversationIterationCount
// ---------------------------------------------------------------------------

// createConversationEvent inserts a conversation event directly via the
// repository, mirroring how the ai-agent service persists SDK events.
func createConversationEvent(t *testing.T, env *e2eEnv, convID string, index int, eventType string) {
	t.Helper()
	createConversationEventWithPayload(t, env, convID, index, eventType, map[string]any{})
}

// createConversationEventWithPayload is createConversationEvent with an
// explicit payload, for tests exercising the search filter (which matches
// against text extracted from payload — see agent_conversation_event_search_text
// in migration 000028).
func createConversationEventWithPayload(t *testing.T, env *e2eEnv, convID string, index int, eventType string, payload map[string]any) {
	t.Helper()
	e := &agentdom.AgentConversationEvent{
		ID:             uuid.New(),
		ConversationID: uuid.MustParse(convID),
		EventIndex:     index,
		EventType:      eventType,
		EventSource:    "agent",
		Payload:        payload,
	}
	if err := env.agentRepo.CreateConversationEvent(env.ctx, e); err != nil {
		t.Fatalf("create conversation event: %v", err)
	}
}

// Regression coverage for https://github.com/Paca-AI/paca/issues/314 — see
// conversationCols in agent_repository.go for the fix. Asserts the list and
// single-conversation endpoints, plus the two other conversationCols call
// sites, all reflect the live ActionEvent count rather than a stale stored
// counter.
func TestE2EConversationIterationCount(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID, agentID, memberID := seedConversationFixture(t, env)

	convID := createConversationAt(t, env, projID, agentID, memberID, "finished", time.Now())
	createConversationEvent(t, env, convID, 0, "SystemPromptEvent")
	createConversationEvent(t, env, convID, 1, "ActionEvent")
	createConversationEvent(t, env, convID, 2, "ObservationEvent")
	createConversationEvent(t, env, convID, 3, "ActionEvent")
	createConversationEvent(t, env, convID, 4, "ActionEvent")

	t.Run("list_endpoint_reflects_action_event_count", func(t *testing.T) {
		data := listConversationsPage(t, env, client, token, projID, nil)
		items, _ := data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected 1 conversation, got %d", len(items))
		}
		item, _ := items[0].(map[string]any)
		if got := item["iteration_count"]; got != float64(3) {
			t.Errorf("expected iteration_count=3, got %v", got)
		}
	})

	t.Run("get_endpoint_reflects_action_event_count", func(t *testing.T) {
		reqURL := fmt.Sprintf("%s/api/v1/projects/%s/conversations/%s", env.base, projID, convID)
		req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var envResp envelope
		decodeJSON(t, resp, &envResp)
		data := assertDataMap(t, envResp)
		if got := data["iteration_count"]; got != float64(3) {
			t.Errorf("expected iteration_count=3, got %v", got)
		}
	})

	// FindLatestConversationByChatSession has no lightweight read-only HTTP
	// endpoint of its own (it backs SendChatMessage's resume path, which
	// needs a running conversation to resume), so it's checked directly
	// against the repository instead — it shares conversationCols with the
	// two HTTP-backed cases above, but exercises the WHERE chat_session_id
	// = ... path rather than WHERE id = ... .
	t.Run("find_latest_by_chat_session_reflects_action_event_count", func(t *testing.T) {
		session := &agentdom.AgentChatSession{
			ID:        uuid.New(),
			AgentID:   agentID,
			ProjectID: uuid.MustParse(projID),
			MemberID:  memberID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := env.agentRepo.CreateChatSession(env.ctx, session); err != nil {
			t.Fatalf("create chat session: %v", err)
		}

		conv := &agentdom.AgentConversation{
			ID:            uuid.New(),
			AgentID:       agentID,
			ProjectID:     uuid.MustParse(projID),
			TriggerType:   "chat_message",
			ChatSessionID: &session.ID,
			Status:        "paused",
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := env.agentRepo.CreateConversation(env.ctx, conv); err != nil {
			t.Fatalf("create conversation: %v", err)
		}
		createConversationEvent(t, env, conv.ID.String(), 0, "ActionEvent")
		createConversationEvent(t, env, conv.ID.String(), 1, "ObservationEvent")
		createConversationEvent(t, env, conv.ID.String(), 2, "ActionEvent")

		got, err := env.agentRepo.FindLatestConversationByChatSession(env.ctx, session.ID)
		if err != nil {
			t.Fatalf("find latest by chat session: %v", err)
		}
		if got == nil {
			t.Fatal("expected a conversation, got nil")
		}
		if got.IterationCount != 2 {
			t.Errorf("expected iteration_count=2, got %d", got.IterationCount)
		}
	})

	t.Run("conversation_with_no_action_events_has_zero_iteration_count", func(t *testing.T) {
		zeroConvID := createConversationAt(t, env, projID, agentID, memberID, "finished", time.Now())
		createConversationEvent(t, env, zeroConvID, 0, "SystemPromptEvent")

		got, err := env.agentRepo.FindConversationByID(env.ctx, uuid.MustParse(zeroConvID))
		if err != nil {
			t.Fatalf("find conversation: %v", err)
		}
		if got.IterationCount != 0 {
			t.Errorf("expected iteration_count=0, got %d", got.IterationCount)
		}
	})
}
