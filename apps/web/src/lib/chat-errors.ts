// Chat error codes (services/api/internal/apierr/codes.go) and the key of the
// translated message in the "chat" namespace under "errors".
export type ChatErrorKey =
	| "generic"
	| "forbidden"
	| "channelNotFound"
	| "nameInvalid"
	| "nameTaken"
	| "kindInvalid"
	| "archived"
	| "protected"
	| "topicTooLong"
	| "messageNotFound"
	| "messageEmpty"
	| "messageInvalid"
	| "messageTooLong"
	| "threadInvalid"
	| "emojiInvalid"
	| "notMember"
	| "notProjectMember"
	| "seqInvalid"
	| "dmSelf"
	| "dmPeerNotFound"
	| "dmNotSupported";

const KEYS: Record<string, ChatErrorKey> = {
	CHANNEL_NOT_FOUND: "channelNotFound",
	CHANNEL_NAME_INVALID: "nameInvalid",
	CHANNEL_NAME_TAKEN: "nameTaken",
	CHANNEL_KIND_INVALID: "kindInvalid",
	CHANNEL_ARCHIVED: "archived",
	CHANNEL_PROTECTED: "protected",
	CHANNEL_TOPIC_TOO_LONG: "topicTooLong",
	MESSAGE_NOT_FOUND: "messageNotFound",
	MESSAGE_EMPTY: "messageEmpty",
	MESSAGE_CONTENT_INVALID: "messageInvalid",
	MESSAGE_TOO_LONG: "messageTooLong",
	THREAD_ROOT_INVALID: "threadInvalid",
	EMOJI_INVALID: "emojiInvalid",
	CHANNEL_NOT_MEMBER: "notMember",
	CHAT_NOT_PROJECT_MEMBER: "notProjectMember",
	CHAT_SEQ_INVALID: "seqInvalid",
	DM_SELF: "dmSelf",
	DM_PEER_NOT_FOUND: "dmPeerNotFound",
	DM_NOT_SUPPORTED: "dmNotSupported",
	FORBIDDEN: "forbidden",
};

/** The "chat" namespace key (under "errors.") for an API error, or "generic". */
export function chatErrorKey(error: unknown): ChatErrorKey {
	// The raw code: getApiErrorCode only knows the codes registered in
	// api-error.ts, and the chat ones are mapped here.
	const code = (error as { response?: { data?: { error_code?: string } } })
		?.response?.data?.error_code;
	return (code && KEYS[code]) || "generic";
}
