package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	chatdom "github.com/Paca-AI/api/internal/domain/chat"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// ChatHandler serves the project chat endpoints. It makes no authorization
// decision: every route is gated in the router.
type ChatHandler struct {
	svc            chatdom.Service
	listScoper     ListScoper
	projectsScoper ProjectsScoper
	rootScoper     RootScoper
}

// RootScoper is the part of *iam.Authorizer a list of workspace-scope
// resources (direct messages) uses to learn which the caller may read.
type RootScoper interface {
	ListRootScope(ctx context.Context, p iam.Principal, action, kind string) (*iam.Node, error)
}

// WithChatRootScoper limits direct-message lists to the conversations the
// caller may read, inside the query.
func (h *ChatHandler) WithChatRootScoper(s RootScoper) *ChatHandler {
	h.rootScoper = s
	return h
}

// rootScopedContext adds the caller's scope over direct messages to ctx.
// Without a scoper nothing is listed (fail closed).
func (h *ChatHandler) rootScopedContext(r *http.Request, ctx context.Context) (context.Context, error) {
	if h.rootScoper == nil {
		return nil, apierr.New(apierr.CodeInternalError, "authorization not configured")
	}
	principal, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		return nil, err
	}
	n, err := h.rootScoper.ListRootScope(ctx, principal, string(iam.ActionChannelsRead), "channel")
	if err != nil {
		return nil, err
	}
	return iam.WithScope(ctx, "channel", n), nil
}

// NewChatHandler returns a ChatHandler.
func NewChatHandler(svc chatdom.Service) *ChatHandler { return &ChatHandler{svc: svc} }

// WithChatListScoper limits the channel list to the channels the caller may
// read, inside the query (private channels are visible to their members).
func (h *ChatHandler) WithChatListScoper(s ListScoper) *ChatHandler {
	h.listScoper = s
	return h
}

// WithChatProjectsScoper limits the Inbox to the channels the caller may read
// in each project, inside the query.
func (h *ChatHandler) WithChatProjectsScoper(s ProjectsScoper) *ChatHandler {
	h.projectsScoper = s
	return h
}

// chatIDs reads the project and channel of a chat route. A route with no
// {projectId} (a direct message) yields uuid.Nil, the workspace scope.
func chatIDs(r *http.Request) (projectID, channelID uuid.UUID, err error) {
	if chi.URLParam(r, "projectId") != "" {
		if projectID, err = parseProjectID(r); err != nil {
			return
		}
	}
	channelID, err = uuid.Parse(chi.URLParam(r, "channelId"))
	if err != nil {
		err = apierr.New(apierr.CodeBadRequest, "invalid channel id")
	}
	return
}

func chatURLID(r *http.Request, param, what string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid "+what+" id")
	}
	return id, nil
}

// ListChannels handles GET /projects/{projectId}/channels.
func (h *ChatHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	ctx, err := scopedContext(r, h.listScoper, iam.ActionChannelsRead, projectID, "channel")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	cs, err := h.svc.ListChannels(ctx, projectID, me, chatdom.ChannelFilter{IncludeArchived: r.URL.Query().Get("include_archived") == "true"})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	items := make([]dto.ChannelResponse, len(cs))
	for i, c := range cs {
		items[i] = dto.ChannelFromEntity(c)
	}
	presenter.OK(w, r, items)
}

// CreateChannel handles POST /projects/{projectId}/channels.
func (h *ChatHandler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.CreateChannelRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	c, err := h.svc.CreateChannel(r.Context(), projectID, me, req.Kind, req.Name, req.Topic, req.MemberIDs)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.ChannelFromEntity(c))
}

// GetChannel handles GET /projects/{projectId}/channels/{channelId}.
func (h *ChatHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	c, err := h.svc.GetChannel(r.Context(), projectID, channelID, me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ChannelFromEntity(c))
}

// UpdateChannel handles PATCH /projects/{projectId}/channels/{channelId}.
func (h *ChatHandler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.UpdateChannelRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	c, err := h.svc.UpdateChannel(r.Context(), projectID, channelID, me, req.Name, req.Topic)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ChannelFromEntity(c))
}

func (h *ChatHandler) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.ArchiveChannel(r.Context(), projectID, channelID, archived); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// ArchiveChannel handles POST .../channels/{channelId}/archive.
func (h *ChatHandler) ArchiveChannel(w http.ResponseWriter, r *http.Request) {
	h.setArchived(w, r, true)
}

