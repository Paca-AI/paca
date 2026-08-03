package events

// ChannelRealtime is the Valkey Pub/Sub channel that services/realtime subscribes to for immediate
// fan-out to connected Socket.IO clients.
const ChannelRealtime = "paca.events"

// StreamAnalytics is the Valkey Stream key used for durable analytics and audit log events.
const StreamAnalytics = "paca.analytics"

// StreamTaskActivities is the Valkey Stream key used to fan out task-activity
// events from the API to the internal consumer that persists them to PostgreSQL.
// System-generated activities (task created, updated, plugin changes, etc.) are
// appended here instead of being written directly to the database; the
// ActivityConsumer worker reads this stream and handles the DB write.
const StreamTaskActivities = "paca.task_activities"

// StreamDocActivities is the Valkey Stream key used to fan out doc-activity
// events from the API to the internal consumer that persists them to PostgreSQL.
// System-generated activities (doc created, updated, etc.) are appended here;
// the DocActivityConsumer worker reads this stream and handles the DB write.
const StreamDocActivities = "paca.doc_activities"

// StreamTaskAssignments is the Valkey Stream key used to fan out task
// assignment events (task created/updated with a new assignee) to the
// NotificationConsumer worker, which creates in-app notifications and
// publishes real-time push events.
const StreamTaskAssignments = "paca.task_assignments"

// StreamPluginEvents is the Valkey Stream key every activity event (task
// activities, comments, links, etc.) is appended to so that the plugin
// runtime can dispatch them to subscribed plugins via PluginEventConsumer.
// The plugin runtime is just another stream subscriber here — the API
// service that records an activity never calls into the plugin runtime
// directly.
const StreamPluginEvents = "paca.plugin_events"

// StreamAutomationExternalTriggers is the Valkey Stream key the webhook
// receiver endpoint appends to once it has verified an inbound POST's
// token, so worker.AutomationConsumer can execute the matched api_trigger
// node's graph walk asynchronously — the HTTP handler returns as soon as
// the token is verified and the event is durably queued, without waiting on
// the walk to finish.
const StreamAutomationExternalTriggers = "paca.automation_external_triggers"

// StreamPluginTriggerEvents is the Valkey Stream key a plugin's own
// EmitEvent call (the plugin SDK's event_emit host function) appends to,
// but only when at least one loaded plugin has actually declared a Trigger
// node for that event's topic (platform/plugin.Runtime.TriggersForTopic) —
// a plugin emitting some other, non-trigger-sourcing event never touches
// this stream, keeping its volume proportional to automation-relevant
// topics only. worker.AutomationConsumer reads it the same way it reads
// StreamAutomationExternalTriggers: resolve which project's automations to
// consider and which task (if any) to bind the walk to from the event's own
// payload (every plugin trigger's payload is expected to carry project_id,
// and task_id when the event concerns a specific task — see
// pluginTriggerEventPayload in automation_consumer.go), then execute.
const StreamPluginTriggerEvents = "paca.plugin_trigger_events"

