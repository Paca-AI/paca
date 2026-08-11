// Package acp is a minimal Agent Client Protocol client for talking to
// `goose serve`'s HTTP+SSE /acp endpoint from a Go process that spawned the
// container it's running in.
//
// This is deliberately hand-rolled instead of built on
// github.com/coder/acp-go-sdk: that SDK's Connection type assumes a single
// continuous duplex stream (the shape stdio-based ACP servers like `goose
// acp` use), but `goose serve` speaks one-POST-per-call with an SSE-framed
// response per call — the same "Streamable HTTP" shape MCP uses, not a
// persistent stream. Forcing the SDK's stdio-shaped Connection onto that
// transport would need an io.Reader/io.Writer adapter faking a duplex
// stream on top of discrete request/response exchanges, which is more
// indirection than just decoding the (small, now-verified) message shapes
// directly. Revisit if the SDK grows a native HTTP transport.
//
// All wire shapes here were captured empirically against a live `goose
// serve` (image ghcr.io/block/goose@sha256:d85a724...30, goose 1.30.0) — see
// docs/ai-agent/goose-migration.md's spike section for the raw transcripts.
// Fields not exercised in that spike are marked below; verify before
// depending on them.
package acp

import "encoding/json"

// ─── JSON-RPC 2.0 envelope ─────────────────────────────────────────────────

// rpcRequest is what the client sends. Every call in this package is a
// request (never a bare notification) — goose serve responds to each with
// an SSE stream containing zero or more notifications followed by exactly
// one terminal response frame carrying the matching id.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcFrame is one `data:` line from the SSE response body, decoded loosely
// enough to tell a notification (Method set, ID nil) apart from the
// terminal response (ID set, Method empty) without committing to either
// shape up front.
type rpcFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (f rpcFrame) isNotification() bool { return f.ID == nil && f.Method != "" }
func (f rpcFrame) isResponse() bool     { return f.ID != nil }

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

// ─── initialize ─────────────────────────────────────────────────────────────

type initializeParams struct {
	ProtocolVersion    int            `json:"protocolVersion"`
	ClientCapabilities map[string]any `json:"clientCapabilities"`
}

// AuthMethod is one entry of initialize's authMethods list. Observed with a
// single "goose-provider" entry when no LLM provider is configured yet —
// see the migration doc's session/new gotcha.
type AuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ─── session/new ────────────────────────────────────────────────────────────

// NewSessionParams are the params for "session/new". See
// MCPServerConfig's doc comment for the (now verified against the real ACP
// schema, not guessed) shape of a non-empty MCPServers entry.
type NewSessionParams struct {
	Cwd        string            `json:"cwd"`
	MCPServers []MCPServerConfig `json:"mcpServers"`
}

// McpServerType is the "type" discriminator ACP's schema.json requires on
// every McpServer entry (a Rust internally-tagged enum: "stdio" | "http" |
// "sse") — confirmed against the real schema
// (https://github.com/agentclientprotocol/agent-client-protocol,
// $defs.McpServer) after a hand-guessed shape without this field made
// goose serve's session/new hang forever rather than return an error. See
// docs/ai-agent/goose-migration.md.
type McpServerType string

const (
	// McpServerStdio is an MCP server launched as a local subprocess.
	McpServerStdio McpServerType = "stdio"
	// McpServerHTTP is an MCP server reached over streamable HTTP.
	McpServerHTTP McpServerType = "http"
	// McpServerSSE is an MCP server reached over Server-Sent Events.
	McpServerSSE McpServerType = "sse"
)

// MCPServerConfig is one entry of session/new's mcpServers array — verified
// against the real ACP schema (see McpServerType's doc comment), not
// guessed. Worth remembering when constructing one:
//
//   - Env is an array of {name, value} pairs, NOT a JSON object/map — the
//     schema's McpServerStdio.env is `EnvVariable[]`.
//   - Command must be an ABSOLUTE path per the schema's own description
//     ("Absolute path to the MCP server executable") — a bare command name
//     like "npx" relying on PATH lookup inside the container is not
//     guaranteed to resolve. executor.go's buildMCPServers hardcodes the
//     absolute path for the one command it controls (the built-in Paca MCP
//     server); a user-configured agent_mcp_servers.command value is NOT
//     currently normalized to an absolute path — see that function's doc
//     comment.
//   - Args and Env are REQUIRED by the schema for the stdio variant, even
//     when empty — confirmed live, the hard way: a stdio entry missing
//     either field (Go's `omitempty` silently drops an empty-but-non-nil
//     slice, indistinguishable on the wire from never having set it at
//     all) makes a real goose serve either hang session/new indefinitely
//     or return a 200 with a permanently-empty SSE stream, depending on
//     which field was missing — neither surfaces as a JSON-RPC error.
//     Both are pointers (not plain slices), the same trick Headers below
//     already used for the identical http/sse problem, so "present but
//     empty" (`&[]string{}`) can be told apart from "absent" (`nil`) at
//     all. See docs/ai-agent/goose-migration.md.
type MCPServerConfig struct {
	Type    McpServerType  `json:"type"`
	Name    string         `json:"name"`
	Command string         `json:"command,omitempty"`
	Args    *[]string      `json:"args,omitempty"`
	Env     *[]EnvVariable `json:"env,omitempty"`
	URL     string         `json:"url,omitempty"`
	// Headers is required by the schema for the http/sse variants — an
	// empty array, not an omitted field, when there are none. A pointer
	// (not a plain slice) so the zero value stays omitted for stdio
	// entries (nil pointer) while &[]HTTPHeader{} still serializes as `[]`
	// for http/sse: Go's `omitempty` on a plain slice can't tell "unset"
	// apart from "present but empty" — both marshal to nothing — so a
	// plain `[]HTTPHeader` field could never satisfy "always present for
	// http/sse, always absent for stdio" the way this one does.
	Headers *[]HTTPHeader `json:"headers,omitempty"`
}

