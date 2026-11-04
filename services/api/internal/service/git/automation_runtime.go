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

// gitNodeSet indexes node manifests by their type.
type gitNodeSet map[string]plugindom.AutomationNodeManifest

func indexNodes(ms []plugindom.AutomationNodeManifest) gitNodeSet {
	set := make(gitNodeSet, len(ms))
	for _, m := range ms {
		set[m.Type] = m
	}
	return set
}

// The node types core owns, per kind.
var gitTriggers, gitConditions, gitActions = func() (gitNodeSet, gitNodeSet, gitNodeSet) {
	triggers, conditions, actions := AutomationNodeTypes()
	return indexNodes(triggers), indexNodes(conditions), indexNodes(actions)
}()

func isGitTrigger(t string) bool {
	_, ok := gitTriggers[t]
	return ok
}

func isGitCondition(t string) bool {
	_, ok := gitConditions[t]
	return ok
}

func isGitAction(t string) bool {
	_, ok := gitActions[t]
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
	if n, ok := gitTriggers[t]; ok {
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
