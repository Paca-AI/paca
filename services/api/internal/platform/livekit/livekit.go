// Package livekit is the small part of LiveKit Paca needs: minting access
// tokens, verifying webhooks and the room-admin calls behind moderation.
//
// It speaks LiveKit's documented wire formats directly (HS256 access tokens,
// signed webhook bodies, the Twirp JSON RoomService) instead of importing the
// server SDK, which pulls in a large dependency tree for three small features.
// See docs/architecture/chat-and-meetings.md.
package livekit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Config is how Paca reaches LiveKit. A zero value (no URL) means calls are
// switched off.
type Config struct {
	// PublicURL is the signalling URL browsers connect to, for example
	// wss://paca.example.com/livekit.
	PublicURL string
	// InternalURL is where the API reaches the LiveKit server for RoomService
	// calls, for example http://livekit:7880. Defaults to PublicURL with the
	// websocket scheme turned into http(s).
	InternalURL string
	APIKey      string
	APISecret   string
}

// Enabled reports whether calls are configured.
func (c Config) Enabled() bool {
	return c.PublicURL != "" && c.APIKey != "" && c.APISecret != ""
}

// Client mints tokens, verifies webhooks and administers rooms.
type Client struct {
	cfg  Config
	http *http.Client
	now  func() time.Time
}

