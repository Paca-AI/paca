// Package apierr defines machine-readable error codes used in API error
// responses. Clients should switch on Code values rather than HTTP status
// codes or human-readable messages, because messages are subject to change.
package apierr

// Code is a stable, machine-readable string that identifies a specific error
// condition. Codes are grouped by domain prefix (e.g. AUTH_, USER_).
type Code string

const (
	// CodeInvalidCredentials represents invalid authentication credentials.
	CodeInvalidCredentials Code = "AUTH_INVALID_CREDENTIALS"
	// CodeMissingToken represents a missing authentication token.
	CodeMissingToken Code = "AUTH_MISSING_TOKEN"
	// CodeTokenInvalid represents an invalid authentication token.
	CodeTokenInvalid Code = "AUTH_TOKEN_INVALID"
	// CodeUnauthenticated represents an unauthenticated request.
	CodeUnauthenticated Code = "AUTH_UNAUTHENTICATED"

	// CodeUserNotFound represents a user that was not found.
	CodeUserNotFound Code = "USER_NOT_FOUND"
	// CodeUsernameTaken represents a username that is already taken.
	CodeUsernameTaken Code = "USER_USERNAME_TAKEN"
	// CodeIdentityTaken indicates the email or oidc_sub is already linked to
	// another account (Galaxy ADR-038).
	CodeIdentityTaken Code = "USER_IDENTITY_TAKEN"
	// CodeForbidden represents a forbidden action.
	CodeForbidden Code = "FORBIDDEN"
	// CodeGlobalRoleNotFound represents a global role that was not found.
	CodeGlobalRoleNotFound Code = "GLOBAL_ROLE_NOT_FOUND"
	// CodeGlobalRoleNameTaken represents a duplicate global role name.
	CodeGlobalRoleNameTaken Code = "GLOBAL_ROLE_NAME_TAKEN"
	// CodeGlobalRoleNameInvalid represents an invalid global role name.
	CodeGlobalRoleNameInvalid Code = "GLOBAL_ROLE_NAME_INVALID"

	// CodeGlobalRoleHasUsers indicates the role cannot be deleted because it
	// still has assigned users.
	CodeGlobalRoleHasUsers Code = "GLOBAL_ROLE_HAS_ASSIGNED_USERS"
	// CodeBadRequest represents a bad request.
	CodeBadRequest Code = "BAD_REQUEST"
	// CodeInternalError represents an internal server error.
	CodeInternalError Code = "INTERNAL_ERROR"
	// CodePasswordChangeRequired indicates the user must change their password
	// before accessing any other endpoint.
	CodePasswordChangeRequired Code = "AUTH_PASSWORD_CHANGE_REQUIRED"
	// CodeInvalidCurrentPassword indicates the supplied current password is wrong.
	CodeInvalidCurrentPassword Code = "USER_INVALID_CURRENT_PASSWORD"

	// CodeProjectNotFound indicates the requested project does not exist.
	CodeProjectNotFound Code = "PROJECT_NOT_FOUND"
	// CodeProjectNameTaken indicates the project name is already in use.
	CodeProjectNameTaken Code = "PROJECT_NAME_TAKEN"
	// CodeProjectNameInvalid indicates the project name is empty or invalid.
	CodeProjectNameInvalid Code = "PROJECT_NAME_INVALID"
	// CodeProjectPrefixInvalid indicates the task ID prefix is not valid.
	CodeProjectPrefixInvalid Code = "PROJECT_PREFIX_INVALID"

	// CodeProjectRoleNotFound indicates the requested project role does not exist.
	CodeProjectRoleNotFound Code = "PROJECT_ROLE_NOT_FOUND"
	// CodeProjectRoleNameTaken indicates the role name is already in use within the project.
	CodeProjectRoleNameTaken Code = "PROJECT_ROLE_NAME_TAKEN"
	// CodeProjectRoleNameInvalid indicates an invalid or empty role name.
	CodeProjectRoleNameInvalid Code = "PROJECT_ROLE_NAME_INVALID"
	// CodeProjectRoleHasMembers indicates the role cannot be deleted because members still use it.
	CodeProjectRoleHasMembers Code = "PROJECT_ROLE_HAS_MEMBERS"

	// CodeProjectMemberNotFound indicates the membership record was not found.
	CodeProjectMemberNotFound Code = "PROJECT_MEMBER_NOT_FOUND"
	// CodeProjectMemberAlreadyAdded indicates the user is already a member of the project.
	CodeProjectMemberAlreadyAdded Code = "PROJECT_MEMBER_ALREADY_ADDED"

	// CodeTaskNotFound indicates the requested task does not exist.
	CodeTaskNotFound Code = "TASK_NOT_FOUND"
	// CodeTaskTitleInvalid indicates an empty or invalid task title.
	CodeTaskTitleInvalid Code = "TASK_TITLE_INVALID"
	// CodeEpicCannotHaveParent indicates an attempt to set a parent on an epic task.
	CodeEpicCannotHaveParent Code = "TASK_EPIC_CANNOT_HAVE_PARENT"
	// CodeTaskCannotBeOwnParent indicates an attempt to set a task as its own parent.
	CodeTaskCannotBeOwnParent Code = "TASK_CANNOT_BE_OWN_PARENT"
	// CodeTaskParentCycleDetected indicates the requested parent assignment would create a cycle.
	CodeTaskParentCycleDetected Code = "TASK_PARENT_CYCLE_DETECTED"

	// CodeTaskTypeNotFound indicates the requested task type does not exist.
	CodeTaskTypeNotFound Code = "TASK_TYPE_NOT_FOUND"
	// CodeTaskTypeNameInvalid indicates an empty or invalid task type name.
	CodeTaskTypeNameInvalid Code = "TASK_TYPE_NAME_INVALID"
	// CodeTaskTypeIsSystem indicates an attempt to modify a system task type.
	CodeTaskTypeIsSystem Code = "TASK_TYPE_IS_SYSTEM"
	// CodeTaskTypeNameReserved indicates an attempt to use a reserved system type name.
	CodeTaskTypeNameReserved Code = "TASK_TYPE_NAME_RESERVED"

	// CodeTaskStatusNotFound indicates the requested task status does not exist.
	CodeTaskStatusNotFound Code = "TASK_STATUS_NOT_FOUND"
	// CodeTaskStatusNameInvalid indicates an empty or invalid task status name.
	CodeTaskStatusNameInvalid Code = "TASK_STATUS_NAME_INVALID"
	// CodeTaskStatusCategoryInvalid indicates an invalid status category value.
	CodeTaskStatusCategoryInvalid Code = "TASK_STATUS_CATEGORY_INVALID"
	// CodeTaskStatusReorderInvalid indicates the provided status IDs do not match the project's statuses.
	CodeTaskStatusReorderInvalid Code = "TASK_STATUS_REORDER_INVALID"
	// CodeTaskStatusInUseByWorkflow indicates the status is still referenced by an automation workflow.
	CodeTaskStatusInUseByWorkflow Code = "TASK_STATUS_IN_USE_BY_WORKFLOW"

	// CodeSprintNotFound indicates the requested sprint does not exist.
	CodeSprintNotFound Code = "SPRINT_NOT_FOUND"
	// CodeSprintNameInvalid indicates an empty or invalid sprint name.
	CodeSprintNameInvalid Code = "SPRINT_NAME_INVALID"
	// CodeSprintStatusInvalid indicates an invalid sprint status value.
	CodeSprintStatusInvalid Code = "SPRINT_STATUS_INVALID"
	// CodeSprintAlreadyComplete indicates the sprint is already completed.
	CodeSprintAlreadyComplete Code = "SPRINT_ALREADY_COMPLETE"

	// CodeViewNotFound indicates the requested sprint view does not exist.
	CodeViewNotFound Code = "VIEW_NOT_FOUND"
	// CodeViewNameInvalid indicates an empty or invalid view name.
	CodeViewNameInvalid Code = "VIEW_NAME_INVALID"
	// CodeViewTypeInvalid indicates an invalid view type value.
	CodeViewTypeInvalid Code = "VIEW_TYPE_INVALID"
	// CodeViewIsLastView indicates the view cannot be deleted because it is the last remaining view.
	CodeViewIsLastView Code = "VIEW_IS_LAST_VIEW"
	// CodeViewReorderInvalid indicates the provided view IDs do not match the interaction's views.
	CodeViewReorderInvalid Code = "VIEW_REORDER_INVALID"

	// CodeCustomFieldNotFound indicates the requested custom field definition does not exist.
	CodeCustomFieldNotFound Code = "CUSTOM_FIELD_NOT_FOUND"
	// CodeCustomFieldKeyInvalid indicates an empty or invalid field key.
	CodeCustomFieldKeyInvalid Code = "CUSTOM_FIELD_KEY_INVALID"
	// CodeCustomFieldKeyTaken indicates the field key is already in use within the project.
	CodeCustomFieldKeyTaken Code = "CUSTOM_FIELD_KEY_TAKEN"
	// CodeCustomFieldTypeInvalid indicates an invalid field type value.
	CodeCustomFieldTypeInvalid Code = "CUSTOM_FIELD_TYPE_INVALID"
	// CodeCustomFieldNameInvalid indicates an empty or invalid display name.
	CodeCustomFieldNameInvalid Code = "CUSTOM_FIELD_NAME_INVALID"

	// CodeFileNotFound indicates the requested file record does not exist.
	CodeFileNotFound Code = "FILE_NOT_FOUND"
	// CodeAttachmentNotFound indicates the requested task attachment does not exist.
	CodeAttachmentNotFound Code = "ATTACHMENT_NOT_FOUND"
	// CodeUploadNotPending indicates the file is not in the pending upload state.
	CodeUploadNotPending Code = "ATTACHMENT_UPLOAD_NOT_PENDING"
	// CodeAttachmentInvalid indicates invalid input for creating an attachment.
	CodeAttachmentInvalid Code = "ATTACHMENT_INVALID"
	// CodeMultipartUploadIDRequired indicates that a multipart upload_id was not provided.
	CodeMultipartUploadIDRequired Code = "ATTACHMENT_MULTIPART_UPLOAD_ID_REQUIRED"
	// CodeNotMultipartUpload indicates that an upload_id was provided for a non-multipart file.
	CodeNotMultipartUpload Code = "ATTACHMENT_NOT_MULTIPART_UPLOAD"
	// CodeUploadIDMismatch indicates that the provided upload_id does not match the stored one.
	CodeUploadIDMismatch Code = "ATTACHMENT_UPLOAD_ID_MISMATCH"
	// CodeMultipartPartsEmpty indicates that no parts were provided for a multipart complete request.
	CodeMultipartPartsEmpty Code = "ATTACHMENT_MULTIPART_PARTS_EMPTY"

	// CodeActivityNotFound indicates the requested activity entry does not exist.
	CodeActivityNotFound Code = "ACTIVITY_NOT_FOUND"
	// CodeActivityForbidden indicates the caller is not the author of the comment.
	CodeActivityForbidden Code = "ACTIVITY_FORBIDDEN"
	// CodeActivityNotAComment indicates the entry is system-generated and cannot be edited.
	CodeActivityNotAComment Code = "ACTIVITY_NOT_A_COMMENT"
	// CodeCommentContentInvalid indicates an empty or invalid comment content.
	CodeCommentContentInvalid Code = "ACTIVITY_COMMENT_CONTENT_INVALID"
	// CodeCommentActorUnidentified indicates the caller authenticated with the
	// shared agent API key but did not supply an X-Agent-ID header, so there is
	// no project member identity to attribute the comment to.
	CodeCommentActorUnidentified Code = "ACTIVITY_COMMENT_ACTOR_UNIDENTIFIED"

	// --- Task link errors ------------------------------------------------

	// CodeTaskLinkNotFound indicates the requested task link does not exist.
	CodeTaskLinkNotFound Code = "TASK_LINK_NOT_FOUND"
	// CodeTaskLinkSelf indicates an attempt to link a task to itself.
	CodeTaskLinkSelf Code = "TASK_LINK_CANNOT_LINK_TO_SELF"
	// CodeTaskLinkDuplicate indicates the relationship already exists.
	CodeTaskLinkDuplicate Code = "TASK_LINK_ALREADY_EXISTS"
	// CodeTaskLinkCrossProject indicates an attempt to link tasks from different projects.
	CodeTaskLinkCrossProject Code = "TASK_LINK_CROSS_PROJECT"

	// --- Document errors --------------------------------------------------

	// CodeDocNotFound indicates the requested document does not exist.
	CodeDocNotFound Code = "DOC_NOT_FOUND"
	// CodeDocTitleInvalid indicates an empty or invalid document title.
	CodeDocTitleInvalid Code = "DOC_TITLE_INVALID"
	// CodeDocFolderNotFound indicates the requested document folder does not exist.
	CodeDocFolderNotFound Code = "DOC_FOLDER_NOT_FOUND"
	// CodeDocFolderNameInvalid indicates an empty or invalid folder name.
	CodeDocFolderNameInvalid Code = "DOC_FOLDER_NAME_INVALID"
	// CodeDocFolderNotInProject indicates the folder does not belong to the project.
	CodeDocFolderNotInProject Code = "DOC_FOLDER_NOT_IN_PROJECT"
	// CodeDocFolderSelfParent indicates a folder cannot be set as its own parent.
	CodeDocFolderSelfParent Code = "DOC_FOLDER_SELF_PARENT"
	// CodeDocSnapshotNotFound indicates the requested snapshot does not exist.
	CodeDocSnapshotNotFound Code = "DOC_SNAPSHOT_NOT_FOUND"
	// CodeDocActivityNotFound indicates the requested doc activity does not exist.
	CodeDocActivityNotFound Code = "DOC_ACTIVITY_NOT_FOUND"
	// CodeDocActivityForbidden indicates the caller is not the author of the comment.
	CodeDocActivityForbidden Code = "DOC_ACTIVITY_FORBIDDEN"
	// CodeDocActivityNotAComment indicates the entry cannot be edited as a comment.
	CodeDocActivityNotAComment Code = "DOC_ACTIVITY_NOT_A_COMMENT"
	// CodeDocCommentContentInvalid indicates an empty or invalid comment content.
	CodeDocCommentContentInvalid Code = "DOC_COMMENT_CONTENT_INVALID"
	// CodeDocCommentActorUnidentified indicates the caller authenticated with
	// the shared agent API key but did not supply an X-Agent-ID header, so
	// there is no project member identity to attribute the comment to.
	CodeDocCommentActorUnidentified Code = "DOC_COMMENT_ACTOR_UNIDENTIFIED"

	// CodeNotificationNotFound indicates the requested notification does not exist
	// or does not belong to the authenticated user.
	CodeNotificationNotFound Code = "NOTIFICATION_NOT_FOUND"

	// CodeGitHubIntegrationNotFound indicates the project has no GitHub integration configured.
	CodeGitHubIntegrationNotFound Code = "GITHUB_INTEGRATION_NOT_FOUND"
	// CodeGitHubRepositoryNotFound indicates the project has no linked GitHub repository.
	CodeGitHubRepositoryNotFound Code = "GITHUB_REPOSITORY_NOT_FOUND"
	// CodeGitHubPRNotFound indicates the pull request does not exist.
	CodeGitHubPRNotFound Code = "GITHUB_PR_NOT_FOUND"
	// CodeGitHubPRLinkNotFound indicates the task-PR link does not exist.
	CodeGitHubPRLinkNotFound Code = "GITHUB_PR_LINK_NOT_FOUND"
	// CodeGitHubPRAlreadyLinked indicates the pull request is already linked to the task.
	CodeGitHubPRAlreadyLinked Code = "GITHUB_PR_ALREADY_LINKED"
	// CodeGitHubInvalidToken indicates the GitHub personal access token was rejected.
	CodeGitHubInvalidToken Code = "GITHUB_INVALID_TOKEN"
	// CodeGitHubWebhookURLRequired indicates the service has no public webhook URL configured.
	CodeGitHubWebhookURLRequired Code = "GITHUB_WEBHOOK_URL_REQUIRED"
	// CodeGitHubRepoNotAccessible indicates the GitHub repository was not found
	// or the PAT does not have access.
	CodeGitHubRepoNotAccessible Code = "GITHUB_REPO_NOT_ACCESSIBLE"
	// CodeGitHubRepoAlreadyLinked indicates the repository is already linked
	// to the project.
	CodeGitHubRepoAlreadyLinked Code = "GITHUB_REPO_ALREADY_LINKED"
	// CodeGitHubWebhookCreationFailed indicates that creating a webhook on the
	// GitHub repository failed.
	CodeGitHubWebhookCreationFailed Code = "GITHUB_WEBHOOK_CREATION_FAILED"
	// CodeGitHubWebhookURLNotPublic indicates the configured webhook URL is not
	// reachable from the public internet (e.g. localhost).
	CodeGitHubWebhookURLNotPublic Code = "GITHUB_WEBHOOK_URL_NOT_PUBLIC"
	// CodeGitHubBranchAlreadyLinked indicates the branch is already linked to the task.
	CodeGitHubBranchAlreadyLinked Code = "GITHUB_BRANCH_ALREADY_LINKED"
	// CodeGitHubTokenInsufficientPermissions indicates the PAT does not have the
	// required permissions to perform the GitHub API operation (e.g. creating a branch).
	CodeGitHubTokenInsufficientPermissions Code = "GITHUB_TOKEN_INSUFFICIENT_PERMISSIONS"

	// CodeAPIKeyNotFound indicates the requested API key was not found.
	CodeAPIKeyNotFound Code = "API_KEY_NOT_FOUND"
	// CodeAPIKeyRevoked indicates the API key has been revoked.
	CodeAPIKeyRevoked Code = "API_KEY_REVOKED"
	// CodeAPIKeyExpired indicates the API key has expired.
	CodeAPIKeyExpired Code = "API_KEY_EXPIRED"
	// CodeAPIKeyNameInvalid indicates an empty or invalid API key name.
	CodeAPIKeyNameInvalid Code = "API_KEY_NAME_INVALID"
	// CodeAPIKeyNameTooLong indicates the API key name exceeds the maximum length.
	CodeAPIKeyNameTooLong Code = "API_KEY_NAME_TOO_LONG"

	// CodePluginNotFound indicates the requested plugin does not exist.
	CodePluginNotFound Code = "PLUGIN_NOT_FOUND"
	// CodePluginNameTaken indicates a plugin with the same reverse-DNS name is already installed.
	CodePluginNameTaken Code = "PLUGIN_NAME_TAKEN"
	// CodePluginAlreadyUpToDate indicates the marketplace version matches the installed version.
	CodePluginAlreadyUpToDate Code = "PLUGIN_ALREADY_UP_TO_DATE"
	// CodePluginDowngradeNotAllowed indicates the marketplace version is older than the installed version.
	CodePluginDowngradeNotAllowed Code = "PLUGIN_DOWNGRADE_NOT_ALLOWED"
	// CodePayloadTooLarge indicates the request body exceeds the server's size limit.
	CodePayloadTooLarge Code = "PAYLOAD_TOO_LARGE"

	// --- Agent errors -------------------------------------------------------

	// CodeAgentNotFound indicates the requested agent does not exist.
	CodeAgentNotFound Code = "AGENT_NOT_FOUND"
	// CodeAgentHandleTaken indicates the handle is already in use.
	CodeAgentHandleTaken Code = "AGENT_HANDLE_TAKEN"
	// CodeAgentHandleInvalid indicates the handle is empty or malformed.
	CodeAgentHandleInvalid Code = "AGENT_HANDLE_INVALID"
	// CodeAgentNameInvalid indicates the agent name is empty or invalid.
	CodeAgentNameInvalid Code = "AGENT_NAME_INVALID"
	// CodeAgentTypeNotFound indicates the requested agent type does not exist.
	CodeAgentTypeNotFound Code = "AGENT_TYPE_NOT_FOUND"
	// CodeAgentMCPServerNotFound indicates the requested MCP server does not exist.
	CodeAgentMCPServerNotFound Code = "AGENT_MCP_SERVER_NOT_FOUND"
	// CodeAgentSkillNotFound indicates the requested skill does not exist.
	CodeAgentSkillNotFound Code = "AGENT_SKILL_NOT_FOUND"
	// CodeAgentSkillNameReserved indicates the skill name collides with a name reserved for internal agent scaffolding.
	CodeAgentSkillNameReserved Code = "AGENT_SKILL_NAME_RESERVED"
	// CodeAgentConversationNotFound indicates the requested conversation does not exist.
	CodeAgentConversationNotFound Code = "AGENT_CONVERSATION_NOT_FOUND"
	// CodeAgentConversationNotRunning indicates the conversation is not in a runnable state.
	CodeAgentConversationNotRunning Code = "AGENT_CONVERSATION_NOT_RUNNING"
	// CodeAgentConversationAlreadyStopped indicates the conversation is already stopped/finished.
	CodeAgentConversationAlreadyStopped Code = "AGENT_CONVERSATION_ALREADY_STOPPED"
	// CodeAgentConversationBusy indicates a chat reply was sent while the agent is still responding to the previous one.
	CodeAgentConversationBusy Code = "AGENT_CONVERSATION_BUSY"
	// CodeAgentChatSessionNotFound indicates the requested chat session does not exist.
	CodeAgentChatSessionNotFound Code = "AGENT_CHAT_SESSION_NOT_FOUND"
	// CodeAgentEnvVarNotFound indicates the requested environment variable does not exist.
	CodeAgentEnvVarNotFound Code = "AGENT_ENV_VAR_NOT_FOUND"
	// CodeAgentEnvVarKeyTaken indicates the environment variable key is already in use on this agent.
	CodeAgentEnvVarKeyTaken Code = "AGENT_ENV_VAR_KEY_TAKEN"
	// CodeAgentEnvVarKeyInvalid indicates the environment variable key is malformed.
	CodeAgentEnvVarKeyInvalid Code = "AGENT_ENV_VAR_KEY_INVALID"
	// CodeAgentEnvVarKeyReserved indicates the environment variable key collides with an internal sandbox variable.
	CodeAgentEnvVarKeyReserved Code = "AGENT_ENV_VAR_KEY_RESERVED"

	// --- Automation workflow errors ------------------------------------------

	// CodeWorkflowNotFound indicates the requested workflow does not exist.
	CodeWorkflowNotFound Code = "WORKFLOW_NOT_FOUND"
	// CodeWorkflowNameInvalid indicates an empty or invalid workflow name.
	CodeWorkflowNameInvalid Code = "WORKFLOW_NAME_INVALID"
	// CodeWorkflowNodeNotFound indicates the requested workflow node does not exist.
	CodeWorkflowNodeNotFound Code = "WORKFLOW_NODE_NOT_FOUND"
	// CodeWorkflowNodeDuplicateTask indicates the task is already a node in this workflow.
	CodeWorkflowNodeDuplicateTask Code = "WORKFLOW_NODE_DUPLICATE_TASK"
	// CodeWorkflowNodeTaskCrossProject indicates the task does not belong to the workflow's project.
	CodeWorkflowNodeTaskCrossProject Code = "WORKFLOW_NODE_TASK_CROSS_PROJECT"
	// CodeWorkflowStatusRuleNotFound indicates the requested status rule does not exist.
	CodeWorkflowStatusRuleNotFound Code = "WORKFLOW_STATUS_RULE_NOT_FOUND"
	// CodeWorkflowStatusRuleCrossProject indicates the status or member does not belong to the workflow's project.
	CodeWorkflowStatusRuleCrossProject Code = "WORKFLOW_STATUS_RULE_CROSS_PROJECT"
	// CodeWorkflowStatusRuleConflict indicates a concurrent request already set this status rule.
	CodeWorkflowStatusRuleConflict Code = "WORKFLOW_STATUS_RULE_CONFLICT"
	// CodeWorkflowStatusTransitionNotFound indicates the requested status transition does not exist.
	CodeWorkflowStatusTransitionNotFound Code = "WORKFLOW_STATUS_TRANSITION_NOT_FOUND"
	// CodeWorkflowStatusTransitionCrossProject indicates the status does not belong to the workflow's project.
	CodeWorkflowStatusTransitionCrossProject Code = "WORKFLOW_STATUS_TRANSITION_CROSS_PROJECT"
	// CodeWorkflowStatusTransitionSelfLoop indicates an attempt to make a status transition to itself.
	CodeWorkflowStatusTransitionSelfLoop Code = "WORKFLOW_STATUS_TRANSITION_SELF_LOOP"
	// CodeWorkflowStatusTransitionConflict indicates a concurrent request already set this status transition.
	CodeWorkflowStatusTransitionConflict Code = "WORKFLOW_STATUS_TRANSITION_CONFLICT"
	// CodeWorkflowEdgeNotFound indicates the requested edge does not exist.
	CodeWorkflowEdgeNotFound Code = "WORKFLOW_EDGE_NOT_FOUND"
	// CodeWorkflowEdgeSelfLoop indicates an attempt to link a node to itself.
	CodeWorkflowEdgeSelfLoop Code = "WORKFLOW_EDGE_SELF_LOOP"
	// CodeWorkflowEdgeCrossWorkflow indicates source and target nodes belong to different workflows.
	CodeWorkflowEdgeCrossWorkflow Code = "WORKFLOW_EDGE_CROSS_WORKFLOW"
	// CodeWorkflowEdgeCycle indicates the edge would create a cycle in the dependency graph.
	CodeWorkflowEdgeCycle Code = "WORKFLOW_EDGE_CYCLE"
	// CodeWorkflowEdgeDuplicate indicates the edge already exists.
	CodeWorkflowEdgeDuplicate Code = "WORKFLOW_EDGE_DUPLICATE"
	// CodeWorkflowNotDraft indicates the workflow can only be activated while in draft.
	CodeWorkflowNotDraft Code = "WORKFLOW_NOT_DRAFT"
	// CodeWorkflowNotActive indicates the workflow is not active.
	CodeWorkflowNotActive Code = "WORKFLOW_NOT_ACTIVE"
	// CodeWorkflowArchived indicates the workflow's graph can no longer be edited once archived.
	CodeWorkflowArchived Code = "WORKFLOW_ARCHIVED"
	// CodeWorkflowActivateNoNodes indicates an empty workflow cannot be activated.
	CodeWorkflowActivateNoNodes Code = "WORKFLOW_ACTIVATE_NO_NODES"
	// CodeWorkflowActivateDoneStatusUndetermined indicates the workflow's status-transition chain doesn't have exactly one terminal (done) status.
	CodeWorkflowActivateDoneStatusUndetermined Code = "WORKFLOW_ACTIVATE_DONE_STATUS_UNDETERMINED"
	// CodeWorkflowActivateTaskMissing indicates a node references a task that no longer exists in the project.
	CodeWorkflowActivateTaskMissing Code = "WORKFLOW_ACTIVATE_TASK_MISSING"
	// CodeWorkflowActivateNoStatusRules indicates the workflow has no status rules, so activating it would never reassign anything.
	CodeWorkflowActivateNoStatusRules Code = "WORKFLOW_ACTIVATE_NO_STATUS_RULES"
)

// Error carries a machine-readable Code alongside a human-readable Message.
// It implements the error interface so it can propagate through service layers
// and be detected by the transport presenter.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return e.Message }

// New returns a new *Error with the given code and message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}
