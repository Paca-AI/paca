package gitsvc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

type emitted struct {
	topic   string
	payload map[string]any
}

type fakeEmitter struct{ got []emitted }

func (f *fakeEmitter) EmitAutomationEvent(_ context.Context, topic string, p map[string]any) error {
	f.got = append(f.got, emitted{topic, p})
	return nil
}

func (f *fakeEmitter) topics() []string {
	var out []string
	for _, e := range f.got {
		out = append(out, e.topic)
	}
	return out
}

func sameTopics(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestAutomation_WebhookEmitsTheGitTriggersOnTheRightTransitions(t *testing.T) {
	ctx := context.Background()
	fs, pr := newFakeStore(), &fakeProv{}
	ts := &trackStore{fakeStore: fs}
	em := &fakeEmitter{}
	svc := New(ts, fakeProviders{p: pr}).WithAutomationEmitter(em)
	ident := fs.addIdentity("bot", "a")
	fs.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
	project := uuid.New()
	linked(fs, project, "acme/web")
	task := uuid.New()
	fs.tasks["paca-12"] = task
	deliver := func(ev gitprovider.Event) {
		t.Helper()
		pr.events = []gitprovider.Event{ev}
		if _, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"}); err != nil {
			t.Fatal(err)
		}
	}

	// Opening a request that names a task links the request and the branch; a
	// request never seen before is not a state change.
	deliver(prEvent("e1", "opened", "feat/PACA-12-login", "Add login"))
	if got, want := em.topics(), []string{TriggerPRLinked, TriggerBranchLinked}; !sameTopics(got, want) {
		t.Fatalf("on open: got %v, want %v", got, want)
	}
	first := em.got[0].payload
	if first["project_id"] != project.String() || first["task_id"] != task.String() || first["state"] != "open" || first["number"] != "7" {
		t.Fatalf("payload: %+v", first)
	}

	// A push to the same request repeats neither link.
	ts.cur = &gitdom.ChangeRequest{ID: fs.crs[0].ID, Number: "7", State: "open"}
	em.got = nil
	deliver(prEvent("e2", "synchronized", "feat/PACA-12-login", "Add login"))
	if len(em.got) != 0 {
		t.Fatalf("a push must not trigger: %v", em.topics())
	}

	// Merging it is the state change automations wait for.
	deliver(prEvent("e3", "merged", "feat/PACA-12-login", "Add login"))
	if got, want := em.topics(), []string{TriggerPRStateChanged}; !sameTopics(got, want) {
		t.Fatalf("on merge: got %v, want %v", got, want)
	}
	if p := em.got[0].payload; p["state"] != "merged" || p["previous_state"] != "open" || p["task_id"] != task.String() {
		t.Fatalf("payload: %+v", p)
	}

	// A redelivered merge, now that the cache says merged, is not a change.
	ts.cur = &gitdom.ChangeRequest{ID: fs.crs[0].ID, Number: "7", State: "merged"}
	em.got = nil
	deliver(prEvent("e4", "merged", "feat/PACA-12-login", "Add login"))
	if len(em.got) != 0 {
		t.Fatalf("a repeat of the same state must not trigger: %v", em.topics())
	}
}

func TestAutomation_NoEmitterMeansNoExtraStoreReads(t *testing.T) {
	ctx := context.Background()
	fs, pr := newFakeStore(), &fakeProv{}
	svc := New(fs, fakeProviders{p: pr}) // FindChangeRequest would panic on fakeStore
	ident := fs.addIdentity("bot", "a")
	fs.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
	linked(fs, uuid.New(), "acme/web")
	pr.events = []gitprovider.Event{prEvent("e1", "opened", "feat/x", "Add")}
	if _, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"}); err != nil {
		t.Fatal(err)
	}
}

func nodePayload(t *testing.T, nodeType, config string, project, task uuid.UUID) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"node_type": nodeType, "config": json.RawMessage(config),
		"project_id": project, "task": map[string]any{"id": task}, "idempotency_key": "run:node"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAutomation_ConditionComparesTheNewestRequestState(t *testing.T) {
	fx, ts, svc, ref := newTaskFx(t)
	matched := func(config string) bool {
		t.Helper()
		out, err := svc.EvaluateAutomationCondition(context.Background(), nodePayload(t, ConditionPRState, config, ref.ProjectID, ref.TaskID))
		if err != nil {
			t.Fatal(err)
		}
		var res struct{ Matched bool }
		_ = json.Unmarshal(out, &res)
		return res.Matched
	}
	if matched(`{"expected_state":"merged"}`) {
		t.Fatal("a task with no request never matches")
	}
	ts.links.ChangeRequests = []*gitdom.TaskChangeRequest{
		{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "6", State: "merged"}},
		{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "5", State: "open"}},
	}
	if !matched(`{"expected_state":"merged"}`) || matched(`{"expected_state":"open"}`) {
		t.Fatal("the newest request (first) decides")
	}
	if matched(`{}`) || matched(`"x"`) {
		t.Fatal("an unusable config never matches")
	}
}

