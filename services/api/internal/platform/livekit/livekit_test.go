package livekit

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testClient(t *testing.T, internal string) *Client {
	t.Helper()
	return New(Config{PublicURL: "wss://paca.test/livekit", InternalURL: internal, APIKey: "key", APISecret: "secret-secret-secret-secret-1234"})
}

func TestMintTokenClaims(t *testing.T) {
	c := testClient(t, "")
	tok, exp, err := c.MintToken(Identity{ID: "acc-1", Name: "Ada", Metadata: `{"kind":"user"}`},
		Grant{Room: "room-1", CanPublish: true, CanSubscribe: true}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(exp) < 9*time.Minute {
		t.Errorf("expiry too soon: %v", exp)
	}
	claims := &accessClaims{}
	if _, err := jwt.ParseWithClaims(tok, claims, func(*jwt.Token) (any, error) { return []byte(c.cfg.APISecret), nil }); err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != "key" || claims.Subject != "acc-1" || claims.Name != "Ada" {
		t.Errorf("claims = %+v", claims)
	}
	v := claims.Video
	if v == nil || v.Room != "room-1" || !v.RoomJoin || v.RoomAdmin || v.CanPublish == nil || !*v.CanPublish {
		t.Errorf("video grant = %+v", v)
	}
}

func TestMintTokenDisabled(t *testing.T) {
	if _, _, err := New(Config{}).MintToken(Identity{ID: "a"}, Grant{Room: "r"}, time.Minute); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func sign(t *testing.T, c *Client, body []byte, secret string) string {
	t.Helper()
	sum := sha256.Sum256(body)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: c.cfg.APIKey, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))},
		SHA256:           base64.StdEncoding.EncodeToString(sum[:]),
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestVerifyWebhook(t *testing.T) {
	c := testClient(t, "")
	body := []byte(`{"event":"participant_joined","room":{"name":"r1","sid":"RM_1"},"participant":{"identity":"acc-1"}}`)

	ev, err := c.VerifyWebhook(sign(t, c, body, c.cfg.APISecret), body)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Event != EventParticipantJoined || ev.Room.Name != "r1" || ev.Participant.Identity != "acc-1" {
		t.Errorf("event = %+v", ev)
	}

	if _, err := c.VerifyWebhook(sign(t, c, body, "another-secret-another-secret-12345"), body); !errors.Is(err, ErrBadWebhook) {
		t.Errorf("wrong secret: err = %v", err)
	}
	if _, err := c.VerifyWebhook(sign(t, c, []byte("other"), c.cfg.APISecret), body); !errors.Is(err, ErrBadWebhook) {
		t.Errorf("body tampered: err = %v", err)
	}
	if _, err := c.VerifyWebhook("", body); !errors.Is(err, ErrBadWebhook) {
		t.Errorf("no header: err = %v", err)
	}
}

func TestMuteParticipantMutesAudioOnly(t *testing.T) {
	var muted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing admin token")
		}
		switch r.URL.Path {
		case "/twirp/livekit.RoomService/GetParticipant":
			_, _ = w.Write([]byte(`{"identity":"acc-1","tracks":[{"sid":"TR_a","type":"AUDIO"},{"sid":"TR_v","type":"VIDEO"},{"sid":"TR_z"}]}`))
		case "/twirp/livekit.RoomService/MutePublishedTrack":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			muted = append(muted, in["track_sid"].(string))
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := testClient(t, srv.URL)
	if err := c.MuteParticipant(context.Background(), "r1", "acc-1"); err != nil {
		t.Fatal(err)
	}
	if len(muted) != 2 || muted[0] != "TR_a" || muted[1] != "TR_z" {
		t.Errorf("muted = %v, want the audio tracks only", muted)
	}
}

func TestRemoveParticipantNotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if err := testClient(t, srv.URL).RemoveParticipant(context.Background(), "r", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestInternalURLDerivedFromPublic(t *testing.T) {
	if got := New(Config{PublicURL: "wss://x.test/livekit/"}).cfg.InternalURL; got != "https://x.test/livekit" {
		t.Errorf("internal url = %q", got)
	}
}

func TestEgressWebhookDecodesProtobufJSON(t *testing.T) {
	body := []byte(`{"event":"egress_ended","egressInfo":{"egressId":"EG_1","roomName":"r","status":"EGRESS_COMPLETE",
		"fileResults":[{"filename":"recordings/a.mp4","location":"s3://b/recordings/a.mp4","size":"2048","duration":"90000000000"}]}}`)
	var ev WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		t.Fatal(err)
	}
	e := ev.EgressInfo
	if e == nil || e.EgressID != "EG_1" || e.Status != "EGRESS_COMPLETE" || len(e.FileResults) != 1 {
		t.Fatalf("%+v", e)
	}
	// int64 fields arrive as strings; a plain number is read too.
	if f := e.FileResults[0]; int64(f.Size) != 2048 || int64(f.Duration) != 90_000_000_000 {
		t.Errorf("%+v", f)
	}
	var n struct {
		V FlexInt `json:"v"`
	}
	if err := json.Unmarshal([]byte(`{"v": 7}`), &n); err != nil || n.V != 7 {
		t.Errorf("number: %v %v", n, err)
	}
}

func TestStartRoomRecordingReadsTheEgressID(t *testing.T) {
	for name, body := range map[string]string{"snake": `{"egress_id":"EG_1","status":"EGRESS_STARTING"}`, "camel": `{"egressId":"EG_1"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/twirp/livekit.Egress/StartRoomCompositeEgress" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				t.Errorf("request %s %q", r.URL.Path, r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(body))
		}))
		c := New(Config{PublicURL: "ws://x", InternalURL: srv.URL, APIKey: "k", APISecret: "secretsecretsecretsecret0123456789"})
		id, err := c.StartRoomRecording(t.Context(), "room", "recordings/a.mp4", S3Output{Bucket: "b"})
		srv.Close()
		if err != nil || id != "EG_1" {
			t.Errorf("%s: id %q err %v", name, id, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()
	c := New(Config{PublicURL: "ws://x", InternalURL: srv.URL, APIKey: "k", APISecret: "secretsecretsecretsecret0123456789"})
	if _, err := c.StartRoomRecording(t.Context(), "room", "a.mp4", S3Output{}); err == nil {
		t.Error("an egress answered without an id must be an error")
	}
}
