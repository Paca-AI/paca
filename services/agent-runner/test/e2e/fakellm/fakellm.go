// Package fakellm is a minimal OpenAI-compatible chat-completions server
// for agent-runner's E2E tests — no model weights, no network egress, just
// enough of the API surface for a real `goose serve` sandbox container to
// get a usable response with no real LLM spend.
//
// Reimplements, in Go, what used to live at
// services/ai-agent/tests/e2e/fake_llm_server.py (deleted along with that
// service). Goose has no SDK-internal "generate a title" call the way the
// OpenHands SDK that Python version also had to special-case did, so that
// part of the original isn't carried forward.
package fakellm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// DefaultReplyText is used when a Server is built with no script at all.
const DefaultReplyText = "Hello! This is a canned reply from the fake LLM server."

// ToolCall is one scripted tool-call reply's shape.
type ToolCall struct {
	Name      string
	Arguments map[string]any
	// CallID defaults to "call_fake_1" when empty.
	CallID string
}

// ScriptedReply is one scripted turn: exactly one of a text reply, a tool
// call, or an error status.
type ScriptedReply struct {
	content     *string
	toolCall    *ToolCall
	errorStatus int
}

// TextReply scripts a plain assistant text reply.
func TextReply(content string) ScriptedReply {
	return ScriptedReply{content: &content}
}

// ToolCallReply scripts an assistant reply that calls tool name with args.
// A script containing only this entry has the fake LLM request the same
// tool call forever (see Server's doc comment on script exhaustion) — the
// shape stop_control_test.go needs to exercise HandleControl's interrupt
// against a turn that would otherwise never converge on its own.
func ToolCallReply(name string, args map[string]any) ScriptedReply {
	return ScriptedReply{toolCall: &ToolCall{Name: name, Arguments: args, CallID: "call_fake_1"}}
}

// ErrorReply scripts an upstream failure: the fake server responds with
// status and a generic error body instead of a completion.
func ErrorReply(status int) ScriptedReply {
	return ScriptedReply{errorStatus: status}
}

// Server is a scripted OpenAI-compatible /v1/chat/completions server.
type Server struct {
	httpSrv *http.Server
	ln      net.Listener
	script  []ScriptedReply
	callIdx atomic.Int64

	mu       sync.Mutex
	requests [][]byte
}

// New starts a Server bound to 0.0.0.0 on an OS-assigned port, driven by
// script in order — the last entry repeats indefinitely once the script is
// exhausted (a single-entry script therefore returns that one entry on
// every call, unbounded). Defaults to one canned text reply if script is
// empty. Registers t.Cleanup to shut the server down.
//
// Binds 0.0.0.0, not "localhost": a sandbox container run by
// sandbox.Manager on a CI runner (not itself inside Docker) reaches this
// process via the Docker default bridge's gateway IP, not loopback — see
// BaseURL and helpers_test.go's dockerBridgeGatewayIP.
func New(t *testing.T, script ...ScriptedReply) *Server {
	t.Helper()
	if len(script) == 0 {
		script = []ScriptedReply{TextReply(DefaultReplyText)}
	}

	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("fakellm: listen: %v", err)
	}

	s := &Server{ln: ln, script: script}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", s.handleModels)
	mux.HandleFunc("GET /health", s.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	s.httpSrv = &http.Server{Handler: mux}

	go func() { _ = s.httpSrv.Serve(ln) }()
	t.Cleanup(func() { _ = s.httpSrv.Close() })

	return s
}

// Port is the OS-assigned port New bound to.
func (s *Server) Port() int {
	return s.ln.Addr().(*net.TCPAddr).Port
}

// BaseURL builds the URL a sandbox container should use to reach this
// server, given the Docker bridge gateway IP visible to that container
// (never "localhost" — that resolves inside the container's own network
// namespace, not this test process's).
func (s *Server) BaseURL(bridgeGatewayIP string) string {
	return fmt.Sprintf("http://%s:%d", bridgeGatewayIP, s.Port())
}

