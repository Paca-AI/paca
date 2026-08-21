// Package turnruntime is the runner-side adapter for services/api's
// authoritative agent-turn control plane. The API remains the sole owner of
// claim, lease, event fencing, result, and outbox transaction semantics.
package turnruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// StableOutputEventType identifies the single publishable agent output event.
const StableOutputEventType = "agent.turn.output.stable"

// ToolPolicy is the validated capability envelope supplied by the API.
type ToolPolicy struct {
	Version             int      `json:"version"`
	Mode                string   `json:"mode"`
	AllowedCapabilities []string `json:"allowed_capabilities"`
	ContextMayGrant     bool     `json:"context_may_grant"`
}

// Envelope contains the immutable execution state for one claimed turn.
type Envelope struct {
	TurnID                 uuid.UUID       `json:"turn_id"`
	RunID                  uuid.UUID       `json:"run_id"`
	ClaimToken             *uuid.UUID      `json:"claim_token,omitempty"`
	ConversationID         uuid.UUID       `json:"conversation_id"`
	SessionID              *uuid.UUID      `json:"session_id,omitempty"`
	ProjectID              uuid.UUID       `json:"project_id"`
	AgentID                uuid.UUID       `json:"agent_id"`
	RequestedByMemberID    *uuid.UUID      `json:"requested_by_member_id,omitempty"`
	InputText              string          `json:"input_text"`
	Backend                string          `json:"backend"`
	Attempt                int             `json:"attempt"`
	Status                 string          `json:"status"`
	DeadlineAt             *time.Time      `json:"deadline_at,omitempty"`
	LeaseExpiresAt         *time.Time      `json:"lease_expires_at,omitempty"`
	ToolPolicy             ToolPolicy      `json:"tool_policy"`
	ToolPolicySHA256       string          `json:"tool_policy_sha256"`
	SnapshotManifest       json.RawMessage `json:"snapshot_manifest"`
	SnapshotManifestSHA256 string          `json:"snapshot_manifest_sha256"`
	SnapshotRenderedText   string          `json:"snapshot_rendered_text"`
	TerminalStatus         *string         `json:"terminal_status,omitempty"`
}

