import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// Mirrors services/api/internal/transport/http/dto/chat_dto.go.

export type ChannelKind = "public" | "private" | "voice";

/**
 * The "project" of a direct message. Every chat call, cache key and component
 * takes a project id; a direct message has none, so this stands in for it and
 * the paths and permissions switch on it (see `base` below).
 */
export const DM_SCOPE = "dm";

export type AccountKind = "user" | "agent" | "service_account";

/** The other party of a direct message. */
export interface ChatPeer {
	account_id: string;
	name: string;
	kind: AccountKind;
}

/** An account a direct message can be started with. */
export interface DirectoryEntry {
	account_id: string;
	name: string;
	kind: AccountKind;
	description: string;
}

export interface Channel {
	id: string;
	scope: "project" | "workspace";
	project_id: string | null;
	kind: ChannelKind | "dm" | "group_dm";
	name: string;
	topic: string;
	last_seq: number;
	archived_at: string | null;
	created_by: string | null;
	created_at: string;
	/** Whether the caller follows the channel (always true for a private one
	 * they can read). Only followed channels have read state. */
	is_member: boolean;
	last_read_seq: number;
	unread_count: number;
	/** The other party, for a direct message. */
	peer: ChatPeer | null;
	/** Unread messages in the channel that mention the caller. */
	mention_count: number;
	/** The caller starred it. */
	starred: boolean;
}

/** One block of a message, BlockNote-compatible (see chat-content.ts). */
export interface ChatBlock {
	type: string;
	content?: ChatInline[];
	children?: ChatBlock[];
	props?: Record<string, unknown>;
}

export interface ChatInline {
	type: string;
	text?: string;
	styles?: Record<string, boolean | string>;
	href?: string;
	content?: ChatInline[];
	props?: Record<string, unknown>;
}

export interface ChatReaction {
	emoji: string;
	count: number;
	mine: boolean;
}

export interface ChatMessage {
	id: string;
	channel_id: string;
	seq: number;
	thread_root_id: string | null;
	author_id: string;
	author_name: string;
	content: ChatBlock[];
	edited_at: string | null;
	deleted_at: string | null;
	created_at: string;
	reply_count: number;
	reactions: ChatReaction[];
	/** The caller saved this message. */
	saved: boolean;
	/** The caller follows the thread this message starts. */
	following: boolean;
	/** The agent run this message reports on (an agent's reply in a direct message). */
	run_id?: string | null;
}

export interface MessagePage {
	items: ChatMessage[];
	has_more: boolean;
}

export interface UnreadSummaryItem {
	/** null: the caller's direct messages. */
	project_id: string | null;
	project_name: string;
	unread: number;
	channels: number;
}

/** One followed channel in the cross-project Inbox, with the newest message. */
export interface InboxItem {
	/** null for a direct message, whose `peer` is set instead. */
	project_id: string | null;
	peer: ChatPeer | null;
	project_name: string;
	channel_id: string;
	channel_name: string;
	kind: Channel["kind"];
	last_seq: number;
	last_read_seq: number;
	unread_count: number;
	last_message_at: string | null;
	last_author: string;
	preview: string;
	/** Unread messages in the channel that mention the caller. */
	mention_count: number;
	/** The caller starred it. */
	starred: boolean;
}

/**
 * One message of the Mentions, Threads or Saved list, with where it lives. For
 * a thread it is the root, with the latest reply as the preview.
 */
export interface MessageRef {
	project_id: string | null;
	project_name: string;
	peer: ChatPeer | null;
	channel_id: string;
	channel_name: string;
	kind: Channel["kind"];
	message_id: string;
	seq: number;
	thread_root_id: string | null;
	/** The thread root's seq (the message's own, for a thread row). */
	root_seq: number | null;
	author_id: string;
	author_name: string;
	preview: string;
	root_preview: string;
	created_at: string;
	activity_at: string;
	unread: boolean;
	unread_count: number;
	reply_count: number;
}

export type MessageRefList = "mentions" | "threads" | "saved";

export interface InboxQuery {
	unreadOnly?: boolean;
	projectId?: string;
}

export const chatKeys = {
	all: (projectId: string) => ["chat", projectId] as const,
	channels: (projectId: string) => ["chat", projectId, "channels"] as const,
	messages: (projectId: string, channelId: string) =>
		["chat", projectId, "messages", channelId] as const,
	thread: (projectId: string, channelId: string, rootId: string) =>
		["chat", projectId, "thread", channelId, rootId] as const,
	unread: () => ["chat", "unread-summary"] as const,
	inboxAll: () => ["chat", "inbox"] as const,
	refs: (list: MessageRefList) => ["chat", "inbox", "refs", list] as const,
	agent: (channelId: string) => ["chat", DM_SCOPE, "agent", channelId] as const,
	inbox: (q: InboxQuery) =>
		["chat", "inbox", q.unreadOnly ?? false, q.projectId ?? ""] as const,
};