// CallCount reports how many /v1/chat/completions requests have been
// served so far.
func (s *Server) CallCount() int {
	return int(s.callIdx.Load())
}

// Requests returns the raw JSON body of every /v1/chat/completions request
// received so far, in order — lets a test inspect exactly what a real
// `goose serve` sandbox sent the model (e.g. which role a given piece of
// text arrived under), not just that a call happened.
func (s *Server) Requests() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *Server) nextReply() ScriptedReply {
	idx := s.callIdx.Add(1) - 1
	if int(idx) >= len(s.script) {
		idx = int64(len(s.script) - 1)
	}
	return s.script[idx]
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   []map[string]any{{"id": "fake-model", "object": "model"}},
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, body)
	s.mu.Unlock()

	var payload struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &payload)

	reply := s.nextReply()
	switch {
	case reply.errorStatus != 0:
		writeJSON(w, reply.errorStatus, map[string]any{"error": map[string]any{"message": "fake upstream failure"}})
	case payload.Stream:
		s.writeStreamingReply(w, reply)
	default:
		s.writeNonStreamingReply(w, reply)
	}
}

func (s *Server) writeNonStreamingReply(w http.ResponseWriter, reply ScriptedReply) {
	var message map[string]any
	finishReason := "stop"
	if reply.toolCall != nil {
		message = map[string]any{
			"role":       "assistant",
			"content":    nil,
			"tool_calls": []map[string]any{toolCallJSON(reply.toolCall)},
		}
		finishReason = "tool_calls"
	} else {
		content := DefaultReplyText
		if reply.content != nil {
			content = *reply.content
		}
		message = map[string]any{"role": "assistant", "content": content}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "fake-completion",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "fake-model",
		"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finishReason}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
}

func (s *Server) writeStreamingReply(w http.ResponseWriter, reply ScriptedReply) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	base := map[string]any{
		"id":      "fake-chunk",
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   "fake-model",
	}

	var chunks []map[string]any
	if reply.toolCall != nil {
		chunks = toolCallChunks(base, reply.toolCall)
	} else {
		content := DefaultReplyText
		if reply.content != nil {
			content = *reply.content
		}
		chunks = textChunks(base, content)
	}

	for _, chunk := range chunks {
		data, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func textChunks(base map[string]any, content string) []map[string]any {
	return []map[string]any{
		withChoices(base, map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": ""}, "finish_reason": nil}),
		withChoices(base, map[string]any{"index": 0, "delta": map[string]any{"content": content}, "finish_reason": nil}),
		withChoices(base, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}),
	}
}

func toolCallChunks(base map[string]any, tc *ToolCall) []map[string]any {
	argsJSON, _ := json.Marshal(tc.Arguments)
	callID := tc.CallID
	if callID == "" {
		callID = "call_fake_1"
	}
	return []map[string]any{
		withChoices(base, map[string]any{
			"index": 0,
			"delta": map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]any{{
					"index":    0,
					"id":       callID,
					"type":     "function",
					"function": map[string]any{"name": tc.Name, "arguments": ""},
				}},
			},
			"finish_reason": nil,
		}),
		withChoices(base, map[string]any{
			"index": 0,
			"delta": map[string]any{
				"tool_calls": []map[string]any{{
					"index":    0,
					"function": map[string]any{"arguments": string(argsJSON)},
				}},
			},
			"finish_reason": nil,
		}),
		withChoices(base, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}),
	}
}

func withChoices(base map[string]any, choice map[string]any) map[string]any {
	out := make(map[string]any, len(base)+1)
	for k, v := range base {
		out[k] = v
	}
	out["choices"] = []map[string]any{choice}
	return out
}

func toolCallJSON(tc *ToolCall) map[string]any {
	argsJSON, _ := json.Marshal(tc.Arguments)
	callID := tc.CallID
	if callID == "" {
		callID = "call_fake_1"
	}
	return map[string]any{
		"id":       callID,
		"type":     "function",
		"function": map[string]any{"name": tc.Name, "arguments": string(argsJSON)},
	}
}

func writeJSON(w http.ResponseWriter, status int, body map[string]any) {
	data, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
