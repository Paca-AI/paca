package notificationdom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// PushSubscription is one browser's Web Push subscription for an account.
type PushSubscription struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	// Endpoint is the push service URL the browser handed out.
	Endpoint string
	// P256dh and Auth are the subscription's client keys (base64url).
	P256dh    string
	Auth      string
	UserAgent string
	CreatedAt time.Time
}

// PushChannelPreference channels a user can switch per notification type.
const (
	PreferenceChannelPush  = "push"
	PreferenceChannelEmail = "email"
)

// AllNotificationTypes lists every notification type, in display order.
var AllNotificationTypes = []NotificationType{
	NotificationTypeAssigned,
	NotificationTypeMentioned,
	NotificationTypeQuestion,
	NotificationTypeAutomation,
}

// IsKnownType reports whether t is a notification type.
func IsKnownType(t NotificationType) bool {
	for _, k := range AllNotificationTypes {
		if k == t {
			return true
		}
	}
	return false
}

// PushRepository persists subscriptions, preferences and the VAPID key pair.
type PushRepository interface {
	// UpsertSubscription stores s, replacing any subscription with the same
	// endpoint (and so re-pointing it at s.AccountID).
	UpsertSubscription(ctx context.Context, s *PushSubscription) error
	// DeleteSubscription removes the account's subscription with this endpoint.
	DeleteSubscription(ctx context.Context, accountID uuid.UUID, endpoint string) error
	// DeleteSubscriptionByEndpoint removes a subscription the push service
	// reported gone, whoever owns it.
	DeleteSubscriptionByEndpoint(ctx context.Context, endpoint string) error
	// ListSubscriptions returns the account's subscriptions.
	ListSubscriptions(ctx context.Context, accountID uuid.UUID) ([]*PushSubscription, error)

	// ListPreferences returns the account's stored preferences as
	// type -> channel -> enabled. Anything not stored is enabled.
	ListPreferences(ctx context.Context, accountID uuid.UUID) (map[NotificationType]map[string]bool, error)
	// SetPreference stores one preference.
	SetPreference(ctx context.Context, accountID uuid.UUID, t NotificationType, channel string, enabled bool) error

	// GetDoNotDisturb returns until when the account does not want pushes, nil
	// when it never set it. The time may be in the past.
	GetDoNotDisturb(ctx context.Context, accountID uuid.UUID) (*time.Time, error)
	// SetDoNotDisturb stores until, or clears Do Not Disturb when it is nil.
	SetDoNotDisturb(ctx context.Context, accountID uuid.UUID, until *time.Time) error

	// GetVAPIDKeys returns the stored key pair (private key as stored, possibly
	// encrypted), or found=false when none exists yet.
	GetVAPIDKeys(ctx context.Context) (public, private string, found bool, err error)
	// SaveVAPIDKeys stores the key pair unless one already exists, and returns
	// the pair that is stored afterwards — so two replicas generating at once
	// agree on the winner.
	SaveVAPIDKeys(ctx context.Context, public, private string) (storedPublic, storedPrivate string, err error)
}

// ErrInvalidSubscription is returned for a push subscription the server will
// not store.
var ErrInvalidSubscription = errors.New("invalid push subscription")

// ErrInvalidPreference is returned for an unknown notification type or channel.
var ErrInvalidPreference = errors.New("invalid notification preference")

// DNDIndefinite is the Do Not Disturb end time that means "until I turn it
// off": far enough away never to arrive.
var DNDIndefinite = time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)

// MaxDNDDuration is the longest a Do Not Disturb with an end time may run.
const MaxDNDDuration = 366 * 24 * time.Hour

// ErrInvalidDND is returned for a Do Not Disturb end time in the past or too
// far away.
var ErrInvalidDND = errors.New("invalid do not disturb time")

// DoNotDisturb is an account's Do Not Disturb state.
type DoNotDisturb struct {
	// Active is whether it is on right now.
	Active bool
	// Until is when it ends; nil when it is off. Indefinite is set instead of a
	// date for "until I turn it off".
	Until      *time.Time
	Indefinite bool
}

// ErrPushUnavailable is returned when Web Push cannot be offered.
var ErrPushUnavailable = errors.New("web push unavailable")

// PushSubscribeInput is a browser's PushSubscription as it is sent to the API.
type PushSubscribeInput struct {
	Endpoint  string
	P256dh    string
	Auth      string
	UserAgent string
}

// PushService is what the HTTP layer needs from Web Push.
type PushService interface {
	// PublicKey returns the VAPID public key a browser subscribes against.
	PublicKey(ctx context.Context) (string, error)
	// Subscribe stores a browser subscription for the account.
	Subscribe(ctx context.Context, accountID uuid.UUID, in PushSubscribeInput) error
	// Unsubscribe removes the account's subscription with this endpoint.
	Unsubscribe(ctx context.Context, accountID uuid.UUID, endpoint string) error
	// SubscriptionCount returns how many browsers the account has subscribed.
	SubscriptionCount(ctx context.Context, accountID uuid.UUID) (int, error)
	// Preferences returns, per notification type, whether push is enabled.
	Preferences(ctx context.Context, accountID uuid.UUID) (map[NotificationType]bool, error)
	// SetPreference switches push on or off for one notification type.
	SetPreference(ctx context.Context, accountID uuid.UUID, t NotificationType, enabled bool) error
	// DoNotDisturb returns the account's Do Not Disturb state.
	DoNotDisturb(ctx context.Context, accountID uuid.UUID) (DoNotDisturb, error)
	// SetDoNotDisturb turns Do Not Disturb on until the given time (indefinite:
	// until it is turned off), or off when until is nil and indefinite is false.
	SetDoNotDisturb(ctx context.Context, accountID uuid.UUID, until *time.Time, indefinite bool) error
}
