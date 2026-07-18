// Package presenter maps domain/infrastructure errors to HTTP responses and
// wraps all payloads in a consistent envelope.
package presenter

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	pluginom "github.com/Paca-AI/api/internal/domain/plugin"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	workflowdom "github.com/Paca-AI/api/internal/domain/workflow"
	"github.com/Paca-AI/api/internal/transport/http/httpx"
)

// envelope is the standard JSON wrapper for every response.
type envelope struct {
	Success   bool   `json:"success"`
	Data      any    `json:"data,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// OK writes a 200 success response.
func OK(w http.ResponseWriter, r *http.Request, data any) {
	httpx.WriteJSON(w, http.StatusOK, envelope{
		Success:   true,
		Data:      data,
		RequestID: httpx.RequestIDFromContext(r.Context()),
	})
}

// Created writes a 201 success response.
func Created(w http.ResponseWriter, r *http.Request, data any) {
	httpx.WriteJSON(w, http.StatusCreated, envelope{
		Success:   true,
		Data:      data,
		RequestID: httpx.RequestIDFromContext(r.Context()),
	})
}

// NoContent writes a 204 No Content response with no body.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error maps a domain/service error to an HTTP status + error code and writes
// a JSON error envelope.  If err is an *apierr.Error, its code is used
// directly; otherwise the code is derived from known domain sentinel errors.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	status, code := statusAndCodeFor(err)

	// For internal/unexpected errors, avoid leaking implementation details to clients.
	publicMsg := err.Error()
	if status == http.StatusInternalServerError || code == apierr.CodeInternalError {
		slog.Error("unhandled error", "error", err, "request_id", httpx.RequestIDFromContext(r.Context()))
		publicMsg = "internal server error"
	}

	httpx.WriteJSON(w, status, envelope{
		Success:   false,
		ErrorCode: string(code),
		Error:     publicMsg,
		RequestID: httpx.RequestIDFromContext(r.Context()),
	})
}

// statusAndCodeFor returns the HTTP status and apierr.Code for err.
func statusAndCodeFor(err error) (int, apierr.Code) {
	// Prefer an explicit apierr.Error if one was constructed upstream.
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return httpStatusForCode(apiErr.Code), apiErr.Code
	}

	// Map domain sentinel errors to codes.
	switch {
	case errors.Is(err, domainauth.ErrInvalidCredentials):
		return http.StatusUnauthorized, apierr.CodeInvalidCredentials
	case errors.Is(err, domainauth.ErrTokenInvalid):
		return http.StatusUnauthorized, apierr.CodeTokenInvalid
	case errors.Is(err, domainauth.ErrSessionInvalidated):
		return http.StatusUnauthorized, apierr.CodeTokenInvalid
	case errors.Is(err, userdom.ErrNotFound):
		return http.StatusNotFound, apierr.CodeUserNotFound
	case errors.Is(err, userdom.ErrUsernameTaken):
		return http.StatusConflict, apierr.CodeUsernameTaken
	case errors.Is(err, userdom.ErrIdentityTaken):
		return http.StatusConflict, apierr.CodeIdentityTaken
	case errors.Is(err, userdom.ErrForbidden):
		return http.StatusForbidden, apierr.CodeForbidden
	case errors.Is(err, userdom.ErrInvalidCurrentPassword):
		return http.StatusUnprocessableEntity, apierr.CodeInvalidCurrentPassword
	case errors.Is(err, globalroledom.ErrNotFound):
		return http.StatusNotFound, apierr.CodeGlobalRoleNotFound
	case errors.Is(err, globalroledom.ErrNameTaken):
		return http.StatusConflict, apierr.CodeGlobalRoleNameTaken
	case errors.Is(err, globalroledom.ErrInvalidName):
		return http.StatusBadRequest, apierr.CodeGlobalRoleNameInvalid
	case errors.Is(err, globalroledom.ErrHasAssignedUsers):
		return http.StatusConflict, apierr.CodeGlobalRoleHasUsers
	case errors.Is(err, globalroledom.ErrPermissionCeilingExceeded):
		return http.StatusForbidden, apierr.CodeForbidden
	case errors.Is(err, projectdom.ErrNotFound):
		return http.StatusNotFound, apierr.CodeProjectNotFound
	case errors.Is(err, projectdom.ErrNameTaken):
		return http.StatusConflict, apierr.CodeProjectNameTaken
	case errors.Is(err, projectdom.ErrNameInvalid):
		return http.StatusBadRequest, apierr.CodeProjectNameInvalid
	case errors.Is(err, projectdom.ErrPrefixInvalid):
		return http.StatusBadRequest, apierr.CodeProjectPrefixInvalid
	case errors.Is(err, projectdom.ErrRoleNotFound):
		return http.StatusNotFound, apierr.CodeProjectRoleNotFound
	case errors.Is(err, projectdom.ErrRoleNameTaken):
		return http.StatusConflict, apierr.CodeProjectRoleNameTaken
	case errors.Is(err, projectdom.ErrRoleNameInvalid):
		return http.StatusBadRequest, apierr.CodeProjectRoleNameInvalid
	case errors.Is(err, projectdom.ErrRoleHasMembers):
		return http.StatusConflict, apierr.CodeProjectRoleHasMembers
	case errors.Is(err, projectdom.ErrMemberNotFound):
		return http.StatusNotFound, apierr.CodeProjectMemberNotFound
	case errors.Is(err, projectdom.ErrMemberAlreadyAdded):
		return http.StatusConflict, apierr.CodeProjectMemberAlreadyAdded
	case errors.Is(err, projectdom.ErrPermissionCeilingExceeded):
		return http.StatusForbidden, apierr.CodeForbidden
	case errors.Is(err, taskdom.ErrTaskNotFound):
		return http.StatusNotFound, apierr.CodeTaskNotFound
	case errors.Is(err, taskdom.ErrTaskTitleInvalid):
		return http.StatusBadRequest, apierr.CodeTaskTitleInvalid
	case errors.Is(err, taskdom.ErrEpicCannotHaveParent):
		return http.StatusBadRequest, apierr.CodeEpicCannotHaveParent
	case errors.Is(err, taskdom.ErrTaskCannotBeOwnParent):
		return http.StatusBadRequest, apierr.CodeTaskCannotBeOwnParent
	case errors.Is(err, taskdom.ErrTaskParentCycleDetected):
		return http.StatusBadRequest, apierr.CodeTaskParentCycleDetected
	case errors.Is(err, taskdom.ErrTypeNotFound):
		return http.StatusNotFound, apierr.CodeTaskTypeNotFound
	case errors.Is(err, taskdom.ErrTypeNameInvalid):
		return http.StatusBadRequest, apierr.CodeTaskTypeNameInvalid
	case errors.Is(err, taskdom.ErrTypeIsSystem):
		return http.StatusForbidden, apierr.CodeTaskTypeIsSystem
	case errors.Is(err, taskdom.ErrTypeNameReserved):
		return http.StatusConflict, apierr.CodeTaskTypeNameReserved
	case errors.Is(err, taskdom.ErrStatusNotFound):
		return http.StatusNotFound, apierr.CodeTaskStatusNotFound
	case errors.Is(err, taskdom.ErrStatusNameInvalid):
		return http.StatusBadRequest, apierr.CodeTaskStatusNameInvalid
	case errors.Is(err, taskdom.ErrStatusCategoryInvalid):
		return http.StatusBadRequest, apierr.CodeTaskStatusCategoryInvalid
	case errors.Is(err, taskdom.ErrStatusReorderInvalid):
		return http.StatusBadRequest, apierr.CodeTaskStatusReorderInvalid
	case errors.Is(err, taskdom.ErrStatusInUseByWorkflow):
		return http.StatusConflict, apierr.CodeTaskStatusInUseByWorkflow
	case errors.Is(err, sprintdom.ErrSprintNotFound):
		return http.StatusNotFound, apierr.CodeSprintNotFound
	case errors.Is(err, sprintdom.ErrSprintNameInvalid):
		return http.StatusBadRequest, apierr.CodeSprintNameInvalid
	case errors.Is(err, sprintdom.ErrSprintStatusInvalid):
		return http.StatusBadRequest, apierr.CodeSprintStatusInvalid
	case errors.Is(err, sprintdom.ErrSprintAlreadyComplete):
		return http.StatusConflict, apierr.CodeSprintAlreadyComplete
	case errors.Is(err, sprintdom.ErrViewNotFound):
		return http.StatusNotFound, apierr.CodeViewNotFound
	case errors.Is(err, sprintdom.ErrViewNameInvalid):
		return http.StatusBadRequest, apierr.CodeViewNameInvalid
	case errors.Is(err, sprintdom.ErrViewTypeInvalid):
		return http.StatusBadRequest, apierr.CodeViewTypeInvalid
	case errors.Is(err, sprintdom.ErrViewIsLastView):
		return http.StatusConflict, apierr.CodeViewIsLastView
	case errors.Is(err, sprintdom.ErrViewReorderInvalid):
		return http.StatusBadRequest, apierr.CodeViewReorderInvalid
	case errors.Is(err, taskdom.ErrCustomFieldNotFound):
		return http.StatusNotFound, apierr.CodeCustomFieldNotFound
	case errors.Is(err, taskdom.ErrCustomFieldKeyInvalid):
		return http.StatusBadRequest, apierr.CodeCustomFieldKeyInvalid
	case errors.Is(err, taskdom.ErrCustomFieldKeyTaken):
		return http.StatusConflict, apierr.CodeCustomFieldKeyTaken
	case errors.Is(err, taskdom.ErrCustomFieldTypeInvalid):
		return http.StatusBadRequest, apierr.CodeCustomFieldTypeInvalid
	case errors.Is(err, taskdom.ErrCustomFieldNameInvalid):
		return http.StatusBadRequest, apierr.CodeCustomFieldNameInvalid
	case errors.Is(err, attachmentdom.ErrFileNotFound):
		return http.StatusNotFound, apierr.CodeFileNotFound
	case errors.Is(err, attachmentdom.ErrAttachmentNotFound):
		return http.StatusNotFound, apierr.CodeAttachmentNotFound
	case errors.Is(err, attachmentdom.ErrTaskNotInProject):
		return http.StatusNotFound, apierr.CodeTaskNotFound
	case errors.Is(err, attachmentdom.ErrUploadNotPending):
		return http.StatusConflict, apierr.CodeUploadNotPending
	case errors.Is(err, attachmentdom.ErrFileSizeZero),
		errors.Is(err, attachmentdom.ErrFileNameEmpty),
		errors.Is(err, attachmentdom.ErrContentTypeEmpty):
		return http.StatusBadRequest, apierr.CodeAttachmentInvalid
	case errors.Is(err, attachmentdom.ErrDocFileMismatch):
		return http.StatusNotFound, apierr.CodeFileNotFound
	case errors.Is(err, attachmentdom.ErrMultipartUploadIDRequired):
		return http.StatusBadRequest, apierr.CodeMultipartUploadIDRequired
	case errors.Is(err, attachmentdom.ErrNotMultipartUpload):
		return http.StatusBadRequest, apierr.CodeNotMultipartUpload
	case errors.Is(err, attachmentdom.ErrUploadIDMismatch):
		return http.StatusBadRequest, apierr.CodeUploadIDMismatch
	case errors.Is(err, attachmentdom.ErrMultipartPartsEmpty):
		return http.StatusBadRequest, apierr.CodeMultipartPartsEmpty
	case errors.Is(err, taskdom.ErrActivityNotFound):
		return http.StatusNotFound, apierr.CodeActivityNotFound
	case errors.Is(err, taskdom.ErrActivityForbidden):
		return http.StatusForbidden, apierr.CodeActivityForbidden
	case errors.Is(err, taskdom.ErrActivityNotAComment):
		return http.StatusBadRequest, apierr.CodeActivityNotAComment
	case errors.Is(err, taskdom.ErrCommentContentInvalid):
		return http.StatusBadRequest, apierr.CodeCommentContentInvalid
	case errors.Is(err, taskdom.ErrCommentActorUnidentified):
		return http.StatusBadRequest, apierr.CodeCommentActorUnidentified
	case errors.Is(err, taskdom.ErrTaskLinkNotFound):
		return http.StatusNotFound, apierr.CodeTaskLinkNotFound
	case errors.Is(err, taskdom.ErrTaskLinkSelf):
		return http.StatusBadRequest, apierr.CodeTaskLinkSelf
	case errors.Is(err, taskdom.ErrTaskLinkDuplicate):
		return http.StatusConflict, apierr.CodeTaskLinkDuplicate
	case errors.Is(err, taskdom.ErrTaskLinkCrossProject):
		return http.StatusBadRequest, apierr.CodeTaskLinkCrossProject
	case errors.Is(err, docdom.ErrDocNotFound):
		return http.StatusNotFound, apierr.CodeDocNotFound
	case errors.Is(err, docdom.ErrDocTitleInvalid):
		return http.StatusBadRequest, apierr.CodeDocTitleInvalid
	case errors.Is(err, docdom.ErrFolderNotFound):
		return http.StatusNotFound, apierr.CodeDocFolderNotFound
	case errors.Is(err, docdom.ErrFolderNameInvalid):
		return http.StatusBadRequest, apierr.CodeDocFolderNameInvalid
	case errors.Is(err, docdom.ErrFolderNotInProject):
		return http.StatusBadRequest, apierr.CodeDocFolderNotInProject
	case errors.Is(err, docdom.ErrFolderSelfParent):
		return http.StatusBadRequest, apierr.CodeDocFolderSelfParent
	case errors.Is(err, docdom.ErrSnapshotNotFound):
		return http.StatusNotFound, apierr.CodeDocSnapshotNotFound
	case errors.Is(err, docdom.ErrActivityNotFound):
		return http.StatusNotFound, apierr.CodeDocActivityNotFound
	case errors.Is(err, docdom.ErrActivityForbidden):
		return http.StatusForbidden, apierr.CodeDocActivityForbidden
	case errors.Is(err, docdom.ErrActivityNotAComment):
		return http.StatusBadRequest, apierr.CodeDocActivityNotAComment
	case errors.Is(err, docdom.ErrCommentContentInvalid):
		return http.StatusBadRequest, apierr.CodeDocCommentContentInvalid
	case errors.Is(err, docdom.ErrCommentActorUnidentified):
		return http.StatusBadRequest, apierr.CodeDocCommentActorUnidentified
	case errors.Is(err, notificationdom.ErrNotificationNotFound):
		return http.StatusNotFound, apierr.CodeNotificationNotFound
	case errors.Is(err, apikeydom.ErrNotFound):
		return http.StatusNotFound, apierr.CodeAPIKeyNotFound
	case errors.Is(err, apikeydom.ErrRevoked):
		return http.StatusUnauthorized, apierr.CodeAPIKeyRevoked
	case errors.Is(err, apikeydom.ErrExpired):
		return http.StatusUnauthorized, apierr.CodeAPIKeyExpired
	case errors.Is(err, apikeydom.ErrNameInvalid):
		return http.StatusBadRequest, apierr.CodeAPIKeyNameInvalid
	case errors.Is(err, apikeydom.ErrNameTooLong):
		return http.StatusBadRequest, apierr.CodeAPIKeyNameTooLong
	case errors.Is(err, apikeydom.ErrForbidden):
		return http.StatusForbidden, apierr.CodeForbidden
	case errors.Is(err, pluginom.ErrNotFound):
		return http.StatusNotFound, apierr.CodePluginNotFound
	case errors.Is(err, pluginom.ErrNameTaken):
		return http.StatusConflict, apierr.CodePluginNameTaken
	// --- Agent errors -------------------------------------------------------
	case errors.Is(err, agentdom.ErrAgentNotFound):
		return http.StatusNotFound, apierr.CodeAgentNotFound
	case errors.Is(err, agentdom.ErrAgentHandleTaken):
		return http.StatusConflict, apierr.CodeAgentHandleTaken
	case errors.Is(err, agentdom.ErrAgentHandleInvalid):
		return http.StatusBadRequest, apierr.CodeAgentHandleInvalid
	case errors.Is(err, agentdom.ErrAgentNameInvalid):
		return http.StatusBadRequest, apierr.CodeAgentNameInvalid
	case errors.Is(err, agentdom.ErrMCPServerNotFound):
		return http.StatusNotFound, apierr.CodeAgentMCPServerNotFound
	case errors.Is(err, agentdom.ErrSkillNotFound):
		return http.StatusNotFound, apierr.CodeAgentSkillNotFound
	case errors.Is(err, agentdom.ErrSkillNameReserved):
		return http.StatusBadRequest, apierr.CodeAgentSkillNameReserved
	case errors.Is(err, agentdom.ErrConversationNotFound):
		return http.StatusNotFound, apierr.CodeAgentConversationNotFound
	case errors.Is(err, agentdom.ErrConversationNotRunning):
		return http.StatusConflict, apierr.CodeAgentConversationNotRunning
	case errors.Is(err, agentdom.ErrConversationAlreadyStopped):
		return http.StatusConflict, apierr.CodeAgentConversationAlreadyStopped
	case errors.Is(err, agentdom.ErrConversationBusy):
		return http.StatusConflict, apierr.CodeAgentConversationBusy
	case errors.Is(err, agentdom.ErrChatSessionNotFound):
		return http.StatusNotFound, apierr.CodeAgentChatSessionNotFound
	case errors.Is(err, agentdom.ErrEnvVarNotFound):
		return http.StatusNotFound, apierr.CodeAgentEnvVarNotFound
	case errors.Is(err, agentdom.ErrEnvVarKeyTaken):
		return http.StatusConflict, apierr.CodeAgentEnvVarKeyTaken
	case errors.Is(err, agentdom.ErrEnvVarKeyInvalid):
		return http.StatusBadRequest, apierr.CodeAgentEnvVarKeyInvalid
	case errors.Is(err, agentdom.ErrEnvVarKeyReserved):
		return http.StatusBadRequest, apierr.CodeAgentEnvVarKeyReserved
	// --- Workflow errors ------------------------------------------------------
	case errors.Is(err, workflowdom.ErrNotFound):
		return http.StatusNotFound, apierr.CodeWorkflowNotFound
	case errors.Is(err, workflowdom.ErrNameInvalid):
		return http.StatusBadRequest, apierr.CodeWorkflowNameInvalid
	case errors.Is(err, workflowdom.ErrNodeNotFound):
		return http.StatusNotFound, apierr.CodeWorkflowNodeNotFound
	case errors.Is(err, workflowdom.ErrNodeDuplicateTask):
		return http.StatusConflict, apierr.CodeWorkflowNodeDuplicateTask
	case errors.Is(err, workflowdom.ErrNodeTaskCrossProject):
		return http.StatusBadRequest, apierr.CodeWorkflowNodeTaskCrossProject
	case errors.Is(err, workflowdom.ErrStatusRuleNotFound):
		return http.StatusNotFound, apierr.CodeWorkflowStatusRuleNotFound
	case errors.Is(err, workflowdom.ErrStatusRuleCrossProject):
		return http.StatusBadRequest, apierr.CodeWorkflowStatusRuleCrossProject
	case errors.Is(err, workflowdom.ErrStatusRuleConflict):
		return http.StatusConflict, apierr.CodeWorkflowStatusRuleConflict
	case errors.Is(err, workflowdom.ErrStatusTransitionNotFound):
		return http.StatusNotFound, apierr.CodeWorkflowStatusTransitionNotFound
	case errors.Is(err, workflowdom.ErrStatusTransitionCrossProject):
		return http.StatusBadRequest, apierr.CodeWorkflowStatusTransitionCrossProject
	case errors.Is(err, workflowdom.ErrStatusTransitionSelfLoop):
		return http.StatusBadRequest, apierr.CodeWorkflowStatusTransitionSelfLoop
	case errors.Is(err, workflowdom.ErrStatusTransitionConflict):
		return http.StatusConflict, apierr.CodeWorkflowStatusTransitionConflict
	case errors.Is(err, workflowdom.ErrEdgeNotFound):
		return http.StatusNotFound, apierr.CodeWorkflowEdgeNotFound
	case errors.Is(err, workflowdom.ErrEdgeSelfLoop):
		return http.StatusBadRequest, apierr.CodeWorkflowEdgeSelfLoop
	case errors.Is(err, workflowdom.ErrEdgeCrossWorkflow):
		return http.StatusBadRequest, apierr.CodeWorkflowEdgeCrossWorkflow
	case errors.Is(err, workflowdom.ErrEdgeCycle):
		return http.StatusBadRequest, apierr.CodeWorkflowEdgeCycle
	case errors.Is(err, workflowdom.ErrEdgeDuplicate):
		return http.StatusConflict, apierr.CodeWorkflowEdgeDuplicate
	case errors.Is(err, workflowdom.ErrNotDraft):
		return http.StatusConflict, apierr.CodeWorkflowNotDraft
	case errors.Is(err, workflowdom.ErrNotActive):
		return http.StatusConflict, apierr.CodeWorkflowNotActive
	case errors.Is(err, workflowdom.ErrArchived):
		return http.StatusConflict, apierr.CodeWorkflowArchived
	case errors.Is(err, workflowdom.ErrActivateNoNodes):
		return http.StatusBadRequest, apierr.CodeWorkflowActivateNoNodes
	case errors.Is(err, workflowdom.ErrActivateDoneStatusUndetermined):
		return http.StatusBadRequest, apierr.CodeWorkflowActivateDoneStatusUndetermined
	case errors.Is(err, workflowdom.ErrActivateTaskMissing):
		return http.StatusBadRequest, apierr.CodeWorkflowActivateTaskMissing
	case errors.Is(err, workflowdom.ErrActivateNoStatusRules):
		return http.StatusBadRequest, apierr.CodeWorkflowActivateNoStatusRules
	default:
		return http.StatusInternalServerError, apierr.CodeInternalError
	}
}

// httpStatusForCode maps an apierr.Code to its conventional HTTP status code.
func httpStatusForCode(code apierr.Code) int {
	switch code {
	case apierr.CodeInvalidCredentials,
		apierr.CodeMissingToken,
		apierr.CodeTokenInvalid,
		apierr.CodeUnauthenticated:
		return http.StatusUnauthorized
	case apierr.CodeUserNotFound:
		return http.StatusNotFound
	case apierr.CodeUsernameTaken,
		apierr.CodeAgentConversationBusy:
		return http.StatusConflict
	case apierr.CodeForbidden:
		return http.StatusForbidden
	case apierr.CodeGlobalRoleNotFound:
		return http.StatusNotFound
	case apierr.CodeGlobalRoleNameTaken:
		return http.StatusConflict
	case apierr.CodeGlobalRoleNameInvalid:
		return http.StatusBadRequest
	case apierr.CodeGlobalRoleHasUsers:
		return http.StatusConflict
	case apierr.CodeProjectNotFound:
		return http.StatusNotFound
	case apierr.CodeProjectNameTaken:
		return http.StatusConflict
	case apierr.CodeProjectNameInvalid,
		apierr.CodeProjectPrefixInvalid:
		return http.StatusBadRequest
	case apierr.CodeProjectRoleNotFound:
		return http.StatusNotFound
	case apierr.CodeProjectRoleNameTaken:
		return http.StatusConflict
	case apierr.CodeProjectRoleNameInvalid:
		return http.StatusBadRequest
	case apierr.CodeProjectRoleHasMembers:
		return http.StatusConflict
	case apierr.CodeProjectMemberNotFound:
		return http.StatusNotFound
	case apierr.CodeProjectMemberAlreadyAdded:
		return http.StatusConflict
	case apierr.CodeTaskNotFound,
		apierr.CodeTaskTypeNotFound,
		apierr.CodeTaskStatusNotFound,
		apierr.CodeSprintNotFound,
		apierr.CodeTaskLinkNotFound,
		apierr.CodeViewNotFound,
		apierr.CodeCustomFieldNotFound,
		apierr.CodeFileNotFound,
		apierr.CodeAttachmentNotFound:
		return http.StatusNotFound
	case apierr.CodeUploadNotPending,
		apierr.CodeAttachmentInvalid,
		apierr.CodeMultipartUploadIDRequired,
		apierr.CodeNotMultipartUpload,
		apierr.CodeUploadIDMismatch,
		apierr.CodeMultipartPartsEmpty,
		apierr.CodeTaskTitleInvalid,
		apierr.CodeEpicCannotHaveParent,
		apierr.CodeTaskCannotBeOwnParent,
		apierr.CodeTaskParentCycleDetected,
		apierr.CodeTaskLinkSelf,
		apierr.CodeTaskLinkCrossProject,
		apierr.CodeTaskTypeNameInvalid,
		apierr.CodeTaskStatusNameInvalid,
		apierr.CodeTaskStatusCategoryInvalid,
		apierr.CodeTaskStatusReorderInvalid,
		apierr.CodeSprintNameInvalid,
		apierr.CodeSprintStatusInvalid,
		apierr.CodeViewNameInvalid,
		apierr.CodeViewTypeInvalid,
		apierr.CodeViewReorderInvalid,
		apierr.CodeCustomFieldKeyInvalid,
		apierr.CodeCustomFieldTypeInvalid,
		apierr.CodeCustomFieldNameInvalid,
		apierr.CodeActivityNotAComment,
		apierr.CodeCommentContentInvalid,
		apierr.CodeCommentActorUnidentified:
		return http.StatusBadRequest
	case apierr.CodeActivityNotFound:
		return http.StatusNotFound
	case apierr.CodeActivityForbidden:
		return http.StatusForbidden
	case apierr.CodeViewIsLastView,
		apierr.CodeSprintAlreadyComplete,
		apierr.CodeTaskLinkDuplicate,
		apierr.CodeCustomFieldKeyTaken,
		apierr.CodeTaskTypeNameReserved,
		apierr.CodeTaskStatusInUseByWorkflow:
		return http.StatusConflict
	case apierr.CodeTaskTypeIsSystem:
		return http.StatusForbidden
	case apierr.CodeDocNotFound,
		apierr.CodeDocFolderNotFound,
		apierr.CodeDocSnapshotNotFound,
		apierr.CodeDocActivityNotFound:
		return http.StatusNotFound
	case apierr.CodeDocTitleInvalid,
		apierr.CodeDocFolderNameInvalid,
		apierr.CodeDocFolderNotInProject,
		apierr.CodeDocFolderSelfParent,
		apierr.CodeDocActivityNotAComment,
		apierr.CodeDocCommentContentInvalid,
		apierr.CodeDocCommentActorUnidentified:
		return http.StatusBadRequest
	case apierr.CodeDocActivityForbidden:
		return http.StatusForbidden
	case apierr.CodeNotificationNotFound:
		return http.StatusNotFound
	case apierr.CodeGitHubIntegrationNotFound,
		apierr.CodeGitHubRepositoryNotFound,
		apierr.CodeGitHubPRNotFound,
		apierr.CodeGitHubPRLinkNotFound:
		return http.StatusNotFound
	case apierr.CodeGitHubPRAlreadyLinked,
		apierr.CodeGitHubBranchAlreadyLinked:
		return http.StatusConflict
	case apierr.CodeGitHubInvalidToken:
		return http.StatusUnprocessableEntity
	case apierr.CodeGitHubWebhookURLRequired:
		return http.StatusInternalServerError
	case apierr.CodeGitHubRepoNotAccessible:
		return http.StatusNotFound
	case apierr.CodeGitHubRepoAlreadyLinked:
		return http.StatusConflict
	case apierr.CodeGitHubWebhookCreationFailed:
		return http.StatusBadRequest
	case apierr.CodeGitHubWebhookURLNotPublic:
		return http.StatusUnprocessableEntity
	case apierr.CodeGitHubTokenInsufficientPermissions:
		return http.StatusForbidden
	case apierr.CodeAPIKeyNotFound:
		return http.StatusNotFound
	case apierr.CodeAPIKeyRevoked, apierr.CodeAPIKeyExpired:
		return http.StatusUnauthorized
	case apierr.CodeAPIKeyNameInvalid, apierr.CodeAPIKeyNameTooLong:
		return http.StatusBadRequest
	case apierr.CodePluginNotFound:
		return http.StatusNotFound
	case apierr.CodePluginNameTaken,
		apierr.CodePluginAlreadyUpToDate,
		apierr.CodePluginDowngradeNotAllowed:
		return http.StatusConflict
	case apierr.CodePayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case apierr.CodeAgentNotFound,
		apierr.CodeAgentTypeNotFound,
		apierr.CodeAgentMCPServerNotFound,
		apierr.CodeAgentSkillNotFound,
		apierr.CodeAgentConversationNotFound,
		apierr.CodeAgentChatSessionNotFound,
		apierr.CodeAgentEnvVarNotFound:
		return http.StatusNotFound
	case apierr.CodeAgentHandleTaken,
		apierr.CodeAgentConversationNotRunning,
		apierr.CodeAgentConversationAlreadyStopped,
		apierr.CodeAgentEnvVarKeyTaken:
		return http.StatusConflict
	case apierr.CodeAgentHandleInvalid,
		apierr.CodeAgentNameInvalid,
		apierr.CodeAgentEnvVarKeyInvalid,
		apierr.CodeAgentEnvVarKeyReserved,
		apierr.CodeAgentSkillNameReserved:
		return http.StatusBadRequest
	case apierr.CodeWorkflowNotFound,
		apierr.CodeWorkflowNodeNotFound,
		apierr.CodeWorkflowStatusRuleNotFound,
		apierr.CodeWorkflowStatusTransitionNotFound,
		apierr.CodeWorkflowEdgeNotFound:
		return http.StatusNotFound
	case apierr.CodeWorkflowNodeDuplicateTask,
		apierr.CodeWorkflowEdgeDuplicate,
		apierr.CodeWorkflowNotDraft,
		apierr.CodeWorkflowNotActive,
		apierr.CodeWorkflowArchived,
		apierr.CodeWorkflowStatusRuleConflict,
		apierr.CodeWorkflowStatusTransitionConflict:
		return http.StatusConflict
	case apierr.CodeWorkflowNameInvalid,
		apierr.CodeWorkflowNodeTaskCrossProject,
		apierr.CodeWorkflowStatusRuleCrossProject,
		apierr.CodeWorkflowStatusTransitionCrossProject,
		apierr.CodeWorkflowStatusTransitionSelfLoop,
		apierr.CodeWorkflowEdgeSelfLoop,
		apierr.CodeWorkflowEdgeCrossWorkflow,
		apierr.CodeWorkflowEdgeCycle,
		apierr.CodeWorkflowActivateNoNodes,
		apierr.CodeWorkflowActivateDoneStatusUndetermined,
		apierr.CodeWorkflowActivateTaskMissing,
		apierr.CodeWorkflowActivateNoStatusRules:
		return http.StatusBadRequest
	case apierr.CodeBadRequest:
		return http.StatusBadRequest
	case apierr.CodePasswordChangeRequired:
		return http.StatusForbidden
	case apierr.CodeInvalidCurrentPassword:
		return http.StatusUnprocessableEntity
	case apierr.CodeInternalError:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