// UnarchiveChannel handles POST .../channels/{channelId}/unarchive.
func (h *ChatHandler) UnarchiveChannel(w http.ResponseWriter, r *http.Request) {
	h.setArchived(w, r, false)
}

// JoinChannel handles POST .../channels/{channelId}/join.
func (h *ChatHandler) JoinChannel(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	if err := h.svc.JoinChannel(r.Context(), projectID, channelID, me); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// LeaveChannel handles POST .../channels/{channelId}/leave.
func (h *ChatHandler) LeaveChannel(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	if err := h.svc.LeaveChannel(r.Context(), projectID, channelID, me); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// AddMember handles PUT .../channels/{channelId}/members/{accountId}.
func (h *ChatHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	accountID, err := chatURLID(r, "accountId", "account")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.AddChannelMember(r.Context(), projectID, channelID, accountID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// SetNotificationPref handles PATCH .../channels/{channelId}/notification-pref.
func (h *ChatHandler) SetNotificationPref(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.NotificationPrefRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.svc.SetNotificationPref(r.Context(), projectID, channelID, me, req.Pref); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

func seqParam(r *http.Request, name string) (*int64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "invalid "+name)
	}
	return &n, nil
}

// ListMessages handles GET .../channels/{channelId}/messages with the query
// parameters after_seq (catch-up, oldest first), before_seq (scroll back),
// thread_root_id and page_size.
func (h *ChatHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	f := chatdom.MessageFilter{}
	if f.AfterSeq, err = seqParam(r, "after_seq"); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if f.BeforeSeq, err = seqParam(r, "before_seq"); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if raw := r.URL.Query().Get("thread_root_id"); raw != "" {
		id, perr := uuid.Parse(raw)
		if perr != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid thread_root_id"))
			return
		}
		f.ThreadRootID = &id
	}
	if f.Limit, err = parsePageSize(r, chatdom.DefaultPage, chatdom.MaxPage); err != nil {
		presenter.Error(w, r, err)
		return
	}
	ms, more, err := h.svc.ListMessages(r.Context(), projectID, channelID, me, f)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	items := make([]dto.MessageResponse, len(ms))
	for i, m := range ms {
		items[i] = dto.MessageFromEntity(m)
	}
	presenter.OK(w, r, dto.MessageListResponse{Items: items, HasMore: more})
}

// PostMessage handles POST .../channels/{channelId}/messages.
func (h *ChatHandler) PostMessage(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.PostMessageRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	m, err := h.svc.PostMessage(r.Context(), projectID, channelID, me, req.Content, req.ThreadRootID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.MessageFromEntity(m))
}

// EditMessage handles PATCH .../messages/{messageId}. Only the author's own
// message is rewritten; there is no moderator edit.
func (h *ChatHandler) EditMessage(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	messageID, err := chatURLID(r, "messageId", "message")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.EditMessageRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	m, err := h.svc.EditMessage(r.Context(), projectID, channelID, messageID, me, req.Content)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MessageFromEntity(m))
}

// DeleteMessage handles DELETE .../messages/{messageId}: the caller's own message.
func (h *ChatHandler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	messageID, err := chatURLID(r, "messageId", "message")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteOwnMessage(r.Context(), projectID, channelID, messageID, me); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// ModerateDeleteMessage handles DELETE .../moderation/messages/{messageId}:
// any message, gated on messages:moderate.
func (h *ChatHandler) ModerateDeleteMessage(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	messageID, err := chatURLID(r, "messageId", "message")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.ModerateDeleteMessage(r.Context(), projectID, channelID, messageID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

func (h *ChatHandler) react(w http.ResponseWriter, r *http.Request, add bool) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	messageID, err := chatURLID(r, "messageId", "message")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	emoji, err := url.PathUnescape(chi.URLParam(r, "emoji"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid emoji"))
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	if add {
		err = h.svc.AddReaction(r.Context(), projectID, channelID, messageID, me, emoji)
	} else {
		err = h.svc.RemoveReaction(r.Context(), projectID, channelID, messageID, me, emoji)
	}
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// AddReaction handles PUT .../messages/{messageId}/reactions/{emoji}.
func (h *ChatHandler) AddReaction(w http.ResponseWriter, r *http.Request) { h.react(w, r, true) }

// RemoveReaction handles DELETE .../messages/{messageId}/reactions/{emoji}.
func (h *ChatHandler) RemoveReaction(w http.ResponseWriter, r *http.Request) { h.react(w, r, false) }

// MarkRead handles POST .../channels/{channelId}/read.
func (h *ChatHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.MarkReadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.svc.MarkRead(r.Context(), projectID, channelID, me, req.Seq); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// UnreadSummary handles GET /chat/unread-summary: the caller's unread counts
// per project they still belong to.
func (h *ChatHandler) UnreadSummary(w http.ResponseWriter, r *http.Request) {
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.UnreadSummary(r.Context(), me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	items := make([]dto.UnreadSummaryItem, len(rows))
	for i, s := range rows {
		items[i] = dto.UnreadSummaryItem{ProjectID: s.ProjectID, ProjectName: s.ProjectName, Unread: s.Unread, Channels: s.Channels}
	}
	presenter.OK(w, r, items)
}

// Inbox handles GET /users/me/chat/inbox: the channels the caller follows,
// across their projects, unread first. Query: unread=true, project_id,
// page_size. What a caller may see is decided by their own policies, per
// project, inside the query.
func (h *ChatHandler) Inbox(w http.ResponseWriter, r *http.Request) {
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	f := chatdom.InboxFilter{UnreadOnly: r.URL.Query().Get("unread") == "true"}
	if raw := r.URL.Query().Get("project_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid project_id"))
			return
		}
		f.ProjectID = &id
	}
	var err error
	if f.Limit, err = parsePageSize(r, chatdom.DefaultInbox, chatdom.MaxInbox); err != nil {
		presenter.Error(w, r, err)
		return
	}
	scoped, ok := h.inboxScope(w, r, me)
	if !ok {
		return
	}
	items, err := h.svc.Inbox(scoped, me, f)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.InboxItemResponse, len(items))
	for i, it := range items {
		out[i] = dto.InboxItemResponse{
			ProjectID: it.ProjectID, Peer: dtoPeer(it.Peer), ProjectName: it.ProjectName, ChannelID: it.ChannelID, ChannelName: it.ChannelName,
			Kind: it.Kind, LastSeq: it.LastSeq, LastReadSeq: it.LastReadSeq, UnreadCount: it.UnreadCount,
			LastMessageAt: it.LastMessageAt, LastAuthor: it.LastAuthor, Preview: it.Preview, MentionCount: it.MentionCount, Starred: it.Starred,
		}
	}
	presenter.OK(w, r, out)
}

// MarkAllRead handles POST /users/me/chat/read-all: moves the caller's own read
// pointers to the head of every channel they follow (of one project when the
// body names it).
func (h *ChatHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.MarkAllReadRequest
	if r.ContentLength != 0 && !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.svc.MarkAllRead(r.Context(), me, req.ProjectID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

func dtoPeer(p *chatdom.DMPeer) *dto.PeerResponse {
	if p == nil {
		return nil
	}
	return &dto.PeerResponse{AccountID: p.AccountID, Name: p.Name, Kind: p.Kind}
}

// ListDMs handles GET /chat/dms: the caller's direct messages, newest first.
func (h *ChatHandler) ListDMs(w http.ResponseWriter, r *http.Request) {
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	ctx, err := h.rootScopedContext(r, r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	cs, err := h.svc.ListDMs(ctx, me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	items := make([]dto.ChannelResponse, len(cs))
	for i, c := range cs {
		items[i] = dto.ChannelFromEntity(c)
	}
	presenter.OK(w, r, items)
}

// StartDM handles POST /chat/dms: opens, or returns, the direct message with
// another account.
func (h *ChatHandler) StartDM(w http.ResponseWriter, r *http.Request) {
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.StartDMRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	c, err := h.svc.StartDM(r.Context(), me, req.AccountID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ChannelFromEntity(c))
}

// Directory handles GET /chat/directory?q=&kind=: accounts the caller can
// start a direct message with.
func (h *ChatHandler) Directory(w http.ResponseWriter, r *http.Request) {
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.Directory(r.Context(), me, r.URL.Query().Get("q"), r.URL.Query().Get("kind"))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.DirectoryEntryResponse, len(rows))
	for i, e := range rows {
		out[i] = dto.DirectoryEntryResponse{AccountID: e.AccountID, Name: e.Name, Kind: e.Kind, Description: e.Description}
	}
	presenter.OK(w, r, out)
}

// inboxScope attaches what the caller may read to the request context: per
// project for project channels, and over direct messages. It writes the
// response itself when that cannot be decided (and fails closed without a
// scoper, answering an empty list).
func (h *ChatHandler) inboxScope(w http.ResponseWriter, r *http.Request, me uuid.UUID) (context.Context, bool) {
	ctx := r.Context()
	if h.projectsScoper == nil {
		presenter.OK(w, r, []any{})
		return nil, false
	}
	projects, err := h.svc.InboxProjects(ctx, me)
	if err != nil {
		presenter.Error(w, r, err)
		return nil, false
	}
	principal, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		presenter.Error(w, r, err)
		return nil, false
	}
	ids := make([]string, len(projects))
	for i, p := range projects {
		ids[i] = p.String()
	}
	byProject, err := h.projectsScoper.ListScopes(ctx, principal, string(iam.ActionChannelsRead), ids, "channel")
	if err != nil {
		presenter.Error(w, r, err)
		return nil, false
	}
	scoped := iam.WithProjectScopes(ctx, "channel", byProject)
	if h.rootScoper != nil {
		if scoped, err = h.rootScopedContext(r, scoped); err != nil {
			presenter.Error(w, r, err)
			return nil, false
		}
	}
	return scoped, true
}

func (h *ChatHandler) messageRefs(w http.ResponseWriter, r *http.Request, list func(context.Context, uuid.UUID) ([]chatdom.MessageRef, error)) {
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	scoped, ok := h.inboxScope(w, r, me)
	if !ok {
		return
	}
	refs, err := list(scoped, me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.MessageRefResponse, len(refs))
	for i, ref := range refs {
		out[i] = dto.MessageRefFromEntity(ref)
	}
	presenter.OK(w, r, out)
}

// Mentions handles GET /users/me/chat/mentions: messages that mention the
// caller, newest first, in channels they may read.
func (h *ChatHandler) Mentions(w http.ResponseWriter, r *http.Request) {
	h.messageRefs(w, r, h.svc.Mentions)
}

// Threads handles GET /users/me/chat/threads: the threads the caller follows,
// those with unread replies first.
func (h *ChatHandler) Threads(w http.ResponseWriter, r *http.Request) {
	h.messageRefs(w, r, h.svc.Threads)
}

// Saved handles GET /users/me/chat/saved: the caller's saved messages.
func (h *ChatHandler) Saved(w http.ResponseWriter, r *http.Request) { h.messageRefs(w, r, h.svc.Saved) }

func (h *ChatHandler) follow(w http.ResponseWriter, r *http.Request, on bool) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	messageID, err := chatURLID(r, "messageId", "message")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	if err := h.svc.FollowThread(r.Context(), projectID, channelID, messageID, me, on); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// FollowThread handles PUT .../messages/{messageId}/follow.
func (h *ChatHandler) FollowThread(w http.ResponseWriter, r *http.Request) { h.follow(w, r, true) }

// UnfollowThread handles DELETE .../messages/{messageId}/follow.
func (h *ChatHandler) UnfollowThread(w http.ResponseWriter, r *http.Request) { h.follow(w, r, false) }

// MarkThreadRead handles POST .../messages/{messageId}/thread-read.
func (h *ChatHandler) MarkThreadRead(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	messageID, err := chatURLID(r, "messageId", "message")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.MarkReadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.svc.MarkThreadRead(r.Context(), projectID, channelID, messageID, me, req.Seq); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

func (h *ChatHandler) save(w http.ResponseWriter, r *http.Request, on bool) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	messageID, err := chatURLID(r, "messageId", "message")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	if err := h.svc.SaveMessage(r.Context(), projectID, channelID, messageID, me, on); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// SaveMessage handles PUT .../messages/{messageId}/save.
func (h *ChatHandler) SaveMessage(w http.ResponseWriter, r *http.Request) { h.save(w, r, true) }

// UnsaveMessage handles DELETE .../messages/{messageId}/save.
func (h *ChatHandler) UnsaveMessage(w http.ResponseWriter, r *http.Request) { h.save(w, r, false) }

func (h *ChatHandler) star(w http.ResponseWriter, r *http.Request, on bool) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	if err := h.svc.StarChannel(r.Context(), projectID, channelID, me, on); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// StarChannel handles PUT .../channels/{channelId}/star (or a direct message).
func (h *ChatHandler) StarChannel(w http.ResponseWriter, r *http.Request) { h.star(w, r, true) }

// UnstarChannel handles DELETE .../channels/{channelId}/star.
func (h *ChatHandler) UnstarChannel(w http.ResponseWriter, r *http.Request) { h.star(w, r, false) }

// AgentRuns handles GET /chat/dms/{channelId}/agent: the runs of the agent a
// direct message talks to, and whether it is answering.
func (h *ChatHandler) AgentRuns(w http.ResponseWriter, r *http.Request) {
	_, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	runs, err := h.svc.AgentRuns(r.Context(), channelID, me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.AgentRunsFromEntity(runs))
}
