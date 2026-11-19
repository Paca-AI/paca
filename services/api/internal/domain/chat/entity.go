// Package chatdom defines the chat aggregate: channels, their members,
// messages and reactions. See docs/architecture/chat-and-meetings.md.
package chatdom

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Channel kinds.
const (
	KindPublic  = "public"
	KindPrivate = "private"
	KindVoice   = "voice"
)

// Channel scopes.
const (
	ScopeProject   = "project"
	ScopeWorkspace = "workspace"
)

// Notification preferences of a channel member.
const (
	PrefDefault  = "default"
	PrefAll      = "all"
	PrefMentions = "mentions"
	PrefNone     = "none"
)

// GeneralChannelName is the name of the channel every project starts with.
const GeneralChannelName = "general"

// Limits.
const (
	MaxNameLen     = 80
	MaxTopicLen    = 250
	MaxEmojiLen    = 32
	MaxContentText = 20000
	DefaultPage    = 50
	MaxPage        = 100
)

// KindDM is a one-to-one direct message between two accounts of any kind.
const KindDM = "dm"

// Account kinds a direct message can be addressed to.
const (
	AccountUser           = "user"
	AccountAgent          = "agent"
	AccountServiceAccount = "service_account"
)

// DMPeer is the other party of a direct message, as the caller sees it.
type DMPeer struct {
	AccountID uuid.UUID
	Name      string
	// Kind is the account kind: user, agent or service_account.
	Kind string
}

// DirectoryEntry is an account the caller may start a direct message with.
type DirectoryEntry struct {
	AccountID   uuid.UUID
	Name        string
	Kind        string
	Description string
}

// Channel is a conversation space inside a project, or a direct message.
type Channel struct {
	ID         uuid.UUID
	Scope      string
	ProjectID  *uuid.UUID
	Kind       string
	Name       string
	Topic      string
	LastSeq    int64
	ArchivedAt *time.Time
	CreatedBy  *uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time

	// Peer is the other party of a direct message; nil for any other channel.
	Peer *DMPeer
	// MentionCount is how many unread messages mention the caller.
	MentionCount int64
	// Starred: the caller starred it.
	Starred bool

	// Per-caller read state; populated by the list and get queries only.
	IsMember    bool
	LastReadSeq int64
	UnreadCount int64
}

// Member is one channel_members row.
type Member struct {
	ChannelID        uuid.UUID
	AccountID        uuid.UUID
	LastReadSeq      int64
	NotificationPref string
	JoinedAt         time.Time
}

// Message is one chat message. Content is a BlockNote-compatible block list.
type Message struct {
	ID           uuid.UUID
	ChannelID    uuid.UUID
	Seq          int64
	ThreadRootID *uuid.UUID
	AuthorID     uuid.UUID
	AuthorName   string
	Content      json.RawMessage
	ContentText  string
	EditedAt     *time.Time
	DeletedAt    *time.Time
	CreatedAt    time.Time
	ReplyCount   int64
	Reactions    []ReactionCount
	// Saved and Following are the caller's own flags: the message is in their
	// Saved list, and they follow the thread it starts.
	Saved     bool
	Following bool
	// RunID is the agent run this message reports on, for a message an agent
	// wrote in a direct message.
	RunID *uuid.UUID
}

// ReactionCount is how many accounts reacted with one emoji, and whether the
// caller is one of them.
type ReactionCount struct {
	Emoji string
	Count int64
	Mine  bool
}

// MessageFilter selects a page of messages.
type MessageFilter struct {
	// AfterSeq returns messages with seq > AfterSeq, oldest first (catch-up).
	AfterSeq *int64
	// BeforeSeq returns messages with seq < BeforeSeq, newest page first
	// (scroll back); with neither bound, the latest page.
	BeforeSeq *int64
	// ThreadRootID lists the replies of one thread instead of the channel's
	// top-level messages.
	ThreadRootID *uuid.UUID
	Limit        int
}

