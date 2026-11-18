package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
)

// CreateChannelRequest is the body of POST /projects/{id}/channels. kind is
// also read by the router gate, which authorizes the channel being created.
type CreateChannelRequest struct {
	Kind      string      `json:"kind"`
	Name      string      `json:"name"`
	Topic     string      `json:"topic"`
	MemberIDs []uuid.UUID `json:"member_ids"`
}

// UpdateChannelRequest is the body of PATCH /projects/{id}/channels/{channelId}.
type UpdateChannelRequest struct {
	Name  *string `json:"name"`
	Topic *string `json:"topic"`
}

// PostMessageRequest is the body of POST .../messages. content is a list of
// BlockNote-style blocks.
type PostMessageRequest struct {
	Content      json.RawMessage `json:"content"`
	ThreadRootID *uuid.UUID      `json:"thread_root_id"`
}

// EditMessageRequest is the body of PATCH .../messages/{messageId}.
type EditMessageRequest struct {
	Content json.RawMessage `json:"content"`
}

// MarkReadRequest is the body of POST .../read.
type MarkReadRequest struct {
	Seq int64 `json:"seq"`
}

// NotificationPrefRequest is the body of PATCH .../notification-pref.
type NotificationPrefRequest struct {
	Pref string `json:"pref"`
}

// PeerResponse is the other party of a direct message.
type PeerResponse struct {
	AccountID uuid.UUID `json:"account_id"`
	Name      string    `json:"name"`
	// Kind is user, agent or service_account.
	Kind string `json:"kind"`
}

func peerFromEntity(p *chatdom.DMPeer) *PeerResponse {
	if p == nil {
		return nil
	}
	return &PeerResponse{AccountID: p.AccountID, Name: p.Name, Kind: p.Kind}
}

// StartDMRequest is the body of POST /chat/dms. account_id is also read by the
// router gate, which authorizes the conversation being created.
type StartDMRequest struct {
	AccountID uuid.UUID `json:"account_id"`
}

// DirectoryEntryResponse is an account a direct message can be started with.
type DirectoryEntryResponse struct {
	AccountID   uuid.UUID `json:"account_id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Description string    `json:"description"`
}

// ChannelResponse is the API form of a channel with the caller's read state.
type ChannelResponse struct {
	ID          uuid.UUID  `json:"id"`
	Scope       string     `json:"scope"`
	ProjectID   *uuid.UUID `json:"project_id"`
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	Topic       string     `json:"topic"`
	LastSeq     int64      `json:"last_seq"`
	ArchivedAt  *time.Time `json:"archived_at"`
	CreatedBy   *uuid.UUID `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	IsMember    bool       `json:"is_member"`
	LastReadSeq int64      `json:"last_read_seq"`
	UnreadCount int64      `json:"unread_count"`
	// Peer is set for a direct message.
	Peer *PeerResponse `json:"peer"`
	// MentionCount is how many unread messages mention the caller.
	MentionCount int64 `json:"mention_count"`
	// Starred: the caller starred it.
	Starred bool `json:"starred"`
}

// ChannelFromEntity converts a channel.
func ChannelFromEntity(c *chatdom.Channel) ChannelResponse {
	return ChannelResponse{
		ID: c.ID, Scope: c.Scope, ProjectID: c.ProjectID, Kind: c.Kind, Name: c.Name, Topic: c.Topic,
		LastSeq: c.LastSeq, ArchivedAt: c.ArchivedAt, CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt,
		IsMember: c.IsMember, LastReadSeq: c.LastReadSeq, UnreadCount: c.UnreadCount, Peer: peerFromEntity(c.Peer), MentionCount: c.MentionCount, Starred: c.Starred,
	}
}

// ReactionResponse is one emoji's reactions on a message.
type ReactionResponse struct {
	Emoji string `json:"emoji"`
	Count int64  `json:"count"`
	Mine  bool   `json:"mine"`
}

// MessageResponse is the API form of a message. A deleted message keeps its
// seq and author but has empty content.
type MessageResponse struct {
	ID           uuid.UUID          `json:"id"`
	ChannelID    uuid.UUID          `json:"channel_id"`
	Seq          int64              `json:"seq"`
	ThreadRootID *uuid.UUID         `json:"thread_root_id"`
	AuthorID     uuid.UUID          `json:"author_id"`
	AuthorName   string             `json:"author_name"`
	Content      json.RawMessage    `json:"content"`
	EditedAt     *time.Time         `json:"edited_at"`
	DeletedAt    *time.Time         `json:"deleted_at"`
	CreatedAt    time.Time          `json:"created_at"`
	ReplyCount   int64              `json:"reply_count"`
	Reactions    []ReactionResponse `json:"reactions"`
	// Saved and Following are the caller's own flags.
	Saved     bool `json:"saved"`
	Following bool `json:"following"`
	// RunID is the agent run the message reports on (a reply of an agent in a direct message).
	RunID *uuid.UUID `json:"run_id"`
}

