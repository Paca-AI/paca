// Package chatsvc implements chatdom.Service. It holds no authorization:
// every permission decision is made by the router's gates (see
// docs/architecture/authorization.md). What it validates is input shape and
// data integrity, such as names, message content and project membership of
// the accounts a channel names.
package chatsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
	"github.com/Paca-AI/api/internal/pkg/mention"
)

// Service is the chat use-case implementation.
type Service struct {
	repo       chatdom.Repository
	notifier   chatdom.Notifier
	agents     chatdom.AgentResponder
	trigger    chatdom.AgentTrigger
	automation AutomationEmitter
	postGuard  PostGuard
}

var _ chatdom.Service = (*Service)(nil)

// New returns a Service over repo.
func New(repo chatdom.Repository) *Service { return &Service{repo: repo} }

func validName(n string) (string, error) {
	n = strings.TrimSpace(n)
	if n == "" || utf8.RuneCountInString(n) > chatdom.MaxNameLen {
		return "", chatdom.ErrChannelNameInvalid
	}
	return n, nil
}

// WithNotifier announces chat activity to realtime.
func (s *Service) WithNotifier(n chatdom.Notifier) *Service {
	s.notifier = n
	return s
}

func (s *Service) notify(ctx context.Context, n chatdom.Notification, withRecipients bool) {
	if s.notifier == nil {
		return
	}
	if withRecipients {
		// A failed lookup only costs the unread nudge; the room still hears it.
		n.Recipients, _ = s.repo.ChannelRecipients(ctx, n.ChannelID)
	}
	s.notifier.Notify(ctx, n)
}

func (s *Service) notifyMessage(ctx context.Context, typ string, projectID uuid.UUID, m *chatdom.Message, actor uuid.UUID) {
	s.notify(ctx, chatdom.Notification{
		Type: typ, ProjectID: projectID, ChannelID: m.ChannelID, MessageID: &m.ID, Seq: m.Seq,
		ThreadRootID: m.ThreadRootID, ActorID: actor,
	}, typ != chatdom.EventReactionChanged)
}

func (s *Service) notifyChannel(ctx context.Context, typ string, projectID, channelID, actor uuid.UUID) {
	s.notify(ctx, chatdom.Notification{Type: typ, ProjectID: projectID, ChannelID: channelID, ActorID: actor}, false)
}

// ListChannels implements chatdom.Service.
func (s *Service) ListChannels(ctx context.Context, projectID, accountID uuid.UUID, f chatdom.ChannelFilter) ([]*chatdom.Channel, error) {
	// A project created before chat existed, or whose creation hook failed,
	// gets its #general channel here. Idempotent.
	if err := s.repo.EnsureGeneral(ctx, projectID, nil); err != nil {
		return nil, err
	}
	return s.repo.ListChannels(ctx, projectID, accountID, f)
}

// GetChannel implements chatdom.Service.
func (s *Service) GetChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID) (*chatdom.Channel, error) {
	return s.repo.FindChannel(ctx, projectID, channelID, accountID)
}

// CreateChannel implements chatdom.Service. The creator is always a member.
func (s *Service) CreateChannel(ctx context.Context, projectID, accountID uuid.UUID, kind, name, topic string, memberIDs []uuid.UUID) (*chatdom.Channel, error) {
	switch kind {
	case chatdom.KindPublic, chatdom.KindPrivate, chatdom.KindVoice:
	default:
		return nil, chatdom.ErrChannelKindInvalid
	}
	name, err := validName(name)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(topic) > chatdom.MaxTopicLen {
		return nil, chatdom.ErrTopicTooLong
	}
	members := []uuid.UUID{accountID}
	seen := map[uuid.UUID]bool{accountID: true}
	for _, id := range memberIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		ok, err := s.repo.IsProjectMember(ctx, projectID, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, chatdom.ErrNotProjectMember
		}
		members = append(members, id)
	}
	if kind != chatdom.KindPrivate {
		// Public and voice channels are open to everyone allowed to read
		// them; only the creator follows at first.
		members = members[:1]
	}
	c := &chatdom.Channel{Scope: chatdom.ScopeProject, ProjectID: &projectID, Kind: kind, Name: name, Topic: strings.TrimSpace(topic), CreatedBy: &accountID}
	if err := s.repo.CreateChannel(ctx, c, members); err != nil {
		return nil, err
	}
	s.notifyChannel(ctx, chatdom.EventChannelChanged, projectID, c.ID, accountID)
	return s.repo.FindChannel(ctx, projectID, c.ID, accountID)
}

