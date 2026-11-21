package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/bootstrap/defaultroles"
	chatdom "github.com/Paca-AI/api/internal/domain/chat"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	chatsvc "github.com/Paca-AI/api/internal/service/chat"
)

func text(s string) []byte {
	return []byte(`[{"type":"paragraph","content":[{"type":"text","text":"` + s + `"}]}]`)
}

// The shipped Editor and Viewer templates, instantiated for a project, give
// the right answer for public and private channels through the real
// authorizer, attribute loaders and list scope: this is the rule the design
// hinges on (private channels visible only to their members, written in role
// definitions and not in code).
func TestChatPrivateChannelVisibility(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	p := fx.project()

	member, outsider, viewer := fx.user(false), fx.user(false), fx.user(false)
	for _, u := range []uuid.UUID{member, outsider, viewer} {
		fx.member(p, u, false)
	}
	tmpl := map[string]uuid.UUID{}
	for _, r := range defaultroles.Instantiate(p) {
		id := fx.role(string(r.Policy))
		pgExec(t, db, `UPDATE roles SET project_id = $2 WHERE id = $1`, id, p)
		tmpl[r.Name] = id
	}
	fx.attach(tmpl["Editor"], member, &p)
	fx.attach(tmpl["Editor"], outsider, &p)
	fx.attach(tmpl["Viewer"], viewer, &p)

	repo := NewChatRepository(db)
	svc := chatsvc.New(repo)
	pub, err := svc.CreateChannel(ctx, p, member, chatdom.KindPublic, "random", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := svc.CreateChannel(ctx, p, member, chatdom.KindPrivate, "secret", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateChannel(ctx, p, member, chatdom.KindPrivate, "SECRET", "", nil); !errors.Is(err, chatdom.ErrChannelNameTaken) {
		t.Fatalf("duplicate name: got %v", err)
	}

	authz := NewIAMAuthorizer(db)
	// The list and create routes are gated on the project itself.
	onProject := func(u uuid.UUID, a iam.Action) bool {
		t.Helper()
		r, err := authz.Authorize(ctx, iam.Principal{Type: iam.PrincipalUser, ID: u.String()}, string(a), "project/"+p.String())
		if err != nil {
			t.Fatal(err)
		}
		return r.Allowed
	}
	if !onProject(outsider, iam.ActionChannelsRead) || !onProject(outsider, iam.ActionChannelsWrite) || !onProject(viewer, iam.ActionChannelsRead) {
		t.Error("editors and viewers must pass the project-level list gate (and editors the create gate)")
	}
	if onProject(viewer, iam.ActionChannelsWrite) || onProject(viewer, iam.ActionMessagesWrite) {
		t.Error("a viewer must not create channels or post")
	}
	stranger := fx.user(false)
	if onProject(stranger, iam.ActionChannelsRead) {
		t.Error("a user with no role must not pass the list gate")
	}
	can := func(u uuid.UUID, a iam.Action, ch *chatdom.Channel) bool {
		t.Helper()
		r, err := authz.Authorize(ctx, iam.Principal{Type: iam.PrincipalUser, ID: u.String()}, string(a), "project/"+p.String()+"/channel/"+ch.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		return r.Allowed
	}
	for _, c := range []struct {
		who    string
		u      uuid.UUID
		a      iam.Action
		ch     *chatdom.Channel
		expect bool
	}{
		{"editor reads public", outsider, iam.ActionChannelsRead, pub, true},
		{"editor posts public", outsider, iam.ActionMessagesWrite, pub, true},
		{"member reads private", member, iam.ActionChannelsRead, priv, true},
		{"member posts private", member, iam.ActionMessagesWrite, priv, true},
		{"non-member cannot read private", outsider, iam.ActionChannelsRead, priv, false},
		{"non-member cannot post private", outsider, iam.ActionMessagesWrite, priv, false},
		{"viewer reads public", viewer, iam.ActionChannelsRead, pub, true},
		{"viewer cannot post", viewer, iam.ActionMessagesWrite, pub, false},
		{"viewer cannot read private", viewer, iam.ActionChannelsRead, priv, false},
	} {
		if got := can(c.u, c.a, c.ch); got != c.expect {
			t.Errorf("%s: allowed=%v want %v", c.who, got, c.expect)
		}
	}

	// Once added, the outsider reads and posts the private channel.
	if err := svc.AddChannelMember(ctx, p, priv.ID, outsider); err != nil {
		t.Fatal(err)
	}
	if !can(outsider, iam.ActionChannelsRead, priv) {
		t.Error("added member cannot read the private channel")
	}

	// The list is scoped in SQL by the same policy.
	names := func(u uuid.UUID) map[string]bool {
		t.Helper()
		n, err := authz.ListScope(ctx, iam.Principal{Type: iam.PrincipalUser, ID: u.String()}, string(iam.ActionChannelsRead), p.String(), "channel")
		if err != nil {
			t.Fatal(err)
		}
		cs, err := svc.ListChannels(iam.WithScope(ctx, "channel", n), p, u, chatdom.ChannelFilter{})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, c := range cs {
			out[c.Name] = true
		}
		return out
	}
	if got := names(member); !got["general"] || !got["random"] || !got["secret"] {
		t.Errorf("member list = %v", got)
	}
	viewerList := names(viewer)
	if !viewerList["general"] || !viewerList["random"] || viewerList["secret"] {
		t.Errorf("viewer list = %v (private channel must be hidden)", viewerList)
	}
}

func TestChatMessages(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	p := fx.project()
	alice, bob := fx.user(false), fx.user(false)
	fx.member(p, alice, false)
	fx.member(p, bob, false)
	svc := chatsvc.New(NewChatRepository(db))

	chans, err := svc.ListChannels(ctx, p, alice, chatdom.ChannelFilter{})
	if err != nil || len(chans) != 1 || chans[0].Name != "general" {
		t.Fatalf("a project starts with #general: %v %v", chans, err)
	}
	ch := chans[0]

	var first *chatdom.Message
	for i := 0; i < 5; i++ {
		m, err := svc.PostMessage(ctx, p, ch.ID, alice, text("hi"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if m.Seq != int64(i+1) {
			t.Fatalf("seq = %d, want %d (gapless)", m.Seq, i+1)
		}
		if first == nil {
			first = m
		}
	}
	reply, err := svc.PostMessage(ctx, p, ch.ID, bob, text("re"), &first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, p, ch.ID, bob, text("nested"), &reply.ID); !errors.Is(err, chatdom.ErrThreadRootInvalid) {
		t.Errorf("reply to a reply: got %v", err)
	}
	if _, err := svc.PostMessage(ctx, p, ch.ID, bob, []byte(`[]`), nil); !errors.Is(err, chatdom.ErrMessageEmpty) {
		t.Errorf("empty message: got %v", err)
	}
	if _, err := svc.PostMessage(ctx, p, ch.ID, bob, []byte(`{"a":1}`), nil); !errors.Is(err, chatdom.ErrMessageContentBad) {
		t.Errorf("bad content: got %v", err)
	}

	// The channel timeline holds the 5 top-level messages; the reply is in the thread.
	top, more, err := svc.ListMessages(ctx, p, ch.ID, bob, chatdom.MessageFilter{Limit: 3})
	if err != nil || len(top) != 3 || !more || top[0].Seq != 3 || top[2].Seq != 5 {
		t.Fatalf("latest page = %v more=%v err=%v", seqs(top), more, err)
	}
	if top[0].ReplyCount != 0 {
		t.Error("unexpected reply count")
	}
	older, more, _ := svc.ListMessages(ctx, p, ch.ID, bob, chatdom.MessageFilter{BeforeSeq: ptr64(3), Limit: 10})
	if len(older) != 2 || more || older[0].Seq != 1 {
		t.Fatalf("older page = %v more=%v", seqs(older), more)
	}
	catch, _, _ := svc.ListMessages(ctx, p, ch.ID, bob, chatdom.MessageFilter{AfterSeq: ptr64(4), Limit: 10})
	if got := seqs(catch); len(got) != 1 || got[0] != 5 {
		t.Fatalf("after_seq=4 (top level only) = %v", got)
	}
	thread, _, _ := svc.ListMessages(ctx, p, ch.ID, bob, chatdom.MessageFilter{ThreadRootID: &first.ID})
	if len(thread) != 1 || thread[0].ID != reply.ID {
		t.Fatalf("thread = %v", seqs(thread))
	}
	rootNow, _ := NewChatRepository(db).FindMessage(ctx, ch.ID, first.ID, bob)
	if rootNow.ReplyCount != 1 {
		t.Errorf("reply count = %d", rootNow.ReplyCount)
	}

	// Only the author edits or deletes their own message.
	if _, err := svc.EditMessage(ctx, p, ch.ID, first.ID, bob, text("hijack")); !errors.Is(err, chatdom.ErrMessageNotFound) {
		t.Errorf("editing someone else's message: got %v", err)
	}
	if err := svc.DeleteOwnMessage(ctx, p, ch.ID, first.ID, bob); !errors.Is(err, chatdom.ErrMessageNotFound) {
		t.Errorf("deleting someone else's message: got %v", err)
	}
	edited, err := svc.EditMessage(ctx, p, ch.ID, first.ID, alice, text("fixed"))
	if err != nil || edited.EditedAt == nil {
		t.Fatalf("edit: %v %v", edited, err)
	}
	if err := svc.ModerateDeleteMessage(ctx, p, ch.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	gone, _ := NewChatRepository(db).FindMessage(ctx, ch.ID, first.ID, bob)
	if gone.DeletedAt == nil || string(gone.Content) != "[]" || gone.Seq != 1 {
		t.Errorf("deleted message keeps its seq but not its content: %+v", gone)
	}

	// Reactions are idempotent and counted per emoji.
	m5 := top[2]
	for _, u := range []uuid.UUID{alice, bob, bob} {
		if err := svc.AddReaction(ctx, p, ch.ID, m5.ID, u, "👍"); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := NewChatRepository(db).FindMessage(ctx, ch.ID, m5.ID, alice)
	if len(got.Reactions) != 1 || got.Reactions[0].Count != 2 || !got.Reactions[0].Mine {
		t.Errorf("reactions = %+v", got.Reactions)
	}
	if err := svc.RemoveReaction(ctx, p, ch.ID, m5.ID, alice, "👍"); err != nil {
		t.Fatal(err)
	}

	// Unread: Bob follows #general by reading it; Alice's posts then count for him.
	if err := svc.MarkRead(ctx, p, ch.ID, bob, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, p, ch.ID, alice, text("one"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, p, ch.ID, alice, text("two"), nil); err != nil {
		t.Fatal(err)
	}
	// Joining starts at the channel head, so only the two new messages count.
	sum, err := svc.UnreadSummary(ctx, bob)
	if err != nil || len(sum) != 1 || !isProject(sum[0].ProjectID, p) || sum[0].Unread != 2 {
		t.Fatalf("unread summary = %+v err=%v", sum, err)
	}
	if err := svc.MarkRead(ctx, p, ch.ID, bob, 999); err != nil {
		t.Fatal(err)
	}
	if sum, _ := svc.UnreadSummary(ctx, bob); len(sum) != 0 {
		t.Errorf("after reading everything: %+v", sum)
	}
	// A removed project member's channels stop counting.
	pgExec(t, db, `UPDATE project_members SET deleted_at = NOW() WHERE project_id = $1 AND account_id = $2`, p, bob)
	_, _ = svc.PostMessage(ctx, p, ch.ID, alice, text("x"), nil)
	if sum, _ := svc.UnreadSummary(ctx, bob); len(sum) != 0 {
		t.Errorf("a removed member still sees unread: %+v", sum)
	}
}

func ptr64(n int64) *int64 { return &n }

func seqs(ms []*chatdom.Message) []int64 {
	out := make([]int64, len(ms))
	for i, m := range ms {
		out[i] = m.Seq
	}
	return out
}

// The Inbox spans projects, lists only what the caller's own policies let them
// read in each project, puts unread first, and previews the newest message.
func TestChatInbox(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	p1, p2, p3 := fx.project(), fx.project(), fx.project()
	alice, bob := fx.user(false), fx.user(false)
	for _, p := range []uuid.UUID{p1, p2, p3} {
		fx.member(p, alice, false)
		fx.member(p, bob, false)
	}
	// Alice may read chat in p1 and p2, not in p3 (a role without chat actions).
	for _, p := range []uuid.UUID{p1, p2} {
		for _, r := range defaultroles.Instantiate(p) {
			if r.Name != "Editor" {
				continue
			}
			id := fx.role(string(r.Policy))
			pgExec(t, db, `UPDATE roles SET project_id = $2 WHERE id = $1`, id, p)
			fx.attach(id, alice, &p)
			fx.attach(id, bob, &p)
		}
	}
	read := fx.role(`{"version":"1","statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/*"]}]}`)
	fx.attach(read, alice, &p3)

	repo := NewChatRepository(db)
	svc := chatsvc.New(repo)
	authz := NewIAMAuthorizer(db)
	general := func(p uuid.UUID) *chatdom.Channel {
		cs, err := svc.ListChannels(ctx, p, bob, chatdom.ChannelFilter{})
		if err != nil || len(cs) == 0 {
			t.Fatalf("channels: %v %v", cs, err)
		}
		return cs[0]
	}
	g1, g2, g3 := general(p1), general(p2), general(p3)
	// Alice follows #general of all three projects.
	for i, c := range []*chatdom.Channel{g1, g2, g3} {
		p := []uuid.UUID{p1, p2, p3}[i]
		if err := repo.AddMember(ctx, c.ID, alice); err != nil {
			t.Fatal(err)
		}
		_ = p
	}
	post := func(p uuid.UUID, c *chatdom.Channel, s string) {
		if _, err := svc.PostMessage(ctx, p, c.ID, bob, text(s), nil); err != nil {
			t.Fatal(err)
		}
	}
	post(p1, g1, "older in one")
	post(p2, g2, "newest in two")
	post(p3, g3, "hidden in three")
	// p1 has been read by alice: only p2 is unread (p3 is unreadable for her).
	if err := repo.MarkRead(ctx, g1.ID, alice, 99); err != nil {
		t.Fatal(err)
	}

	inbox := func(f chatdom.InboxFilter) []chatdom.InboxItem {
		t.Helper()
		ids, err := svc.InboxProjects(ctx, alice)
		if err != nil || len(ids) != 3 {
			t.Fatalf("projects: %v %v", ids, err)
		}
		strs := make([]string, len(ids))
		for i, id := range ids {
			strs[i] = id.String()
		}
		by, err := authz.ListScopes(ctx, iam.Principal{Type: iam.PrincipalUser, ID: alice.String()}, string(iam.ActionChannelsRead), strs, "channel")
		if err != nil {
			t.Fatal(err)
		}
		items, err := svc.Inbox(iam.WithProjectScopes(ctx, "channel", by), alice, f)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}

	all := inbox(chatdom.InboxFilter{})
	if len(all) != 2 {
		t.Fatalf("inbox = %+v (the project alice cannot read chat in must not appear)", all)
	}
	if !isProject(all[0].ProjectID, p2) || all[0].UnreadCount != 1 || all[0].Preview != "newest in two" || all[0].LastAuthor == "" {
		t.Errorf("unread channel first with a preview: %+v", all[0])
	}
	if !isProject(all[1].ProjectID, p1) || all[1].UnreadCount != 0 {
		t.Errorf("read channel second: %+v", all[1])
	}
	if un := inbox(chatdom.InboxFilter{UnreadOnly: true}); len(un) != 1 || !isProject(un[0].ProjectID, p2) {
		t.Errorf("unread only = %+v", un)
	}
	if one := inbox(chatdom.InboxFilter{ProjectID: &p1}); len(one) != 1 || !isProject(one[0].ProjectID, p1) {
		t.Errorf("one project = %+v", one)
	}

	// The summary names the project; read-all clears it, limited to one project first.
	sum, _ := svc.UnreadSummary(ctx, alice)
	if len(sum) == 0 || sum[0].ProjectName == "" {
		t.Errorf("summary = %+v", sum)
	}
	if err := svc.MarkAllRead(ctx, alice, &p1); err != nil {
		t.Fatal(err)
	}
	if un := inbox(chatdom.InboxFilter{UnreadOnly: true}); len(un) != 1 {
		t.Errorf("read-all for p1 must not touch p2: %+v", un)
	}
	if err := svc.MarkAllRead(ctx, alice, nil); err != nil {
		t.Fatal(err)
	}
	if un := inbox(chatdom.InboxFilter{UnreadOnly: true}); len(un) != 0 {
		t.Errorf("after read-all: %+v", un)
	}
}

// Direct messages: a pair has one conversation whichever side starts it; it is
// readable only by its two members, by policy and not by code; it works with
// any account kind; and the Inbox, unread counts and directory include it.
func TestChatDirectMessages(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	p := fx.project()
	alice, bob, carol := fx.user(false), fx.user(false), fx.user(false)
	agent := fx.agent(nil, false)
	sa := fx.serviceAccount(false)
	for _, a := range []uuid.UUID{alice, bob, agent, sa} {
		fx.member(p, a, false)
	}
	var userPolicy string
	for _, r := range defaultroles.Platform() {
		if r.Name == defaultroles.User {
			userPolicy = string(r.Policy)
		}
	}
	userRole := fx.role(userPolicy)
	for _, a := range []uuid.UUID{alice, bob, carol, agent} {
		fx.attach(userRole, a, nil)
	}

	repo := NewChatRepository(db)
	svc := chatsvc.New(repo)
	authz := NewIAMAuthorizer(db)
	who := func(id uuid.UUID, kind string) iam.Principal { return iam.Principal{Type: kind, ID: id.String()} }

	// One DM per pair, found from either side.
	dm, err := svc.StartDM(ctx, alice, bob)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.StartDM(ctx, bob, alice)
	if err != nil || again.ID != dm.ID {
		t.Fatalf("the pair must share one dm: %v %v", again, err)
	}
	if dm.Peer == nil || dm.Peer.AccountID != bob || dm.Kind != chatdom.KindDM || dm.ProjectID != nil || again.Peer.AccountID != alice {
		t.Errorf("each side sees the other as peer: %+v / %+v", dm.Peer, again.Peer)
	}
	if _, err := svc.StartDM(ctx, alice, alice); !errors.Is(err, chatdom.ErrDMSelf) {
		t.Errorf("self dm: %v", err)
	}
	if _, err := svc.StartDM(ctx, alice, uuid.New()); !errors.Is(err, chatdom.ErrDMPeerNotFound) {
		t.Errorf("unknown peer: %v", err)
	}

	// The policy, through the real authorizer and loaders: members only.
	can := func(u uuid.UUID, kind string, a iam.Action) bool {
		t.Helper()
		r, err := authz.Authorize(ctx, who(u, kind), string(a), "channel/"+dm.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		return r.Allowed
	}
	if !can(alice, iam.PrincipalUser, iam.ActionChannelsRead) || !can(bob, iam.PrincipalUser, iam.ActionMessagesWrite) {
		t.Error("both members may read and post")
	}
	if can(carol, iam.PrincipalUser, iam.ActionChannelsRead) || can(carol, iam.PrincipalUser, iam.ActionMessagesWrite) {
		t.Error("an outsider must not read or post in someone else's dm")
	}
	if r, _ := authz.Authorize(ctx, who(alice, iam.PrincipalUser), string(iam.ActionChannelsRead), "channel"); !r.Allowed {
		t.Error("the collection check (list, directory) must pass")
	}

	// Messages, with the workspace scope (no project).
	m, err := svc.PostMessage(ctx, uuid.Nil, dm.ID, alice, text("hi bob"), nil)
	if err != nil || m.Seq != 1 {
		t.Fatalf("post: %v %v", m, err)
	}
	if _, err := svc.PostMessage(ctx, p, dm.ID, alice, text("wrong scope"), nil); !errors.Is(err, chatdom.ErrChannelNotFound) {
		t.Errorf("a dm is not reachable through a project: %v", err)
	}
	if ms, _, err := svc.ListMessages(ctx, uuid.Nil, dm.ID, bob, chatdom.MessageFilter{}); err != nil || len(ms) != 1 {
		t.Fatalf("list: %v %v", ms, err)
	}
	rcpt, _ := repo.ChannelRecipients(ctx, dm.ID)
	if len(rcpt) != 2 {
		t.Errorf("a dm notifies both members even with no project: %v", rcpt)
	}

	// A DM with an agent and one with a service account.
	agentDM, err := svc.StartDM(ctx, alice, agent)
	if err != nil || agentDM.Peer.Kind != "agent" {
		t.Fatalf("agent dm: %+v %v", agentDM, err)
	}
	saDM, err := svc.StartDM(ctx, alice, sa)
	if err != nil || saDM.Peer.Kind != "service_account" {
		t.Fatalf("service account dm: %+v %v", saDM, err)
	}
	if _, err := svc.PostMessage(ctx, uuid.Nil, saDM.ID, alice, text("deploy now"), nil); err != nil {
		t.Fatal(err)
	}

	// The list is scoped by the caller's policy in SQL.
	list := func(u uuid.UUID) []*chatdom.Channel {
		t.Helper()
		n, err := authz.ListRootScope(ctx, who(u, iam.PrincipalUser), string(iam.ActionChannelsRead), "channel")
		if err != nil {
			t.Fatal(err)
		}
		cs, err := svc.ListDMs(iam.WithScope(ctx, "channel", n), u)
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	if got := list(alice); len(got) != 3 {
		t.Errorf("alice has three dms, got %d", len(got))
	}
	if got := list(bob); len(got) != 1 || got[0].Peer.AccountID != alice || got[0].UnreadCount != 1 {
		t.Errorf("bob's list: %+v", got)
	}
	if got := list(carol); len(got) != 0 {
		t.Errorf("carol has none: %+v", got)
	}
	// A principal whose policies hold no direct-message statement sees none, though
	// she is a member of the row.
	noRole := fx.user(false)
	if _, err := svc.StartDM(ctx, noRole, bob); err != nil {
		t.Fatal(err)
	}
	if got := list(noRole); len(got) != 0 {
		t.Errorf("without the policy statement nothing is listed: %+v", got)
	}

	// Inbox and unread summary include direct messages, unread first.
	by, err := authz.ListScopes(ctx, who(bob, iam.PrincipalUser), string(iam.ActionChannelsRead), nil, "channel")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := authz.ListRootScope(ctx, who(bob, iam.PrincipalUser), string(iam.ActionChannelsRead), "channel")
	items, err := svc.Inbox(iam.WithScope(iam.WithProjectScopes(ctx, "channel", by), "channel", root), bob, chatdom.InboxFilter{})
	if err != nil || len(items) == 0 || items[0].ProjectID != nil || items[0].Peer == nil || items[0].Peer.AccountID != alice || items[0].Preview != "hi bob" {
		t.Fatalf("inbox = %+v %v", items, err)
	}
	sum, _ := svc.UnreadSummary(ctx, bob)
	var dmRow *chatdom.UnreadSummary
	for i := range sum {
		if sum[i].ProjectID == nil {
			dmRow = &sum[i]
		}
	}
	if dmRow == nil || dmRow.Unread != 1 {
		t.Errorf("unread summary must carry a direct-message row: %+v", sum)
	}
	if err := svc.MarkAllRead(ctx, bob, nil); err != nil {
		t.Fatal(err)
	}
	if s, _ := svc.UnreadSummary(ctx, bob); len(s) != 0 {
		t.Errorf("after read-all: %+v", s)
	}

	// The directory: people sharing a project, their agents and service accounts,
	// global agents; never strangers, never yourself, and kind narrows it.
	names := func(kind, q string) map[string]bool {
		es, err := svc.Directory(ctx, alice, q, kind)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, e := range es {
			out[e.Kind+":"+e.AccountID.String()] = true
		}
		return out
	}
	all := names("", "")
	for _, want := range []string{"user:" + bob.String(), "agent:" + agent.String(), "service_account:" + sa.String()} {
		if !all[want] {
			t.Errorf("directory is missing %s: %v", want, all)
		}
	}
	if all["user:"+carol.String()] || all["user:"+alice.String()] {
		t.Errorf("directory must not list strangers or the caller: %v", all)
	}
	if only := names("agent", ""); len(only) != 1 || !only["agent:"+agent.String()] {
		t.Errorf("kind filter: %v", only)
	}
	if got := names("", "%"); len(got) != 0 {
		t.Errorf("LIKE wildcards in the query are literal: %v", got)
	}
}

func isProject(got *uuid.UUID, want uuid.UUID) bool { return got != nil && *got == want }

func mention(id uuid.UUID, name string) map[string]any {
	return map[string]any{"type": "teamMention", "props": map[string]any{"id": id.String(), "name": name}}
}

func blocksWith(text string, inlines ...map[string]any) []byte {
	content := []any{map[string]any{"type": "text", "text": text}}
	for _, in := range inlines {
		content = append(content, in)
	}
	b, _ := json.Marshal([]any{map[string]any{"type": "paragraph", "content": content}})
	return b
}

// Mentions, followed threads and saved messages: written with the messages,
// listed across projects, and limited by what the caller's own policies let
// them read.
func TestChatMentionsThreadsSaved(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	p1, p2 := fx.project(), fx.project()
	alice, bob, carol := fx.user(false), fx.user(false), fx.user(false)
	for _, p := range []uuid.UUID{p1, p2} {
		for _, u := range []uuid.UUID{alice, bob, carol} {
			fx.member(p, u, false)
		}
	}
	// Editors in p1; in p2 bob holds a role with no chat actions.
	for _, r := range defaultroles.Instantiate(p1) {
		if r.Name == "Editor" {
			id := fx.role(string(r.Policy))
			pgExec(t, db, `UPDATE roles SET project_id = $2 WHERE id = $1`, id, p1)
			for _, u := range []uuid.UUID{alice, bob, carol} {
				fx.attach(id, u, &p1)
			}
		}
	}
	for _, r := range defaultroles.Instantiate(p2) {
		if r.Name == "Editor" {
			id := fx.role(string(r.Policy))
			pgExec(t, db, `UPDATE roles SET project_id = $2 WHERE id = $1`, id, p2)
			fx.attach(id, alice, &p2)
		}
	}
	noChat := fx.role(`{"version":"1","statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/*"]}]}`)
	fx.attach(noChat, bob, &p2)

	repo := NewChatRepository(db)
	svc := chatsvc.New(repo)
	authz := NewIAMAuthorizer(db)
	general := func(p uuid.UUID) *chatdom.Channel {
		cs, err := svc.ListChannels(ctx, p, alice, chatdom.ChannelFilter{})
		if err != nil || len(cs) == 0 {
			t.Fatalf("channels: %v %v", cs, err)
		}
		return cs[0]
	}
	g1, g2 := general(p1), general(p2)
	secret, err := svc.CreateChannel(ctx, p1, alice, chatdom.KindPrivate, "secret", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	scoped := func(u uuid.UUID) context.Context {
		t.Helper()
		who := iam.Principal{Type: iam.PrincipalUser, ID: u.String()}
		ids, _ := svc.InboxProjects(ctx, u)
		strs := make([]string, len(ids))
		for i, id := range ids {
			strs[i] = id.String()
		}
		by, err := authz.ListScopes(ctx, who, string(iam.ActionChannelsRead), strs, "channel")
		if err != nil {
			t.Fatal(err)
		}
		root, err := authz.ListRootScope(ctx, who, string(iam.ActionChannelsRead), "channel")
		if err != nil {
			t.Fatal(err)
		}
		return iam.WithScope(iam.WithProjectScopes(ctx, "channel", by), "channel", root)
	}

	// The channel list of one project is scoped as the route does it.
	listCh := func(u, p uuid.UUID) []*chatdom.Channel {
		t.Helper()
		n, err := authz.ListScope(ctx, iam.Principal{Type: iam.PrincipalUser, ID: u.String()}, string(iam.ActionChannelsRead), p.String(), "channel")
		if err != nil {
			t.Fatal(err)
		}
		cs, err := svc.ListChannels(iam.WithScope(ctx, "channel", n), p, u, chatdom.ChannelFilter{})
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}

	// ---- Mentions
	m1, err := svc.PostMessage(ctx, p1, g1.ID, alice, blocksWith("review please ", mention(bob, "Bob")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, p2, g2.ID, alice, blocksWith("and here ", mention(bob, "Bob")), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, p1, secret.ID, alice, blocksWith("secret ", mention(bob, "Bob")), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, p1, g1.ID, alice, blocksWith("self ", mention(alice, "Alice")), nil); err != nil {
		t.Fatal(err)
	}
	refs, err := svc.Mentions(scoped(bob), bob)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].MessageID != m1.ID || !refs[0].Unread || refs[0].AuthorName == "" || !strings.Contains(refs[0].Preview, "@Bob") || !isProject(refs[0].ProjectID, p1) {
		t.Fatalf("bob's mentions = %+v (only the readable channel's, never p2's or the private channel's)", refs)
	}
	if mine, _ := svc.Mentions(scoped(alice), alice); len(mine) != 0 {
		t.Errorf("you are never notified of your own mention: %+v", mine)
	}
	// Mentioned in a public channel, bob follows it: it shows with a mention count.
	cs := listCh(bob, p1)
	var bg *chatdom.Channel
	for _, c := range cs {
		if c.ID == g1.ID {
			bg = c
		}
		if c.ID == secret.ID {
			t.Errorf("bob must not see the private channel")
		}
	}
	if bg == nil || !bg.IsMember || bg.MentionCount != 1 || bg.UnreadCount < 1 {
		t.Fatalf("a mentioned member follows the channel, with a mention count: %+v", bg)
	}
	if err := svc.MarkRead(ctx, p1, g1.ID, bob, 99); err != nil {
		t.Fatal(err)
	}
	refs, _ = svc.Mentions(scoped(bob), bob)
	cs = listCh(bob, p1)
	for _, c := range cs {
		if c.ID == g1.ID && c.MentionCount != 0 {
			t.Errorf("reading clears the mention count: %+v", c)
		}
	}
	if len(refs) != 1 || refs[0].Unread {
		t.Errorf("a read mention stays listed but not unread: %+v", refs)
	}
	// Editing replaces mentions.
	if _, err := svc.EditMessage(ctx, p1, g1.ID, m1.ID, alice, blocksWith("no mention now")); err != nil {
		t.Fatal(err)
	}
	if refs, _ = svc.Mentions(scoped(bob), bob); len(refs) != 0 {
		t.Errorf("an edit that removes the mention removes it from the list: %+v", refs)
	}

	// ---- Threads
	root, err := svc.PostMessage(ctx, p1, g1.ID, alice, blocksWith("let's discuss"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, p1, g1.ID, bob, blocksWith("my view"), &root.ID); err != nil {
		t.Fatal(err)
	}
	ths, err := svc.Threads(scoped(alice), alice)
	if err != nil || len(ths) != 1 || ths[0].MessageID != root.ID || ths[0].UnreadCount != 1 || !ths[0].Unread || ths[0].ReplyCount != 1 ||
		ths[0].RootPreview != "let's discuss" || ths[0].Preview != "my view" || ths[0].AuthorName == "" {
		t.Fatalf("the root's author follows and sees the reply unread: %+v %v", ths, err)
	}
	if mine, _ := svc.Threads(scoped(bob), bob); len(mine) != 1 || mine[0].UnreadCount != 0 {
		t.Errorf("a replier follows the thread, having read it: %+v", mine)
	}
	if err := svc.MarkThreadRead(ctx, p1, g1.ID, root.ID, alice, 999); err != nil {
		t.Fatal(err)
	}
	if ths, _ = svc.Threads(scoped(alice), alice); ths[0].UnreadCount != 0 || ths[0].Unread {
		t.Errorf("reading the thread clears it: %+v", ths)
	}
	// Following by hand starts from the newest reply; unfollowing removes it.
	if err := svc.FollowThread(ctx, p1, g1.ID, root.ID, carol, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.FollowThread(ctx, p1, g1.ID, root.ID, carol, true); err != nil {
		t.Errorf("following twice is fine: %v", err)
	}
	if _, err := svc.PostMessage(ctx, p1, g1.ID, bob, blocksWith("another"), &root.ID); err != nil {
		t.Fatal(err)
	}
	if cth, _ := svc.Threads(scoped(carol), carol); len(cth) != 1 || cth[0].UnreadCount != 1 {
		t.Errorf("a hand-followed thread counts replies after the follow: %+v", cth)
	}
	if err := svc.FollowThread(ctx, p1, g1.ID, root.ID, carol, false); err != nil {
		t.Fatal(err)
	}
	if cth, _ := svc.Threads(scoped(carol), carol); len(cth) != 0 {
		t.Errorf("unfollowed: %+v", cth)
	}
	reply, _, _ := svc.ListMessages(ctx, p1, g1.ID, bob, chatdom.MessageFilter{ThreadRootID: &root.ID})
	if err := svc.FollowThread(ctx, p1, g1.ID, reply[0].ID, carol, true); !errors.Is(err, chatdom.ErrThreadRootInvalid) {
		t.Errorf("only a top-level message starts a thread: %v", err)
	}
	if err := svc.FollowThread(ctx, p1, g2.ID, root.ID, carol, true); err == nil {
		t.Errorf("a message of another channel is not a thread of this one")
	}

	// ---- Saved
	if err := svc.SaveMessage(ctx, p1, g1.ID, root.ID, bob, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveMessage(ctx, p1, g1.ID, root.ID, bob, true); err != nil {
		t.Errorf("saving twice is fine: %v", err)
	}
	if err := svc.SaveMessage(ctx, p1, g1.ID, uuid.New(), bob, true); !errors.Is(err, chatdom.ErrMessageNotFound) {
		t.Errorf("saving a message that does not exist: %v", err)
	}
	if saved, _ := svc.Saved(scoped(bob), bob); len(saved) != 1 || saved[0].MessageID != root.ID || saved[0].Unread {
		t.Errorf("saved = %+v", saved)
	}
	msgs, _, _ := svc.ListMessages(ctx, p1, g1.ID, bob, chatdom.MessageFilter{Limit: 50})
	var flagged, followed int
	for _, m := range msgs {
		if m.Saved {
			flagged++
		}
		if m.Following {
			followed++
		}
	}
	if flagged != 1 || followed != 1 {
		t.Errorf("messages carry the caller's flags: saved=%d following=%d", flagged, followed)
	}
	if other, _ := svc.Saved(scoped(alice), alice); len(other) != 0 {
		t.Errorf("saves are private: %+v", other)
	}
	// Losing the right to read the channel hides the saved message too.
	pgExec(t, db, `DELETE FROM role_attachments WHERE account_id = $1 AND project_id = $2`, bob, p1)
	if saved, _ := svc.Saved(scoped(bob), bob); len(saved) != 0 {
		t.Errorf("a saved message in a channel I can no longer read must not be listed: %+v", saved)
	}
	if err := svc.SaveMessage(ctx, p1, g1.ID, root.ID, bob, false); err != nil {
		t.Fatal(err)
	}
}

// Starring follows the channel, shows on lists and the Inbox, and unstarring
// keeps following.
func TestChatStarred(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	p := fx.project()
	alice, bob := fx.user(false), fx.user(false)
	fx.member(p, alice, false)
	fx.member(p, bob, false)
	svc := chatsvc.New(NewChatRepository(db))
	cs, err := svc.ListChannels(ctx, p, alice, chatdom.ChannelFilter{})
	if err != nil || len(cs) == 0 {
		t.Fatalf("channels: %v %v", cs, err)
	}
	ch := cs[0]
	if ch.Starred || ch.IsMember {
		t.Fatalf("a fresh channel is neither followed nor starred: %+v", ch)
	}
	if err := svc.StarChannel(ctx, p, ch.ID, alice, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.StarChannel(ctx, p, ch.ID, alice, true); err != nil {
		t.Errorf("starring twice is fine: %v", err)
	}
	got, _ := svc.GetChannel(ctx, p, ch.ID, alice)
	if !got.Starred || !got.IsMember {
		t.Errorf("starring follows and stars: %+v", got)
	}
	if other, _ := svc.GetChannel(ctx, p, ch.ID, bob); other.Starred {
		t.Errorf("stars are per person")
	}
	items, err := svc.Inbox(ctx, alice, chatdom.InboxFilter{})
	if err != nil || len(items) != 1 || !items[0].Starred {
		t.Fatalf("inbox rows carry the flag: %+v %v", items, err)
	}
	if err := svc.StarChannel(ctx, p, ch.ID, alice, false); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.GetChannel(ctx, p, ch.ID, alice)
	if got.Starred || !got.IsMember {
		t.Errorf("unstarring keeps following: %+v", got)
	}
	if err := svc.StarChannel(ctx, p, uuid.New(), alice, true); !errors.Is(err, chatdom.ErrChannelNotFound) {
		t.Errorf("a channel that does not exist: %v", err)
	}
}

func TestChatAgentDirectMessage(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	alice := fx.user(false)
	agent := fx.agent(nil, false)
	repo := NewChatRepository(db)
	svc := chatsvc.New(repo)

	if ok, err := repo.IsGlobalAgent(ctx, agent); err != nil || !ok {
		t.Fatalf("a global agent is one: %v %v", ok, err)
	}
	if ok, _ := repo.IsGlobalAgent(ctx, alice); ok {
		t.Fatal("a person is not an agent")
	}

	dm, err := svc.StartDM(ctx, alice, agent)
	if err != nil {
		t.Fatal(err)
	}
	link, err := repo.EnsureAgentLink(ctx, dm.ID, agent, alice)
	if err != nil || link.ForwardedSeq != 0 || link.BridgedIndex != -1 || link.SessionID != nil {
		t.Fatalf("new link: %+v %v", link, err)
	}
	if again, _ := repo.EnsureAgentLink(ctx, dm.ID, agent, alice); again.AccountID != alice {
		t.Fatal("the link is created once")
	}

	// What the person wrote is handed over once, even when two callers race.
	for _, s := range []string{"one", "two"} {
		if _, err := svc.PostMessage(ctx, uuid.Nil, dm.ID, alice, text(s), nil); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := repo.MessagesToForward(ctx, dm.ID, alice, 0)
	if err != nil || len(pending) != 2 || pending[0].ContentText != "one" {
		t.Fatalf("pending: %v %v", pending, err)
	}
	if ok, _ := repo.ClaimForward(ctx, dm.ID, 0, 2); !ok {
		t.Fatal("first claim wins")
	}
	if ok, _ := repo.ClaimForward(ctx, dm.ID, 0, 2); ok {
		t.Fatal("second claim must lose")
	}
	if err := repo.ReleaseForward(ctx, dm.ID, 0, 2); err != nil {
		t.Fatal(err)
	}
	if ok, _ := repo.ClaimForward(ctx, dm.ID, 0, 2); !ok {
		t.Fatal("a released claim can be taken again")
	}
	if rest, _ := repo.MessagesToForward(ctx, dm.ID, alice, 2); len(rest) != 0 {
		t.Fatalf("nothing is left after seq 2: %v", rest)
	}

	// The agent runtime's own rows are not part of this fixture, so their
	// foreign keys are switched off for this connection.
	db.SetMaxOpenConns(1)
	pgExec(t, db, `SET session_replication_role = replica`)
	session, conv, other := uuid.New(), uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, status, chat_session_id, title)
		VALUES ($1, $2, $3, 'chat_message', $4, 'running', $5, 'First run')`, conv, agent, uuid.New(), alice, session)
	pgExec(t, db, `INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, status, chat_session_id, created_at)
		VALUES ($1, $2, $3, 'chat_message', $4, 'finished', $5, NOW() - interval '1 hour')`, other, agent, uuid.New(), alice, session)
	for i, e := range []struct{ typ, body string }{
		{"user_message", `{"content":{"type":"text","text":"one"}}`},
		{"agent_message_chunk", `{"content":{"type":"text","text":"Hello "}}`},
		{"tool_call", `{"title":"search"}`},
		{"agent_message_chunk", `{"content":{"type":"text","text":"there."}}`},
		{"turn_end", `{}`},
	} {
		pgExec(t, db, `INSERT INTO agent_conversation_events (conversation_id, event_index, event_type, event_source, payload)
			VALUES ($1, $2, $3, 'agent', $4::jsonb)`, conv, i, e.typ, e.body)
	}

	if err := repo.SetAgentRun(ctx, dm.ID, session, conv); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.FindAgentLinkBySession(ctx, session); err != nil || got == nil || got.ChannelID != dm.ID || *got.ConversationID != conv {
		t.Fatalf("link by session: %+v %v", got, err)
	}
	reply, err := repo.AgentReply(ctx, conv, -1, 4)
	if err != nil || reply != "Hello there." {
		t.Fatalf("reply = %q, %v", reply, err)
	}
	if reply, _ := repo.AgentReply(ctx, conv, 3, 4); reply != "" {
		t.Fatalf("nothing after the bridged event: %q", reply)
	}
	if reply, _ := repo.AgentReply(ctx, conv, -1, 1); reply != "Hello" {
		t.Fatalf("the turn's own events only: %q", reply)
	}
	if err := repo.SetBridgedIndex(ctx, dm.ID, 3); err != nil {
		t.Fatal(err)
	}
	// Moving to another run starts over; staying on the same one does not.
	if err := repo.SetAgentRun(ctx, dm.ID, session, conv); err != nil {
		t.Fatal(err)
	}
	if l, _ := repo.FindAgentLinkBySession(ctx, session); l.BridgedIndex != 3 {
		t.Fatalf("same run keeps its index: %d", l.BridgedIndex)
	}
	if err := repo.SetAgentRun(ctx, dm.ID, session, other); err != nil {
		t.Fatal(err)
	}
	l, _ := repo.FindAgentLinkBySession(ctx, session)
	if l.BridgedIndex != -1 {
		t.Fatalf("a new run starts over: %d", l.BridgedIndex)
	}

	runs, err := repo.ListAgentRuns(ctx, l, 10)
	if err != nil || len(runs) != 2 || runs[0].ID != conv || runs[0].Title != "First run" || runs[0].Status != "running" {
		t.Fatalf("runs (newest first): %+v %v", runs, err)
	}

	// A message of the agent remembers the run it reports on.
	m := &chatdom.Message{ChannelID: dm.ID, AuthorID: agent, Content: text("Hello there."), ContentText: "Hello there.", RunID: &conv}
	if err := repo.CreateMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindMessage(ctx, dm.ID, m.ID, alice)
	if err != nil || got.RunID == nil || *got.RunID != conv || got.AuthorName == "" {
		t.Fatalf("message with run: %+v %v", got, err)
	}
}

// SearchMessages matches message text with a trigram ILIKE. The query argument
// is $2 (the account is $1) and the optional project is $3, so a project
// filter, the LIKE escape and the LIMIT must all line up.
func TestChatSearchMessages(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	p1, p2 := fx.project(), fx.project()
	alice := fx.user(false)
	repo := NewChatRepository(db)
	svc := chatsvc.New(repo)
	var chans [2]*chatdom.Channel
	for i, p := range []uuid.UUID{p1, p2} {
		fx.member(p, alice, false)
		list, err := svc.ListChannels(ctx, p, alice, chatdom.ChannelFilter{})
		if err != nil || len(list) != 1 {
			t.Fatalf("project %d channels: %v %v", i, list, err)
		}
		chans[i] = list[0]
	}
	post := func(p uuid.UUID, ch *chatdom.Channel, s string) {
		t.Helper()
		if _, err := svc.PostMessage(ctx, p, ch.ID, alice, text(s), nil); err != nil {
			t.Fatal(err)
		}
	}
	post(p1, chans[0], "the rollback plan for the migration")
	post(p1, chans[0], "we are 100% sure about it")
	post(p1, chans[0], "we are 1000 sure about it")
	post(p2, chans[1], "another rollback plan elsewhere")

	all, err := repo.SearchMessages(ctx, alice, "rollback", nil, 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("without a project filter both rollback messages match: %d %v", len(all), err)
	}
	one, err := repo.SearchMessages(ctx, alice, "rollback", &p1, 10)
	if err != nil || len(one) != 1 || one[0].ProjectID == nil || *one[0].ProjectID != p1 {
		t.Fatalf("a project filter keeps that project's match: %v %v", one, err)
	}
	limited, err := repo.SearchMessages(ctx, alice, "rollback", nil, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("the limit applies: %d %v", len(limited), err)
	}
	// "%" in the query is text, not a wildcard.
	pct, err := repo.SearchMessages(ctx, alice, "100%", nil, 10)
	if err != nil || len(pct) != 1 {
		t.Fatalf("100%% matches only the literal text: %d %v", len(pct), err)
	}
}
