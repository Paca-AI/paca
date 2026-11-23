-- Reliable notification delivery.
--
-- One migration for the whole feature, in three parts:
--   1. notification_deliveries: the delivery outbox, so a lost realtime or push
--      announcement is retried instead of vanishing;
--   2. Web Push: browser subscriptions, per-type push preferences and the
--      server's VAPID key pair;
--   3. Do Not Disturb: silences push while it is on.

-- =============================================================================
-- 1. Notification delivery outbox
-- =============================================================================

-- Delivery outbox for notifications.
--
-- A notification row is the source of truth for what a user should see; a
-- delivery row records one attempt-tracked push of it over one channel, so a
-- push that was lost (a Valkey outage, a process that died between the insert
-- and the publish) is retried instead of vanishing. Created in the same
-- transaction as the notification, so a notification never exists without the
-- delivery that announces it.
--
-- Only 'realtime' is dispatched today. 'email' and 'push' are reserved so the
-- later channels need no table change.
CREATE TABLE IF NOT EXISTS notification_deliveries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id UUID        NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    channel         TEXT        NOT NULL CHECK (channel IN ('realtime', 'email', 'push')),
    status          TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts        INT         NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error      TEXT,
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- One delivery per channel per notification: also the idempotency key a
    -- later channel's retries rely on.
    CONSTRAINT notification_deliveries_unique_channel UNIQUE (notification_id, channel)
);

-- The dispatcher's claim query: due pending rows only, oldest first.
CREATE INDEX IF NOT EXISTS idx_notification_deliveries_due
    ON notification_deliveries (next_attempt_at)
    WHERE status = 'pending';

-- Retention pruning scans finished rows by age.
CREATE INDEX IF NOT EXISTS idx_notification_deliveries_finished
    ON notification_deliveries (updated_at)
    WHERE status <> 'pending';

-- =============================================================================
-- 2. Web Push: subscriptions, preferences and VAPID keys
-- =============================================================================

-- Web Push: browser push subscriptions, per-user notification preferences and
-- the server's VAPID key pair.

-- One row per browser subscription (a user may have several devices). The
-- endpoint is globally unique: a browser that signs in as another user is
-- re-pointed at that user rather than getting a second row.
CREATE TABLE IF NOT EXISTS push_subscriptions (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint     TEXT        NOT NULL UNIQUE,
    p256dh       TEXT        NOT NULL,
    auth         TEXT        NOT NULL,
    user_agent   TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_push_subscriptions_account ON push_subscriptions (account_id);

-- Sparse per-user preferences: a missing row means "enabled", so adding a
-- notification type or channel later needs no backfill. Only 'push' is
-- consulted today; 'email' is reserved (email opt-out lives in the SMTP
-- plugin).
CREATE TABLE IF NOT EXISTS notification_preferences (
    account_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       TEXT        NOT NULL CHECK (type IN ('assigned', 'mentioned', 'question', 'automation')),
    channel    TEXT        NOT NULL CHECK (channel IN ('push', 'email')),
    enabled    BOOLEAN     NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, type, channel)
);

-- The server's VAPID key pair, generated on first use. Singleton row (same
-- trick as workspace_settings). private_key is encrypted with ENCRYPTION_KEY
-- when one is configured, like other secrets stored at rest.
CREATE TABLE IF NOT EXISTS push_vapid_keys (
    id          BOOLEAN     PRIMARY KEY DEFAULT TRUE CHECK (id),
    public_key  TEXT        NOT NULL,
    private_key TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- 3. Do Not Disturb
-- =============================================================================

-- Do Not Disturb: while dnd_until is in the future, no Web Push is sent to the
-- account (bell, chat, calls and meeting proposals alike). The inbox, the chat
-- unread counts and the bell badge are unaffected: DND silences, it does not
-- hide. A far-future value (9999-12-31) means "until I turn it off".
CREATE TABLE IF NOT EXISTS user_notification_settings (
    account_id UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    dnd_until  TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
