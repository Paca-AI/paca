// Package pushsvc implements Web Push for notifications: browser subscription
// management, per-user push preferences, and sending.
package pushsvc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/platform/netguard"
	"github.com/Paca-AI/api/internal/platform/secret"
)

const (
	// maxEndpointLen and maxKeyLen bound client-supplied subscription fields.
	maxEndpointLen = 2048
	maxKeyLen      = 256
	maxUserAgent   = 512

	// pushTTL is how long a push service holds a message for an offline device.
	pushTTL = int((24 * time.Hour) / time.Second)

	sendTimeout = 10 * time.Second
)

var (
	ErrInvalidSubscription = notificationdom.ErrInvalidSubscription
	ErrInvalidPreference   = notificationdom.ErrInvalidPreference
)

var _ notificationdom.PushService = (*Service)(nil)

// sendFunc is webpush.SendNotificationWithContext, replaceable in tests.
type sendFunc func(ctx context.Context, payload []byte, sub *webpush.Subscription, opts *webpush.Options) (*http.Response, error)

// Service manages Web Push.
type Service struct {
	repo       notificationdom.PushRepository
	enc        *secret.Encryptor
	subscriber string
	client     *http.Client
	send       sendFunc
	now        func() time.Time

	mu      sync.Mutex
	public  string
	private string
}

// New returns a Service. subscriber is the contact URL push services may use to
// reach the operator (a https URL or mailto:); it is sent as the VAPID "sub".
func New(repo notificationdom.PushRepository, subscriber string) *Service {
	if !strings.HasPrefix(subscriber, "https://") && !strings.HasPrefix(subscriber, "mailto:") {
		subscriber = "mailto:admin@paca.local"
	}
	return &Service{
		repo:       repo,
		subscriber: subscriber,
		// Endpoints are client-supplied URLs, so every send goes through the SSRF
		// guard: a subscription naming an internal address is never dialled.
		client: netguard.NewSafeHTTPClient(sendTimeout),
		send:   webpush.SendNotificationWithContext,
		now:    time.Now,
	}
}

// WithEncryptor encrypts the VAPID private key at rest. Without one it is
// stored as-is, like the other secrets in this codebase.
func (s *Service) WithEncryptor(enc *secret.Encryptor) *Service {
	s.enc = enc
	return s
}

// keys returns the VAPID key pair, generating and storing it on first use.
func (s *Service) keys(ctx context.Context) (public, private string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.public != "" {
		return s.public, s.private, nil
	}
	pub, priv, found, err := s.repo.GetVAPIDKeys(ctx)
	if err != nil {
		return "", "", err
	}
	if !found {
		genPriv, genPub, genErr := webpush.GenerateVAPIDKeys()
		if genErr != nil {
			return "", "", fmt.Errorf("push: generate vapid keys: %w", genErr)
		}
		stored := genPriv
		if s.enc != nil {
			if stored, err = s.enc.Encrypt(genPriv); err != nil {
				return "", "", fmt.Errorf("push: encrypt vapid key: %w", err)
			}
		}
		// Another replica may have generated first: use whatever is stored.
		if pub, priv, err = s.repo.SaveVAPIDKeys(ctx, genPub, stored); err != nil {
			return "", "", err
		}
	}
	if s.enc != nil {
		if plain, decErr := s.enc.Decrypt(priv); decErr == nil {
			priv = plain
		}
	}
	s.public, s.private = pub, priv
	return pub, priv, nil
}

// PublicKey returns the VAPID public key a browser subscribes against.
func (s *Service) PublicKey(ctx context.Context) (string, error) {
	pub, _, err := s.keys(ctx)
	return pub, err
}

func validateSubscription(in notificationdom.PushSubscribeInput) error {
	if in.Endpoint == "" || len(in.Endpoint) > maxEndpointLen {
		return fmt.Errorf("%w: endpoint", ErrInvalidSubscription)
	}
	u, err := url.Parse(in.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%w: endpoint must be an https URL", ErrInvalidSubscription)
	}
	if in.P256dh == "" || len(in.P256dh) > maxKeyLen || in.Auth == "" || len(in.Auth) > maxKeyLen {
		return fmt.Errorf("%w: keys", ErrInvalidSubscription)
	}
	return nil
}

// Subscribe stores a browser subscription for the account.
func (s *Service) Subscribe(ctx context.Context, accountID uuid.UUID, in notificationdom.PushSubscribeInput) error {
	if err := validateSubscription(in); err != nil {
		return err
	}
	ua := in.UserAgent
	if len(ua) > maxUserAgent {
		ua = ua[:maxUserAgent]
	}
	return s.repo.UpsertSubscription(ctx, &notificationdom.PushSubscription{
		ID: uuid.New(), AccountID: accountID, Endpoint: in.Endpoint, P256dh: in.P256dh, Auth: in.Auth, UserAgent: ua,
	})
}

// Unsubscribe removes the account's subscription with this endpoint.
func (s *Service) Unsubscribe(ctx context.Context, accountID uuid.UUID, endpoint string) error {
	return s.repo.DeleteSubscription(ctx, accountID, endpoint)
}

