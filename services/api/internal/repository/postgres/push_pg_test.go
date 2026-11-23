package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

func (f *deliveryFx) subscribe(t *testing.T, endpoint string) {
	t.Helper()
	if err := NewPushRepository(f.db).UpsertSubscription(context.Background(), &notificationdom.PushSubscription{
		ID: uuid.New(), AccountID: f.user, Endpoint: endpoint, P256dh: "pk", Auth: "au",
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *deliveryFx) pushDeliveries(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := f.db.GetContext(context.Background(), &n, `SELECT COUNT(*) FROM notification_deliveries WHERE notification_id = $1 AND channel = 'push'`, id); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateNotification_NoPushDeliveryWithoutSubscription(t *testing.T) {
	f := newDeliveryFx(t)
	if got := f.pushDeliveries(t, f.notify(t).ID); got != 0 {
		t.Fatalf("a user with no push subscription must not get a push delivery row, got %d", got)
	}
}

func TestCreateNotification_PushDeliveryWhenSubscribedAndEnabled(t *testing.T) {
	f := newDeliveryFx(t)
	f.subscribe(t, "https://push.example/a")
	n := f.notify(t)
	if got := f.pushDeliveries(t, n.ID); got != 1 {
		t.Fatalf("expected one push delivery, got %d", got)
	}
	// Due at once, unlike the realtime announcement.
	claimed, err := f.repo.ClaimDue(context.Background(), notificationdom.DeliveryChannelPush, time.Now().Add(time.Second), 10,
		func(p []notificationdom.PendingDelivery) []notificationdom.DeliveryOutcome {
			if len(p) == 1 && p[0].Notification.Message != "hi" {
				t.Errorf("claimed push delivery should carry display fields, message = %q", p[0].Notification.Message)
			}
			return nil
		})
	if err != nil || claimed != 1 {
		t.Fatalf("ClaimDue(push) = %d, %v; want 1, nil", claimed, err)
	}
}

func TestCreateNotification_NoPushDeliveryWhenTypeSwitchedOff(t *testing.T) {
	f := newDeliveryFx(t)
	f.subscribe(t, "https://push.example/a")
	repo := NewPushRepository(f.db)
	if err := repo.SetPreference(context.Background(), f.user, notificationdom.NotificationTypeAutomation, notificationdom.PreferenceChannelPush, false); err != nil {
		t.Fatal(err)
	}
	if got := f.pushDeliveries(t, f.notify(t).ID); got != 0 {
		t.Fatalf("a type the user switched off must not get a push delivery, got %d", got)
	}
	// Switching it back on restores delivery.
	if err := repo.SetPreference(context.Background(), f.user, notificationdom.NotificationTypeAutomation, notificationdom.PreferenceChannelPush, true); err != nil {
		t.Fatal(err)
	}
	if got := f.pushDeliveries(t, f.notify(t).ID); got != 1 {
		t.Fatalf("expected a push delivery after switching the type back on, got %d", got)
	}
}

func TestPushRepository_SubscriptionLifecycle(t *testing.T) {
	f := newDeliveryFx(t)
	ctx := context.Background()
	repo := NewPushRepository(f.db)
	other := newIAMFx(t, f.db).user(false)

	f.subscribe(t, "https://push.example/a")
	// The same browser signing in as someone else is re-pointed, not duplicated.
	if err := repo.UpsertSubscription(ctx, &notificationdom.PushSubscription{ID: uuid.New(), AccountID: other, Endpoint: "https://push.example/a", P256dh: "pk2", Auth: "au2"}); err != nil {
		t.Fatal(err)
	}
	mine, _ := repo.ListSubscriptions(ctx, f.user)
	theirs, _ := repo.ListSubscriptions(ctx, other)
	if len(mine) != 0 || len(theirs) != 1 || theirs[0].P256dh != "pk2" {
		t.Fatalf("subscription should have moved to the new account: mine=%d theirs=%+v", len(mine), theirs)
	}

	// Deleting needs the owner; the by-endpoint delete (a push service saying "gone") does not.
	if err := repo.DeleteSubscription(ctx, f.user, "https://push.example/a"); err != nil {
		t.Fatal(err)
	}
	if theirs, _ = repo.ListSubscriptions(ctx, other); len(theirs) != 1 {
		t.Fatal("another account must not be able to delete the subscription")
	}
	if err := repo.DeleteSubscriptionByEndpoint(ctx, "https://push.example/a"); err != nil {
		t.Fatal(err)
	}
	if theirs, _ = repo.ListSubscriptions(ctx, other); len(theirs) != 0 {
		t.Fatal("the subscription should be gone")
	}
}

func TestPushRepository_Preferences(t *testing.T) {
	f := newDeliveryFx(t)
	ctx := context.Background()
	repo := NewPushRepository(f.db)
	if err := repo.SetPreference(ctx, f.user, notificationdom.NotificationTypeAssigned, notificationdom.PreferenceChannelPush, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPreference(ctx, f.user, notificationdom.NotificationTypeAssigned, notificationdom.PreferenceChannelPush, false); err != nil {
		t.Fatalf("setting twice must be idempotent: %v", err)
	}
	got, err := repo.ListPreferences(ctx, f.user)
	if err != nil || got[notificationdom.NotificationTypeAssigned][notificationdom.PreferenceChannelPush] != false || len(got) != 1 {
		t.Fatalf("preferences = %v, %v", got, err)
	}
}

func TestPushRepository_VAPIDKeysFirstWriterWins(t *testing.T) {
	f := newDeliveryFx(t)
	ctx := context.Background()
	repo := NewPushRepository(f.db)
	if _, _, found, err := repo.GetVAPIDKeys(ctx); err != nil || found {
		t.Fatalf("no keys yet: found=%v err=%v", found, err)
	}
	pub, priv, err := repo.SaveVAPIDKeys(ctx, "pub1", "priv1")
	if err != nil || pub != "pub1" || priv != "priv1" {
		t.Fatalf("first save = %q %q %v", pub, priv, err)
	}
	pub, priv, err = repo.SaveVAPIDKeys(ctx, "pub2", "priv2")
	if err != nil || pub != "pub1" || priv != "priv1" {
		t.Fatalf("a second writer must get the stored pair back, got %q %q %v", pub, priv, err)
	}
}

func TestPushRepository_DoNotDisturb(t *testing.T) {
	f := newDeliveryFx(t)
	ctx := context.Background()
	repo := NewPushRepository(f.db)

	if got, err := repo.GetDoNotDisturb(ctx, f.user); err != nil || got != nil {
		t.Fatalf("no row yet: %v %v", got, err)
	}
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	if err := repo.SetDoNotDisturb(ctx, f.user, &until); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetDoNotDisturb(ctx, f.user); err != nil || got == nil || !got.Equal(until) {
		t.Fatalf("stored = %v, %v; want %v", got, err, until)
	}
	// The indefinite sentinel round-trips.
	end := notificationdom.DNDIndefinite
	if err := repo.SetDoNotDisturb(ctx, f.user, &end); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetDoNotDisturb(ctx, f.user); got == nil || got.Year() != 9999 {
		t.Fatalf("indefinite = %v", got)
	}
	if err := repo.SetDoNotDisturb(ctx, f.user, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetDoNotDisturb(ctx, f.user); got != nil {
		t.Fatalf("cleared = %v", got)
	}
}
