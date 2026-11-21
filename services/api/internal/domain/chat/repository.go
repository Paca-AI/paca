package chatdom

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the storage contract of the chat aggregate.
type Repository interface {
	// ListChannels returns the project's channels the caller's IAM scope
	// allows (see iam.WithScope, kind "channel"), with the caller's read state.
	ListChannels(ctx context.Context, projectID, accountID uuid.UUID, f ChannelFilter) ([]*Channel, error)
	// FindChannel returns one channel of the project with the caller's read
	// state, or ErrChannelNotFound.
	// FindChannel returns one channel with the caller's read state, or
	// ErrChannelNotFound. A projectID of uuid.Nil means a workspace-scope
	// channel (a direct message); otherwise the channel must belong to it.
	FindChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID) (*Channel, error)
	CreateChannel(ctx context.Context, c *Channel, members []uuid.UUID) error
	// UpdateChannel changes name and topic.
	UpdateChannel(ctx context.Context, projectID, channelID uuid.UUID, name, topic string) error
	SetArchived(ctx context.Context, projectID, channelID uuid.UUID, archived bool) error
	// EnsureGeneral creates the project's #general channel when it has none.
	EnsureGeneral(ctx context.Context, projectID uuid.UUID, createdBy *uuid.UUID) error

	AddMember(ctx context.Context, channelID, accountID uuid.UUID) error
	RemoveMember(ctx context.Context, channelID, accountID uuid.UUID) error
	// IsProjectMember reports whether accountID is an active member of the project.
	IsProjectMember(ctx context.Context, projectID, accountID uuid.UUID) (bool, error)
	// SetNotificationPref changes the caller's preference, joining nothing.
	SetNotificationPref(ctx context.Context, channelID, accountID uuid.UUID, pref string) error

	// CreateMessage allocates the next seq of the channel and stores m.
	CreateMessage(ctx context.Context, m *Message) error
	ListMessages(ctx context.Context, channelID, accountID uuid.UUID, f MessageFilter) ([]*Message, bool, error)
	FindMessage(ctx context.Context, channelID, messageID, accountID uuid.UUID) (*Message, error)
	// EditMessage rewrites a message only when authorID wrote it.
	EditMessage(ctx context.Context, channelID, messageID, authorID uuid.UUID, content []byte, contentText string) error
	// DeleteMessage soft-deletes a message; authorID restricts it to the
	// author's own message when non-nil.
	DeleteMessage(ctx context.Context, channelID, messageID uuid.UUID, authorID *uuid.UUID) error

	AddReaction(ctx context.Context, messageID, accountID uuid.UUID, emoji string) error
	RemoveReaction(ctx context.Context, messageID, accountID uuid.UUID, emoji string) error

	// MarkRead moves the caller's read pointer up to seq (never back),
	// joining the channel's member list when the caller was only following it
	// implicitly.
	MarkRead(ctx context.Context, channelID, accountID uuid.UUID, seq int64) error
	// FindOrCreateDM returns the direct message between two accounts, creating
	// it (and both memberships) when the pair has none. a and b may be any
	// account kinds. Safe to call concurrently for one pair.
	FindOrCreateDM(ctx context.Context, a, b uuid.UUID) (channelID uuid.UUID, created bool, err error)
	// ListDMs returns the caller's direct messages, newest activity first, with
	// the other party and read state. The IAM scope attached to ctx for kind
	// "channel" (iam.WithScope) decides which ones the caller may read.
	ListDMs(ctx context.Context, accountID uuid.UUID) ([]*Channel, error)
	// AccountKind returns the kind of an existing, not deleted account.
	AccountKind(ctx context.Context, id uuid.UUID) (string, bool, error)
	// Directory searches accounts the caller may message: people they share a
	// project with, those projects' agents and service accounts, and global
	// agents. kind narrows to one account kind when non-empty.
	Directory(ctx context.Context, accountID uuid.UUID, query, kind string, limit int) ([]DirectoryEntry, error)
	// RecordMessageEffects stores what a new message implies, in one transaction:
	// its mentions (of existing accounts other than the author), the author's
	// follow of the thread it replies in, the follows of the root's author and of
	// the mentioned accounts, and the mentioned project members' following of a
	// public channel (so the mention reaches their Inbox).
	RecordMessageEffects(ctx context.Context, m *Message, mentionIDs []uuid.UUID) error
	// SetMentions replaces the mentions of an edited message.
	SetMentions(ctx context.Context, messageID, authorID uuid.UUID, mentionIDs []uuid.UUID) error
	// FollowThread follows (rootID, a top-level message of the channel) from its
	// newest reply on; UnfollowThread stops. MarkThreadRead moves the pointer up.
	FollowThread(ctx context.Context, accountID, channelID, rootID uuid.UUID) error
	UnfollowThread(ctx context.Context, accountID, rootID uuid.UUID) error
	MarkThreadRead(ctx context.Context, accountID, channelID, rootID uuid.UUID, seq int64) error
	// SetStarred stars or unstars a channel for the caller, following it first
	// when they were not.
	SetStarred(ctx context.Context, accountID, channelID uuid.UUID, starred bool) error
	SaveMessage(ctx context.Context, accountID, channelID, messageID uuid.UUID) error
	UnsaveMessage(ctx context.Context, accountID, messageID uuid.UUID) error
	// ListMentions, ListThreads and ListSaved page the caller's lists. The IAM
	// scopes attached to ctx (per project, and for direct messages) decide which
	// messages they may see.
	ListMentions(ctx context.Context, accountID uuid.UUID, limit int) ([]MessageRef, error)
	ListThreads(ctx context.Context, accountID uuid.UUID, limit int) ([]MessageRef, error)
	ListSaved(ctx context.Context, accountID uuid.UUID, limit int) ([]MessageRef, error)
	// SearchMessages returns the messages whose text contains query, newest
	// first, in the channels the caller may read (the IAM scopes attached to ctx,
	// as for the lists above); projectID narrows to one project.
	SearchMessages(ctx context.Context, accountID uuid.UUID, query string, projectID *uuid.UUID, limit int) ([]MessageRef, error)
	// ChannelRecipients returns the members of a channel that are still active
	// members of its project: whoever should hear about its activity.
	ChannelRecipients(ctx context.Context, channelID uuid.UUID) ([]uuid.UUID, error)
	// ListInbox returns the channels the caller follows in projects they still
	// belong to, unread first, then by latest message. The IAM scopes attached
	// to ctx per project (iam.WithProjectScopes, kind "channel") decide which
	// channels, and so which previews, the caller may see.
	ListInbox(ctx context.Context, accountID uuid.UUID, f InboxFilter) ([]InboxItem, error)
	// MemberProjectIDs returns the projects the account is an active member of.
	MemberProjectIDs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error)
	// MarkAllRead moves every read pointer of the caller to its channel's head
	// (within one project when projectID is set).
	MarkAllRead(ctx context.Context, accountID uuid.UUID, projectID *uuid.UUID) error
	// UnreadByProject returns the caller's unread message counts per project.
	UnreadByProject(ctx context.Context, accountID uuid.UUID) ([]UnreadSummary, error)

	// EnsureAgentLink creates the channel's agent link when it has none and
	// returns it either way.
	EnsureAgentLink(ctx context.Context, channelID, agentID, accountID uuid.UUID) (*AgentLink, error)
	// FindAgentLinkBySession returns the link whose agent session is sessionID,
	// or nil.
	FindAgentLinkBySession(ctx context.Context, sessionID uuid.UUID) (*AgentLink, error)
	// ClaimForward moves ForwardedSeq from `from` to `to` and reports whether it
	// was still `from`: of two callers racing to forward, one wins. ReleaseForward
	// undoes a claim whose hand-off failed.
	ClaimForward(ctx context.Context, channelID uuid.UUID, from, to int64) (bool, error)
	ReleaseForward(ctx context.Context, channelID uuid.UUID, from, to int64) error
	// MessagesToForward returns the not deleted top-level messages of authorID in
	// the channel with seq > after, oldest first.
	MessagesToForward(ctx context.Context, channelID, authorID uuid.UUID, after int64) ([]*Message, error)
	// SetAgentRun records the session and run the agent now answers in; moving to
	// another run restarts BridgedIndex.
	SetAgentRun(ctx context.Context, channelID, sessionID, conversationID uuid.UUID) error
	SetBridgedIndex(ctx context.Context, channelID uuid.UUID, index int) error
	// AgentReply returns the text the agent wrote in a run in the events with
	// index after < i <= upTo.
	AgentReply(ctx context.Context, conversationID uuid.UUID, after, upTo int) (string, error)
	// ResetAgentRun detaches the channel from its agent session, so the next
	// message starts a new conversation.
	ResetAgentRun(ctx context.Context, channelID uuid.UUID) error
	// ListAgentRuns returns the runs of the link's session, and the ones the
	// person's channel mentions of the agent started, newest first.
	ListAgentRuns(ctx context.Context, link *AgentLink, limit int) ([]AgentRun, error)
	// ProjectMemberID returns the project_members id of an active member of the
	// project, and false when the account is not one.
	ProjectMemberID(ctx context.Context, projectID, accountID uuid.UUID) (uuid.UUID, bool, error)
	// AgentAccountOfMember returns the agent account behind a project member, and
	// false when the member is not an active agent of the project.
	AgentAccountOfMember(ctx context.Context, projectID, memberID uuid.UUID) (uuid.UUID, bool, error)
	// CreateAgentRun links an agent run to the message that started it.
	CreateAgentRun(ctx context.Context, r *ChannelAgentRun) error
	// FindAgentRun returns the link of a run, or nil when no channel started it.
	FindAgentRun(ctx context.Context, conversationID uuid.UUID) (*ChannelAgentRun, error)
	// ClaimAgentRunPost marks the run's reply as posted and reports whether this
	// call did it: of two deliveries of one turn_end, one wins.
	// ReleaseAgentRunPost undoes a claim whose post failed.
	ClaimAgentRunPost(ctx context.Context, conversationID uuid.UUID) (bool, error)
	ReleaseAgentRunPost(ctx context.Context, conversationID uuid.UUID) error
	// IsGlobalAgent reports whether the account is an agent that answers direct
	// messages (a global agent).
	IsGlobalAgent(ctx context.Context, accountID uuid.UUID) (bool, error)
}

