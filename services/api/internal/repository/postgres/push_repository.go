package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

var _ notificationdom.PushRepository = (*PushRepository)(nil)

// PushRepository stores Web Push subscriptions, notification preferences and
// the VAPID key pair.
type PushRepository struct {
	db *sqlx.DB
}

// NewPushRepository returns a PushRepository backed by db.
func NewPushRepository(db *sqlx.DB) *PushRepository {
	return &PushRepository{db: db}
}

// UpsertSubscription stores s, replacing the subscription with the same endpoint.
func (r *PushRepository) UpsertSubscription(ctx context.Context, s *notificationdom.PushSubscription) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO push_subscriptions (id, account_id, endpoint, p256dh, auth, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (endpoint) DO UPDATE
		SET account_id = EXCLUDED.account_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth,
		    user_agent = EXCLUDED.user_agent, updated_at = NOW()`,
		s.ID.String(), s.AccountID.String(), s.Endpoint, s.P256dh, s.Auth, s.UserAgent)
	if err != nil {
		return fmt.Errorf("push repo: upsert subscription: %w", err)
	}
	return nil
}

// DeleteSubscription removes the account's subscription with this endpoint.
func (r *PushRepository) DeleteSubscription(ctx context.Context, accountID uuid.UUID, endpoint string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE account_id = $1 AND endpoint = $2`, accountID.String(), endpoint); err != nil {
		return fmt.Errorf("push repo: delete subscription: %w", err)
	}
	return nil
}

// DeleteSubscriptionByEndpoint removes a subscription the push service reported gone.
func (r *PushRepository) DeleteSubscriptionByEndpoint(ctx context.Context, endpoint string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint); err != nil {
		return fmt.Errorf("push repo: delete subscription by endpoint: %w", err)
	}
	return nil
}

type pushSubscriptionRow struct {
	ID        string `db:"id"`
	AccountID string `db:"account_id"`
	Endpoint  string `db:"endpoint"`
	P256dh    string `db:"p256dh"`
	Auth      string `db:"auth"`
	UserAgent string `db:"user_agent"`
}

// ListSubscriptions returns the account's subscriptions.
func (r *PushRepository) ListSubscriptions(ctx context.Context, accountID uuid.UUID) ([]*notificationdom.PushSubscription, error) {
	var rows []pushSubscriptionRow
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT id, account_id, endpoint, p256dh, auth, user_agent
		FROM push_subscriptions WHERE account_id = $1 ORDER BY created_at`, accountID.String()); err != nil {
		return nil, fmt.Errorf("push repo: list subscriptions: %w", err)
	}
	out := make([]*notificationdom.PushSubscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, &notificationdom.PushSubscription{
			ID: uuid.MustParse(row.ID), AccountID: uuid.MustParse(row.AccountID),
			Endpoint: row.Endpoint, P256dh: row.P256dh, Auth: row.Auth, UserAgent: row.UserAgent,
		})
	}
	return out, nil
}

// ListPreferences returns the account's stored preferences.
func (r *PushRepository) ListPreferences(ctx context.Context, accountID uuid.UUID) (map[notificationdom.NotificationType]map[string]bool, error) {
	var rows []struct {
		Type    string `db:"type"`
		Channel string `db:"channel"`
		Enabled bool   `db:"enabled"`
	}
	if err := r.db.SelectContext(ctx, &rows, `SELECT type, channel, enabled FROM notification_preferences WHERE account_id = $1`, accountID.String()); err != nil {
		return nil, fmt.Errorf("push repo: list preferences: %w", err)
	}
	out := make(map[notificationdom.NotificationType]map[string]bool)
	for _, row := range rows {
		t := notificationdom.NotificationType(row.Type)
		if out[t] == nil {
			out[t] = make(map[string]bool)
		}
		out[t][row.Channel] = row.Enabled
	}
	return out, nil
}

// SetPreference stores one preference.
func (r *PushRepository) SetPreference(ctx context.Context, accountID uuid.UUID, t notificationdom.NotificationType, channel string, enabled bool) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO notification_preferences (account_id, type, channel, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (account_id, type, channel) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`,
		accountID.String(), string(t), channel, enabled)
	if err != nil {
		return fmt.Errorf("push repo: set preference: %w", err)
	}
	return nil
}

// GetDoNotDisturb returns until when the account does not want pushes.
func (r *PushRepository) GetDoNotDisturb(ctx context.Context, accountID uuid.UUID) (*time.Time, error) {
	var until *time.Time
	err := r.db.GetContext(ctx, &until, `SELECT dnd_until FROM user_notification_settings WHERE account_id = $1`, accountID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("push repo: get do not disturb: %w", err)
	}
	return until, nil
}

// SetDoNotDisturb stores until, or clears it when nil.
func (r *PushRepository) SetDoNotDisturb(ctx context.Context, accountID uuid.UUID, until *time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_notification_settings (account_id, dnd_until) VALUES ($1, $2)
		ON CONFLICT (account_id) DO UPDATE SET dnd_until = EXCLUDED.dnd_until, updated_at = NOW()`,
		accountID.String(), until)
	if err != nil {
		return fmt.Errorf("push repo: set do not disturb: %w", err)
	}
	return nil
}

// GetVAPIDKeys returns the stored key pair, or found=false when none exists.
func (r *PushRepository) GetVAPIDKeys(ctx context.Context) (string, string, bool, error) {
	var row struct {
		Public  string `db:"public_key"`
		Private string `db:"private_key"`
	}
	err := r.db.GetContext(ctx, &row, `SELECT public_key, private_key FROM push_vapid_keys WHERE id = TRUE`)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("push repo: get vapid keys: %w", err)
	}
	return row.Public, row.Private, true, nil
}

// SaveVAPIDKeys stores the pair unless one exists, and returns what is stored.
func (r *PushRepository) SaveVAPIDKeys(ctx context.Context, public, private string) (string, string, error) {
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO push_vapid_keys (id, public_key, private_key) VALUES (TRUE, $1, $2)
		ON CONFLICT (id) DO NOTHING`, public, private); err != nil {
		return "", "", fmt.Errorf("push repo: save vapid keys: %w", err)
	}
	pub, priv, found, err := r.GetVAPIDKeys(ctx)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", fmt.Errorf("push repo: vapid keys missing after save")
	}
	return pub, priv, nil
}
