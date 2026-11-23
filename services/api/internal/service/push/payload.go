package pushsvc

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

const maxBodyRunes = 200

// Payload is the JSON the service worker receives and shows.
type Payload struct {
	// Title and Body are the text shown. English only: a push is rendered on
	// the server, which has no per-user locale.
	Title string `json:"title"`
	Body  string `json:"body"`
	// URL is the in-app path to open when the notification is clicked.
	URL string `json:"url"`
	// ID is the notification id, also used as the tag so a repeat replaces the
	// earlier one instead of stacking.
	ID string `json:"id"`
}

// RenderPayload renders n as a Web Push payload.
func RenderPayload(n *notificationdom.Notification) Payload {
	actor := n.ActorFullName
	if actor == "" {
		actor = "Someone"
	}
	task := ""
	if n.TaskID != nil {
		task = fmt.Sprintf("#%d — %s", n.TaskNumber, n.TaskTitle)
	}

	var body string
	switch n.Type {
	case notificationdom.NotificationTypeAssigned:
		body = fmt.Sprintf("%s assigned you %s", actor, task)
	case notificationdom.NotificationTypeQuestion:
		title := n.ConversationTitle
		if title == "" {
			title = "a conversation"
		}
		body = fmt.Sprintf("%s asked you a question in %s", actor, title)
	case notificationdom.NotificationTypeAutomation:
		body = n.Message
		if task != "" {
			body = strings.TrimSpace(fmt.Sprintf("%s %s", n.Message, task))
		}
	default: // mentioned
		if n.DocumentID != nil {
			title := n.DocumentTitle
			if title == "" {
				title = "a document"
			}
			body = fmt.Sprintf("%s mentioned you in document %s", actor, title)
		} else {
			body = fmt.Sprintf("%s mentioned you in %s", actor, task)
		}
	}
	if r := []rune(body); len(r) > maxBodyRunes {
		body = string(r[:maxBodyRunes]) + "…"
	}

	title := n.ProjectName
	if title == "" {
		title = "Paca"
	}
	return Payload{Title: title, Body: strings.TrimSpace(body), URL: notificationURL(n), ID: n.ID.String()}
}

// notificationURL is the in-app path that opens what n is about, mirroring the
// web client's notification bell navigation.
func notificationURL(n *notificationdom.Notification) string {
	switch {
	case n.Type == notificationdom.NotificationTypeQuestion && n.ConversationID != nil:
		if n.ProjectID != uuid.Nil {
			return fmt.Sprintf("/projects/%s/conversations/%s", n.ProjectID, n.ConversationID)
		}
		return fmt.Sprintf("/conversations/%s", n.ConversationID)
	case n.ProjectID == uuid.Nil:
		return "/"
	case n.DocumentID != nil:
		return fmt.Sprintf("/projects/%s/docs/%s", n.ProjectID, n.DocumentID)
	case n.TaskID != nil:
		return fmt.Sprintf("/projects/%s/tasks/%s", n.ProjectID, n.TaskID)
	default:
		return fmt.Sprintf("/projects/%s", n.ProjectID)
	}
}