func TestAutomation_MergeIsIdempotentAndUsesTheLiveHead(t *testing.T) {
	fx, ts, svc, ref := newTaskFx(t)
	ts.links.ChangeRequests = []*gitdom.TaskChangeRequest{{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "7", State: "open", HeadSHA: "stale"}}}
	fx.pr.info.Capabilities.Merge = true
	run := func() map[string]any {
		t.Helper()
		out, err := svc.RunAutomationAction(context.Background(), nodePayload(t, ActionMergePR, `{"merge_method":"squash"}`, ref.ProjectID, ref.TaskID))
		if err != nil {
			t.Fatal(err)
		}
		var res map[string]any
		_ = json.Unmarshal(out, &res)
		return res
	}

	fx.pr.cr = gitprovider.ChangeRequest{Number: "7", State: gitprovider.CROpen, Head: gitprovider.Branch{SHA: "live"}}
	if res := run(); res["applied"] != true {
		t.Fatalf("open request must merge: %v", res)
	}
	req, _ := fx.pr.last.(gitprovider.MergeRequest)
	if req.Method != "squash" || req.ExpectedHeadSHA != "live" {
		t.Fatalf("merge request: %+v", req)
	}

	fx.pr.calls = nil
	fx.pr.cr.State = gitprovider.CRMerged
	if res := run(); res["applied"] != false || res["error"] != nil || countCalls(fx.pr.calls, "MergeChangeRequest") != 0 {
		t.Fatalf("an already merged request is done, not an error: %v", res)
	}

	fx.pr.cr.State = gitprovider.CRClosed
	if res := run(); res["applied"] != false || res["error"] == nil {
		t.Fatalf("a closed request cannot merge: %v", res)
	}
}

func TestAutomation_CommentPostsOnTheTaskRequest(t *testing.T) {
	fx, ts, svc, ref := newTaskFx(t)
	ts.links.ChangeRequests = []*gitdom.TaskChangeRequest{{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "7", State: "open"}}}
	out, err := svc.RunAutomationAction(context.Background(), nodePayload(t, ActionCommentPR, `{"body":"Shipped"}`, ref.ProjectID, ref.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	var res map[string]any
	_ = json.Unmarshal(out, &res)
	req, _ := fx.pr.last.(gitprovider.CommentRequest)
	if res["applied"] != true || req.Subject.Number != "7" || req.IdempotencyKey != "run:node" {
		t.Fatalf("res %v, request %+v", res, req)
	}

	ts.links.ChangeRequests = nil
	out, _ = svc.RunAutomationAction(context.Background(), nodePayload(t, ActionCommentPR, `{"body":"x"}`, ref.ProjectID, ref.TaskID))
	_ = json.Unmarshal(out, &res)
	if res["applied"] != false || res["error"] == nil {
		t.Fatalf("no linked request must fail the node: %v", res)
	}
}

func TestAutomation_EventTriggersReachTheTasksOfTheRequest(t *testing.T) {
	ctx := context.Background()
	fs, pr := newFakeStore(), &fakeProv{}
	ts := &trackStore{fakeStore: fs}
	em := &fakeEmitter{}
	svc := New(ts, fakeProviders{p: pr}).WithAutomationEmitter(em)
	ident := fs.addIdentity("bot", "a")
	fs.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
	linked(fs, uuid.New(), "acme/web")
	task := uuid.New()
	fs.tasks["paca-12"] = task
	ts.cur = &gitdom.ChangeRequest{ID: uuid.New(), Number: "7", State: "open"}

	subject := prEvent("x", "", "feat/PACA-12-x", "T").Subject
	human := gitprovider.Actor{Login: "ann", Kind: gitprovider.ActorUser}
	bot := gitprovider.Actor{Login: "ci", Kind: gitprovider.ActorBot}
	repo := gitprovider.RepoRef{Host: "git.example", FullPath: "Acme/Web"}
	cases := []struct {
		name string
		ev   gitprovider.Event
		want string
	}{
		{"ready", gitprovider.Event{Kind: gitprovider.EventChangeRequest, Action: "ready", Actor: human}, TriggerPRReady},
		{"review", gitprovider.Event{Kind: gitprovider.EventReview, Action: "submitted", Actor: human, Text: "lgtm"}, TriggerPRReviewed},
		{"comment", gitprovider.Event{Kind: gitprovider.EventComment, Action: "created", Actor: human, Text: "hi"}, TriggerPRCommented},
		{"bot comment never triggers", gitprovider.Event{Kind: gitprovider.EventComment, Action: "created", Actor: bot}, ""},
		{"automation comment from a user identity", gitprovider.Event{Kind: gitprovider.EventComment, Action: "created", Actor: human, Text: "done\n\n---\n<sub>Posted by a Paca automation</sub>"}, ""},
		{"agent comment from a user identity", gitprovider.Event{Kind: gitprovider.EventComment, Action: "created", Actor: human, Text: "ok\n\n---\n<sub>Posted by **Dev** via Paca</sub>"}, ""},
		{"reply quoting a Paca comment", gitprovider.Event{Kind: gitprovider.EventComment, Action: "created", Actor: human, Text: "> ok\n> <sub>Posted by **Dev** via Paca</sub>\n\nthanks, but please redo"}, TriggerPRCommented},
		{"edited comment", gitprovider.Event{Kind: gitprovider.EventComment, Action: "edited", Actor: human}, ""},
		{"check", gitprovider.Event{Kind: gitprovider.EventCheck, Actor: bot, Check: &gitprovider.CheckInfo{Name: "ci/test", Conclusion: "failure"}}, TriggerPRCICompleted},
		{"Paca's own status", gitprovider.Event{Kind: gitprovider.EventCheck, Actor: bot, Check: &gitprovider.CheckInfo{Name: runStatusPrefix + "run", Conclusion: "failure"}}, ""},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			em.got = nil
			c.ev.ID, c.ev.Repo, c.ev.Subject = "ev"+string(rune('a'+i)), repo, subject
			pr.events = []gitprovider.Event{c.ev}
			if _, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"}); err != nil {
				t.Fatal(err)
			}
			var got []emitted
			for _, e := range em.got {
				if e.topic != TriggerPRLinked && e.topic != TriggerBranchLinked { // the first event links the request
					got = append(got, e)
				}
			}
			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("must not trigger: %v", em.topics())
				}
				return
			}
			if len(got) != 1 || got[0].topic != c.want || got[0].payload["task_id"] != task.String() {
				t.Fatalf("got %v, want %s", em.topics(), c.want)
			}
		})
	}
}