// ChannelFilter selects channels of a project.
type ChannelFilter struct {
	IncludeArchived bool
}

// UnreadSummary is the caller's unread counts for one project.
type UnreadSummary struct {
	// ProjectID is nil for the caller's direct messages.
	ProjectID   *uuid.UUID
	ProjectName string
	Unread      int64
	Channels    int64
}

// InboxItem is one followed channel in the caller's cross-project Inbox, with
// the newest message as a preview.
type InboxItem struct {
	// ProjectID is nil for a direct message, whose Peer is set instead.
	ProjectID     *uuid.UUID
	Peer          *DMPeer
	ProjectName   string
	ChannelID     uuid.UUID
	ChannelName   string
	Kind          string
	LastSeq       int64
	LastReadSeq   int64
	UnreadCount   int64
	LastMessageAt *time.Time
	LastAuthor    string
	Preview       string
	// MentionCount is how many unread messages in the channel mention the caller.
	MentionCount int64
	// Starred: the caller starred the channel.
	Starred bool
}

// MessageRef is one message in the caller's Mentions, Threads or Saved list,
// with where it lives. For a thread it is the thread's root, with the latest
// reply as the preview and the caller's unread replies counted.
type MessageRef struct {
	ProjectID    *uuid.UUID
	ProjectName  string
	Peer         *DMPeer
	ChannelID    uuid.UUID
	ChannelName  string
	Kind         string
	MessageID    uuid.UUID
	Seq          int64
	ThreadRootID *uuid.UUID
	// RootSeq is the seq of the thread root: the message's own for a thread, its
	// thread's for a reply, nil for a top-level message outside a thread.
	RootSeq    *int64
	AuthorID   uuid.UUID
	AuthorName string
	Preview    string
	// RootPreview is the text of the thread's root, for a thread.
	RootPreview string
	CreatedAt   time.Time
	// Unread: for a mention, the message is past the caller's read pointer; for
	// a thread, UnreadCount replies are. A saved message is never "unread".
	Unread      bool
	UnreadCount int64
	ReplyCount  int64
	// ActivityAt is when the thread last had a reply (the message time otherwise).
	ActivityAt time.Time
}

// MaxMessageRefs bounds a Mentions, Threads or Saved page.
const MaxMessageRefs = 50

// InboxFilter narrows the Inbox.
type InboxFilter struct {
	UnreadOnly bool
	ProjectID  *uuid.UUID
	Limit      int
}

// MaxInbox bounds one Inbox page; DefaultInbox is its default size.
const (
	DefaultInbox = 50
	MaxInbox     = 100
	// PreviewLen is the longest message preview, in characters.
	PreviewLen = 160
)

// AgentLink ties a direct message with a global agent to the agent's chat
// session: what the person writes goes to the agent, its reply comes back.
type AgentLink struct {
	ChannelID uuid.UUID
	AgentID   uuid.UUID
	// AccountID is the person on the other side.
	AccountID uuid.UUID
	SessionID *uuid.UUID
	// ForwardedSeq is the newest channel message already handed to the agent.
	ForwardedSeq int64
	// ConversationID is the run whose reply is awaited; BridgedIndex the last of
	// its events already turned into a message (-1: none).
	ConversationID *uuid.UUID
	BridgedIndex   int
}

// AgentRun is one run (conversation) of the agent in a direct message.
type AgentRun struct {
	ID        uuid.UUID
	Title     string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AgentRuns is the agent's side of a direct message: who it is and what it did.
type AgentRuns struct {
	// Replies is false when the peer is an agent that cannot answer in a direct
	// message (a project's agent): the channel then only holds the writing.
	Replies bool
	// Working: the agent is answering (or about to).
	Working bool
	Runs    []AgentRun
}

// MaxAgentRuns bounds the runs listed for a direct message.
const MaxAgentRuns = 30