const base = (projectId: string) =>
	projectId === DM_SCOPE ? "/chat/dms" : `/projects/${projectId}/channels`;

/** The scope a channel or inbox row lives in: its project, or direct messages. */
export const scopeOf = (projectId: string | null | undefined): string =>
	projectId ?? DM_SCOPE;

export async function listChannels(projectId: string): Promise<Channel[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Channel[]>>(
		base(projectId),
	);
	return data.data;
}

export async function createChannel(
	projectId: string,
	body: {
		kind: ChannelKind;
		name: string;
		topic?: string;
		member_ids?: string[];
	},
): Promise<Channel> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Channel>>(
		base(projectId),
		body,
	);
	return data.data;
}

export async function updateChannel(
	projectId: string,
	channelId: string,
	body: { name?: string; topic?: string },
): Promise<Channel> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Channel>>(
		`${base(projectId)}/${channelId}`,
		body,
	);
	return data.data;
}

export async function archiveChannel(
	projectId: string,
	channelId: string,
): Promise<void> {
	await apiClient.instance.post(`${base(projectId)}/${channelId}/archive`);
}

export async function joinChannel(
	projectId: string,
	channelId: string,
): Promise<void> {
	await apiClient.instance.post(`${base(projectId)}/${channelId}/join`);
}

export async function leaveChannel(
	projectId: string,
	channelId: string,
): Promise<void> {
	await apiClient.instance.post(`${base(projectId)}/${channelId}/leave`);
}

export interface MessageQuery {
	afterSeq?: number;
	beforeSeq?: number;
	threadRootId?: string;
	pageSize?: number;
}

export async function listMessages(
	projectId: string,
	channelId: string,
	q: MessageQuery = {},
): Promise<MessagePage> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<MessagePage>>(
		`${base(projectId)}/${channelId}/messages`,
		{
			params: {
				after_seq: q.afterSeq,
				before_seq: q.beforeSeq,
				thread_root_id: q.threadRootId,
				page_size: q.pageSize,
			},
		},
	);
	return data.data;
}

export async function postMessage(
	projectId: string,
	channelId: string,
	content: ChatBlock[],
	threadRootId?: string,
): Promise<ChatMessage> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<ChatMessage>>(
		`${base(projectId)}/${channelId}/messages`,
		{ content, thread_root_id: threadRootId ?? null },
	);
	return data.data;
}

export async function editMessage(
	projectId: string,
	channelId: string,
	messageId: string,
	content: ChatBlock[],
): Promise<ChatMessage> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<ChatMessage>>(
		`${base(projectId)}/${channelId}/messages/${messageId}`,
		{ content },
	);
	return data.data;
}

export async function deleteMessage(
	projectId: string,
	channelId: string,
	messageId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`${base(projectId)}/${channelId}/messages/${messageId}`,
	);
}

/** Removes someone else's message; needs messages:moderate. */
export async function moderateDeleteMessage(
	projectId: string,
	channelId: string,
	messageId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`${base(projectId)}/${channelId}/moderation/messages/${messageId}`,
	);
}

export async function setReaction(
	projectId: string,
	channelId: string,
	messageId: string,
	emoji: string,
	on: boolean,
): Promise<void> {
	const url = `${base(projectId)}/${channelId}/messages/${messageId}/reactions/${encodeURIComponent(emoji)}`;
	if (on) await apiClient.instance.put(url);
	else await apiClient.instance.delete(url);
}

export async function markChannelRead(
	projectId: string,
	channelId: string,
	seq: number,
): Promise<void> {
	await apiClient.instance.post(`${base(projectId)}/${channelId}/read`, {
		seq,
	});
}

export async function getUnreadSummary(): Promise<UnreadSummaryItem[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<UnreadSummaryItem[]>
	>("/users/me/chat/unread-summary");
	return data.data;
}

export async function getInbox(q: InboxQuery = {}): Promise<InboxItem[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<InboxItem[]>>(
		"/users/me/chat/inbox",
		{
			params: {
				unread: q.unreadOnly ? "true" : undefined,
				project_id: q.projectId,
			},
		},
	);
	return data.data;
}

/** Moves the caller's read pointers to the head of every channel they follow,
 * in one project when `projectId` is given. */
