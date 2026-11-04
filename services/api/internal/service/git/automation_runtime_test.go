package gitsvc

import (
	"context"
	"testing"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

type fakeNodeRuntime struct{ ran []string }

func (f *fakeNodeRuntime) AutomationNodeTypes() (t, c, a []plugindom.AutomationNodeManifest) {
	return []plugindom.AutomationNodeManifest{{Type: "x.trigger"}}, nil, nil
}
func (f *fakeNodeRuntime) IsPluginTrigger(t string) bool                    { return t == "x.trigger" }
func (f *fakeNodeRuntime) IsTasklessTrigger(t string) bool                  { return t == "x.trigger" }
func (f *fakeNodeRuntime) IsPluginCondition(string) bool                    { return false }
func (f *fakeNodeRuntime) IsPluginAction(t string) bool                     { return t == "x.act" }
func (f *fakeNodeRuntime) ResolveAutomationCondition(string) (string, bool) { return "", false }
func (f *fakeNodeRuntime) ResolveAutomationAction(t string) (string, bool) {
	return "plug", t == "x.act"
}
func (f *fakeNodeRuntime) EvaluateCondition(context.Context, string, []byte) ([]byte, error) {
	return nil, nil
}
func (f *fakeNodeRuntime) RunAction(_ context.Context, owner string, _ []byte) ([]byte, error) {
	f.ran = append(f.ran, owner)
	return []byte(`{"applied":true}`), nil
}
func (f *fakeNodeRuntime) TriggersForTopic(string) []plugindom.AutomationNodeManifest { return nil }

func TestAutomationRuntime_ServesGitNodesAndDefersTheRest(t *testing.T) {
	plugins := &fakeNodeRuntime{}
	rt := NewAutomationRuntime(plugins, New(newFakeStore(), fakeProviders{}))

	for _, ty := range []string{TriggerPRLinked, TriggerPRStateChanged, TriggerBranchLinked, TriggerPRReady, TriggerPRReviewed, TriggerPRCommented, TriggerPRCICompleted, TriggerIssueCreated} {
		if !rt.IsPluginTrigger(ty) || len(rt.TriggersForTopic(ty)) != 1 {
			t.Fatalf("%s must be a trigger fed by its own topic", ty)
		}
	}
	if !rt.IsPluginCondition(ConditionPRState) || !rt.IsPluginAction(ActionMergePR) || !rt.IsPluginAction(ActionCommentPR) {
		t.Fatal("git condition and actions must resolve")
	}
	if owner, ok := rt.ResolveAutomationAction(ActionMergePR); !ok || owner != AutomationOwner {
		t.Fatalf("owner %q %v", owner, ok)
	}
	if !rt.IsTasklessTrigger(TriggerIssueCreated) || rt.IsTasklessTrigger(TriggerPRLinked) || !rt.IsTasklessTrigger("x.trigger") {
		t.Fatal("only issue_created (and the plugin's declared trigger) carry no task")
	}
	// The legacy plugin-only names are gone for good: nothing answers for them.
	if rt.IsPluginTrigger("github.pr_state_changed") || rt.IsPluginCondition("github.pr_state") {
		t.Fatal("github.* is not a node type anymore")
	}

	// The plugin's own nodes still go to the plugin.
	owner, _ := rt.ResolveAutomationAction("x.act")
	if _, err := rt.RunAction(context.Background(), owner, nil); err != nil || len(plugins.ran) != 1 || plugins.ran[0] != "plug" {
		t.Fatalf("plugin action: %v %v", err, plugins.ran)
	}

	triggers, conditions, actions := rt.AutomationNodeTypes()
	gt, gc, ga := AutomationNodeTypes()
	if len(triggers) != 1+len(gt) || len(conditions) != len(gc) || len(actions) != len(ga) {
		t.Fatalf("palette: %d/%d/%d", len(triggers), len(conditions), len(actions))
	}
}