// AgentTrigger starts an agent run for a message that mentions the agent. It
// is gated like every other non-HTTP trigger: memberID must be allowed to use
// the agent and its environment.
type AgentTrigger interface {
	TriggerChannelMention(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string) (conversationID uuid.UUID, err error)
}

// Service is the chat use-case contract.
type Service interface {
	ListChannels(ctx context.Context, projectID, accountID uuid.UUID, f ChannelFilter) ([]*Channel, error)
	GetChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID) (*Channel, error)
	CreateChannel(ctx context.Context, projectID, accountID uuid.UUID, kind, name, topic string, memberIDs []uuid.UUID) (*Channel, error)
	UpdateChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID, name, topic *string) (*Channel, error)
	ArchiveChannel(ctx context.Context, projectID, channelID uuid.UUID, archived bool) error
	JoinChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID) error
	LeaveChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID) error
	AddChannelMember(ctx context.Context, projectID, channelID, accountID uuid.UUID) error
	SetNotificationPref(ctx context.Context, projectID, channelID, accountID uuid.UUID, pref string) error

	PostMessage(ctx context.Context, projectID, channelID, authorID uuid.UUID, content []byte, threadRootID *uuid.UUID) (*Message, error)
	ListMessages(ctx context.Context, projectID, channelID, accountID uuid.UUID, f MessageFilter) ([]*Message, bool, error)
	EditMessage(ctx context.Context, projectID, channelID, messageID, authorID uuid.UUID, content []byte) (*Message, error)
	DeleteOwnMessage(ctx context.Context, projectID, channelID, messageID, authorID uuid.UUID) error
	ModerateDeleteMessage(ctx context.Context, projectID, channelID, messageID uuid.UUID) error
	AddReaction(ctx context.Context, projectID, channelID, messageID, accountID uuid.UUID, emoji string) error
	RemoveReaction(ctx context.Context, projectID, channelID, messageID, accountID uuid.UUID, emoji string) error
	MarkRead(ctx context.Context, projectID, channelID, accountID uuid.UUID, seq int64) error
	UnreadSummary(ctx context.Context, accountID uuid.UUID) ([]UnreadSummary, error)
	// StartDM opens (or finds) the direct message with another account.
	// AgentRuns returns the agent side of a direct message the caller reads.
	AgentRuns(ctx context.Context, channelID, accountID uuid.UUID) (*AgentRuns, error)
	// NewAgentConversation makes the next message to the agent start a fresh
	// conversation instead of continuing the current one.
	NewAgentConversation(ctx context.Context, channelID, accountID uuid.UUID) error
	StartDM(ctx context.Context, accountID, peerID uuid.UUID) (*Channel, error)
	// ListDMs lists the caller's direct messages; ctx carries their IAM scope
	// for kind "channel".
	ListDMs(ctx context.Context, accountID uuid.UUID) ([]*Channel, error)
	// Directory searches accounts the caller can message.
	Directory(ctx context.Context, accountID uuid.UUID, query, kind string) ([]DirectoryEntry, error)
	FollowThread(ctx context.Context, projectID, channelID, rootID, accountID uuid.UUID, follow bool) error
	MarkThreadRead(ctx context.Context, projectID, channelID, rootID, accountID uuid.UUID, seq int64) error
	StarChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID, starred bool) error
	SaveMessage(ctx context.Context, projectID, channelID, messageID, accountID uuid.UUID, saved bool) error
	// Mentions, Threads and Saved are the Inbox's other lists; ctx carries the
	// caller's IAM scopes as for Inbox.
	Mentions(ctx context.Context, accountID uuid.UUID) ([]MessageRef, error)
	Threads(ctx context.Context, accountID uuid.UUID) ([]MessageRef, error)
	Saved(ctx context.Context, accountID uuid.UUID) ([]MessageRef, error)
	// Search finds messages by their text across the caller's channels; ctx
	// carries their IAM scopes as for Inbox. A query shorter than MinSearchLen
	// finds nothing.
	Search(ctx context.Context, accountID uuid.UUID, query string, projectID *uuid.UUID) ([]MessageRef, error)
	// Inbox lists the caller's followed channels across projects. ctx carries
	// the caller's per-project IAM scopes (see ListInbox).
	Inbox(ctx context.Context, accountID uuid.UUID, f InboxFilter) ([]InboxItem, error)
	// InboxProjects returns the projects the Inbox spans, for the handler to
	// compute per-project scopes for.
	InboxProjects(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error)
	MarkAllRead(ctx context.Context, accountID uuid.UUID, projectID *uuid.UUID) error
}

