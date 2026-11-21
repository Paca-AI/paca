package chatsvc

import (
	"context"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// NodeRuntime is the node-runtime surface the automation engine uses; the Git
// runtime and the plugin runtime both satisfy it.
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

// AutomationRuntime serves the chat node types next to another runtime's own.
type AutomationRuntime struct {
	next NodeRuntime
	svc  *Service
}

// NewAutomationRuntime wraps next (the Git runtime) with the chat node types.
func NewAutomationRuntime(next NodeRuntime, svc *Service) *AutomationRuntime {
	return &AutomationRuntime{next: next, svc: svc}
}

func isChatTrigger(t string) bool { return t == TriggerMessagePosted || t == TriggerMention }
func isChatAction(t string) bool  { return t == ActionSendMessage }

// AutomationNodeTypes lists the wrapped runtime's node types and the chat ones.
func (r *AutomationRuntime) AutomationNodeTypes() (triggers, conditions, actions []plugindom.AutomationNodeManifest) {
	triggers, conditions, actions = r.next.AutomationNodeTypes()
	ct, cc, ca := AutomationNodeTypes()
	return append(triggers, ct...), append(conditions, cc...), append(actions, ca...)
}

// IsPluginTrigger implements NodeRuntime.
func (r *AutomationRuntime) IsPluginTrigger(t string) bool {
	return isChatTrigger(t) || r.next.IsPluginTrigger(t)
}

// IsPluginCondition implements NodeRuntime.
func (r *AutomationRuntime) IsPluginCondition(t string) bool { return r.next.IsPluginCondition(t) }

// IsPluginAction implements NodeRuntime.
func (r *AutomationRuntime) IsPluginAction(t string) bool {
	return isChatAction(t) || r.next.IsPluginAction(t)
}

// IsTasklessTrigger implements NodeRuntime.
func (r *AutomationRuntime) IsTasklessTrigger(t string) bool {
	return isChatTrigger(t) || r.next.IsTasklessTrigger(t)
}

// ResolveAutomationCondition implements NodeRuntime.
func (r *AutomationRuntime) ResolveAutomationCondition(t string) (string, bool) {
	return r.next.ResolveAutomationCondition(t)
}

// ResolveAutomationAction implements NodeRuntime.
func (r *AutomationRuntime) ResolveAutomationAction(t string) (string, bool) {
	if isChatAction(t) {
		return AutomationOwner, true
	}
	return r.next.ResolveAutomationAction(t)
}

// EvaluateCondition implements NodeRuntime.
func (r *AutomationRuntime) EvaluateCondition(ctx context.Context, owner string, payload []byte) ([]byte, error) {
	return r.next.EvaluateCondition(ctx, owner, payload)
}

// RunAction implements NodeRuntime.
func (r *AutomationRuntime) RunAction(ctx context.Context, owner string, payload []byte) ([]byte, error) {
	if owner == AutomationOwner {
		return r.svc.RunAutomationAction(ctx, payload)
	}
	return r.next.RunAction(ctx, owner, payload)
}

// TriggersForTopic implements NodeRuntime.
func (r *AutomationRuntime) TriggersForTopic(topic string) []plugindom.AutomationNodeManifest {
	out := r.next.TriggersForTopic(topic)
	if isChatTrigger(topic) {
		out = append(out, plugindom.AutomationNodeManifest{Type: topic, EventTopic: topic})
	}
	return out
}
