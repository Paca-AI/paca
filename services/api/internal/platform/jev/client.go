// Package jev is a client for the Jev "System One" decision API
// (https://docs.typesafe.ai): given `state` (context) and a batch of typed
// questions, it returns constrained, typed answers with probability/
// confidence — not generated prose. Used to route automation decisions,
// auto-fill task fields, and pick an agent/assignee automatically, instead
// of a hand-written comparison or free-text generation. No official Go SDK
// exists (only Python/JS), hence this package.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/Paca-AI/api/internal/platform/netguard"
)

const (
	defaultBaseURL = "https://api.typesafe.ai/v1/systemone"
	// DefaultModel is the model alias every caller should use unless it has a
	// specific reason not to — see https://docs.typesafe.ai/models.
	DefaultModel = "jev-latest"
	maxRetries   = 3
	// maxErrorBodyBytes caps how much of a failed response's body is kept on
	// APIError. The status code is the only part callers act on (see
	// APIError's doc comment) — the body exists purely for the log line, so
	// echoing a whole 1 MiB error page back is waste. Applied to the error
	// path only; a successful response is always parsed in full.
	maxErrorBodyBytes = 512
)

// Question type discriminants — see https://docs.typesafe.ai/primitives.
const (
	TypeChoice = "choice"
	TypeScore  = "score"
	TypeNoul   = "noul"
)

// Question is one typed question sent in a SystemOne request. Criteria's
// shape depends on Type:
//   - choice: map[string]any (option key -> description), max 255 options
//   - score:  []any, an ORDERED list of 2-10 level descriptions
//   - noul:   optional map[string]any with "true"/"false" keys clarifying
//     what a yes/no answer means, or nil
type Question struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Request is the body of POST /v1/systemone.
type Request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// Answer is one entry of Response.Answers, keyed by the question ID the
// caller chose. Only the fields relevant to Type are populated by the API —
// callers should switch on Type (or, since they already know which question
// ID maps to which type, just read the field(s) that type defines).
type Answer struct {
	Type   string  `json:"type"`
	Choice string  `json:"choice,omitempty"` // type=choice: the selected option key
	Score  float64 `json:"score,omitempty"`  // type=score: position on the spectrum, can fall between levels
	Noul   float64 `json:"noul,omitempty"`   // type=noul: probability the statement is true, 0-1

	// Probabilities is the full distribution (choice: over option keys,
	// score: over level indices as strings). Noul has no distribution.
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	// Confidence is nil for noul answers (Jev has no separate confidence for
	// noul — the noul value itself, distance from 0.5, is the signal).
	Confidence *float64 `json:"confidence,omitempty"`
	// Legend maps score level indices ("0".."N-1") to their criteria text.
	Legend map[string]string `json:"legend,omitempty"`
}

// Response is the body of a successful POST /v1/systemone.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// APIError is returned when Jev responds with a non-2xx status. Every caller
// in this codebase treats any SystemOne error as "no answer, fall back
// gracefully" rather than inspecting APIError specifically — it's exposed
// mainly so callers can log/record the status code on their run/audit trail.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jev: api returned %d: %s", e.StatusCode, e.Body)
}

// Client calls the Jev System One API.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// New returns a Client for the given API key, or nil if apiKey is empty.
// Every method is safe to call on a nil *Client (see Enabled) so callers can
// hold a possibly-nil *Client — when the caller hasn't configured Jev —
// without a separate presence check at every call site.
//
// baseURL and model are optional: an empty baseURL uses TypeSafe's own
// endpoint (defaultBaseURL) and an empty model uses DefaultModel. Jev-
// compatible third-party providers (e.g. OpenJev, https://openjev.sh/docs)
// implement the same state/questions/choice/score/noul request-response
// contract at a different host with a different default model name, so
// supporting them is purely a matter of overriding these two — no request/
// response shape differences to account for.
func New(apiKey, baseURL, model string) *Client {
	if apiKey == "" {
		return nil
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		// SSRF-safe by default: baseURL is a per-project, user-supplied value
		// (see UpdateJevConfig — it's only TrimSpace'd, never validated), so
		// without this any project admin could point it at an internal
		// address and make the API process issue an authenticated POST there.
		// Same client the automation engine's call_api action uses for its
		// own user-configurable URL — see netguard's package doc.
		httpClient: netguard.NewSafeHTTPClient(30 * time.Second),
	}
}

// WithHTTPClient overrides the client's transport. Defaults to
// netguard.NewSafeHTTPClient — override only for tests that need to reach a
// local httptest.Server, which the default client would otherwise reject as
// a private address. Mirrors AutomationConsumer.WithHTTPClient.
func (c *Client) WithHTTPClient(client *http.Client) *Client {
	c.httpClient = client
	return c
}

// Enabled reports whether c is a usable, configured client. Safe to call on
// a nil receiver — every Jev-dependent feature in this codebase guards on
// `client.Enabled()` before doing any Jev-specific work.
func (c *Client) Enabled() bool {
	return c != nil
}

// SystemOne sends state and questions to Jev and returns the typed answers.
// All questions in a single call are evaluated in parallel by Jev and cost
// is dominated by state+instructions size, not question count — callers
// should batch every question they need for one decision into one call
// rather than issuing several.
//
// Transient errors (429 rate-limited, 529 overloaded) are retried with
// exponential backoff; everything else (401 bad key, 422 invalid
// questions, network errors) is returned immediately since a retry won't
// help.
func (c *Client) SystemOne(ctx context.Context, state any, questions map[string]Question) (*Response, error) {
	if c == nil {
		return nil, fmt.Errorf("jev: client not configured")
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("jev: at least one question is required")
	}

	body, err := json.Marshal(Request{State: state, Model: c.model, Questions: questions})
	if err != nil {
		return nil, fmt.Errorf("jev: marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt); err != nil {
				return nil, err
			}
		}

		resp, err := c.doRequest(ctx, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			return nil, err // network/marshal error — not retryable
		}
		// Only rate-limit and overload are documented as transient; anything
		// else (401/422/...) won't succeed on retry.
		if apiErr.StatusCode != http.StatusTooManyRequests && apiErr.StatusCode != 529 {
			return nil, err
		}
	}
	return nil, fmt.Errorf("jev: exhausted retries: %w", lastErr)
}

func sleepBackoff(ctx context.Context, attempt int) error {
	delay := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
	jitter := time.Duration(rand.Int63n(int64(delay)/2 + 1))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay + jitter):
		return nil
	}
}

func (c *Client) doRequest(ctx context.Context, body []byte) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("jev: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jev: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("jev: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: httpResp.StatusCode, Body: truncateBody(respBody)}
	}

	var out Response
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("jev: unmarshal response: %w", err)
	}
	return &out, nil
}

// truncateBody clips a failed response's body to maxErrorBodyBytes so a
// verbose error page can't bloat the log line APIError's Error() builds.
func truncateBody(body []byte) string {
	if len(body) <= maxErrorBodyBytes {
		return string(body)
	}
	return string(body[:maxErrorBodyBytes]) + "... (truncated)"
}