func TestAutomation_MoreConditions(t *testing.T) {
	fx, ts, svc, ref := newTaskFx(t)
	eval := func(nodeType, config string) bool {
		t.Helper()
		out, err := svc.EvaluateAutomationCondition(context.Background(), nodePayload(t, nodeType, config, ref.ProjectID, ref.TaskID))
		if err != nil {
			t.Fatal(err)
		}
		var res struct{ Matched bool }
		_ = json.Unmarshal(out, &res)
		return res.Matched
	}
	if !eval(ConditionHasPR, `{"expected":"none"}`) || eval(ConditionHasPR, `{"expected":"linked"}`) || eval(ConditionPRDraft, `{"expected":"ready"}`) {
		t.Fatal("no request: has_pr none holds, everything else is false")
	}
	ts.links.ChangeRequests = []*gitdom.TaskChangeRequest{{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "7", State: "open", Draft: true, BaseRef: "main", HeadSHA: "abc"}}}
	if !eval(ConditionHasPR, `{"expected":"linked"}`) || !eval(ConditionPRDraft, `{"expected":"draft"}`) || eval(ConditionPRDraft, `{"expected":"ready"}`) {
		t.Fatal("draft / has_pr")
	}
	if !eval(ConditionPRBase, `{"branch":" main "}`) || eval(ConditionPRBase, `{"branch":"dev"}`) || eval(ConditionPRBase, `{}`) {
		t.Fatal("base branch")
	}
	fx.pr.checks = []gitprovider.Check{{Name: "ci/test", State: gitprovider.CheckFailure}}
	if !eval(ConditionPRCI, `{"expected_status":"failing"}`) || eval(ConditionPRCI, `{"expected_status":"passing"}`) {
		t.Fatal("ci failing")
	}
	fx.pr.checks = nil
	if !eval(ConditionPRCI, `{"expected_status":"none"}`) {
		t.Fatal("no checks is none")
	}
}

