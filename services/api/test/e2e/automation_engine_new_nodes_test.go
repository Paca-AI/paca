package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/api/internal/events"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	notificationsvc "github.com/Paca-AI/api/internal/service/notification"
	"github.com/Paca-AI/api/internal/worker"
)

// End-to-end coverage for the automation engine's second wave of nodes:
// add/remove/clear on list fields, dynamic values, create_task, link_task,
// add_comment, notify, the siblings target, the field_changed / comment /
// link / attachment / child_changed triggers, the child_count condition and
// relative-date conditions. Each test drives the real HTTP API, the real
// worker consumer, Postgres and Valkey — nothing in the engine is faked.

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// nnStartConsumer is startAutomationConsumer plus the notify action's
// notification service (the shared helper predates notify).
func nnStartConsumer(t *testing.T, env *e2eEnv) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	notifier := notificationsvc.New(pgRepo.NewNotificationRepository(env.db), env.projectRepo, nil)
	ac := worker.NewAutomationConsumer(env.redisClient, env.automationRepo, env.taskRepo, env.taskSvc, env.activitySvc, nil, log).
		WithHTTPClient(http.DefaultClient).
		WithNotifier(notifier).
		WithConsumerGroup("e2e." + uuid.NewString())
	ac.Start(env.ctx)
	t.Cleanup(ac.Stop)
}

// nnProject is a signed-in owner with an empty project.
type nnProject struct {
	env      *e2eEnv
	client   *http.Client
	token    string
	id       string
	statuses []map[string]any
}

func nnNewProject(t *testing.T, prefix string) *nnProject {
	t.Helper()
	env := newE2EEnv(t)
	nnStartConsumer(t, env)
	username := prefix + "-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "nnautomationowner1")
	client, token := taskMemberLogin(t, env, username, "nnautomationowner1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	return &nnProject{
		env: env, client: client, token: token, id: projID,
		statuses: listTaskStatusesViaAPI(t, env, client, token, projID),
	}
}

func (p *nnProject) status(name string) string { return statusIDByName(p.statuses, name) }

func (p *nnProject) task(t *testing.T, body map[string]any) string {
	t.Helper()
	return idOf(createTaskViaAPIWithBody(t, p.env, p.client, p.token, p.id, body))
}

func (p *nnProject) get(t *testing.T, taskID string) map[string]any {
	t.Helper()
	return getTaskViaAPI(t, p.env, p.client, p.token, p.id, taskID)
}

func (p *nnProject) setStatus(t *testing.T, taskID, status string) {
	t.Helper()
	setTaskStatusViaAPI(t, p.env, p.client, p.token, p.id, taskID, p.status(status))
}

func (p *nnProject) patch(t *testing.T, taskID string, fields map[string]any) {
	t.Helper()
	patchTaskFieldsViaAPI(t, p.env, p.client, p.token, p.id, taskID, fields)
}

func (p *nnProject) member(t *testing.T) string {
	t.Helper()
	return addProjectMemberWithAutomationPerms(t, p.env, p.client, p.token, p.id,
		"nn-member-"+uuid.NewString(), "nnautomationmember1")
}

func (p *nnProject) children(t *testing.T, parentID string) []map[string]any {
	t.Helper()
	tasks, err := p.env.taskRepo.ListChildTasks(p.env.ctx, uuid.MustParse(p.id), uuid.MustParse(parentID))
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, c := range tasks {
		out = append(out, p.get(t, c.ID.String()))
	}
	return out
}

func (p *nnProject) links(t *testing.T, taskID string) []any {
	t.Helper()
	data := apiJSON(t, p.env, p.client, p.token, http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/links", p.id, taskID), nil, http.StatusOK)
	items, _ := data["items"].([]any)
	return items
}

func (p *nnProject) comment(t *testing.T, taskID, text string) {
	t.Helper()
	body := jsonBody(t, map[string]any{"content": []map[string]any{{
		"type": "paragraph", "content": []map[string]any{{"type": "text", "text": text, "styles": map[string]any{}}},
	}}})
	req := mustRequest(p.env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/activities/comments", p.env.base, p.id, taskID), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp := mustDo(t, p.client, req)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add comment: HTTP %d: %s", resp.StatusCode, raw)
	}
}

