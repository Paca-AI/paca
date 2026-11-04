package gitsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// The automation node types the Git integration contributes. They belong to
// core, not to a provider plugin, so every Git platform (GitHub, GitLab, ...)
// drives the same triggers, condition and actions: a provider only has to
// deliver normalized events (see ingest) and implement the SPI operations.
const (
	// Triggers: the node type doubles as the topic the event is emitted on.
	TriggerPRLinked       = "git.pr_linked"
	TriggerPRStateChanged = "git.pr_state_changed"
	TriggerBranchLinked   = "git.branch_linked"
	// TriggerIssueCreated fires when an issue is opened in a linked repository.
	// An issue belongs to no task, so the run has none: it can reach call_api,
	// trigger_ai_agent and wait nodes, and read the issue through the
	// {{event.number}}, {{event.title}}, {{event.text}}, {{event.url}} and
	// {{event.author}} placeholders.
	TriggerIssueCreated = "git.issue_created"
	// TriggerPRReady fires when a draft request is marked ready for review.
	TriggerPRReady = "git.pr_ready_for_review"
	// TriggerPRReviewed fires when someone submits a review on a task's request.
	TriggerPRReviewed = "git.pr_review_submitted"
	// TriggerPRCommented fires when someone comments on a task's request.
	TriggerPRCommented = "git.pr_comment_created"
	// TriggerPRCICompleted fires when a CI check finishes on a task's request
	// (Paca's own run status excluded). Follow it with a git.pr_ci_status
	// condition to react to the overall result.
	TriggerPRCICompleted = "git.pr_ci_completed"

	// ConditionPRState compares the state of the task's newest pull/merge
	// request with an expected one.
	ConditionPRState = "git.pr_state"
	// ConditionPRCI compares the CI result of the task's newest request.
	ConditionPRCI = "git.pr_ci_status"
	// ConditionPRDraft tells a draft request from one ready for review.
	ConditionPRDraft = "git.pr_draft"
	// ConditionPRBase matches the branch the newest request targets.
	ConditionPRBase = "git.pr_base_branch"
	// ConditionHasPR tells whether the task has a pull request at all.
	ConditionHasPR = "git.has_pr"

	ActionMergePR   = "git.merge_pr"
	ActionCommentPR = "git.comment_pr"
	ActionClosePR   = "git.close_pr"
	ActionReopenPR  = "git.reopen_pr"
	// ActionMarkReady takes the newest request out of draft.
	ActionMarkReady = "git.mark_pr_ready"
	// ActionAddLabels labels the newest request; labels is comma separated.
	ActionAddLabels = "git.add_labels"
	// ActionRequestReviewers asks people (comma separated logins) to review.
	ActionRequestReviewers = "git.request_reviewers"

	// AutomationOwner is the name the automation engine resolves these node
	// types to, in place of a plugin's name.
	AutomationOwner = "core.git"
)

// AutomationEmitter hands a Git event to the automation engine. The payload
// carries project_id and task_id, the contract of every plugin-style trigger.
type AutomationEmitter interface {
	EmitAutomationEvent(ctx context.Context, topic string, payload map[string]any) error
}

// WithAutomationEmitter makes the service emit the Git automation triggers.
func (s *Service) WithAutomationEmitter(e AutomationEmitter) *Service {
	s.automation = e
	return s
}

// WithTaskGitNotifier makes the service tell clients when what Git knows about
// a task changed (a request or branch linked, a request's state moved), so an
// open task page refreshes.
func (s *Service) WithTaskGitNotifier(f func(ctx context.Context, projectID, taskID uuid.UUID)) *Service {
	s.taskGitChanged = f
	return s
}

func (s *Service) notifyTask(ctx context.Context, repo *gitdom.Repository, task uuid.UUID) {
	if s.taskGitChanged != nil {
		s.taskGitChanged(ctx, repo.ProjectID, task)
	}
}

