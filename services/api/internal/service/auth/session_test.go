package auth_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
)

var _ domainauth.SessionService = (*authsvc.Service)(nil)

// memSessions is an in-memory domainauth.SessionStore.
type memSessions struct {
	mu sync.Mutex
	m  map[string]domainauth.Session
}

func newMemSessions() *memSessions { return &memSessions{m: map[string]domainauth.Session{}} }

func (s *memSessions) Save(_ context.Context, in domainauth.Session, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.m[in.ID]; ok {
		in.CreatedAt = old.CreatedAt
	}
	s.m[in.ID] = in
	return nil
}
func (s *memSessions) Get(_ context.Context, id string) (*domainauth.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m[id]; ok {
		return &v, nil
	}
	return nil, domainauth.ErrSessionNotFound
}
func (s *memSessions) ListByUser(_ context.Context, uid uuid.UUID) ([]domainauth.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domainauth.Session
	for _, v := range s.m {
		if v.UserID == uid {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (s *memSessions) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}

// sessionFixture is a service whose refresh store really remembers revocations.
type sessionFixture struct {
	svc      *authsvc.Service
	sessions *memSessions
	revoked  map[string]bool
	user     *userdom.User
}

func newSessionFixture(t *testing.T) *sessionFixture {
	t.Helper()
	u := &userdom.User{ID: uuid.New(), Username: "alice", Roles: userRoles("USER")}
	f := &sessionFixture{sessions: newMemSessions(), revoked: map[string]bool{}, user: u}
	store := &stubRefreshStore{
		revokeFamily: func(_ context.Context, id string, _ time.Duration) error { f.revoked[id] = true; return nil },
		isFamilyRevoked: func(_ context.Context, id string) (bool, error) {
			return f.revoked[id], nil
		},
	}
	repo := &stubUserRepo{findByID: func(context.Context, uuid.UUID) (*userdom.User, error) { return u, nil }}
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	f.svc = authsvc.New(repo, tm, store, 7*24*time.Hour, 24*time.Hour).WithSessionStore(f.sessions)
	return f
}

func (f *sessionFixture) signIn(t *testing.T, ua, ip string) string {
	t.Helper()
	ctx := domainauth.WithClientInfo(context.Background(), domainauth.ClientInfo{IP: ip, UserAgent: ua})
	pair, err := f.svc.IssueSession(ctx, f.user, true)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwttoken.New("test-secret", time.Minute, time.Hour).Verify(pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return claims.FamilyID
}

func TestIssueSession_RecordsClientDetails(t *testing.T) {
	f := newSessionFixture(t)
	id := f.signIn(t, "Mozilla/5.0 Firefox/130.0", "203.0.113.9")

	got, err := f.sessions.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != f.user.ID || got.IP != "203.0.113.9" || got.UserAgent != "Mozilla/5.0 Firefox/130.0" || !got.RememberMe {
		t.Fatalf("unexpected session: %+v", got)
	}
}

func TestListSessions_MarksCurrentAndScopesToUser(t *testing.T) {
	f := newSessionFixture(t)
	a := f.signIn(t, "ua-a", "1.1.1.1")
	b := f.signIn(t, "ua-b", "2.2.2.2")
	// Someone else's session never shows up.
	_ = f.sessions.Save(context.Background(), domainauth.Session{ID: "foreign", UserID: uuid.New()}, time.Hour)

	list, err := f.svc.ListSessions(context.Background(), f.user.ID, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d sessions, want 2", len(list))
	}
	for _, s := range list {
		if s.Current != (s.ID == b) {
			t.Fatalf("session %s current=%v", s.ID, s.Current)
		}
	}
	_ = a
}

func TestListSessions_SkipsRevokedLeftovers(t *testing.T) {
	f := newSessionFixture(t)
	id := f.signIn(t, "ua", "1.1.1.1")
	f.revoked[id] = true // revoked, but its registry entry survived

	list, _ := f.svc.ListSessions(context.Background(), f.user.ID, "")
	if len(list) != 0 {
		t.Fatalf("revoked session listed: %+v", list)
	}
}

func TestRevokeSession_RevokesFamilyAndForgetsIt(t *testing.T) {
	f := newSessionFixture(t)
	id := f.signIn(t, "ua", "1.1.1.1")

	if err := f.svc.RevokeSession(context.Background(), f.user.ID, id); err != nil {
		t.Fatal(err)
	}
	if !f.revoked[id] {
		t.Fatal("family was not revoked")
	}
	if _, err := f.sessions.Get(context.Background(), id); !errors.Is(err, domainauth.ErrSessionNotFound) {
		t.Fatalf("session still registered: %v", err)
	}
}

func TestRevokeSession_OtherUsersSessionIsNotFound(t *testing.T) {
	f := newSessionFixture(t)
	id := f.signIn(t, "ua", "1.1.1.1")

	err := f.svc.RevokeSession(context.Background(), uuid.New(), id)
	if !errors.Is(err, domainauth.ErrSessionNotFound) {
		t.Fatalf("got %v, want ErrSessionNotFound", err)
	}
	if f.revoked[id] {
		t.Fatal("someone else's session was revoked")
	}
}

func TestRevokeSession_UnknownIsNotFound(t *testing.T) {
	f := newSessionFixture(t)
	if err := f.svc.RevokeSession(context.Background(), f.user.ID, "nope"); !errors.Is(err, domainauth.ErrSessionNotFound) {
		t.Fatalf("got %v, want ErrSessionNotFound", err)
	}
}

func TestRevokeOtherSessions_KeepsCurrent(t *testing.T) {
	f := newSessionFixture(t)
	keep := f.signIn(t, "ua", "1.1.1.1")
	x := f.signIn(t, "ua", "2.2.2.2")
	y := f.signIn(t, "ua", "3.3.3.3")

	n, err := f.svc.RevokeOtherSessions(context.Background(), f.user.ID, keep)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v, want 2", n, err)
	}
	if f.revoked[keep] || !f.revoked[x] || !f.revoked[y] {
		t.Fatalf("revoked = %v", f.revoked)
	}
}

func TestLogout_ForgetsSession(t *testing.T) {
	f := newSessionFixture(t)
	id := f.signIn(t, "ua", "1.1.1.1")

	if err := f.svc.Logout(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.Get(context.Background(), id); !errors.Is(err, domainauth.ErrSessionNotFound) {
		t.Fatalf("session still registered: %v", err)
	}
}

func TestRefresh_TouchesSessionAndKeepsCreatedAt(t *testing.T) {
	f := newSessionFixture(t)
	ctx := domainauth.WithClientInfo(context.Background(), domainauth.ClientInfo{IP: "1.1.1.1", UserAgent: "ua"})
	pair, err := f.svc.IssueSession(ctx, f.user, true)
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := jwttoken.New("test-secret", time.Minute, time.Hour).Verify(pair.RefreshToken)
	first, _ := f.sessions.Get(ctx, claims.FamilyID)

	time.Sleep(5 * time.Millisecond)
	moved := domainauth.WithClientInfo(context.Background(), domainauth.ClientInfo{IP: "9.9.9.9", UserAgent: "ua"})
	if _, err := f.svc.Refresh(moved, pair.RefreshToken); err != nil {
		t.Fatal(err)
	}
	after, _ := f.sessions.Get(ctx, claims.FamilyID)
	if after.IP != "9.9.9.9" || !after.CreatedAt.Equal(first.CreatedAt) || !after.LastActiveAt.After(first.LastActiveAt) {
		t.Fatalf("before %+v after %+v", first, after)
	}
}

func TestSessions_NoStoreConfigured(t *testing.T) {
	svc := newAuthSvc(&stubUserRepo{}, &stubRefreshStore{})
	ctx := context.Background()
	if list, err := svc.ListSessions(ctx, uuid.New(), ""); err != nil || len(list) != 0 {
		t.Fatalf("got %v, %v", list, err)
	}
	if err := svc.RevokeSession(ctx, uuid.New(), "x"); !errors.Is(err, domainauth.ErrSessionNotFound) {
		t.Fatalf("got %v", err)
	}
}