// nnNode is one node of a test graph; ref names it in nnEdge.
type nnNode struct {
	ref, kind, typ string
	cfg            map[string]any
}

// nnEdge connects two refs; handle is a condition branch handle ("" = none).
type nnEdge struct{ from, to, handle string }

// build creates the automation, wires the edges and activates it.
func (p *nnProject) build(t *testing.T, name string, nodes []nnNode, edges []nnEdge) string {
	t.Helper()
	autoID := createAutomationViaAPI(t, p.env, p.client, p.token, p.id, name)
	ids := map[string]string{}
	for _, n := range nodes {
		created := addAutomationNodeViaAPI(t, p.env, p.client, p.token, p.id, autoID, n.kind, n.typ, n.cfg)
		ids[n.ref], _ = created["id"].(string)
	}
	for _, e := range edges {
		var handle *string
		if e.handle != "" {
			h := e.handle
			handle = &h
		}
		addAutomationEdgeViaAPIExpect(t, p.env, p.client, p.token, p.id, autoID, ids[e.from], ids[e.to], handle, http.StatusCreated)
	}
	activateAutomationViaAPI(t, p.env, p.client, p.token, p.id, autoID)
	return autoID
}

// wait polls a task until check accepts it.
func (p *nnProject) wait(t *testing.T, taskID string, check func(map[string]any) bool) map[string]any {
	t.Helper()
	return waitForTaskField(t, p.env, p.client, p.token, p.id, taskID, 20*time.Second, check)
}