// UpdateChannel implements chatdom.Service.
func (s *Service) UpdateChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID, name, topic *string) (*chatdom.Channel, error) {
	cur, err := s.repo.FindChannel(ctx, projectID, channelID, accountID)
	if err != nil {
		return nil, err
	}
	if cur.ArchivedAt != nil {
		return nil, chatdom.ErrChannelArchived
	}
	newName, newTopic := cur.Name, cur.Topic
	if name != nil {
		if newName, err = validName(*name); err != nil {
			return nil, err
		}
		if strings.EqualFold(cur.Name, chatdom.GeneralChannelName) && !strings.EqualFold(newName, cur.Name) {
			return nil, chatdom.ErrChannelProtected
		}
	}
	if topic != nil {
		newTopic = strings.TrimSpace(*topic)
		if utf8.RuneCountInString(newTopic) > chatdom.MaxTopicLen {
			return nil, chatdom.ErrTopicTooLong
		}
	}
	if err := s.repo.UpdateChannel(ctx, projectID, channelID, newName, newTopic); err != nil {
		return nil, err
	}
	s.notifyChannel(ctx, chatdom.EventChannelChanged, projectID, channelID, accountID)
	return s.repo.FindChannel(ctx, projectID, channelID, accountID)
}

// ArchiveChannel implements chatdom.Service.
func (s *Service) ArchiveChannel(ctx context.Context, projectID, channelID uuid.UUID, archived bool) error {
	if archived {
		// The general channel is the project's home; it stays.
		c, err := s.repo.FindChannel(ctx, projectID, channelID, uuid.Nil)
		if err != nil {
			return err
		}
		if strings.EqualFold(c.Name, chatdom.GeneralChannelName) {
			return chatdom.ErrChannelProtected
		}
	}
	if err := s.repo.SetArchived(ctx, projectID, channelID, archived); err != nil {
		return err
	}
	s.notifyChannel(ctx, chatdom.EventChannelChanged, projectID, channelID, uuid.Nil)
	return nil
}

func (s *Service) requireChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID) (*chatdom.Channel, error) {
	return s.repo.FindChannel(ctx, projectID, channelID, accountID)
}

// JoinChannel implements chatdom.Service.
func (s *Service) JoinChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID) error {
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	if err := s.repo.AddMember(ctx, channelID, accountID); err != nil {
		return err
	}
	s.notifyChannel(ctx, chatdom.EventAccessChanged, projectID, channelID, accountID)
	return nil
}

// LeaveChannel implements chatdom.Service.
func (s *Service) LeaveChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID) error {
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	if err := s.repo.RemoveMember(ctx, channelID, accountID); err != nil {
		return err
	}
	s.notifyChannel(ctx, chatdom.EventAccessChanged, projectID, channelID, accountID)
	return nil
}

// AddChannelMember implements chatdom.Service.
func (s *Service) AddChannelMember(ctx context.Context, projectID, channelID, accountID uuid.UUID) error {
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	ok, err := s.repo.IsProjectMember(ctx, projectID, accountID)
	if err != nil {
		return err
	}
	if !ok {
		return chatdom.ErrNotProjectMember
	}
	if err := s.repo.AddMember(ctx, channelID, accountID); err != nil {
		return err
	}
	s.notifyChannel(ctx, chatdom.EventAccessChanged, projectID, channelID, accountID)
	return nil
}

// SetNotificationPref implements chatdom.Service.
func (s *Service) SetNotificationPref(ctx context.Context, projectID, channelID, accountID uuid.UUID, pref string) error {
	switch pref {
	case chatdom.PrefDefault, chatdom.PrefAll, chatdom.PrefMentions, chatdom.PrefNone:
	default:
		return chatdom.ErrChannelKindInvalid
	}
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	return s.repo.SetNotificationPref(ctx, channelID, accountID, pref)
}