// MessageFromEntity converts a message.
func MessageFromEntity(m *chatdom.Message) MessageResponse {
	rs := make([]ReactionResponse, len(m.Reactions))
	for i, r := range m.Reactions {
		rs[i] = ReactionResponse{Emoji: r.Emoji, Count: r.Count, Mine: r.Mine}
	}
	return MessageResponse{
		ID: m.ID, ChannelID: m.ChannelID, Seq: m.Seq, ThreadRootID: m.ThreadRootID, AuthorID: m.AuthorID,
		AuthorName: m.AuthorName, Content: m.Content, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt,
		CreatedAt: m.CreatedAt, ReplyCount: m.ReplyCount, Reactions: rs, Saved: m.Saved, Following: m.Following, RunID: m.RunID,
	}
}

// MessageListResponse is a page of messages, oldest first. has_more says
// whether more exist beyond the page in the direction that was requested.
type MessageListResponse struct {
	Items   []MessageResponse `json:"items"`
	HasMore bool              `json:"has_more"`
}

// UnreadSummaryItem is the caller's unread count for one project.
type UnreadSummaryItem struct {
	// ProjectID is null for the caller's direct messages.
	ProjectID   *uuid.UUID `json:"project_id"`
	ProjectName string     `json:"project_name"`
	Unread      int64      `json:"unread"`
	Channels    int64      `json:"channels"`
}

// InboxItemResponse is one followed channel in the cross-project Inbox.
type InboxItemResponse struct {
	// ProjectID is null for a direct message, whose peer is set instead.
	ProjectID     *uuid.UUID    `json:"project_id"`
	Peer          *PeerResponse `json:"peer"`
	ProjectName   string        `json:"project_name"`
	ChannelID     uuid.UUID     `json:"channel_id"`
	ChannelName   string        `json:"channel_name"`
	Kind          string        `json:"kind"`
	LastSeq       int64         `json:"last_seq"`
	LastReadSeq   int64         `json:"last_read_seq"`
	UnreadCount   int64         `json:"unread_count"`
	LastMessageAt *time.Time    `json:"last_message_at"`
	LastAuthor    string        `json:"last_author"`
	Preview       string        `json:"preview"`
	// MentionCount is how many unread messages in the channel mention the caller.
	MentionCount int64 `json:"mention_count"`
	// Starred: the caller starred the channel.
	Starred bool `json:"starred"`
}

// MarkAllReadRequest is the body of POST /users/me/chat/read-all; a project id
// limits it to one project.
type MarkAllReadRequest struct {
	ProjectID *uuid.UUID `json:"project_id"`
}

// MessageRefResponse is one message of the Mentions, Threads or Saved list,
// with where it lives. For a thread it is the root.
type MessageRefResponse struct {
	// ProjectID is null for a direct message, whose peer is set instead.
	ProjectID    *uuid.UUID    `json:"project_id"`
	ProjectName  string        `json:"project_name"`
	Peer         *PeerResponse `json:"peer"`
	ChannelID    uuid.UUID     `json:"channel_id"`
	ChannelName  string        `json:"channel_name"`
	Kind         string        `json:"kind"`
	MessageID    uuid.UUID     `json:"message_id"`
	Seq          int64         `json:"seq"`
	ThreadRootID *uuid.UUID    `json:"thread_root_id"`
	// RootSeq is the thread root's seq (the message's own, for a thread row).
	RootSeq     *int64    `json:"root_seq"`
	AuthorID    uuid.UUID `json:"author_id"`
	AuthorName  string    `json:"author_name"`
	Preview     string    `json:"preview"`
	RootPreview string    `json:"root_preview"`
	CreatedAt   time.Time `json:"created_at"`
	ActivityAt  time.Time `json:"activity_at"`
	Unread      bool      `json:"unread"`
	UnreadCount int64     `json:"unread_count"`
	ReplyCount  int64     `json:"reply_count"`
}

// MessageRefFromEntity converts a list entry.
func MessageRefFromEntity(r chatdom.MessageRef) MessageRefResponse {
	return MessageRefResponse{
		ProjectID: r.ProjectID, ProjectName: r.ProjectName, Peer: peerFromEntity(r.Peer), ChannelID: r.ChannelID,
		ChannelName: r.ChannelName, Kind: r.Kind, MessageID: r.MessageID, Seq: r.Seq, ThreadRootID: r.ThreadRootID, RootSeq: r.RootSeq,
		AuthorID: r.AuthorID, AuthorName: r.AuthorName, Preview: r.Preview, RootPreview: r.RootPreview,
		CreatedAt: r.CreatedAt, ActivityAt: r.ActivityAt, Unread: r.Unread, UnreadCount: r.UnreadCount, ReplyCount: r.ReplyCount,
	}
}

// AgentRunResponse is one run of an agent in a direct message.
type AgentRunResponse struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AgentRunsResponse is the agent side of a direct message.
type AgentRunsResponse struct {
	Replies bool               `json:"replies"`
	Working bool               `json:"working"`
	Runs    []AgentRunResponse `json:"runs"`
}

// AgentRunsFromEntity maps the agent side of a direct message.
func AgentRunsFromEntity(a *chatdom.AgentRuns) AgentRunsResponse {
	out := AgentRunsResponse{Replies: a.Replies, Working: a.Working, Runs: make([]AgentRunResponse, len(a.Runs))}
	for i, r := range a.Runs {
		out.Runs[i] = AgentRunResponse{ID: r.ID, Title: r.Title, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	}
	return out
}