// stays asserts check holds for the whole of d — used for "the automation must
// NOT have acted", which a single read right after the event cannot prove.
func (p *nnProject) stays(t *testing.T, taskID string, d time.Duration, check func(map[string]any) bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if data := p.get(t, taskID); !check(data) {
			t.Fatalf("%s; task: %v", msg, data)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func nnTags(data map[string]any) []string {
	raw, _ := data["tags"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func nnSameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, g := range got {
		seen[g]++
	}
	for _, w := range want {
		seen[w]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

func nnStrings(data map[string]any, key string) []string {
	raw, _ := data[key].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func nnTime(t *testing.T, data map[string]any, key string) time.Time {
	t.Helper()
	s, _ := data[key].(string)
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("task field %q = %v is not a timestamp", key, data[key])
	}
	return parsed
}

// nnAddTag is an update_task node config that adds one tag.
func nnAddTag(tag string) map[string]any {
	return map[string]any{"update": map[string]any{"tags": []string{tag}, "tags_mode": "add"}}
}

// ---------------------------------------------------------------------------
// update_task: add / remove / clear
// ---------------------------------------------------------------------------

func TestE2EAutomationNew_UpdateTaskTagsAddAndRemoveKeepTheRest(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-tags")
	task := p.task(t, map[string]any{"title": "Tagged", "tags": []string{"keep", "drop"}})

	p.build(t, "Tag edits",
		[]nnNode{
			{"t", "trigger", "status_changed", map[string]any{"status_id": p.status("In Progress")}},
			{"add", "action", "update_task", nnAddTag("added")},
			{"rm", "action", "update_task", map[string]any{"update": map[string]any{"tags": []string{"drop"}, "tags_mode": "remove"}}},
		},
		[]nnEdge{{"t", "add", ""}, {"add", "rm", ""}})

	p.setStatus(t, task, "In Progress")
	// Added without wiping "keep"; "drop" removed without wiping the rest.
	p.wait(t, task, func(d map[string]any) bool { return nnSameSet(nnTags(d), []string{"keep", "added"}) })
}

func TestE2EAutomationNew_UpdateTaskAssigneesAddKeepsExistingAndIsIdempotent(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-assignees")
	first, second := p.member(t), p.member(t)
	task := p.task(t, map[string]any{"title": "Assigned", "assignee_ids": []string{first}})

	autoID := p.build(t, "Add assignee",
		[]nnNode{
			{"t", "trigger", "status_changed", map[string]any{"status_id": p.status("In Progress")}},
			{"a", "action", "update_task", map[string]any{"update": map[string]any{
				"assignee_ids": []string{second}, "assignees_mode": "add",
			}}},
		},
		[]nnEdge{{"t", "a", ""}})

	p.setStatus(t, task, "In Progress")
	p.wait(t, task, func(d map[string]any) bool { return nnSameSet(nnStrings(d, "assignee_ids"), []string{first, second}) })

	// Fire again: the second member is already assigned, so nothing changes.
	p.setStatus(t, task, "Todo")
	p.setStatus(t, task, "In Progress")
	time.Sleep(2 * time.Second)
	if got := nnStrings(p.get(t, task), "assignee_ids"); !nnSameSet(got, []string{first, second}) {
		t.Fatalf("a second fire must not duplicate or drop assignees, got %v", got)
	}
	_ = autoID
}

func TestE2EAutomationNew_UpdateTaskClearEmptiesFields(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-clear")
	member := p.member(t)
	sprint := createSprintViaAPI(t, p.env, p.client, p.token, p.id, "Sprint 1")
	due := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	task := p.task(t, map[string]any{
		"title": "To clear", "assignee_ids": []string{member}, "sprint_id": sprint,
		"due_date": due, "story_points": 5, "tags": []string{"x"},
	})

	p.build(t, "Clear on done",
		[]nnNode{
			{"t", "trigger", "status_changed", map[string]any{"status_id": p.status("Done")}},
			{"a", "action", "update_task", map[string]any{"update": map[string]any{
				"clear": []string{"assignee_ids", "sprint_id", "due_date", "story_points", "tags"},
			}}},
		},
		[]nnEdge{{"t", "a", ""}})

	p.setStatus(t, task, "Done")
	p.wait(t, task, func(d map[string]any) bool {
		return len(nnStrings(d, "assignee_ids")) == 0 && d["sprint_id"] == nil && d["due_date"] == nil &&
			d["story_points"] == nil && len(nnTags(d)) == 0
	})
}

// ---------------------------------------------------------------------------
// Dynamic values
// ---------------------------------------------------------------------------

func TestE2EAutomationNew_UpdateTaskDynamicFromParentAndToday(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-dynamic")
	parentAssignee := p.member(t)
	reporterLikeAssignee := p.member(t)
	// Due dates are stored as dates, so use a whole UTC day.
	parentDue := time.Now().Add(10 * 24 * time.Hour).UTC().Truncate(24 * time.Hour)
	parent := p.task(t, map[string]any{
		"title": "Parent", "assignee_ids": []string{parentAssignee}, "due_date": parentDue.Format(time.RFC3339),
	})
	child := p.task(t, map[string]any{"title": "Child", "parent_task_id": parent, "assignee_ids": []string{reporterLikeAssignee}})

	p.build(t, "Dynamic update",
		[]nnNode{
			{"t", "trigger", "status_changed", map[string]any{"status_id": p.status("In Progress")}},
			{"a", "action", "update_task", map[string]any{"update": map[string]any{"dynamic": map[string]any{
				"assignee_ids": map[string]any{"source": "parent"},
				"due_date":     map[string]any{"source": "parent", "offset_days": -2},
				"start_date":   map[string]any{"source": "now", "offset_days": 1},
			}}}},
		},
		[]nnEdge{{"t", "a", ""}})

	p.setStatus(t, child, "In Progress")
	got := p.wait(t, child, func(d map[string]any) bool {
		return nnSameSet(nnStrings(d, "assignee_ids"), []string{parentAssignee})
	})
	if due := nnTime(t, got, "due_date"); !due.Equal(parentDue.AddDate(0, 0, -2)) {
		t.Fatalf("due date should be the parent's minus two days (%v), got %v", parentDue.AddDate(0, 0, -2), due)
	}
	// Dates are stored as whole UTC days: "today + 1" is tomorrow's date.
	start := nnTime(t, got, "start_date").Format("2006-01-02")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	if start != tomorrow {
		t.Fatalf("start date should be tomorrow (%s), got %s", tomorrow, start)
	}
}

// ---------------------------------------------------------------------------
// create_task
// ---------------------------------------------------------------------------

func TestE2EAutomationNew_CreateTaskSubtaskInheritsAndLinksWithoutRecursing(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-create")
	assignee := p.member(t)
	sprint := createSprintViaAPI(t, p.env, p.client, p.token, p.id, "Sprint 1")
	due := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(24 * time.Hour)

	p.build(t, "QA subtask",
		[]nnNode{
			{"t", "trigger", "task_created", nil},
			{"a", "action", "create_task", map[string]any{"create": map[string]any{
				"title":      "{{task.title}} - QA",
				"importance": 3,
				"tags":       []string{"qa"},
				"dynamic": map[string]any{
					"assignee_ids": map[string]any{"source": "task"},
					"sprint_id":    map[string]any{"source": "task"},
					"due_date":     map[string]any{"source": "task", "offset_days": -1},
				},
				"link": "blocks",
			}}},
		},
		[]nnEdge{{"t", "a", ""}})

	parent := p.task(t, map[string]any{
		"title": "Login page", "assignee_ids": []string{assignee}, "sprint_id": sprint, "due_date": due.Format(time.RFC3339),
	})

	var child map[string]any
	waitFor(t, 20*time.Second, "the subtask to be created", func() bool {
		kids := p.children(t, parent)
		if len(kids) == 0 {
			return false
		}
		child = kids[0]
		return true
	})
	if child["title"] != "Login page - QA" {
		t.Fatalf("title should be templated from the parent, got %v", child["title"])
	}
	if !nnSameSet(nnStrings(child, "assignee_ids"), []string{assignee}) || child["sprint_id"] != sprint {
		t.Fatalf("assignee and sprint should be copied from the parent, got %v", child)
	}
	if got := nnTime(t, child, "due_date"); !got.Equal(due.AddDate(0, 0, -1)) {
		t.Fatalf("due date should be the parent's minus a day, got %v", got)
	}
	if !nnSameSet(nnTags(child), []string{"qa"}) {
		t.Fatalf("static tags should be applied, got %v", nnTags(child))
	}
	if links := p.links(t, parent); len(links) != 1 {
		t.Fatalf("the new subtask should be linked to its parent, got %d links", len(links))
	}

	// The created subtask must not fire task_created again (origin=automation),
	// so there is still exactly one child after the engine has had time to loop.
	time.Sleep(3 * time.Second)
	if kids := p.children(t, parent); len(kids) != 1 {
		t.Fatalf("expected exactly one subtask (no recursion), got %d", len(kids))
	}
}

func TestE2EAutomationNew_CreateTaskIsIdempotentPerParentAndTitle(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-create-idem")
	parent := p.task(t, map[string]any{"title": "Epic work"})

	p.build(t, "Review subtask",
		[]nnNode{
			{"t", "trigger", "status_changed", map[string]any{"status_id": p.status("In Progress")}},
			{"a", "action", "create_task", map[string]any{"create": map[string]any{"title": "Review {{task.title}}"}}},
		},
		[]nnEdge{{"t", "a", ""}})

	p.setStatus(t, parent, "In Progress")
	waitFor(t, 20*time.Second, "the subtask", func() bool { return len(p.children(t, parent)) == 1 })
	p.setStatus(t, parent, "Todo")
	p.setStatus(t, parent, "In Progress")
	time.Sleep(3 * time.Second)
	if kids := p.children(t, parent); len(kids) != 1 {
		t.Fatalf("firing again must not duplicate the subtask, got %d", len(kids))
	}
}

// ---------------------------------------------------------------------------
// siblings target and child_changed / child_count
// ---------------------------------------------------------------------------

func TestE2EAutomationNew_SiblingsAllDoneClosesParent(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-siblings")
	parent := p.task(t, map[string]any{"title": "Parent"})
	c1 := p.task(t, map[string]any{"title": "C1", "parent_task_id": parent})
	c2 := p.task(t, map[string]any{"title": "C2", "parent_task_id": parent})
	done := p.status("Done")

	p.build(t, "Close parent when siblings done",
		[]nnNode{
			{"t", "trigger", "status_changed", map[string]any{"status_id": done}},
			{"c", "condition", "condition", map[string]any{"branches": []map[string]any{{
				"handle": "all_done", "label": "All sibling tasks done",
				"tree": map[string]any{
					"field": "status_id", "operator": "equals", "value": done,
					"target": map[string]any{"kind": "siblings"}, "match_mode": "all",
				},
			}}}},
			{"a", "action", "update_task", map[string]any{
				"update": map[string]any{"status_id": done}, "target": map[string]any{"kind": "parent"},
			}},
		},
		[]nnEdge{{"t", "c", ""}, {"c", "a", "all_done"}})

	p.setStatus(t, c1, "Done")
	p.stays(t, parent, 2500*time.Millisecond, func(d map[string]any) bool { return d["status_id"] != done },
		"the parent must stay open while a sibling is unfinished")

	p.setStatus(t, c2, "Done")
	p.wait(t, parent, func(d map[string]any) bool { return d["status_id"] == done })
}

func TestE2EAutomationNew_ChildChangedTriggerClosesParentWhenAllChildrenDone(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-child-changed")
	parent := p.task(t, map[string]any{"title": "Parent"})
	c1 := p.task(t, map[string]any{"title": "C1", "parent_task_id": parent})
	c2 := p.task(t, map[string]any{"title": "C2", "parent_task_id": parent})
	done := p.status("Done")

	p.build(t, "Parent follows children",
		[]nnNode{
			// Runs on the PARENT whenever a subtask enters Done.
			{"t", "trigger", "child_changed", map[string]any{"status_id": done}},
			{"c", "condition", "condition", map[string]any{"branches": []map[string]any{{
				"handle": "all_done",
				"tree": map[string]any{
					"field": "status_id", "operator": "equals", "value": done,
					"target": map[string]any{"kind": "children"}, "match_mode": "all",
				},
			}}}},
			{"a", "action", "update_task", map[string]any{"update": map[string]any{"status_id": done}}},
		},
		[]nnEdge{{"t", "c", ""}, {"c", "a", "all_done"}})

	p.setStatus(t, c1, "Done")
	p.stays(t, parent, 2500*time.Millisecond, func(d map[string]any) bool { return d["status_id"] != done },
		"the parent must stay open while a child is unfinished")
	p.setStatus(t, c2, "Done")
	p.wait(t, parent, func(d map[string]any) bool { return d["status_id"] == done })
}

func TestE2EAutomationNew_ChildChangedOnCreateWithChildCountCondition(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-child-count")
	parent := p.task(t, map[string]any{"title": "Growing parent"})

	p.build(t, "Busy parent",
		[]nnNode{
			{"t", "trigger", "child_changed", nil},
			{"c", "condition", "condition", map[string]any{"branches": []map[string]any{{
				"handle": "many", "tree": map[string]any{"field": "child_count", "operator": "greater_than", "value": 1},
			}}}},
			{"a", "action", "update_task", nnAddTag("busy")},
		},
		[]nnEdge{{"t", "c", ""}, {"c", "a", "many"}})

	p.task(t, map[string]any{"title": "Only child", "parent_task_id": parent})
	p.stays(t, parent, 2500*time.Millisecond, func(d map[string]any) bool { return len(nnTags(d)) == 0 },
		"one subtask is not more than one")
	p.task(t, map[string]any{"title": "Second child", "parent_task_id": parent})
	p.wait(t, parent, func(d map[string]any) bool { return nnSameSet(nnTags(d), []string{"busy"}) })
}

// ---------------------------------------------------------------------------
// field_changed, comment_added, link_added, attachment_added
// ---------------------------------------------------------------------------

func TestE2EAutomationNew_FieldChangedTriggerFiresOnlyForTheWatchedField(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-field-changed")
	sprint := createSprintViaAPI(t, p.env, p.client, p.token, p.id, "Sprint 1")
	task := p.task(t, map[string]any{"title": "Watched"})

	p.build(t, "Sprint moved",
		[]nnNode{
			{"t", "trigger", "field_changed", map[string]any{"field": "sprint"}},
			{"a", "action", "update_task", nnAddTag("sprint-moved")},
		},
		[]nnEdge{{"t", "a", ""}})

	p.patch(t, task, map[string]any{"title": "Watched (renamed)"})
	p.stays(t, task, 2*time.Second, func(d map[string]any) bool { return len(nnTags(d)) == 0 },
		"renaming must not fire a sprint trigger")

	p.patch(t, task, map[string]any{"sprint_id": sprint})
	p.wait(t, task, func(d map[string]any) bool { return nnSameSet(nnTags(d), []string{"sprint-moved"}) })
}

func TestE2EAutomationNew_CommentAddedTriggerRunsAddCommentWithoutLooping(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-comment")
	task := p.task(t, map[string]any{"title": "Discussed"})

	p.build(t, "Reply to comments",
		[]nnNode{
			{"t", "trigger", "comment_added", nil},
			{"a", "action", "add_comment", map[string]any{"comment": map[string]any{"text": "Seen on {{task.title}}\nThanks!"}}},
		},
		[]nnEdge{{"t", "a", ""}})

	p.comment(t, task, "Hello there")

	automationComment := func(payload map[string]any) bool {
		if payload["activity_type"] != "comment" {
			return false
		}
		content, _ := payload["content"].(string)
		return strings.Contains(content, "Seen on Discussed")
	}
	payload := waitForStreamActivity(t, p.env, task, 20*time.Second, automationComment)
	var blocks []map[string]any
	if err := json.Unmarshal([]byte(payload["content"].(string)), &blocks); err != nil || len(blocks) != 2 {
		t.Fatalf("the comment should be two paragraphs, got %v (%v)", payload["content"], err)
	}
	if payload["actor_id"] != nil {
		t.Fatalf("an automation comment has no human author, got actor %v", payload["actor_id"])
	}

	// The automation's own comment must not trigger it again.
	time.Sleep(3 * time.Second)
	count := 0
	msgs, err := p.env.redisClient.XRevRangeN(p.env.ctx, events.StreamActivities, "+", "-", 2000).Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		raw, _ := m.Values["payload"].(string)
		var pl map[string]any
		if json.Unmarshal([]byte(raw), &pl) == nil && pl["task_id"] == task && automationComment(pl) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one automation comment, got %d", count)
	}
}

func TestE2EAutomationNew_LinkAddedTriggerAndLinkTaskAction(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-link")
	blocker := p.task(t, map[string]any{"title": "Blocker"})
	blocked := p.task(t, map[string]any{"title": "Blocked"})
	watched := p.task(t, map[string]any{"title": "Watched for links"})

	// link_task: moving "Blocker" to In Progress makes it block "Blocked".
	p.build(t, "Create link",
		[]nnNode{
			{"t", "trigger", "status_changed", map[string]any{"status_id": p.status("In Progress")}},
			{"a", "action", "link_task", map[string]any{"link": map[string]any{
				"type": "blocks", "to": map[string]any{"kind": "other", "other_task_id": blocked},
			}}},
		},
		[]nnEdge{{"t", "a", ""}})
	// link_added: any task getting a link is tagged.
	p.build(t, "Tag linked tasks",
		[]nnNode{
			{"t", "trigger", "link_added", nil},
			{"a", "action", "update_task", nnAddTag("linked")},
		},
		[]nnEdge{{"t", "a", ""}})

	p.setStatus(t, blocker, "In Progress")
	waitFor(t, 20*time.Second, "the blocks link", func() bool { return len(p.links(t, blocker)) == 1 })
	if links := p.links(t, blocked); len(links) != 1 {
		t.Fatalf("the link shows on both tasks, got %d on the blocked one", len(links))
	}
	// The automation-made link carries origin=automation, so it must not fire
	// the link_added automation.
	p.stays(t, blocker, 2*time.Second, func(d map[string]any) bool { return len(nnTags(d)) == 0 },
		"a link created by an automation must not fire link_added")

	// A person adding a link does.
	apiJSON(t, p.env, p.client, p.token, http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/links", p.id, watched),
		map[string]any{"target_task_id": blocked, "link_type": "relates_to"}, http.StatusCreated)
	p.wait(t, watched, func(d map[string]any) bool { return nnSameSet(nnTags(d), []string{"linked"}) })

	// Re-running link_task is a no-op (the link already exists).
	p.setStatus(t, blocker, "Todo")
	p.setStatus(t, blocker, "In Progress")
	time.Sleep(2 * time.Second)
	if links := p.links(t, blocker); len(links) != 1 {
		t.Fatalf("an existing link must not be duplicated, got %d", len(links))
	}
}

func TestE2EAutomationNew_AttachmentAddedTriggerFires(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-attachment")
	task := p.task(t, map[string]any{"title": "With files"})

	p.build(t, "Tag attached",
		[]nnNode{
			{"t", "trigger", "attachment_added", nil},
			{"a", "action", "update_task", nnAddTag("has-file")},
		},
		[]nnEdge{{"t", "a", ""}})

	// Uploading needs object storage, which this harness does not run, so put
	// the event the attachment endpoint records onto the activity stream — the
	// engine's input is that stream entry, whoever wrote it.
	payload, _ := json.Marshal(map[string]any{
		"id": uuid.NewString(), "task_id": task, "project_id": p.id,
		"activity_type": "task.attachment.added", "content": "{}",
	})
	if err := p.env.redisClient.XAdd(p.env.ctx, &redis.XAddArgs{
		Stream: events.StreamActivities,
		Values: map[string]any{
			"type": "task.attachment.added", "payload": string(payload), "project_id": p.id,
			"origin": "user", "entity_type": "task", "task_id": task,
		},
	}).Err(); err != nil {
		t.Fatalf("publish attachment event: %v", err)
	}
	p.wait(t, task, func(d map[string]any) bool { return nnSameSet(nnTags(d), []string{"has-file"}) })
}

// ---------------------------------------------------------------------------
// notify
// ---------------------------------------------------------------------------

func TestE2EAutomationNew_NotifyDeliversOneNotificationPerRecipient(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-notify")
	assignee := p.member(t)
	reporterMember := p.member(t)
	task := p.task(t, map[string]any{
		"title": "Ship it", "assignee_ids": []string{assignee}, "reporter_id": reporterMember,
	})

	p.build(t, "Tell people",
		[]nnNode{
			{"t", "trigger", "status_changed", map[string]any{"status_id": p.status("In Progress")}},
			// The assignee is named twice (role + explicit): still one notification.
			{"a", "action", "notify", map[string]any{"notify": map[string]any{
				"recipients": []string{"assignees", "reporter"}, "member_ids": []string{assignee},
				"message": "{{task.title}} is in progress",
			}}},
		},
		[]nnEdge{{"t", "a", ""}})

	p.setStatus(t, task, "In Progress")

	type row struct {
		Message   string `db:"message"`
		Recipient string `db:"member_id"`
	}
	var rows []row
	waitFor(t, 20*time.Second, "both notifications", func() bool {
		rows = nil
		err := p.env.db.SelectContext(p.env.ctx, &rows, `
			SELECT n.message, pm.id::text AS member_id
			FROM notifications n
			JOIN project_members pm ON pm.account_id = n.recipient_account_id AND pm.project_id = $2
			WHERE n.type = 'automation' AND n.task_id = $1`, task, p.id)
		return err == nil && len(rows) >= 2
	})
	time.Sleep(time.Second) // allow a (wrong) duplicate to land
	_ = p.env.db.SelectContext(p.env.ctx, &rows, `
		SELECT n.message, pm.id::text AS member_id
		FROM notifications n
		JOIN project_members pm ON pm.account_id = n.recipient_account_id AND pm.project_id = $2
		WHERE n.type = 'automation' AND n.task_id = $1`, task, p.id)
	got := map[string]int{}
	for _, r := range rows {
		got[r.Recipient]++
		if r.Message != "Ship it is in progress" {
			t.Fatalf("the message should be templated, got %q", r.Message)
		}
	}
	if len(got) != 2 || got[assignee] != 1 || got[reporterMember] != 1 {
		t.Fatalf("expected one notification each for the assignee and the reporter, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Conditions: relative dates
// ---------------------------------------------------------------------------

func TestE2EAutomationNew_RelativeDateConditionSplitsOverdueFromOnTime(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-overdue")
	overdue := p.task(t, map[string]any{"title": "Late", "due_date": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)})
	onTime := p.task(t, map[string]any{"title": "Fine", "due_date": time.Now().Add(5 * 24 * time.Hour).UTC().Format(time.RFC3339)})
	soon := p.task(t, map[string]any{"title": "Soon", "due_date": time.Now().Add(36 * time.Hour).UTC().Format(time.RFC3339)})

	p.build(t, "Overdue check",
		[]nnNode{
			{"t", "trigger", "status_changed", map[string]any{"status_id": p.status("In Progress")}},
			{"c", "condition", "condition", map[string]any{"branches": []map[string]any{
				{"handle": "late", "tree": map[string]any{"field": "due_date", "operator": "less_than", "value": "now"}},
				{"handle": "soon", "tree": map[string]any{"field": "due_date", "operator": "less_than", "value": "now+2d"}},
			}}},
			{"late", "action", "update_task", nnAddTag("overdue")},
			{"soon", "action", "update_task", nnAddTag("due-soon")},
			{"ok", "action", "update_task", nnAddTag("on-track")},
		},
		[]nnEdge{{"t", "c", ""}, {"c", "late", "late"}, {"c", "soon", "soon"}, {"c", "ok", "else"}})

	for _, id := range []string{overdue, onTime, soon} {
		p.setStatus(t, id, "In Progress")
	}
	p.wait(t, overdue, func(d map[string]any) bool { return nnSameSet(nnTags(d), []string{"overdue"}) })
	p.wait(t, soon, func(d map[string]any) bool { return nnSameSet(nnTags(d), []string{"due-soon"}) })
	p.wait(t, onTime, func(d map[string]any) bool { return nnSameSet(nnTags(d), []string{"on-track"}) })
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestE2EAutomationNew_InvalidConfigsAreRejected(t *testing.T) {
	t.Parallel()
	p := nnNewProject(t, "nn-validate")
	autoID := createAutomationViaAPI(t, p.env, p.client, p.token, p.id, "Validation")

	post := func(kind, typ string, cfg map[string]any, want int) {
		t.Helper()
		body := jsonBody(t, map[string]any{"kind": kind, "type": typ, "config": cfg, "pos_x": 0, "pos_y": 0})
		req := mustRequest(p.env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/nodes", p.env.base, p.id, autoID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.token)
		resp := mustDo(t, p.client, req)
		defer func() { _ = resp.Body.Close() }()
		if want == http.StatusCreated {
			assertStatus(t, resp, http.StatusCreated)
			return
		}
		if resp.StatusCode < 400 || resp.StatusCode >= 500 {
			t.Fatalf("%s %v: expected a 4xx rejection, got %d", typ, cfg, resp.StatusCode)
		}
	}
	reject := func(kind, typ string, cfg map[string]any) { t.Helper(); post(kind, typ, cfg, http.StatusBadRequest) }

	reject("action", "update_task", map[string]any{"update": map[string]any{"tags": []string{"a"}, "tags_mode": "merge"}})
	reject("action", "update_task", map[string]any{"update": map[string]any{"clear": []string{"title"}}})
	reject("action", "update_task", map[string]any{"update": map[string]any{"dynamic": map[string]any{"title": map[string]any{"source": "task"}}}})
	reject("action", "update_task", map[string]any{"update": map[string]any{"dynamic": map[string]any{"importance": map[string]any{"source": "now"}}}})
	reject("action", "update_task", map[string]any{"update": map[string]any{"dynamic": map[string]any{"sprint_id": map[string]any{"source": "task", "offset_days": 1}}}})
	reject("action", "notify", map[string]any{"notify": map[string]any{"recipients": []string{"boss"}, "message": "x"}})
	reject("action", "link_task", map[string]any{"link": map[string]any{"type": "loves", "to": map[string]any{"kind": "parent"}}})
	reject("action", "link_task", map[string]any{"link": map[string]any{"type": "blocks", "to": map[string]any{"kind": "children"}}})
	reject("action", "create_task", map[string]any{"create": map[string]any{"title": "t", "link": "nope"}})
	reject("trigger", "field_changed", map[string]any{"field": "status"})
	reject("condition", "condition", map[string]any{"branches": []map[string]any{{
		"handle": "h", "tree": map[string]any{"field": "child_count", "operator": "contains", "value": 1},
	}}})

	// A draft may be incomplete; activating it is what demands the full config.
	post("trigger", "comment_added", nil, http.StatusCreated)
	post("action", "notify", map[string]any{"notify": map[string]any{"message": "no recipients yet"}}, http.StatusCreated)
}