export async function markAllChatRead(projectId?: string): Promise<void> {
	await apiClient.instance.post("/users/me/chat/read-all", {
		project_id: projectId ?? null,
	});
}

export async function getMessageRefs(
	list: MessageRefList,
): Promise<MessageRef[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<MessageRef[]>>(
		`/users/me/chat/${list}`,
	);
	return data.data;
}

export const messageRefsQueryOptions = (list: MessageRefList) =>
	queryOptions({
		queryKey: chatKeys.refs(list),
		queryFn: () => getMessageRefs(list),
		staleTime: 15_000,
	});

/** Stars or unstars a channel or direct message for the caller (starring
 * also follows it). */
export async function setChannelStarred(
	scope: string,
	channelId: string,
	starred: boolean,
): Promise<void> {
	const url = `${base(scope)}/${channelId}/star`;
	if (starred) await apiClient.instance.put(url);
	else await apiClient.instance.delete(url);
}

/** Saves or unsaves a message for the caller. */
export async function setMessageSaved(
	scope: string,
	channelId: string,
	messageId: string,
	saved: boolean,
): Promise<void> {
	const url = `${base(scope)}/${channelId}/messages/${messageId}/save`;
	if (saved) await apiClient.instance.put(url);
	else await apiClient.instance.delete(url);
}

/** Follows or unfollows the thread a top-level message starts. */
export async function setThreadFollowed(
	scope: string,
	channelId: string,
	rootId: string,
	followed: boolean,
): Promise<void> {
	const url = `${base(scope)}/${channelId}/messages/${rootId}/follow`;
	if (followed) await apiClient.instance.put(url);
	else await apiClient.instance.delete(url);
}

export async function markThreadRead(
	scope: string,
	channelId: string,
	rootId: string,
	seq: number,
): Promise<void> {
	await apiClient.instance.post(
		`${base(scope)}/${channelId}/messages/${rootId}/thread-read`,
		{ seq },
	);
}

export const inboxQueryOptions = (q: InboxQuery) =>
	queryOptions({
		queryKey: chatKeys.inbox(q),
		queryFn: () => getInbox(q),
		staleTime: 15_000,
	});

export async function listDMs(): Promise<Channel[]> {
	const { data } =
		await apiClient.instance.get<SuccessEnvelope<Channel[]>>("/chat/dms");
	return data.data;
}

/** Opens, or returns, the direct message with another account. */
export async function startDM(accountId: string): Promise<Channel> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Channel>>(
		"/chat/dms",
		{ account_id: accountId },
	);
	return data.data;
}

export async function searchDirectory(
	q: string,
	kind?: AccountKind,
): Promise<DirectoryEntry[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<DirectoryEntry[]>
	>("/chat/directory", { params: { q: q || undefined, kind } });
	return data.data;
}

/** The channels of a project, or, for DM_SCOPE, the caller's direct messages. */
export const channelsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: chatKeys.channels(projectId),
		queryFn: () =>
			projectId === DM_SCOPE ? listDMs() : listChannels(projectId),
		staleTime: 30_000,
	});

export const directoryQueryOptions = (q: string, kind?: AccountKind) =>
	queryOptions({
		queryKey: ["chat", "directory", kind ?? "", q] as const,
		queryFn: () => searchDirectory(q, kind),
		staleTime: 30_000,
	});

export const unreadSummaryQueryOptions = queryOptions({
	queryKey: chatKeys.unread(),
	queryFn: getUnreadSummary,
	staleTime: 30_000,
});

/** One run (conversation) of the agent a direct message talks to. */
export interface AgentRun {
	id: string;
	title: string;
	status: "queued" | "running" | "paused" | "finished" | "failed" | "stopped";
	created_at: string;
	updated_at: string;
}

/** The agent side of a direct message. */
export interface AgentRuns {
	/** False for an agent that cannot answer in a direct message (a project's). */
	replies: boolean;
	/** The agent is answering. */
	working: boolean;
	/** Newest first. */
	runs: AgentRun[];
}

export async function getAgentRuns(channelId: string): Promise<AgentRuns> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<AgentRuns>>(
		`/chat/dms/${channelId}/agent`,
	);
	return data.data;
}

/** The runs of a direct message's agent. While it works they are re-read, so
 * the header notices when it finishes even before its reply arrives. */
export const agentRunsQueryOptions = (channelId: string, enabled: boolean) =>
	queryOptions({
		queryKey: chatKeys.agent(channelId),
		queryFn: () => getAgentRuns(channelId),
		enabled,
		staleTime: 10_000,
		refetchInterval: (q) => (q.state.data?.working ? 3_000 : false),
	});