// ContentText flattens BlockNote-style content into searchable text. It
// returns an error when content is not a JSON array of objects.
func ContentText(content []byte) (string, error) {
	var blocks []any
	dec := json.NewDecoder(bytes.NewReader(content))
	if err := dec.Decode(&blocks); err != nil || dec.More() {
		return "", chatdom.ErrMessageContentBad
	}
	var parts []string
	var walk func(v any)
	walk = func(v any) {
		switch n := v.(type) {
		case []any:
			for _, c := range n {
				walk(c)
			}
		case map[string]any:
			if t, ok := n["text"].(string); ok && t != "" {
				parts = append(parts, t)
			}
			// A mention has no text of its own: its label is what a reader sees.
			if n["type"] == "teamMention" {
				if props, ok := n["props"].(map[string]any); ok {
					if name, ok := props["name"].(string); ok && name != "" {
						parts = append(parts, "@"+name)
					}
				}
			}
			// A task or document chip stores ids only; its label is "#<number>"
			// (a task) or "#doc" so a preview or a search hit still shows one.
			switch n["type"] {
			case "taskReference":
				if props, ok := n["props"].(map[string]any); ok {
					if num, ok := props["number"].(float64); ok && num > 0 {
						parts = append(parts, "#"+strconv.FormatInt(int64(num), 10))
					} else {
						parts = append(parts, "#task")
					}
				}
			case "docReference":
				parts = append(parts, "#doc")
			}
			walk(n["content"])
			walk(n["children"])
		}
	}
	for _, b := range blocks {
		if _, ok := b.(map[string]any); !ok {
			return "", chatdom.ErrMessageContentBad
		}
	}
	walk(blocks)
	return strings.TrimSpace(strings.Join(parts, " ")), nil
}

func normalizeContent(content []byte) ([]byte, string, error) {
	text, err := ContentText(content)
	if err != nil {
		return nil, "", err
	}
	if text == "" {
		return nil, "", chatdom.ErrMessageEmpty
	}
	if utf8.RuneCountInString(text) > chatdom.MaxContentText {
		return nil, "", chatdom.ErrMessageTooLong
	}
	return content, text, nil
}

