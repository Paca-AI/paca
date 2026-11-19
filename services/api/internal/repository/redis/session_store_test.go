package redis

import (
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
)

func newSessionStore(t *testing.T) (*SessionStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return NewSessionStore(redis.NewClient(&redis.Options{Addr: mr.Addr()})), mr
}

func TestSessionStore_SaveGetKeepsCreatedAt(t *testing.T) {
	st, _ := newSessionStore(t)
	ctx := t.Context()
	uid := uuid.New()
	t0 := time.UnixMilli(1_700_000_000_000)

	if err := st.Save(ctx, domainauth.Session{ID: "f1", UserID: uid, IP: "1.1.1.1", UserAgent: "ua1", RememberMe: true, CreatedAt: t0, LastActiveAt: t0}, time.Hour); err != nil {
		t.Fatal(err)
	}
	// A refresh later: new activity time and address, but created_at must stay.
	t1 := t0.Add(10 * time.Minute)
	if err := st.Save(ctx, domainauth.Session{ID: "f1", UserID: uid, IP: "2.2.2.2", UserAgent: "ua2", CreatedAt: t1, LastActiveAt: t1}, time.Hour); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get(ctx, "f1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(t0) || !got.LastActiveAt.Equal(t1) {
		t.Fatalf("created=%v last=%v, want %v / %v", got.CreatedAt, got.LastActiveAt, t0, t1)
	}
	if got.IP != "2.2.2.2" || got.UserAgent != "ua2" || !got.RememberMe || got.UserID != uid {
		t.Fatalf("unexpected session: %+v", got)
	}
}

func TestSessionStore_GetMissing(t *testing.T) {
	st, _ := newSessionStore(t)
	if _, err := st.Get(t.Context(), "nope"); !errors.Is(err, domainauth.ErrSessionNotFound) {
		t.Fatalf("got %v, want ErrSessionNotFound", err)
	}
}

func TestSessionStore_ListByUser_OrderScopeAndStale(t *testing.T) {
	st, mr := newSessionStore(t)
	ctx := t.Context()
	me, other := uuid.New(), uuid.New()
	base := time.UnixMilli(1_700_000_000_000)
	save := func(id string, uid uuid.UUID, age time.Duration) {
		t.Helper()
		at := base.Add(age)
		if err := st.Save(ctx, domainauth.Session{ID: id, UserID: uid, CreatedAt: at, LastActiveAt: at}, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	save("old", me, 0)
	save("new", me, time.Minute)
	save("theirs", other, 2*time.Minute)
	save("gone", me, 3*time.Minute)
	mr.Del(sessionKey("gone")) // hash expired, index entry left behind

	got, err := st.ListByUser(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("got %+v, want [new old]", got)
	}
	// The dead index entry was pruned.
	if n, _ := st.client.ZCard(ctx, sessionUserKey(me)).Result(); n != 2 {
		t.Fatalf("index size %d, want 2", n)
	}
}

func TestSessionStore_Delete(t *testing.T) {
	st, _ := newSessionStore(t)
	ctx := t.Context()
	uid := uuid.New()
	now := time.Now()
	_ = st.Save(ctx, domainauth.Session{ID: "f1", UserID: uid, CreatedAt: now, LastActiveAt: now}, time.Hour)

	if err := st.Delete(ctx, "f1"); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(ctx, "f1"); err != nil { // unknown id is fine
		t.Fatal(err)
	}
	if _, err := st.Get(ctx, "f1"); !errors.Is(err, domainauth.ErrSessionNotFound) {
		t.Fatalf("got %v, want ErrSessionNotFound", err)
	}
	if list, _ := st.ListByUser(ctx, uid); len(list) != 0 {
		t.Fatalf("still listed: %+v", list)
	}
}

func TestSessionStore_ExpiresWithTTL(t *testing.T) {
	st, mr := newSessionStore(t)
	ctx := t.Context()
	uid := uuid.New()
	now := time.Now()
	_ = st.Save(ctx, domainauth.Session{ID: "f1", UserID: uid, CreatedAt: now, LastActiveAt: now}, time.Minute)
	mr.FastForward(2 * time.Minute)
	if list, _ := st.ListByUser(ctx, uid); len(list) != 0 {
		t.Fatalf("expired session still listed: %+v", list)
	}
}
