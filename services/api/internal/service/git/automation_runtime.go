package gitsvc

import (
	"context"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// NodeRuntime is the plugin-runtime surface the automation engine, its node
// validation and the node palette use.
type NodeRuntime interface {
	AutomationNodeTypes() (triggers, conditions, actions []plugindom.AutomationNodeManifest)
	IsPluginTrigger(nodeType string) bool
	IsPluginCondition(nodeType string) bool
	IsPluginAction(nodeType string) bool
	IsTasklessTrigger(nodeType string) bool
	ResolveAutomationCondition(nodeType string) (pluginName string, ok bool)
	ResolveAutomationAction(nodeType string) (pluginName string, ok bool)
	EvaluateCondition(ctx context.Context, pluginName string, payload []byte) ([]byte, error)
	RunAction(ctx context.Context, pluginName string, payload []byte) ([]byte, error)
	TriggersForTopic(topic string) []plugindom.AutomationNodeManifest
}

// AutomationRuntime serves the Git automation node types next to the plugins'
// own: it answers for every git.* type and passes everything else to the
// plugin runtime, so the engine needs no knowledge of which provider a
// repository lives on.
type AutomationRuntime struct {
	plugins NodeRuntime
	svc     *Service
}

// NewAutomationRuntime wraps the plugin runtime with the Git node types.
func NewAutomationRuntime(plugins NodeRuntime, svc *Service) *AutomationRuntime {
	return &AutomationRuntime{plugins: plugins, svc: svc}
}

// AutomationNodeTypes lists the plugins' node types and the Git ones.
func (r *AutomationRuntime) AutomationNodeTypes() (triggers, conditions, actions []plugindom.AutomationNodeManifest) {
	triggers, conditions, actions = r.plugins.AutomationNodeTypes()
	gt, gc, ga := AutomationNodeTypes()
	return append(triggers, gt...), append(conditions, gc...), append(actions, ga...)
}

// gitNodes indexes the node types core owns by their type, per kind.
var gitNodes = func() (n struct {
	triggers, conditions, actions map[string]plugindom.AutomationNodeManifest
}) { index := func(ms []plugindom.AutomationNodeManifest) map[string]plugindom.AutomationNodeManifest {
	m := make(map[string]plugindom.AutomationNodeManifest, len(ms))
	for _, node := range ms {
		m[node.Type] = node
	}
	return m
}; triggers, conditions, actions := AutomationNodeTypes(); n.triggers, n.conditions, n.actions = index(triggers), index(conditions), index(actions); return n }()

func isGitTrigger(t string) bool {
	_, ok := gitNodes.triggers[t]
	return ok
}

func isGitCondition(t string) bool {
	_, ok := gitNodes.conditions[t]
	return ok
}

func isGitAction(t string) bool {
	_, ok := gitNodes.actions[t]
	return ok
}

// IsPluginTrigger reports whether nodeType is a Git or plugin trigger.
func (r *AutomationRuntime) IsPluginTrigger(t string) bool {
	return isGitTrigger(t) || r.plugins.IsPluginTrigger(t)
}

// IsPluginCondition reports whether nodeType is a Git or plugin condition.
func (r *AutomationRuntime) IsPluginCondition(t string) bool {
	return isGitCondition(t) || r.plugins.IsPluginCondition(t)
}

// IsPluginAction reports whether nodeType is a Git or plugin action.
func (r *AutomationRuntime) IsPluginAction(t string) bool {
	return isGitAction(t) || r.plugins.IsPluginAction(t)
}

// IsTasklessTrigger reports whether a Git or plugin trigger carries no task.
func (r *AutomationRuntime) IsTasklessTrigger(t string) bool {
	if n, ok := gitNodes.triggers[t]; ok {
		return n.Taskless
	}
	return r.plugins.IsTasklessTrigger(t)
}

// ResolveAutomationCondition names the owner of a condition type.
func (r *AutomationRuntime) ResolveAutomationCondition(t string) (string, bool) {
	if isGitCondition(t) {
		return AutomationOwner, true
	}
	return r.plugins.ResolveAutomationCondition(t)
}

// ResolveAutomationAction names the owner of an action type.
func (r *AutomationRuntime) ResolveAutomationAction(t string) (string, bool) {
	if isGitAction(t) {
		return AutomationOwner, true
	}
	return r.plugins.ResolveAutomationAction(t)
}

// EvaluateCondition evaluates a condition node of the named owner.
func (r *AutomationRuntime) EvaluateCondition(ctx context.Context, owner string, payload []byte) ([]byte, error) {
	if owner == AutomationOwner {
		return r.svc.EvaluateAutomationCondition(ctx, payload)
	}
	return r.plugins.EvaluateCondition(ctx, owner, payload)
}

// RunAction runs an action node of the named owner.
func (r *AutomationRuntime) RunAction(ctx context.Context, owner string, payload []byte) ([]byte, error) {
	if owner == AutomationOwner {
		return r.svc.RunAutomationAction(ctx, payload)
	}
	return r.plugins.RunAction(ctx, owner, payload)
}

// TriggersForTopic returns the trigger nodes fed by an event topic.
func (r *AutomationRuntime) TriggersForTopic(topic string) []plugindom.AutomationNodeManifest {
	out := r.plugins.TriggersForTopic(topic)
	if isGitTrigger(topic) {
		out = append(out, plugindom.AutomationNodeManifest{Type: topic, EventTopic: topic})
	}
	return out
}