// mentionedAccounts returns the distinct account ids the content mentions.
func mentionedAccounts(content []byte) []uuid.UUID {
	var out []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, m := range mention.ExtractTeamMentionsFromBlocks(content) {
		id, err := uuid.Parse(m.ID)
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// PostMessage implements chatdom.Service.
func (s *Service) PostMessage(ctx context.Context, projectID, channelID, authorID uuid.UUID, content []byte, threadRootID *uuid.UUID) (*chatdom.Message, error) {
	content, text, err := normalizeContent(content)
	if err != nil {
		return nil, err
	}
	channel, err := s.requireChannel(ctx, projectID, channelID, authorID)
	if err != nil {
		return nil, err
	}
	m := &chatdom.Message{ChannelID: channelID, AuthorID: authorID, Content: content, ContentText: text, ThreadRootID: threadRootID}
	if err := s.repo.CreateMessage(ctx, m); err != nil {
		return nil, err
	}
	// Mentions and follows are what the Inbox lists are built from; a failure to
	// record them must not lose the message, which is already stored.
	mentioned := mentionedAccounts(content)
	_ = s.repo.RecordMessageEffects(ctx, m, mentioned)
	full, err := s.repo.FindMessage(ctx, channelID, m.ID, authorID)
	if err != nil {
		return nil, err
	}
	s.notifyMessage(ctx, chatdom.EventMessageNew, projectID, full, authorID)
	s.triggerMentionedAgents(ctx, projectID, channel, full, mentioned)
	s.emitAutomation(ctx, projectID, channel, full, mentioned)
	if threadRootID == nil {
		s.forwardToAgent(ctx, channel, authorID)
	}
	return full, nil
}

// ListMessages implements chatdom.Service.
func (s *Service) ListMessages(ctx context.Context, projectID, channelID, accountID uuid.UUID, f chatdom.MessageFilter) ([]*chatdom.Message, bool, error) {
	if (f.AfterSeq != nil && *f.AfterSeq < 0) || (f.BeforeSeq != nil && *f.BeforeSeq < 1) || (f.AfterSeq != nil && f.BeforeSeq != nil) {
		return nil, false, chatdom.ErrSeqInvalid
	}
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return nil, false, err
	}
	return s.repo.ListMessages(ctx, channelID, accountID, f)
}

// EditMessage implements chatdom.Service. Only the author can edit.
func (s *Service) EditMessage(ctx context.Context, projectID, channelID, messageID, authorID uuid.UUID, content []byte) (*chatdom.Message, error) {
	content, text, err := normalizeContent(content)
	if err != nil {
		return nil, err
	}
	if _, err := s.requireChannel(ctx, projectID, channelID, authorID); err != nil {
		return nil, err
	}
	if err := s.repo.EditMessage(ctx, channelID, messageID, authorID, content, text); err != nil {
		return nil, err
	}
	_ = s.repo.SetMentions(ctx, messageID, authorID, mentionedAccounts(content))
	full, err := s.repo.FindMessage(ctx, channelID, messageID, authorID)
	if err != nil {
		return nil, err
	}
	s.notifyMessage(ctx, chatdom.EventMessageUpdated, projectID, full, authorID)
	return full, nil
}

// DeleteOwnMessage implements chatdom.Service.
func (s *Service) DeleteOwnMessage(ctx context.Context, projectID, channelID, messageID, authorID uuid.UUID) error {
	if _, err := s.requireChannel(ctx, projectID, channelID, authorID); err != nil {
		return err
	}
	if err := s.repo.DeleteMessage(ctx, channelID, messageID, &authorID); err != nil {
		return err
	}
	s.announceDeleted(ctx, projectID, channelID, messageID, authorID)
	return nil
}

// announceDeleted tells the channel a message is gone. The seq is read after
// the soft delete, which keeps the row.
func (s *Service) announceDeleted(ctx context.Context, projectID, channelID, messageID, actor uuid.UUID) {
	if m, err := s.repo.FindMessage(ctx, channelID, messageID, actor); err == nil {
		s.notifyMessage(ctx, chatdom.EventMessageDeleted, projectID, m, actor)
	}
}

// ModerateDeleteMessage implements chatdom.Service. The router gate
// (messages:moderate) is what allows deleting someone else's message.
func (s *Service) ModerateDeleteMessage(ctx context.Context, projectID, channelID, messageID uuid.UUID) error {
	if _, err := s.requireChannel(ctx, projectID, channelID, uuid.Nil); err != nil {
		return err
	}
	if err := s.repo.DeleteMessage(ctx, channelID, messageID, nil); err != nil {
		return err
	}
	s.announceDeleted(ctx, projectID, channelID, messageID, uuid.Nil)
	return nil
}

func validEmoji(e string) bool {
	return e != "" && len(e) <= chatdom.MaxEmojiLen && strings.TrimSpace(e) == e
}

// AddReaction implements chatdom.Service.
func (s *Service) AddReaction(ctx context.Context, projectID, channelID, messageID, accountID uuid.UUID, emoji string) error {
	if !validEmoji(emoji) {
		return chatdom.ErrEmojiInvalid
	}
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	m, err := s.repo.FindMessage(ctx, channelID, messageID, accountID)
	if err != nil {
		return err
	}
	if err := s.repo.AddReaction(ctx, messageID, accountID, emoji); err != nil {
		return err
	}
	s.notifyMessage(ctx, chatdom.EventReactionChanged, projectID, m, accountID)
	return nil
}

// RemoveReaction implements chatdom.Service.
func (s *Service) RemoveReaction(ctx context.Context, projectID, channelID, messageID, accountID uuid.UUID, emoji string) error {
	if !validEmoji(emoji) {
		return chatdom.ErrEmojiInvalid
	}
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	m, err := s.repo.FindMessage(ctx, channelID, messageID, accountID)
	if err != nil {
		return err
	}
	if err := s.repo.RemoveReaction(ctx, messageID, accountID, emoji); err != nil {
		return err
	}
	s.notifyMessage(ctx, chatdom.EventReactionChanged, projectID, m, accountID)
	return nil
}

// MarkRead implements chatdom.Service. Marking read also follows the channel,
// so a public channel the caller reads shows up in their unread summary.
func (s *Service) MarkRead(ctx context.Context, projectID, channelID, accountID uuid.UUID, seq int64) error {
	if seq < 0 {
		return chatdom.ErrSeqInvalid
	}
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	if err := s.repo.AddMember(ctx, channelID, accountID); err != nil {
		return err
	}
	return s.repo.MarkRead(ctx, channelID, accountID, seq)
}

// UnreadSummary implements chatdom.Service.
func (s *Service) UnreadSummary(ctx context.Context, accountID uuid.UUID) ([]chatdom.UnreadSummary, error) {
	return s.repo.UnreadByProject(ctx, accountID)
}

// Inbox implements chatdom.Service.
func (s *Service) Inbox(ctx context.Context, accountID uuid.UUID, f chatdom.InboxFilter) ([]chatdom.InboxItem, error) {
	return s.repo.ListInbox(ctx, accountID, f)
}

// InboxProjects implements chatdom.Service.
func (s *Service) InboxProjects(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error) {
	return s.repo.MemberProjectIDs(ctx, accountID)
}

// MarkAllRead implements chatdom.Service.
func (s *Service) MarkAllRead(ctx context.Context, accountID uuid.UUID, projectID *uuid.UUID) error {
	return s.repo.MarkAllRead(ctx, accountID, projectID)
}

// StartDM implements chatdom.Service. Any existing, not deleted account can be
// messaged: a person, an agent or a service account. Whether the caller may
// start a conversation at all is the router's decision (channels:write on
// channel/*), and who may read it is the DirectMessages policy statement.
func (s *Service) StartDM(ctx context.Context, accountID, peerID uuid.UUID) (*chatdom.Channel, error) {
	if accountID == peerID {
		return nil, chatdom.ErrDMSelf
	}
	_, ok, err := s.repo.AccountKind(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, chatdom.ErrDMPeerNotFound
	}
	id, created, err := s.repo.FindOrCreateDM(ctx, accountID, peerID)
	if err != nil {
		return nil, err
	}
	if created {
		s.notify(ctx, chatdom.Notification{Type: chatdom.EventDMCreated, ChannelID: id, ActorID: accountID,
			Recipients: []uuid.UUID{accountID, peerID}}, false)
	}
	return s.repo.FindChannel(ctx, uuid.Nil, id, accountID)
}

// ListDMs implements chatdom.Service.
func (s *Service) ListDMs(ctx context.Context, accountID uuid.UUID) ([]*chatdom.Channel, error) {
	return s.repo.ListDMs(ctx, accountID)
}

// Directory implements chatdom.Service.
func (s *Service) Directory(ctx context.Context, accountID uuid.UUID, query, kind string) ([]chatdom.DirectoryEntry, error) {
	switch kind {
	case "", chatdom.AccountUser, chatdom.AccountAgent, chatdom.AccountServiceAccount:
	default:
		return nil, chatdom.ErrChannelKindInvalid
	}
	return s.repo.Directory(ctx, accountID, query, kind, 25)
}

// FollowThread implements chatdom.Service.
func (s *Service) FollowThread(ctx context.Context, projectID, channelID, rootID, accountID uuid.UUID, follow bool) error {
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	if follow {
		return s.repo.FollowThread(ctx, accountID, channelID, rootID)
	}
	return s.repo.UnfollowThread(ctx, accountID, rootID)
}

// MarkThreadRead implements chatdom.Service.
func (s *Service) MarkThreadRead(ctx context.Context, projectID, channelID, rootID, accountID uuid.UUID, seq int64) error {
	if seq < 0 {
		return chatdom.ErrSeqInvalid
	}
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	return s.repo.MarkThreadRead(ctx, accountID, channelID, rootID, seq)
}

// SaveMessage implements chatdom.Service.
func (s *Service) SaveMessage(ctx context.Context, projectID, channelID, messageID, accountID uuid.UUID, saved bool) error {
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	if saved {
		return s.repo.SaveMessage(ctx, accountID, channelID, messageID)
	}
	return s.repo.UnsaveMessage(ctx, accountID, messageID)
}

// Mentions implements chatdom.Service.
func (s *Service) Mentions(ctx context.Context, accountID uuid.UUID) ([]chatdom.MessageRef, error) {
	return s.repo.ListMentions(ctx, accountID, chatdom.MaxMessageRefs)
}

// Threads implements chatdom.Service.
func (s *Service) Threads(ctx context.Context, accountID uuid.UUID) ([]chatdom.MessageRef, error) {
	return s.repo.ListThreads(ctx, accountID, chatdom.MaxMessageRefs)
}

// Saved implements chatdom.Service.
func (s *Service) Saved(ctx context.Context, accountID uuid.UUID) ([]chatdom.MessageRef, error) {
	return s.repo.ListSaved(ctx, accountID, chatdom.MaxMessageRefs)
}

// Search implements chatdom.Service.
func (s *Service) Search(ctx context.Context, accountID uuid.UUID, query string, projectID *uuid.UUID) ([]chatdom.MessageRef, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < chatdom.MinSearchLen {
		return []chatdom.MessageRef{}, nil
	}
	if r := []rune(query); len(r) > chatdom.MaxSearchLen {
		query = string(r[:chatdom.MaxSearchLen])
	}
	return s.repo.SearchMessages(ctx, accountID, query, projectID, chatdom.MaxMessageRefs)
}

// StarChannel implements chatdom.Service.
func (s *Service) StarChannel(ctx context.Context, projectID, channelID, accountID uuid.UUID, starred bool) error {
	if _, err := s.requireChannel(ctx, projectID, channelID, accountID); err != nil {
		return err
	}
	return s.repo.SetStarred(ctx, accountID, channelID, starred)
}