// emitTask emits one trigger for one task, best effort: an automation that
// cannot be queued must not fail the webhook or the link that caused it.
func (s *Service) emitTask(ctx context.Context, topic string, repo *gitdom.Repository, task uuid.UUID, fields map[string]any) {
	if s.automation == nil {
		return
	}
	payload := map[string]any{
		"project_id":    repo.ProjectID.String(),
		"task_id":       task.String(),
		"repository_id": repo.ID.String(),
		"provider_id":   repo.ProviderID,
		"repository":    repo.FullPath,
	}
	for k, v := range fields {
		payload[k] = v
	}
	if err := s.automation.EmitAutomationEvent(ctx, topic, payload); err != nil {
		slog.Warn("git: emitting automation event", "topic", topic, "task_id", task, "error", err)
	}
}

func crFields(cr *gitdom.ChangeRequest) map[string]any {
	return map[string]any{
		"number": cr.Number, "title": cr.Title, "url": cr.URL, "state": cr.State,
		"draft": cr.Draft, "head_ref": cr.HeadRef, "base_ref": cr.BaseRef, "author": cr.Author,
	}
}

// emitProject emits a trigger that belongs to no task (the payload has no
// task_id), for the project the repository is linked to.
func (s *Service) emitProject(ctx context.Context, topic string, repo *gitdom.Repository, fields map[string]any) {
	if s.automation == nil {
		return
	}
	payload := map[string]any{
		"project_id":    repo.ProjectID.String(),
		"repository_id": repo.ID.String(),
		"provider_id":   repo.ProviderID,
		"repository":    repo.FullPath,
	}
	for k, v := range fields {
		payload[k] = v
	}
	if err := s.automation.EmitAutomationEvent(ctx, topic, payload); err != nil {
		slog.Warn("git: emitting automation event", "topic", topic, "error", err)
	}
}

// linkChangeRequest links a task to a change request, emitting git.pr_linked
// when the link is new.
func (s *Service) linkChangeRequest(ctx context.Context, repo *gitdom.Repository, task uuid.UUID, cr *gitdom.ChangeRequest) error {
	created, err := s.store.LinkTaskToChangeRequest(ctx, task, cr.ID)
	if err != nil {
		return err
	}
	if created {
		s.notifyTask(ctx, repo, task)
		s.emitTask(ctx, TriggerPRLinked, repo, task, crFields(cr))
	}
	return nil
}

// linkBranch links a task to a branch, emitting git.branch_linked when the
// link is new.
func (s *Service) linkBranch(ctx context.Context, repo *gitdom.Repository, task uuid.UUID, branch string) error {
	created, err := s.store.LinkTaskBranch(ctx, task, repo.ID, branch)
	if err != nil {
		return err
	}
	if created {
		s.notifyTask(ctx, repo, task)
		s.emitTask(ctx, TriggerBranchLinked, repo, task, map[string]any{"branch": branch})
	}
	return nil
}

// emitStateChange emits git.pr_state_changed for every task linked to the
// change request when its lifecycle state moved. previous is the cached state
// before the update ("" when the request was never seen: it counts as open, so
// a request first met already merged still fires).
//
// Best effort, like every emit: the request is already saved, so failing here
// would only lose the trigger for good (a redelivery no longer sees a change).
func (s *Service) emitStateChange(ctx context.Context, repo *gitdom.Repository, previous string, saved *gitdom.ChangeRequest) {
	if (s.automation == nil && s.taskGitChanged == nil) || saved.State == "" {
		return
	}
	if previous == "" {
		previous = "open"
	}
	if previous == saved.State {
		return
	}
	tasks, err := s.store.ListChangeRequestTasks(ctx, saved.ID)
	if err != nil {
		slog.Warn("git: listing tasks of a change request", "error", err)
		return
	}
	fields := crFields(saved)
	fields["previous_state"] = previous
	for _, t := range tasks {
		s.notifyTask(ctx, repo, t)
		s.emitTask(ctx, TriggerPRStateChanged, repo, t, fields)
	}
}

