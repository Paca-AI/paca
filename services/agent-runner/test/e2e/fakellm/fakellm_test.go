package fakellm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func postJSON(t *testing.T, url string, body map[string]any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestNonStreamingTextReply(t *testing.T) {
	s := New(t, TextReply("hi there"))
	resp := postJSON(t, fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", s.Port()), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Choices) != 1 || body.Choices[0].Message.Content != "hi there" {
		t.Fatalf("got %+v, want content %q", body, "hi there")
	}
	if body.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %q, want %q", body.Choices[0].FinishReason, "stop")
	}
	if s.CallCount() != 1 {
		t.Fatalf("CallCount() = %d, want 1", s.CallCount())
	}
}

func TestNonStreamingToolCallReply(t *testing.T) {
	s := New(t, ToolCallReply("shell", map[string]any{"command": "echo hi"}))
	resp := postJSON(t, fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", s.Port()), nil)
	defer resp.Body.Close()
	var body struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Choices) != 1 || len(body.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("got %+v, want one tool call", body)
	}
	tc := body.Choices[0].Message.ToolCalls[0]
	if tc.Function.Name != "shell" {
		t.Fatalf("tool name = %q, want %q", tc.Function.Name, "shell")
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if args["command"] != "echo hi" {
		t.Fatalf("arguments = %+v, want command=echo hi", args)
	}
	if body.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q, want %q", body.Choices[0].FinishReason, "tool_calls")
	}
}

func TestScriptExhaustionRepeatsLastEntry(t *testing.T) {
	s := New(t, TextReply("only entry"))
	for i := 0; i < 3; i++ {
		resp := postJSON(t, fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", s.Port()), nil)
		var body struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		resp.Body.Close()
		if body.Choices[0].Message.Content != "only entry" {
			t.Fatalf("call %d: content = %q, want %q", i, body.Choices[0].Message.Content, "only entry")
		}
	}
	if s.CallCount() != 3 {
		t.Fatalf("CallCount() = %d, want 3", s.CallCount())
	}
}

func TestStreamingReply(t *testing.T) {
	s := New(t, TextReply("streamed"))
	resp := postJSON(t, fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", s.Port()), map[string]any{"stream": true})
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}
	got := string(data)
	if !strings.Contains(got, `"content":"streamed"`) || !strings.Contains(got, "[DONE]") {
		t.Fatalf("streamed body missing expected content: %s", got)
	}
}

func TestErrorReply(t *testing.T) {
	s := New(t, ErrorReply(429))
	resp := postJSON(t, fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", s.Port()), nil)
	defer resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
}
