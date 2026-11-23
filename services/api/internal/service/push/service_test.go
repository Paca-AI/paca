package pushsvc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/platform/secret"
)

// fakePushRepo is an in-memory notificationdom.PushRepository.
type fakePushRepo struct {
	mu      sync.Mutex
	subs    []*notificationdom.PushSubscription
	prefs   map[notificationdom.NotificationType]map[string]bool
	pub     string
	priv    string
	deleted []string
	saves   int
	dnd     *time.Time
}

func newFakePushRepo() *fakePushRepo {
	return &fakePushRepo{prefs: map[notificationdom.NotificationType]map[string]bool{}}
}

func (r *fakePushRepo) UpsertSubscription(_ context.Context, s *notificationdom.PushSubscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.subs {
		if e.Endpoint == s.Endpoint {
			r.subs[i] = s
			return nil
		}
	}
	r.subs = append(r.subs, s)
	return nil
}
func (r *fakePushRepo) DeleteSubscription(_ context.Context, acct uuid.UUID, endpoint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, endpoint)
	kept := r.subs[:0]
	for _, s := range r.subs {
		if s.AccountID != acct || s.Endpoint != endpoint {
			kept = append(kept, s)
		}
	}
	r.subs = kept
	return nil
}
func (r *fakePushRepo) DeleteSubscriptionByEndpoint(_ context.Context, endpoint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, endpoint)
	kept := r.subs[:0]
	for _, s := range r.subs {
		if s.Endpoint != endpoint {
			kept = append(kept, s)
		}
	}
	r.subs = kept
	return nil
}
func (r *fakePushRepo) ListSubscriptions(_ context.Context, acct uuid.UUID) ([]*notificationdom.PushSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*notificationdom.PushSubscription
	for _, s := range r.subs {
		if s.AccountID == acct {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *fakePushRepo) ListPreferences(context.Context, uuid.UUID) (map[notificationdom.NotificationType]map[string]bool, error) {
	return r.prefs, nil
}
func (r *fakePushRepo) SetPreference(_ context.Context, _ uuid.UUID, t notificationdom.NotificationType, ch string, en bool) error {
	if r.prefs[t] == nil {
		r.prefs[t] = map[string]bool{}
	}
	r.prefs[t][ch] = en
	return nil
}
func (r *fakePushRepo) GetDoNotDisturb(context.Context, uuid.UUID) (*time.Time, error) {
	return r.dnd, nil
}
func (r *fakePushRepo) SetDoNotDisturb(_ context.Context, _ uuid.UUID, until *time.Time) error {
	r.dnd = until
	return nil
}
func (r *fakePushRepo) GetVAPIDKeys(context.Context) (string, string, bool, error) {
	return r.pub, r.priv, r.pub != "", nil
}
func (r *fakePushRepo) SaveVAPIDKeys(_ context.Context, pub, priv string) (string, string, error) {
	r.saves++
	if r.pub == "" {
		r.pub, r.priv = pub, priv
	}
	return r.pub, r.priv, nil
}

func response(status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}
}

func validInput() notificationdom.PushSubscribeInput {
	return notificationdom.PushSubscribeInput{Endpoint: "https://push.example/abc", P256dh: "pk", Auth: "au", UserAgent: "ua"}
}

func notificationFor(acct uuid.UUID) *notificationdom.Notification {
	return &notificationdom.Notification{ID: uuid.New(), RecipientAccountID: acct, Type: notificationdom.NotificationTypeAssigned, ProjectID: uuid.New()}
}

func TestSubscribe_RejectsUnsafeOrMalformedSubscriptions(t *testing.T) {
	svc := New(newFakePushRepo(), "")
	acct := uuid.New()
	bad := map[string]notificationdom.PushSubscribeInput{
		"plain http":    {Endpoint: "http://push.example/a", P256dh: "pk", Auth: "au"},
		"no host":       {Endpoint: "https:///a", P256dh: "pk", Auth: "au"},
		"not a url":     {Endpoint: "::::", P256dh: "pk", Auth: "au"},
		"empty":         {},
		"missing keys":  {Endpoint: "https://push.example/a"},
		"oversized key": {Endpoint: "https://push.example/a", P256dh: strings.Repeat("k", maxKeyLen+1), Auth: "au"},
	}
	for name, in := range bad {
		if err := svc.Subscribe(context.Background(), acct, in); !errors.Is(err, ErrInvalidSubscription) {
			t.Errorf("%s: err = %v, want ErrInvalidSubscription", name, err)
		}
	}
	if err := svc.Subscribe(context.Background(), acct, validInput()); err != nil {
		t.Fatalf("valid subscription rejected: %v", err)
	}
}

func TestPreferences_DefaultToEnabled(t *testing.T) {
	repo := newFakePushRepo()
	svc := New(repo, "")
	acct := uuid.New()
	if err := svc.SetPreference(context.Background(), acct, notificationdom.NotificationTypeMentioned, false); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Preferences(context.Background(), acct)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range notificationdom.AllNotificationTypes {
		want := typ != notificationdom.NotificationTypeMentioned
		if got[typ] != want {
			t.Errorf("push for %s = %v, want %v", typ, got[typ], want)
		}
	}
	if err := svc.SetPreference(context.Background(), acct, "bogus", true); !errors.Is(err, ErrInvalidPreference) {
		t.Fatalf("unknown type: err = %v, want ErrInvalidPreference", err)
	}
}

func TestKeys_GeneratedOnceAndEncryptedAtRest(t *testing.T) {
	enc, err := secret.NewEncryptor(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakePushRepo()
	svc := New(repo, "").WithEncryptor(enc)

	pub, err := svc.PublicKey(context.Background())
	if err != nil || pub == "" {
		t.Fatalf("PublicKey = %q, %v", pub, err)
	}
	if again, _ := svc.PublicKey(context.Background()); again != pub || repo.saves != 1 {
		t.Fatalf("keys must be generated and saved once, saves=%d", repo.saves)
	}
	if repo.priv == "" {
		t.Fatal("private key not stored")
	}
	if _, derr := enc.Decrypt(repo.priv); derr != nil {
		t.Fatalf("the stored private key must be encrypted: %v", derr)
	}

	// A second replica reads the stored pair instead of generating its own.
	other := New(repo, "").WithEncryptor(enc)
	if got, _ := other.PublicKey(context.Background()); got != pub {
		t.Fatalf("a second service must use the stored key, got %q want %q", got, pub)
	}
	if repo.saves != 1 {
		t.Fatalf("the second service must not save a new pair, saves=%d", repo.saves)
	}
}

type sendScript struct {
	statuses map[string]int // endpoint -> status
	errs     map[string]error
	mu       sync.Mutex
	calls    []string
}

func (s *sendScript) send(_ context.Context, _ []byte, sub *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
	s.mu.Lock()
	s.calls = append(s.calls, sub.Endpoint)
	s.mu.Unlock()
	if err := s.errs[sub.Endpoint]; err != nil {
		return nil, err
	}
	return response(s.statuses[sub.Endpoint]), nil
}

func serviceWithSubs(t *testing.T, endpoints ...string) (*Service, *fakePushRepo, uuid.UUID) {
	t.Helper()
	repo := newFakePushRepo()
	svc := New(repo, "")
	acct := uuid.New()
	for _, e := range endpoints {
		in := validInput()
		in.Endpoint = e
		if err := svc.Subscribe(context.Background(), acct, in); err != nil {
			t.Fatal(err)
		}
	}
	return svc, repo, acct
}

func TestSend_NoSubscriptionsIsNotAnError(t *testing.T) {
	svc, _, acct := serviceWithSubs(t)
	script := &sendScript{}
	svc.send = script.send
	if err := svc.Send(context.Background(), notificationFor(acct)); err != nil || len(script.calls) != 0 {
		t.Fatalf("err=%v calls=%v; want nil and no sends", err, script.calls)
	}
}

func TestSend_DeletesGoneSubscriptionsAndSucceedsOnTheRest(t *testing.T) {
	svc, repo, acct := serviceWithSubs(t, "https://push.example/gone", "https://push.example/ok")
	script := &sendScript{statuses: map[string]int{"https://push.example/gone": 410, "https://push.example/ok": 201}}
	svc.send = script.send

	if err := svc.Send(context.Background(), notificationFor(acct)); err != nil {
		t.Fatalf("Send = %v, want nil", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "https://push.example/gone" {
		t.Fatalf("expected the 410 subscription to be deleted, got %v", repo.deleted)
	}
}

func TestSend_TransientFailureWithNoSuccessIsRetried(t *testing.T) {
	svc, repo, acct := serviceWithSubs(t, "https://push.example/a")
	svc.send = (&sendScript{statuses: map[string]int{"https://push.example/a": 503}}).send
	if err := svc.Send(context.Background(), notificationFor(acct)); err == nil {
		t.Fatal("a push service outage with nothing delivered must be returned for retry")
	}
	if len(repo.deleted) != 0 {
		t.Fatal("a 503 must not delete the subscription")
	}

	svc2, _, acct2 := serviceWithSubs(t, "https://push.example/a")
	svc2.send = (&sendScript{errs: map[string]error{"https://push.example/a": errors.New("timeout")}}).send
	if err := svc2.Send(context.Background(), notificationFor(acct2)); err == nil {
		t.Fatal("a network error with nothing delivered must be returned for retry")
	}
}

func TestSend_OneSuccessOutweighsAnotherDevicesTransientFailure(t *testing.T) {
	svc, _, acct := serviceWithSubs(t, "https://push.example/down", "https://push.example/ok")
	svc.send = (&sendScript{statuses: map[string]int{"https://push.example/down": 503, "https://push.example/ok": 201}}).send
	if err := svc.Send(context.Background(), notificationFor(acct)); err != nil {
		t.Fatalf("Send = %v; delivered to one device, so a retry would only duplicate it", err)
	}
}

func TestSend_ForbiddenWithPlaceholderContactKeepsTheSubscription(t *testing.T) {
	svc, repo, acct := serviceWithSubs(t, "https://push.example/a")
	svc.send = (&sendScript{statuses: map[string]int{"https://push.example/a": 403}}).send
	if err := svc.Send(context.Background(), notificationFor(acct)); err != nil {
		t.Fatal(err)
	}
	if len(repo.deleted) != 0 {
		t.Fatalf("a 403 under a placeholder contact is not proof of a stale subscription, deleted = %v", repo.deleted)
	}
}

func TestSubscribe_EvictsOldestPastTheCap(t *testing.T) {
	endpoints := make([]string, maxSubscriptionsPerAccount)
	for i := range endpoints {
		endpoints[i] = fmt.Sprintf("https://push.example/%d", i)
	}
	svc, repo, acct := serviceWithSubs(t, endpoints...)
	in := validInput()
	in.Endpoint = "https://push.example/new"
	if err := svc.Subscribe(context.Background(), acct, in); err != nil {
		t.Fatal(err)
	}
	n, _ := svc.SubscriptionCount(context.Background(), acct)
	if n != maxSubscriptionsPerAccount {
		t.Fatalf("count = %d, want %d", n, maxSubscriptionsPerAccount)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != endpoints[0] {
		t.Fatalf("the oldest must be evicted, deleted = %v", repo.deleted)
	}
}

func TestSend_KeyMismatchDeletesTheSubscription(t *testing.T) {
	svc, repo, acct := serviceWithSubs(t, "https://push.example/stale")
	svc.subscriberDefault = false
	svc.send = (&sendScript{statuses: map[string]int{"https://push.example/stale": 403}}).send
	if err := svc.Send(context.Background(), notificationFor(acct)); err != nil {
		t.Fatalf("a 403 cannot succeed on retry, got %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "https://push.example/stale" {
		t.Fatalf("a subscription the push service rejects for our key must be forgotten, deleted = %v", repo.deleted)
	}
}

func TestSend_RejectedMessageIsNotRetried(t *testing.T) {
	svc, _, acct := serviceWithSubs(t, "https://push.example/a")
	svc.send = (&sendScript{statuses: map[string]int{"https://push.example/a": 400}}).send
	if err := svc.Send(context.Background(), notificationFor(acct)); err != nil {
		t.Fatalf("a 400 cannot succeed on retry, got %v", err)
	}
}

func TestRenderPayload(t *testing.T) {
	project, task, doc, conv := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	cases := []struct {
		name     string
		n        notificationdom.Notification
		wantBody string
		wantURL  string
	}{
		{"assigned", notificationdom.Notification{Type: notificationdom.NotificationTypeAssigned, ActorFullName: "Ann", TaskID: &task, TaskNumber: 7, TaskTitle: "Fix it", ProjectID: project},
			"Ann assigned you #7 — Fix it", "/projects/" + project.String() + "/tasks/" + task.String()},
		{"mentioned in a document", notificationdom.Notification{Type: notificationdom.NotificationTypeMentioned, ActorFullName: "Ann", DocumentID: &doc, DocumentTitle: "Spec", ProjectID: project},
			"Ann mentioned you in document Spec", "/projects/" + project.String() + "/docs/" + doc.String()},
		{"question in a project", notificationdom.Notification{Type: notificationdom.NotificationTypeQuestion, ActorFullName: "Bot", ConversationID: &conv, ProjectID: project},
			"Bot asked you a question in a conversation", "/projects/" + project.String() + "/conversations/" + conv.String()},
		{"question in global chat", notificationdom.Notification{Type: notificationdom.NotificationTypeQuestion, ConversationID: &conv, ConversationTitle: "Plan"},
			"Someone asked you a question in Plan", "/conversations/" + conv.String()},
		{"automation", notificationdom.Notification{Type: notificationdom.NotificationTypeAutomation, Message: "Due soon", TaskID: &task, TaskNumber: 3, TaskTitle: "Ship", ProjectID: project},
			"Due soon #3 — Ship", "/projects/" + project.String() + "/tasks/" + task.String()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := RenderPayload(&c.n)
			if p.Body != c.wantBody || p.URL != c.wantURL {
				t.Fatalf("got body=%q url=%q\nwant body=%q url=%q", p.Body, p.URL, c.wantBody, c.wantURL)
			}
		})
	}
}

func TestRenderPayload_TruncatesLongBodies(t *testing.T) {
	n := notificationdom.Notification{Type: notificationdom.NotificationTypeAutomation, Message: strings.Repeat("x", 500)}
	if got := []rune(RenderPayload(&n).Body); len(got) != maxBodyRunes+1 {
		t.Fatalf("body is %d runes, want %d plus an ellipsis", len(got), maxBodyRunes)
	}
}

// --- Do Not Disturb ---------------------------------------------------------

var noon = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func dndService(repo *fakePushRepo) *Service {
	s := New(repo, "")
	s.now = func() time.Time { return noon }
	return s
}

func TestDoNotDisturb_OffByDefaultAndWhenExpired(t *testing.T) {
	repo := newFakePushRepo()
	s := dndService(repo)
	acct := uuid.New()
	if d, err := s.DoNotDisturb(context.Background(), acct); err != nil || d.Active {
		t.Fatalf("a new account must not be on DND: %+v %v", d, err)
	}
	past := noon.Add(-time.Minute)
	repo.dnd = &past
	if d, _ := s.DoNotDisturb(context.Background(), acct); d.Active || d.Until != nil {
		t.Fatalf("an ended DND must read as off: %+v", d)
	}
}

func TestSetDoNotDisturb_UntilAndIndefiniteAndOff(t *testing.T) {
	repo := newFakePushRepo()
	s := dndService(repo)
	ctx, acct := context.Background(), uuid.New()

	until := noon.Add(2 * time.Hour)
	if err := s.SetDoNotDisturb(ctx, acct, &until, false); err != nil {
		t.Fatal(err)
	}
	d, _ := s.DoNotDisturb(ctx, acct)
	if !d.Active || d.Indefinite || d.Until == nil || !d.Until.Equal(until) {
		t.Fatalf("timed DND = %+v", d)
	}

	if err := s.SetDoNotDisturb(ctx, acct, nil, true); err != nil {
		t.Fatal(err)
	}
	if d, _ = s.DoNotDisturb(ctx, acct); !d.Active || !d.Indefinite || d.Until != nil {
		t.Fatalf("indefinite DND = %+v", d)
	}

	if err := s.SetDoNotDisturb(ctx, acct, nil, false); err != nil {
		t.Fatal(err)
	}
	if d, _ = s.DoNotDisturb(ctx, acct); d.Active {
		t.Fatalf("DND must be off after clearing, got %+v", d)
	}
}

func TestSetDoNotDisturb_RejectsPastAndTooFarTimes(t *testing.T) {
	s := dndService(newFakePushRepo())
	past, far := noon.Add(-time.Hour), noon.Add(notificationdom.MaxDNDDuration+time.Hour)
	for name, u := range map[string]time.Time{"past": past, "now": noon, "too far": far} {
		if err := s.SetDoNotDisturb(context.Background(), uuid.New(), &u, false); !errors.Is(err, notificationdom.ErrInvalidDND) {
			t.Errorf("%s: err = %v, want ErrInvalidDND", name, err)
		}
	}
}

func TestSend_NothingIsPushedDuringDoNotDisturb(t *testing.T) {
	svc, repo, acct := serviceWithSubs(t, "https://push.example/a")
	svc.now = func() time.Time { return noon }
	script := &sendScript{statuses: map[string]int{"https://push.example/a": 201}}
	svc.send = script.send

	until := noon.Add(time.Hour)
	repo.dnd = &until
	if err := svc.Send(context.Background(), notificationFor(acct)); err != nil {
		t.Fatalf("a push dropped for DND is not an error to retry, got %v", err)
	}
	if len(script.calls) != 0 {
		t.Fatalf("sent %d pushes during Do Not Disturb, want none", len(script.calls))
	}

	// Once it ends, pushes flow again.
	svc.now = func() time.Time { return noon.Add(2 * time.Hour) }
	if err := svc.Send(context.Background(), notificationFor(acct)); err != nil {
		t.Fatal(err)
	}
	if len(script.calls) != 1 {
		t.Fatalf("sent %d pushes after DND ended, want 1", len(script.calls))
	}
}

func TestKeys_UndecryptableStoredKeyIsAnError(t *testing.T) {
	enc, err := secret.NewEncryptor([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakePushRepo()
	if _, err := New(repo, "").WithEncryptor(enc).PublicKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Same stored (encrypted) key, but no encryptor now: must fail, not sign with ciphertext.
	if _, err := New(repo, "").PublicKey(context.Background()); err == nil {
		t.Fatal("an encrypted key with no ENCRYPTION_KEY must be an error")
	}
	other, _ := secret.NewEncryptor([]byte(strings.Repeat("z", 32)))
	if _, err := New(repo, "").WithEncryptor(other).PublicKey(context.Background()); err == nil {
		t.Fatal("an encrypted key that does not decrypt must be an error")
	}
}