// New returns a Client.
func New(cfg Config) *Client {
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	cfg.InternalURL = strings.TrimRight(cfg.InternalURL, "/")
	if cfg.InternalURL == "" {
		u := cfg.PublicURL
		switch {
		case strings.HasPrefix(u, "wss://"):
			u = "https://" + strings.TrimPrefix(u, "wss://")
		case strings.HasPrefix(u, "ws://"):
			u = "http://" + strings.TrimPrefix(u, "ws://")
		}
		cfg.InternalURL = u
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

// Enabled reports whether calls are configured.
func (c *Client) Enabled() bool { return c != nil && c.cfg.Enabled() }

// PublicURL is the signalling URL handed to browsers.
func (c *Client) PublicURL() string { return c.cfg.PublicURL }

// Grant is what a token lets its holder do.
type Grant struct {
	Room         string
	CanPublish   bool
	CanSubscribe bool
}

// Identity is who a token is for.
type Identity struct {
	ID       string
	Name     string
	Metadata string
}

type videoGrant struct {
	Room           string `json:"room,omitempty"`
	RoomJoin       bool   `json:"roomJoin,omitempty"`
	RoomAdmin      bool   `json:"roomAdmin,omitempty"`
	CanPublish     *bool  `json:"canPublish,omitempty"`
	CanSubscribe   *bool  `json:"canSubscribe,omitempty"`
	CanPublishData *bool  `json:"canPublishData,omitempty"`
}

type accessClaims struct {
	jwt.RegisteredClaims
	Name     string      `json:"name,omitempty"`
	Metadata string      `json:"metadata,omitempty"`
	SHA256   string      `json:"sha256,omitempty"`
	Video    *videoGrant `json:"video,omitempty"`
}

// MintToken returns a signed access token that lets id join g.Room for ttl.
func (c *Client) MintToken(id Identity, g Grant, ttl time.Duration) (string, time.Time, error) {
	if !c.Enabled() {
		return "", time.Time{}, ErrDisabled
	}
	if id.ID == "" || g.Room == "" {
		return "", time.Time{}, errors.New("livekit: identity and room are required")
	}
	now := c.now()
	exp := now.Add(ttl)
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    c.cfg.APIKey,
			Subject:   id.ID,
			NotBefore: jwt.NewNumericDate(now.Add(-time.Second)),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		Name:     id.Name,
		Metadata: id.Metadata,
		Video: &videoGrant{
			Room: g.Room, RoomJoin: true,
			CanPublish: new(g.CanPublish), CanSubscribe: new(g.CanSubscribe), CanPublishData: new(g.CanPublish),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(c.cfg.APISecret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("livekit: sign token: %w", err)
	}
	return s, exp, nil
}

// adminToken is a short-lived token for one RoomService call.
func (c *Client) adminToken(room string) (string, error) {
	now := c.now()
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    c.cfg.APIKey,
			NotBefore: jwt.NewNumericDate(now.Add(-time.Second)),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
		Video: &videoGrant{Room: room, RoomAdmin: true},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(c.cfg.APISecret))
}

// Errors.
var (
	// ErrDisabled means LiveKit is not configured.
	ErrDisabled = errors.New("livekit: calls are not configured")
	// ErrBadWebhook means a webhook could not be verified.
	ErrBadWebhook = errors.New("livekit: webhook signature is not valid")
)

// Webhook events Paca reacts to.
const (
	EventRoomStarted       = "room_started"
	EventRoomFinished      = "room_finished"
	EventParticipantJoined = "participant_joined"
	EventParticipantLeft   = "participant_left"
)

// WebhookEvent is the part of a LiveKit webhook body Paca reads.
type WebhookEvent struct {
	Event string `json:"event"`
	ID    string `json:"id"`
	Room  struct {
		Name string `json:"name"`
		SID  string `json:"sid"`
	} `json:"room"`
	Participant struct {
		Identity string `json:"identity"`
		SID      string `json:"sid"`
	} `json:"participant"`
}

// VerifyWebhook checks the Authorization header of a webhook (a token signed
// with the API secret that carries the SHA-256 of the body) and decodes it.
func (c *Client) VerifyWebhook(authHeader string, body []byte) (*WebhookEvent, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	tok := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if tok == "" {
		return nil, ErrBadWebhook
	}
	claims := &accessClaims{}
	parsed, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (any, error) {
		return []byte(c.cfg.APISecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(c.cfg.APIKey), jwt.WithTimeFunc(c.now))
	if err != nil || !parsed.Valid {
		return nil, ErrBadWebhook
	}
	sum := sha256.Sum256(body)
	want := base64.StdEncoding.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(claims.SHA256), []byte(want)) != 1 {
		return nil, ErrBadWebhook
	}
	var ev WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("livekit: decode webhook: %w", err)
	}
	return &ev, nil
}

// track and participant are what ListParticipants returns, read loosely: the
// JSON form of a protobuf enum is its name or its number.
type track struct {
	SID  string          `json:"sid"`
	Type json.RawMessage `json:"type"`
}

type participantInfo struct {
	Identity string  `json:"identity"`
	Tracks   []track `json:"tracks"`
}

func (t track) isAudio() bool {
	s := strings.Trim(string(t.Type), `"`)
	// AUDIO is the zero value, which protobuf JSON omits by default.
	return s == "" || s == "null" || s == "AUDIO" || s == "0"
}

func (c *Client) rpc(ctx context.Context, room, method string, in, out any) error {
	if !c.Enabled() {
		return ErrDisabled
	}
	tok, err := c.adminToken(room)
	if err != nil {
		return err
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.InternalURL+"/twirp/livekit.RoomService/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("livekit: %s: %w", method, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("livekit: %s: status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("livekit: %s: decode: %w", method, err)
		}
	}
	return nil
}

// ErrNotFound means the room or participant is not in LiveKit (they already
// left).
var ErrNotFound = errors.New("livekit: not found")

// RemoveParticipant disconnects identity from room.
func (c *Client) RemoveParticipant(ctx context.Context, room, identity string) error {
	return c.rpc(ctx, room, "RemoveParticipant", map[string]string{"room": room, "identity": identity}, nil)
}

// MuteParticipant mutes every audio track identity publishes in room.
func (c *Client) MuteParticipant(ctx context.Context, room, identity string) error {
	// GetParticipant answers with the ParticipantInfo itself.
	var out participantInfo
	if err := c.rpc(ctx, room, "GetParticipant", map[string]string{"room": room, "identity": identity}, &out); err != nil {
		return err
	}
	for _, t := range out.Tracks {
		if !t.isAudio() || t.SID == "" {
			continue
		}
		if err := c.rpc(ctx, room, "MutePublishedTrack",
			map[string]any{"room": room, "identity": identity, "track_sid": t.SID, "muted": true}, nil); err != nil {
			return err
		}
	}
	return nil
}