// Event type constants used in both Pub/Sub messages and Stream entries.
const (
	// --- Auth events --------------------------------------------------------
	TopicUserCreated = "user.created"
	TopicUserDeleted = "user.deleted"
	TopicAuthLogin   = "auth.login"
	TopicAuthLogout  = "auth.logout"

	// --- Task events --------------------------------------------------------
	TopicTaskCreated = "task.created"
	TopicTaskUpdated = "task.updated"
	TopicTaskDeleted = "task.deleted"

	// --- Task attachment events ---------------------------------------------
	TopicTaskAttachmentAdded   = "task.attachment.added"
	TopicTaskAttachmentRemoved = "task.attachment.removed"

	// --- Comment events -----------------------------------------------------
	TopicTaskCommentAdded   = "task.comment.added"
	TopicTaskCommentUpdated = "task.comment.updated"
	TopicTaskCommentDeleted = "task.comment.deleted"

	// --- Doc events ---------------------------------------------------------
	TopicDocCreated = "doc.created"
	TopicDocUpdated = "doc.updated"
	TopicDocDeleted = "doc.deleted"
	TopicDocMoved   = "doc.moved"

	// --- Doc folder events --------------------------------------------------
	TopicDocFolderCreated = "doc.folder.created"
	TopicDocFolderUpdated = "doc.folder.updated"
	TopicDocFolderDeleted = "doc.folder.deleted"

	// --- Doc comment events -------------------------------------------------
	TopicDocCommentAdded   = "doc.comment.added"
	TopicDocCommentUpdated = "doc.comment.updated"
	TopicDocCommentDeleted = "doc.comment.deleted"

	// --- Sprint events --------------------------------------------------------
	TopicSprintCreated   = "sprint.created"
	TopicSprintUpdated   = "sprint.updated"
	TopicSprintDeleted   = "sprint.deleted"
	TopicSprintCompleted = "sprint.completed"

	// --- Interaction view events ------------------------------------------------
	// Published directly to ChannelRealtime by sprintsvc.ViewService whenever a
	// sprint/backlog/timeline view (sprintdom.SprintView) is created, updated,
	// deleted, or reordered, so every connected client viewing that project's
	// interaction views stays in sync instead of relying on query staleTime.
	TopicViewCreated   = "view.created"
	TopicViewUpdated   = "view.updated"
	TopicViewDeleted   = "view.deleted"
	TopicViewReordered = "view.reordered"

	// --- Notification events ------------------------------------------------
	// TopicNotificationCreated is published to ChannelRealtime when a new
	// notification is created.  The payload includes recipient_user_id so the
	// realtime service can route the event to the correct user room.
	TopicNotificationCreated = "notification.created"

	// --- Agent trigger events -----------------------------------------------
	// These are appended to StreamAgentTriggers and consumed by services/ai-agent.
	TopicAgentTaskAssigned     = "agent.task_assigned"
	TopicAgentCommentMention   = "agent.comment_mention"
	TopicAgentChatMessage      = "agent.chat_message"
	TopicAgentDescriptionWrite = "agent.description_write"
	// TopicAgentAutomationMessage fires a standalone message at an agent, no
	// task involved — used by the automation engine's trigger_ai_agent
	// action when its trigger has no target task (nothing to assign).
	TopicAgentAutomationMessage = "agent.automation_message"
	// TopicAgentStop interrupts (if running) and tears the sandbox down for
	// good — unchanged from before. TopicAgentPause interrupts the in-flight
	// turn only, leaving the sandbox running so the conversation can be
	// replied to again.
	TopicAgentStop  = "agent.stop"
	TopicAgentPause = "agent.pause"
	// TopicAgentHeartbeat refreshes a chat conversation's idle timer; fired
	// periodically by the frontend while a conversation is loaded in a tab.
	TopicAgentHeartbeat = "agent.heartbeat"

	// --- Agent event topics (emitted by ai-agent, consumed by realtime) ------
	TopicAgentConversationStarted  = "agent.conversation.started"
	TopicAgentConversationFinished = "agent.conversation.finished"
	TopicAgentConversationFailed   = "agent.conversation.failed"
	TopicAgentConversationPaused   = "agent.conversation.paused"
	TopicAgentConversationResumed  = "agent.conversation.resumed"
	TopicAgentConversationStopped  = "agent.conversation.stopped"
	TopicAgentThinkingEvent        = "agent.thinking"
	TopicAgentActionEvent          = "agent.action"
	TopicAgentObservationEvent     = "agent.observation"
	TopicAgentMessageEvent         = "agent.message"

	// --- Automation graph events ----------------------------------------------
	// Published directly to ChannelRealtime by automationsvc.Service whenever
	// an automation's graph or lifecycle changes, so every connected client
	// viewing that project's automation builder stays in sync. Note:
	// automation.applied (the engine mutating a task) is a separate,
	// task-scoped event defined as taskdom.ActivityTypeAutomationApplied, not
	// here.
	TopicAutomationCreated         = "automation.created"
	TopicAutomationUpdated         = "automation.updated"
	TopicAutomationDeleted         = "automation.deleted"
	TopicAutomationActivated       = "automation.activated"
	TopicAutomationArchived        = "automation.archived"
	TopicAutomationRevertedToDraft = "automation.reverted_to_draft"
	TopicAutomationNodeAdded       = "automation.node.added"
	TopicAutomationNodeUpdated     = "automation.node.updated"
	TopicAutomationNodeRemoved     = "automation.node.removed"
	TopicAutomationEdgeAdded       = "automation.edge.added"
	TopicAutomationEdgeRemoved     = "automation.edge.removed"
	// TopicAutomationAPITriggerFired is appended to
	// StreamAutomationExternalTriggers by the webhook receiver handler once
	// a POST's token has been verified.
	TopicAutomationAPITriggerFired = "automation.api_trigger.fired"
)

// Streams for AI Agent pipeline.
const (
	// StreamAgentTriggers is the Valkey Stream key that services/api publishes
	// trigger events to. services/ai-agent consumes with consumer group "ai-agent-workers".
	StreamAgentTriggers = "paca:agent:triggers"

	// StreamAgentEvents is the Valkey Stream key that services/ai-agent publishes
	// conversation events to. services/realtime consumes and fans out to Socket.IO.
	StreamAgentEvents = "paca:agent:events"
)