// schema builds a node's config schema: one entry per property, in the shape
// the generic config form renders.
func schema(required []string, props map[string]any) json.RawMessage {
	doc := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		doc["required"] = required
	}
	b, _ := json.Marshal(doc)
	return b
}

func enumProp(title string, values ...string) map[string]any {
	return map[string]any{"type": "string", "title": title, "enum": values}
}

func textProp(title string) map[string]any {
	return map[string]any{"type": "string", "title": title}
}

// AutomationNodeTypes describes the node types for the automation palette.
func AutomationNodeTypes() (triggers, conditions, actions []plugindom.AutomationNodeManifest) {
	trig := func(topic, label string) plugindom.AutomationNodeManifest {
		return plugindom.AutomationNodeManifest{Type: topic, Label: label, EventTopic: topic}
	}
	triggers = []plugindom.AutomationNodeManifest{
		trig(TriggerPRLinked, "Git: Pull Request Created"),
		trig(TriggerPRStateChanged, "Git: Pull Request State Changed"),
		trig(TriggerPRReady, "Git: Pull Request Ready for Review"),
		trig(TriggerPRReviewed, "Git: Pull Request Review Submitted"),
		trig(TriggerPRCommented, "Git: Pull Request Commented"),
		trig(TriggerPRCICompleted, "Git: Pull Request CI Completed"),
		trig(TriggerBranchLinked, "Git: Branch Created"),
		{Type: TriggerIssueCreated, Label: "Git: Issue Created", EventTopic: TriggerIssueCreated, Taskless: true},
	}
	conditions = []plugindom.AutomationNodeManifest{
		{Type: ConditionPRState, Label: "Git: Pull Request State",
			ConfigSchema: schema([]string{"expected_state"}, map[string]any{"expected_state": enumProp("Expected State", "open", "closed", "merged")})},
		{Type: ConditionPRCI, Label: "Git: Pull Request CI Status",
			ConfigSchema: schema([]string{"expected_status"}, map[string]any{"expected_status": enumProp("Expected CI Status", "passing", "failing", "pending", "none")})},
		{Type: ConditionPRDraft, Label: "Git: Pull Request Draft",
			ConfigSchema: schema([]string{"expected"}, map[string]any{"expected": enumProp("Expected", "draft", "ready")})},
		{Type: ConditionPRBase, Label: "Git: Pull Request Target Branch",
			ConfigSchema: schema([]string{"branch"}, map[string]any{"branch": textProp("Target Branch")})},
		{Type: ConditionHasPR, Label: "Git: Task Has Pull Request",
			ConfigSchema: schema([]string{"expected"}, map[string]any{"expected": enumProp("Expected", "linked", "none")})},
	}
	actions = []plugindom.AutomationNodeManifest{
		{Type: ActionMergePR, Label: "Git: Merge Pull Request",
			ConfigSchema: schema(nil, map[string]any{"merge_method": map[string]any{"type": "string", "title": "Merge Method", "enum": []string{"merge", "squash", "rebase"}, "default": "merge"}})},
		{Type: ActionCommentPR, Label: "Git: Comment on Pull Request",
			ConfigSchema: schema([]string{"body"}, map[string]any{"body": map[string]any{"type": "string", "title": "Comment Body", "format": "textarea"}})},
		{Type: ActionClosePR, Label: "Git: Close Pull Request", ConfigSchema: schema(nil, map[string]any{})},
		{Type: ActionReopenPR, Label: "Git: Reopen Pull Request", ConfigSchema: schema(nil, map[string]any{})},
		{Type: ActionMarkReady, Label: "Git: Mark Pull Request Ready for Review", ConfigSchema: schema(nil, map[string]any{})},
		{Type: ActionAddLabels, Label: "Git: Add Labels to Pull Request",
			ConfigSchema: schema([]string{"labels"}, map[string]any{"labels": textProp("Labels (comma separated)")})},
		{Type: ActionRequestReviewers, Label: "Git: Request Reviewers",
			ConfigSchema: schema([]string{"reviewers"}, map[string]any{"reviewers": textProp("Reviewer logins (comma separated)")})},
	}
	return triggers, conditions, actions
}