// EnvVariable is one entry of MCPServerConfig.Env.
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HTTPHeader is one entry of MCPServerConfig.Headers.
type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type newSessionResult struct {
	SessionID string          `json:"sessionId"`
	Modes     json.RawMessage `json:"modes,omitempty"`
	Models    json.RawMessage `json:"models,omitempty"`
}

// ─── session/prompt ─────────────────────────────────────────────────────────

type promptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// ContentBlock is a single prompt/response content item. Only Type=="text"
// was exercised in the spike; initialize's promptCapabilities advertised
// image support but that path is untested here.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// TextBlock is a convenience constructor for the one verified ContentBlock shape.
func TextBlock(text string) ContentBlock { return ContentBlock{Type: "text", Text: text} }

type promptResult struct {
	// StopReason observed value: "end_turn". Others (e.g. a cancellation or
	// max-turn-requests reason) are plausible per the ACP spec but not seen
	// here — surfaced as-is rather than validated against an enum.
	StopReason string `json:"stopReason"`
}

// ─── session/update notifications ──────────────────────────────────────────

// SessionUpdateKind is the "sessionUpdate" discriminator. Content shape
// (notably the "content" field) differs by kind — see Event's doc comment —
// so decode the discriminator first and only then decode the concrete
// payload.
type SessionUpdateKind string

const (
	// UpdateAgentMessageChunk is a streamed piece of the agent's reply text.
	UpdateAgentMessageChunk SessionUpdateKind = "agent_message_chunk"
	// UpdateToolCall announces a new tool call the agent has started.
	UpdateToolCall SessionUpdateKind = "tool_call"
	// UpdateToolCallUpdate reports a status change for an existing tool call.
	UpdateToolCallUpdate SessionUpdateKind = "tool_call_update"
)

type sessionUpdateNotification struct {
	SessionID string                `json:"sessionId"`
	Update    sessionUpdateEnvelope `json:"update"`
}

// sessionUpdateEnvelope captures the discriminator and keeps the raw bytes
// around so the caller can re-decode into the concrete shape for that
// discriminator. A single flat struct can't represent all variants: e.g.
// "content" is a single object under agent_message_chunk but an array of a
// differently-shaped wrapper under tool_call_update.
type sessionUpdateEnvelope struct {
	Kind SessionUpdateKind
	raw  json.RawMessage
}

func (e *sessionUpdateEnvelope) UnmarshalJSON(data []byte) error {
	var disc struct {
		SessionUpdate SessionUpdateKind `json:"sessionUpdate"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		return err
	}
	e.Kind = disc.SessionUpdate
	e.raw = append(json.RawMessage(nil), data...)
	return nil
}

// AgentMessageChunk is the payload of an UpdateAgentMessageChunk event.
type AgentMessageChunk struct {
	Content ContentBlock `json:"content"`
}

// ToolCall is the payload of an UpdateToolCall event (announces a tool call
// has started; the result follows in a later ToolCallUpdate with the same
// ToolCallID).
type ToolCall struct {
	ToolCallID string `json:"toolCallId"`
	Title      string `json:"title"`
}

// ToolCallUpdate is the payload of an UpdateToolCallUpdate event. Status
// values observed: "completed", "failed".
type ToolCallUpdate struct {
	ToolCallID string            `json:"toolCallId"`
	Status     string            `json:"status"`
	Content    []toolCallContent `json:"content"`
}

type toolCallContent struct {
	Type    string       `json:"type"`
	Content ContentBlock `json:"content"`
}

// Text concatenates the text of every content block in this update — a
// convenience since ToolCallUpdate.Content's wrapper nesting isn't
// otherwise pleasant to consume.
func (u ToolCallUpdate) Text() string {
	out := ""
	for _, c := range u.Content {
		out += c.Content.Text
	}
	return out
}
