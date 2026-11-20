package chatdom

import "errors"

// Chat errors.
var (
	ErrChannelNotFound    = errors.New("channel not found")
	ErrChannelNameInvalid = errors.New("channel name is empty or too long")
	ErrChannelNameTaken   = errors.New("a channel with this name already exists")
	ErrChannelKindInvalid = errors.New("channel kind is not valid")
	ErrChannelArchived    = errors.New("channel is archived")
	ErrChannelProtected   = errors.New("the general channel cannot be archived or made private")
	ErrTopicTooLong       = errors.New("channel topic is too long")
	ErrMessageNotFound    = errors.New("message not found")
	ErrMessageEmpty       = errors.New("message is empty")
	ErrMessageContentBad  = errors.New("message content must be a JSON array of blocks")
	ErrMessageTooLong     = errors.New("message is too long")
	ErrThreadRootInvalid  = errors.New("thread root is not a top-level message of this channel")
	ErrEmojiInvalid       = errors.New("emoji is empty or too long")
	ErrNotAMember         = errors.New("account is not a member of this channel")
	ErrNotProjectMember   = errors.New("account is not a member of this project")
	ErrSeqInvalid         = errors.New("sequence cursor is not valid")
	ErrDMSelf             = errors.New("you cannot message yourself")
	ErrDMPeerNotFound     = errors.New("that account does not exist")
	ErrAgentBusy          = errors.New("the agent is still answering")
	ErrDMNotSupported     = errors.New("this action is not available for direct messages")
)