// Event is one fenced, sequenced runtime event appended to the API.
type Event struct {
	ID          uuid.UUID       `json:"id"`
	RunID       uuid.UUID       `json:"run_id"`
	ClaimToken  uuid.UUID       `json:"claim_token"`
	Sequence    int             `json:"sequence"`
	EventType   string          `json:"event_type"`
	EventSource string          `json:"event_source"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   time.Time       `json:"created_at"`
}

// FinalizeInput contains the fenced terminal result contract.
type FinalizeInput struct {
	RunID               uuid.UUID  `json:"run_id"`
	ClaimToken          uuid.UUID  `json:"claim_token"`
	TerminalStatus      string     `json:"terminal_status"`
	StableOutputEventID *uuid.UUID `json:"stable_output_event_id,omitempty"`
	GeneratedByAgentID  uuid.UUID  `json:"generated_by_agent_id"`
	ErrorCode           *string    `json:"error_code,omitempty"`
	ErrorMessage        *string    `json:"error_message,omitempty"`
	RuntimeDisposition  string     `json:"runtime_disposition"`
	FinalSequence       *int       `json:"final_sequence,omitempty"`
}

// APIError carries the stable error code returned by turn-control endpoints.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("turn runtime: %s (%d): %s", e.Code, e.Status, e.Message)
}

// ErrorCode extracts a stable turn-control error code when present.
func ErrorCode(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// IsTerminalOrExpired reports whether retrying the same run is no longer useful.
func IsTerminalOrExpired(err error) bool {
	switch ErrorCode(err) {
	case "TURN_FINALIZED", "TURN_DEADLINE_EXCEEDED", "TURN_AUTHORIZATION_REVOKED", "TURN_NOT_FOUND":
		return true
	default:
		return false
	}
}

// Client calls the API's internal authoritative turn-control endpoints.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient constructs an authenticated turn-control client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), token: token,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// Claim leases a turn and returns its immutable execution envelope.
func (c *Client) Claim(ctx context.Context, turnID uuid.UUID, workerID string, lease time.Duration) (*Envelope, error) {
	var out Envelope
	err := c.request(ctx, http.MethodPost, c.turnPath(turnID, "/claim"), map[string]any{
		"worker_id": workerID, "lease_ms": lease.Milliseconds(),
	}, &out)
	return &out, err
}

// Get retrieves the current immutable execution envelope for a turn.
func (c *Client) Get(ctx context.Context, turnID uuid.UUID) (*Envelope, error) {
	var out Envelope
	err := c.request(ctx, http.MethodGet, c.turnPath(turnID, "/"), nil, &out)
	return &out, err
}

// Renew extends a valid fenced execution lease.
func (c *Client) Renew(ctx context.Context, turnID, runID, claimToken uuid.UUID, lease time.Duration) (time.Time, error) {
	var out struct {
		LeaseExpiresAt time.Time `json:"lease_expires_at"`
	}
	err := c.request(ctx, http.MethodPost, c.turnPath(turnID, "/lease"), map[string]any{
		"run_id": runID, "claim_token": claimToken, "lease_ms": lease.Milliseconds(),
	}, &out)
	return out.LeaseExpiresAt, err
}

// AppendEvent stores one fenced, sequenced runtime event.
func (c *Client) AppendEvent(ctx context.Context, turnID uuid.UUID, event Event) error {
	return c.request(ctx, http.MethodPost, c.turnPath(turnID, "/events"), event, nil)
}

// Finalize records the fenced terminal result for a turn.
func (c *Client) Finalize(ctx context.Context, turnID uuid.UUID, input FinalizeInput) error {
	return c.request(ctx, http.MethodPost, c.turnPath(turnID, "/finalize"), input, nil)
}

func (c *Client) turnPath(turnID uuid.UUID, suffix string) string {
	return fmt.Sprintf("%s/api/internal/v1/agent-turns/%s%s", c.baseURL, turnID, suffix)
}

func (c *Client) request(ctx context.Context, method, url string, input, output any) error {
	var encoded []byte
	if input != nil {
		var err error
		encoded, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		var body io.Reader
		if encoded != nil {
			body = bytes.NewReader(encoded)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return err
		}
		req.Header.Set("X-Internal-Token", c.token)
		if input != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if attempt < 2 && ctx.Err() == nil {
				if err := waitRuntimeRetry(ctx, attempt); err != nil {
					return err
				}
				continue
			}
			return err
		}
		limited := io.LimitReader(resp.Body, 4<<20)
		var envelope struct {
			Success   bool            `json:"success"`
			Data      json.RawMessage `json:"data"`
			ErrorCode string          `json:"error_code"`
			Error     string          `json:"error"`
		}
		decodeErr := json.NewDecoder(limited).Decode(&envelope)
		_ = resp.Body.Close()
		if decodeErr != nil {
			if attempt < 2 && ctx.Err() == nil {
				if err := waitRuntimeRetry(ctx, attempt); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("turn runtime: decode response: %w", decodeErr)
		}
		if resp.StatusCode >= http.StatusInternalServerError && attempt < 2 && ctx.Err() == nil {
			if err := waitRuntimeRetry(ctx, attempt); err != nil {
				return err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || !envelope.Success {
			return &APIError{Status: resp.StatusCode, Code: envelope.ErrorCode, Message: envelope.Error}
		}
		if output != nil && len(envelope.Data) > 0 {
			if err := json.Unmarshal(envelope.Data, output); err != nil {
				return fmt.Errorf("turn runtime: decode data: %w", err)
			}
		}
		return nil
	}
	return errors.New("turn runtime: retry budget exhausted")
}

func waitRuntimeRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
