package automationdom

import "errors"

// Sentinel errors for the automation aggregate.
var (
	ErrNotFound               = errors.New("automation: not found")
	ErrNameInvalid            = errors.New("automation: name is required")
	ErrNodeNotFound           = errors.New("automation: node not found")
	ErrNodeInvalidKind        = errors.New("automation: node kind must be trigger, condition, or action")
	ErrNodeInvalidType        = errors.New("automation: unrecognized node type for its kind")
	ErrNodeConfigInvalid      = errors.New("automation: node config is invalid")
	ErrNodeCrossProject       = errors.New("automation: node config references an entity outside this automation's project")
	ErrEdgeNotFound           = errors.New("automation: edge not found")
	ErrEdgeSelfLoop           = errors.New("automation: a node cannot link to itself")
	ErrEdgeCrossAutomation    = errors.New("automation: source and target nodes must belong to the same automation")
	ErrEdgeCycle              = errors.New("automation: this edge would create a cycle")
	ErrEdgeIntoTrigger        = errors.New("automation: a trigger node cannot have an incoming edge")
	ErrEdgeDuplicate          = errors.New("automation: this edge already exists")
	ErrEdgeRequiresTargetTask = errors.New("automation: this connection requires the upstream trigger to have a target task set — only a call_api action can run without one")
	ErrEdgeHandleRequired     = errors.New("automation: an edge from a condition node must specify a branch handle")
	ErrEdgeHandleNotAllowed   = errors.New("automation: an edge from a trigger or action node cannot specify a branch handle")
	ErrActivateNoTrigger      = errors.New("automation: at least one trigger node is required before this automation can be activated")
	ErrActivateNoAction       = errors.New("automation: at least one action node is required before this automation can be activated — without one, automation would run but never do anything")
	ErrWebhookTokenInvalid    = errors.New("automation: webhook token invalid")
)