// Event types the chat service announces. Realtime delivers the first four to
// the channel's room and an unread nudge to the members' own rooms; none of
// them carries message content, clients fetch it with after_seq.
const (
	EventMessageNew      = "chat.message.new"
	EventMessageUpdated  = "chat.message.updated"
	EventMessageDeleted  = "chat.message.deleted"
	EventReactionChanged = "chat.reaction.changed"
	// EventChannelChanged says a channel was created, renamed or archived. It
	// reaches the project's sockets that may read that channel.
	EventChannelChanged = "chat.channel.changed"
	// EventDMCreated tells the other party a direct message with them exists.
	EventDMCreated = "chat.dm.created"
	// EventAccessChanged says who may read a channel may have changed (a
	// member joined, left or was added): sockets in its room are re-checked.
	EventAccessChanged = "chat.channel.access_changed"
)

// Notification is one announcement.
type Notification struct {
	Type      string
	ProjectID uuid.UUID
	ChannelID uuid.UUID
	MessageID *uuid.UUID
	// Seq is the message's seq (or the channel's last_seq for a channel event).
	Seq          int64
	ThreadRootID *uuid.UUID
	ActorID      uuid.UUID
	// Recipients get the unread nudge in their own rooms.
	Recipients []uuid.UUID
}

// Notifier announces chat activity. It is best effort: an announcement that
// cannot be sent is dropped, since clients also catch up on their own.
type Notifier interface {
	Notify(ctx context.Context, n Notification)
}

// AgentResponder hands a message to a global agent's chat session. Send starts
// a session when sessionID is nil. It returns ErrAgentBusy while the agent is
// still answering the previous message.
type AgentResponder interface {
	Send(ctx context.Context, agentID, accountID uuid.UUID, sessionID *uuid.UUID, text string) (session, conversation uuid.UUID, err error)
}