// SubscriptionCount returns how many browsers the account has subscribed.
func (s *Service) SubscriptionCount(ctx context.Context, accountID uuid.UUID) (int, error) {
	subs, err := s.repo.ListSubscriptions(ctx, accountID)
	return len(subs), err
}

// Preferences returns, for every notification type, whether push is enabled
// (the default when nothing is stored).
func (s *Service) Preferences(ctx context.Context, accountID uuid.UUID) (map[notificationdom.NotificationType]bool, error) {
	stored, err := s.repo.ListPreferences(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make(map[notificationdom.NotificationType]bool, len(notificationdom.AllNotificationTypes))
	for _, t := range notificationdom.AllNotificationTypes {
		enabled, ok := stored[t][notificationdom.PreferenceChannelPush]
		out[t] = !ok || enabled
	}
	return out, nil
}

// SetPreference switches push on or off for one notification type.
func (s *Service) SetPreference(ctx context.Context, accountID uuid.UUID, t notificationdom.NotificationType, enabled bool) error {
	if !notificationdom.IsKnownType(t) {
		return fmt.Errorf("%w: unknown type %q", ErrInvalidPreference, t)
	}
	return s.repo.SetPreference(ctx, accountID, t, notificationdom.PreferenceChannelPush, enabled)
}

// DoNotDisturb returns the account's Do Not Disturb state.
func (s *Service) DoNotDisturb(ctx context.Context, accountID uuid.UUID) (notificationdom.DoNotDisturb, error) {
	until, err := s.repo.GetDoNotDisturb(ctx, accountID)
	if err != nil || until == nil || !until.After(s.now()) {
		return notificationdom.DoNotDisturb{}, err
	}
	d := notificationdom.DoNotDisturb{Active: true}
	if until.Year() >= notificationdom.DNDIndefinite.Year() {
		d.Indefinite = true
	} else {
		u := *until
		d.Until = &u
	}
	return d, nil
}

// SetDoNotDisturb turns Do Not Disturb on until the given time, or until it is
// turned off when indefinite is set, and off when until is nil.
func (s *Service) SetDoNotDisturb(ctx context.Context, accountID uuid.UUID, until *time.Time, indefinite bool) error {
	switch {
	case indefinite:
		end := notificationdom.DNDIndefinite
		return s.repo.SetDoNotDisturb(ctx, accountID, &end)
	case until == nil:
		return s.repo.SetDoNotDisturb(ctx, accountID, nil)
	}
	now := s.now()
	if !until.After(now) || until.After(now.Add(notificationdom.MaxDNDDuration)) {
		return notificationdom.ErrInvalidDND
	}
	end := until.UTC()
	return s.repo.SetDoNotDisturb(ctx, accountID, &end)
}

// Send pushes n to every browser the recipient has subscribed. It returns an
// error only when nothing was delivered and at least one failure could succeed
// on a retry (a push service outage, a timeout); subscriptions the push
// service reports gone are deleted, and failures a retry cannot fix are logged.
func (s *Service) Send(ctx context.Context, n *notificationdom.Notification) error {
	// Do Not Disturb holds when the push is due, not when it was queued, and a
	// push dropped for it is not retried: a notification that arrived during DND
	// should not buzz an hour after it ends.
	if dnd, err := s.DoNotDisturb(ctx, n.RecipientAccountID); err != nil {
		return err
	} else if dnd.Active {
		slog.DebugContext(ctx, "push dropped: do not disturb", "notification_id", n.ID)
		return nil
	}
	subs, err := s.repo.ListSubscriptions(ctx, n.RecipientAccountID)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}
	pub, priv, err := s.keys(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(RenderPayload(n))
	if err != nil {
		return fmt.Errorf("push: marshal payload: %w", err)
	}

	delivered, transient := 0, error(nil)
	for _, sub := range subs {
		resp, sendErr := s.send(ctx, payload, &webpush.Subscription{
			Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
		}, &webpush.Options{
			Subscriber: s.subscriber, VAPIDPublicKey: pub, VAPIDPrivateKey: priv,
			TTL: pushTTL, Urgency: webpush.UrgencyNormal, HTTPClient: s.client,
		})
		if sendErr != nil {
			slog.WarnContext(ctx, "push send failed", "notification_id", n.ID, "error", sendErr)
			transient = sendErr
			continue
		}
		status := resp.StatusCode
		_ = resp.Body.Close()
		switch {
		case status >= 200 && status < 300:
			delivered++
		case status == http.StatusNotFound || status == http.StatusGone:
			// The browser unsubscribed or the subscription expired.
			if delErr := s.repo.DeleteSubscriptionByEndpoint(ctx, sub.Endpoint); delErr != nil {
				slog.WarnContext(ctx, "push: could not delete expired subscription", "error", delErr)
			}
		case status == http.StatusTooManyRequests || status >= 500:
			transient = fmt.Errorf("push service returned %d", status)
		default:
			slog.WarnContext(ctx, "push service rejected the message; not retrying", "notification_id", n.ID, "status", status)
		}
	}
	if delivered == 0 && transient != nil {
		return transient
	}
	return nil
}