func TestAutomation_MetadataActions(t *testing.T) {
	fx, ts, svc, ref := newTaskFx(t)
	ts.links.ChangeRequests = []*gitdom.TaskChangeRequest{{ChangeRequest: gitdom.ChangeRequest{RepositoryID: fx.repo.ID, Number: "7", State: "open"}}}
	run := func(nodeType, config string) map[string]any {
		t.Helper()
		out, err := svc.RunAutomationAction(context.Background(), nodePayload(t, nodeType, config, ref.ProjectID, ref.TaskID))
		if err != nil {
			t.Fatal(err)
		}
		var res map[string]any
		_ = json.Unmarshal(out, &res)
		return res
	}

	if res := run(ActionAddLabels, `{"labels":"bug, ready ,"}`); res["applied"] != true {
		t.Fatalf("labels: %v", res)
	}
	if req, _ := fx.pr.last.(gitprovider.LabelsRequest); len(req.Labels) != 2 || req.Labels[1] != "ready" {
		t.Fatalf("labels request: %+v", fx.pr.last)
	}
	if res := run(ActionAddLabels, `{"labels":" , "}`); res["error"] == nil {
		t.Fatal("empty labels must fail")
	}
	if res := run(ActionRequestReviewers, `{"reviewers":"ann,bob"}`); res["applied"] != true {
		t.Fatalf("reviewers: %v", res)
	}

	fx.pr.cr = gitprovider.ChangeRequest{Number: "7", State: gitprovider.CROpen}
	if res := run(ActionClosePR, `{}`); res["applied"] != true {
		t.Fatalf("close: %v", res)
	}
	if req, _ := fx.pr.last.(gitprovider.UpdateCRRequest); req.State == nil || *req.State != gitprovider.CRClosed {
		t.Fatalf("close request: %+v", fx.pr.last)
	}
	if res := run(ActionReopenPR, `{}`); res["applied"] != false || res["error"] != nil {
		t.Fatalf("reopening an open request is done: %v", res)
	}
	fx.pr.cr.State = gitprovider.CRMerged
	if res := run(ActionClosePR, `{}`); res["error"] == nil {
		t.Fatalf("a merged request cannot be closed: %v", res)
	}

	fx.pr.cr = gitprovider.ChangeRequest{Number: "7", State: gitprovider.CROpen, Draft: true}
	if res := run(ActionMarkReady, `{}`); res["applied"] != true {
		t.Fatalf("ready: %v", res)
	}
	if req, _ := fx.pr.last.(gitprovider.UpdateCRRequest); req.Draft == nil || *req.Draft {
		t.Fatalf("ready request: %+v", fx.pr.last)
	}
	fx.pr.cr.Draft = false
	if res := run(ActionMarkReady, `{}`); res["applied"] != false || res["error"] != nil {
		t.Fatalf("already ready: %v", res)
	}
}

func TestAutomation_IssueCreatedHasNoTask(t *testing.T) {
	ctx := context.Background()
	fs, pr := newFakeStore(), &fakeProv{}
	em := &fakeEmitter{}
	svc := New(&trackStore{fakeStore: fs}, fakeProviders{p: pr}).WithAutomationEmitter(em)
	ident := fs.addIdentity("bot", "a")
	fs.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
	project := uuid.New()
	linked(fs, project, "acme/web")
	repo := gitprovider.RepoRef{Host: "git.example", FullPath: "Acme/Web"}
	issue := &gitprovider.EventSubject{Kind: gitprovider.SubjectIssue, Number: "9", URL: "https://git.example/acme/web/issues/9"}
	human := gitprovider.Actor{Login: "ann", Kind: gitprovider.ActorUser}
	deliver := func(ev gitprovider.Event) {
		t.Helper()
		em.got = nil
		ev.Repo, ev.Subject = repo, issue
		pr.events = []gitprovider.Event{ev}
		if _, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"}); err != nil {
			t.Fatal(err)
		}
	}

	deliver(gitprovider.Event{ID: "i1", Kind: gitprovider.EventComment, Action: "created", Actor: human, Text: "Crash on login\n\nSteps: ..."})
	if len(em.got) != 1 || em.got[0].topic != TriggerIssueCreated {
		t.Fatalf("got %v", em.topics())
	}
	p := em.got[0].payload
	if _, has := p["task_id"]; has || p["project_id"] != project.String() || p["number"] != "9" || p["title"] != "Crash on login" || p["author"] != "ann" || p["url"] == "" {
		t.Fatalf("payload: %+v", p)
	}

	// A later comment, a bot's issue and an edit are not new issues.
	deliver(gitprovider.Event{ID: "i2", Kind: gitprovider.EventComment, Action: "created", Actor: human, Text: "me too", Location: &gitprovider.Location{CommentID: "c-1"}})
	deliver2 := len(em.got)
	deliver(gitprovider.Event{ID: "i3", Kind: gitprovider.EventComment, Action: "created", Actor: gitprovider.Actor{Login: "bot", Kind: gitprovider.ActorBot}, Text: "auto"})
	if deliver2 != 0 || len(em.got) != 0 {
		t.Fatalf("must not trigger: %v", em.topics())
	}
}
