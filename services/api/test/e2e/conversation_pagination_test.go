package e2e_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/google/uuid"
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
	conv := &agentdom.AgentConversation{
		ID:                  uuid.New(),
		AgentID:             agentID,
		ProjectID:           uuid.MustParse(projID),
		TriggerType:         "chat_message",
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
// TestE2EListConversationPagination_EmptyProject
// ---------------------------------------------------------------------------

func TestE2EListConversationPagination_EmptyProject(t *testing.T) {
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