// automationNode is the payload the engine sends a condition or action node
// (the same shape it sends a plugin's).
type automationNode struct {
	NodeType       string          `json:"node_type"`
	Config         json.RawMessage `json:"config"`
	ProjectID      uuid.UUID       `json:"project_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Task           struct {
		ID uuid.UUID `json:"id"`
	} `json:"task"`
}

func decodeAutomationNode(raw []byte) (*automationNode, error) {
	var n automationNode
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, fmt.Errorf("git automation: decode node: %w", err)
	}
	return &n, nil
}

// newestChangeRequest is the pull/merge request the automation means by "the
// task's pull request": the one updated last. nil when none is linked.
func (s *Service) newestChangeRequest(ctx context.Context, projectID, taskID uuid.UUID) (*gitdom.TaskChangeRequest, error) {
	tg, err := s.store.ListTaskGit(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	if len(tg.ChangeRequests) == 0 {
		return nil, nil
	}
	return tg.ChangeRequests[0], nil
}

// EvaluateAutomationCondition answers a git.* condition node with
// {"matched": bool}. Every condition but git.has_pr is false for a task with no
// linked request.
func (s *Service) EvaluateAutomationCondition(ctx context.Context, raw []byte) ([]byte, error) {
	n, err := decodeAutomationNode(raw)
	if err != nil {
		return nil, err
	}
	matched, err := s.conditionMatches(ctx, n)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]bool{"matched": matched})
}

func (s *Service) conditionMatches(ctx context.Context, n *automationNode) (bool, error) {
	var cfg struct {
		ExpectedState  string `json:"expected_state"`
		ExpectedStatus string `json:"expected_status"`
		Expected       string `json:"expected"`
		Branch         string `json:"branch"`
	}
	switch n.NodeType {
	case ConditionPRState, ConditionPRCI, ConditionPRDraft, ConditionPRBase, ConditionHasPR:
	default:
		return false, fmt.Errorf("git automation: unknown condition %q", n.NodeType)
	}
	if err := json.Unmarshal(n.Config, &cfg); err != nil {
		slog.Warn("git: unusable automation condition config", "node_type", n.NodeType, "error", err)
		return false, nil // an unusable config never matches
	}
	cr, err := s.newestChangeRequest(ctx, n.ProjectID, n.Task.ID)
	if err != nil {
		return false, err
	}
	if n.NodeType == ConditionHasPR {
		return (cfg.Expected == "linked" && cr != nil) || (cfg.Expected == "none" && cr == nil), nil
	}
	if cr == nil {
		return false, nil
	}
	switch n.NodeType {
	case ConditionPRState:
		return cfg.ExpectedState != "" && cr.State == cfg.ExpectedState, nil
	case ConditionPRDraft:
		return (cfg.Expected == "draft" && cr.Draft) || (cfg.Expected == "ready" && !cr.Draft), nil
	case ConditionPRBase:
		return cfg.Branch != "" && cr.BaseRef == strings.TrimSpace(cfg.Branch), nil
	default: // ConditionPRCI
		status := "none"
		if ci := s.readCI(ctx, n.ProjectID, cr); ci != nil {
			status = ci.State
		}
		return cfg.ExpectedStatus != "" && status == cfg.ExpectedStatus, nil
	}
}

// RunAutomationAction performs a git.* action node and answers with
// {"applied": bool, "error": string}. An action that finds nothing to do (the
// request is already merged) is not applied and not an error. Labels and
// reviewers are always sent: the platforms ignore ones already present.
func (s *Service) RunAutomationAction(ctx context.Context, raw []byte) ([]byte, error) {
	n, err := decodeAutomationNode(raw)
	if err != nil {
		return nil, err
	}
	var applied bool
	switch n.NodeType {
	case ActionMergePR:
		applied, err = s.automationMerge(ctx, n)
	case ActionCommentPR:
		applied, err = s.automationComment(ctx, n)
	case ActionClosePR:
		applied, err = s.automationSetState(ctx, n, gitprovider.CRClosed)
	case ActionReopenPR:
		applied, err = s.automationSetState(ctx, n, gitprovider.CROpen)
	case ActionMarkReady:
		applied, err = s.automationMarkReady(ctx, n)
	case ActionAddLabels:
		applied, err = s.automationAddLabels(ctx, n)
	case ActionRequestReviewers:
		applied, err = s.automationRequestReviewers(ctx, n)
	default:
		err = fmt.Errorf("unknown action %q", n.NodeType)
	}
	res := map[string]any{"applied": applied && err == nil}
	if err != nil {
		res["error"] = err.Error()
	}
	return json.Marshal(res)
}

var errNoLinkedChangeRequest = errors.New("no pull request linked to this task")

func (s *Service) automationTarget(ctx context.Context, n *automationNode) (Scope, *opTarget, *gitdom.TaskChangeRequest, error) {
	cr, err := s.newestChangeRequest(ctx, n.ProjectID, n.Task.ID)
	if err != nil {
		return Scope{}, nil, nil, err
	}
	if cr == nil {
		return Scope{}, nil, nil, errNoLinkedChangeRequest
	}
	sc := Scope{ProjectID: n.ProjectID, RepositoryID: cr.RepositoryID}
	t, err := s.resolve(ctx, sc)
	if err != nil {
		return Scope{}, nil, nil, err
	}
	return sc, t, cr, nil
}

func (s *Service) automationMerge(ctx context.Context, n *automationNode) (bool, error) {
	var cfg struct {
		MergeMethod string `json:"merge_method"`
	}
	if err := json.Unmarshal(n.Config, &cfg); err != nil {
		return false, errors.New("invalid config")
	}
	sc, t, cr, err := s.automationTarget(ctx, n)
	if err != nil {
		return false, err
	}
	// Read the live request: the cache may be behind, and a retried run must
	// find an already merged request done rather than fail.
	live, err := t.api.GetChangeRequest(ctx, gitprovider.CRRequest{Target: t.target, Number: cr.Number})
	if err != nil {
		return false, fmt.Errorf("fetch pull request: %w", err)
	}
	switch live.State {
	case gitprovider.CRMerged:
		return false, nil
	case gitprovider.CRClosed:
		return false, errors.New("pull request is closed")
	case gitprovider.CROpen:
	}
	if _, err := s.MergeChangeRequest(ctx, sc, cr.Number, cfg.MergeMethod, live.Head.SHA); err != nil {
		return false, fmt.Errorf("merge pull request: %w", err)
	}
	return true, nil
}

func (s *Service) automationComment(ctx context.Context, n *automationNode) (bool, error) {
	var cfg struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(n.Config, &cfg); err != nil || strings.TrimSpace(cfg.Body) == "" {
		return false, errors.New("invalid config: body is required")
	}
	if err := checkBody(cfg.Body); err != nil {
		return false, err
	}
	sc, t, cr, err := s.automationTarget(ctx, n)
	if err != nil {
		return false, err
	}
	subject := subjectLabel(gitprovider.SubjectChangeRequest, cr.Number)
	key := n.IdempotencyKey
	if key == "" {
		key = idempotencyKey(sc, "automation-comment", subject, cfg.Body)
	}
	_, err = run(s, ctx, sc, t, "automation.comment", subject, func() (gitprovider.CommentResult, error) {
		return t.api.Comment(ctx, gitprovider.CommentRequest{Target: t.target,
			Subject: gitprovider.SubjectRef{Kind: gitprovider.SubjectChangeRequest, Number: cr.Number},
			Body:    cfg.Body + "\n\n---\n<sub>Posted by a Paca automation</sub>", IdempotencyKey: key})
	})
	if err != nil {
		return false, fmt.Errorf("comment on pull request: %w", err)
	}
	return true, nil
}

// splitList reads a comma (or newline) separated config value.
func splitList(v string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// automationSetState closes or reopens the newest request. A merged request
// cannot change; one already in the wanted state is done, not an error.
func (s *Service) automationSetState(ctx context.Context, n *automationNode, want gitprovider.CRState) (bool, error) {
	sc, t, cr, err := s.automationTarget(ctx, n)
	if err != nil {
		return false, err
	}
	live, err := t.api.GetChangeRequest(ctx, gitprovider.CRRequest{Target: t.target, Number: cr.Number})
	if err != nil {
		return false, fmt.Errorf("fetch pull request: %w", err)
	}
	if live.State == gitprovider.CRMerged {
		return false, errors.New("pull request is already merged")
	}
	if live.State == want {
		return false, nil
	}
	if _, err := s.UpdateChangeRequest(ctx, sc, cr.Number, UpdateInput{State: &want}); err != nil {
		return false, fmt.Errorf("update pull request: %w", err)
	}
	return true, nil
}

func (s *Service) automationMarkReady(ctx context.Context, n *automationNode) (bool, error) {
	sc, t, cr, err := s.automationTarget(ctx, n)
	if err != nil {
		return false, err
	}
	live, err := t.api.GetChangeRequest(ctx, gitprovider.CRRequest{Target: t.target, Number: cr.Number})
	if err != nil {
		return false, fmt.Errorf("fetch pull request: %w", err)
	}
	if !live.Draft {
		return false, nil
	}
	ready := false
	if _, err := s.UpdateChangeRequest(ctx, sc, cr.Number, UpdateInput{Draft: &ready}); err != nil {
		return false, fmt.Errorf("update pull request: %w", err)
	}
	return true, nil
}

func (s *Service) automationAddLabels(ctx context.Context, n *automationNode) (bool, error) {
	var cfg struct {
		Labels string `json:"labels"`
	}
	if err := json.Unmarshal(n.Config, &cfg); err != nil || len(splitList(cfg.Labels)) == 0 {
		return false, errors.New("invalid config: labels is required")
	}
	sc, _, cr, err := s.automationTarget(ctx, n)
	if err != nil {
		return false, err
	}
	if err := s.AddLabels(ctx, sc, gitprovider.SubjectChangeRequest, cr.Number, splitList(cfg.Labels)); err != nil {
		return false, fmt.Errorf("add labels: %w", err)
	}
	return true, nil
}

func (s *Service) automationRequestReviewers(ctx context.Context, n *automationNode) (bool, error) {
	var cfg struct {
		Reviewers string `json:"reviewers"`
	}
	if err := json.Unmarshal(n.Config, &cfg); err != nil || len(splitList(cfg.Reviewers)) == 0 {
		return false, errors.New("invalid config: reviewers is required")
	}
	sc, _, cr, err := s.automationTarget(ctx, n)
	if err != nil {
		return false, err
	}
	if err := s.RequestReviewers(ctx, sc, cr.Number, splitList(cfg.Reviewers)); err != nil {
		return false, fmt.Errorf("request reviewers: %w", err)
	}
	return true, nil
}

// isIssueBody reports whether the event is the opening message of a new issue.
// Providers send the issue's own text as a comment on it, the way they send
// every later reply; what sets it apart is that it is not a comment of its
// own, so it has no comment id (every real comment has one, and a reaction or
// reply needs it).
func isIssueBody(ev *gitprovider.Event) bool {
	return ev.Kind == gitprovider.EventComment && ev.Action == "created" &&
		ev.Subject != nil && ev.Subject.Kind == gitprovider.SubjectIssue &&
		(ev.Location == nil || ev.Location.CommentID == "")
}

// postedByPaca reports whether text carries the footer Paca appends to what it
// posts (an agent's comment, or git.comment_pr). Through a personal access
// token or OAuth identity such a comment comes back from the platform with a
// human author, so the footer is what tells it apart from a person's.
//
// The footer is always the trailing block, so only text that ends with it
// counts: a person's reply that quotes a Paca comment keeps its own footer
// mid-text and still triggers.
func postedByPaca(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasSuffix(text, "</sub>") {
		return false
	}
	i := strings.LastIndex(text, "<sub>Posted by ")
	if i < 0 || strings.Contains(text[i:], "\n") {
		return false
	}
	tail := text[i:]
	return strings.HasSuffix(tail, "via Paca</sub>") || strings.Contains(tail, "via [Paca](") ||
		strings.HasSuffix(tail, "a Paca automation</sub>")
}

// emitEventTriggers feeds the triggers that come straight from a webhook event
// (ready for review, a review, a comment, a finished check) to every task the
// event's request is linked to. A comment or review by a bot, or one Paca
// posted itself, never triggers: an automation that comments would otherwise
// start itself again.
func (s *Service) emitEventTriggers(ctx context.Context, repo *gitdom.Repository, ev *gitprovider.Event) {
	if s.automation == nil && s.taskGitChanged == nil {
		return
	}
	sub := ev.Subject
	human := ev.Actor.Kind != gitprovider.ActorBot && !postedByPaca(ev.Text)
	if sub != nil && sub.Kind == gitprovider.SubjectIssue {
		if isIssueBody(ev) && human {
			title, _, _ := strings.Cut(strings.TrimSpace(ev.Text), "\n")
			s.emitProject(ctx, TriggerIssueCreated, repo, map[string]any{
				"number": sub.Number, "title": truncate(title, 500), "text": truncate(ev.Text, 4000),
				"url": sub.URL, "author": ev.Actor.Login,
			})
		}
		return
	}
	if sub == nil || sub.Kind != gitprovider.SubjectChangeRequest {
		return
	}
	topic, extra := "", map[string]any{}
	switch ev.Kind { //nolint:exhaustive // only these kinds have a trigger
	case gitprovider.EventChangeRequest:
		if ev.Action == "ready" && human {
			topic = TriggerPRReady
		}
	case gitprovider.EventReview:
		if ev.Action == "submitted" && human {
			topic, extra["reviewer"], extra["text"] = TriggerPRReviewed, ev.Actor.Login, truncate(ev.Text, 2000)
		}
	case gitprovider.EventComment:
		if ev.Action == "created" && human {
			topic, extra["author"], extra["text"] = TriggerPRCommented, ev.Actor.Login, truncate(ev.Text, 2000)
		}
	case gitprovider.EventCheck:
		if ev.Check != nil && !strings.HasPrefix(ev.Check.Name, runStatusPrefix) {
			topic, extra["check_name"], extra["conclusion"] = TriggerPRCICompleted, ev.Check.Name, ev.Check.Conclusion
		}
	}
	if topic == "" {
		return
	}
	cr, err := s.store.FindChangeRequest(ctx, repo.ID, sub.Number)
	if err != nil {
		return // a request Paca never cached has no linked task
	}
	tasks, err := s.store.ListChangeRequestTasks(ctx, cr.ID)
	if err != nil {
		slog.Warn("git: listing tasks of a change request", "error", err)
		return
	}
	fields := crFields(cr)
	for k, v := range extra {
		fields[k] = v
	}
	for _, t := range tasks {
		s.emitTask(ctx, topic, repo, t, fields)
		if ev.Kind == gitprovider.EventCheck {
			s.notifyTask(ctx, repo, t) // the panel shows CI
		}
	}
}
