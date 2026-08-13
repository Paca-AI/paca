package acp

import (
	"errors"
	"fmt"
	"testing"
)

func TestClassifyProviderError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantOk   bool
		wantKind ProviderErrorKind
	}{
		{
			name:   "not an rpcError",
			err:    errors.New("boom"),
			wantOk: false,
		},
		{
			name: "unrelated rpcError",
			err: fmt.Errorf("acp: session/prompt: %w", &rpcError{
				Code: -32603, Message: "Internal error",
			}),
			wantOk: false,
		},
		{
			name: "anthropic low credit balance",
			err: fmt.Errorf("executor: acp session/prompt: %w", fmt.Errorf("acp: session/prompt: %w", &rpcError{
				Code:    -32603,
				Message: "Provider error",
				Data:    "Status: 400 Bad Request. Response: {\"error\":{\"type\":\"invalid_request_error\",\"message\":\"Your credit balance is too low to access the Anthropic API.\"}}",
			})),
			wantOk:   true,
			wantKind: ProviderErrorQuotaExceeded,
		},
		{
			// Real failure captured from conversation
			// 76b6854b-f5ab-4ddb-9c17-04318ac00644 (a Mistral agent whose
			// account had run out of credits): session/new itself failed
			// with this exact text, which the original pattern set
			// (targeting Anthropic/OpenAI wordings) didn't match at all,
			// so the conversation showed the raw "Internal error: Credits
			// exhausted: ..." blob instead of a friendly message.
			name: "mistral credits exhausted on session/new",
			err: fmt.Errorf("executor: acp session/new: %w", fmt.Errorf("acp: session/new: %w", &rpcError{
				Code:    -32603,
				Message: "Internal error",
				Data:    `Credits exhausted: {"detail":"Check your subscription on https://admin.mistral.ai/subscription"}`,
			})),
			wantOk:   true,
			wantKind: ProviderErrorQuotaExceeded,
		},
		{
			name: "openai insufficient quota",
			err: fmt.Errorf("acp: session/prompt: %w", &rpcError{
				Code:    -32603,
				Message: "Provider error",
				Data:    "Status: 429 Too Many Requests. Response: {\"error\":{\"code\":\"insufficient_quota\",\"message\":\"You exceeded your current quota, please check your plan and billing details.\"}}",
			}),
			wantOk:   true,
			wantKind: ProviderErrorQuotaExceeded,
		},
		{
			name: "anthropic rate limit",
			err: fmt.Errorf("acp: session/prompt: %w", &rpcError{
				Code:    -32603,
				Message: "Provider error",
				Data:    "Status: 429 Too Many Requests. Response: {\"error\":{\"type\":\"rate_limit_error\",\"message\":\"Number of request tokens has exceeded your rate limit.\"}}",
			}),
			wantOk:   true,
			wantKind: ProviderErrorRateLimited,
		},
		{
			name: "anthropic overloaded",
			err: fmt.Errorf("acp: session/prompt: %w", &rpcError{
				Code:    -32603,
				Message: "Provider error",
				Data:    "Status: 529 Overloaded. Response: {\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}",
			}),
			wantOk:   true,
			wantKind: ProviderErrorRateLimited,
		},
		{
			name: "auth error stays unclassified",
			err: fmt.Errorf("acp: session/new: %w", &rpcError{
				Code:    -32603,
				Message: "Authentication error",
				Data:    "Authentication failed. Status: 401 Unauthorized. Response: {\"detail\":\"Invalid API Key\"}",
			}),
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, ok := ClassifyProviderError(tt.err)
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v (kind=%q)", ok, tt.wantOk, kind)
			}
			if ok && kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", kind, tt.wantKind)
			}
		})
	}
}

func TestProviderErrorKindFriendlyMessage(t *testing.T) {
	if ProviderErrorRateLimited.FriendlyMessage() == "" {
		t.Error("ProviderErrorRateLimited.FriendlyMessage() is empty")
	}
	if ProviderErrorQuotaExceeded.FriendlyMessage() == "" {
		t.Error("ProviderErrorQuotaExceeded.FriendlyMessage() is empty")
	}
}
