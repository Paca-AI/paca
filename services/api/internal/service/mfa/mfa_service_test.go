package mfasvc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	mfadom "github.com/Paca-AI/api/internal/domain/mfa"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/secret"
	"github.com/Paca-AI/api/internal/platform/totp"
)

type memStore struct {
	fails  map[uuid.UUID]int
	clock  func() time.Time
	enr    map[uuid.UUID]*mfadom.Enrollment
	cidrs  map[uuid.UUID][]string
	policy mfadom.Policy
}

func newMemStore() *memStore {
	return &memStore{fails: map[uuid.UUID]int{}, enr: map[uuid.UUID]*mfadom.Enrollment{}, cidrs: map[uuid.UUID][]string{}}
}

func (m *memStore) GetEnrollment(_ context.Context, id uuid.UUID) (*mfadom.Enrollment, error) {
	e, ok := m.enr[id]
	if !ok {
		return nil, mfadom.ErrNotFound
	}
	c := *e
	return &c, nil
}
func (m *memStore) SaveEnrollment(_ context.Context, e *mfadom.Enrollment) error {
	c := *e
	m.enr[e.UserID] = &c
	return nil
}
func (m *memStore) DeleteEnrollment(_ context.Context, id uuid.UUID) error {
	delete(m.enr, id)
	return nil
}
func (m *memStore) AdvanceStep(_ context.Context, id uuid.UUID, step int64) (bool, error) {
	e := m.enr[id]
	if e.LastUsedStep >= step {
		return false, nil
	}
	e.LastUsedStep = step
	return true, nil
}
func (m *memStore) ConsumeRecoveryCode(_ context.Context, id uuid.UUID, h string) (bool, error) {
	e := m.enr[id]
	for i, c := range e.RecoveryCodes {
		if c == h {
			e.RecoveryCodes = append(e.RecoveryCodes[:i], e.RecoveryCodes[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}
func (m *memStore) ReplaceRecoveryCodes(_ context.Context, id uuid.UUID, h []string) error {
	m.enr[id].RecoveryCodes = h
	return nil
}
func (m *memStore) RegisterFailure(_ context.Context, id uuid.UUID, max int, d time.Duration) error {
	m.fails[id]++
	if m.fails[id] >= max {
		m.fails[id] = 0
		t := m.clock().Add(d)
		m.enr[id].LockedUntil = &t
	}
	return nil
}
func (m *memStore) ClearFailures(_ context.Context, id uuid.UUID) error {
	m.fails[id] = 0
	if e := m.enr[id]; e != nil {
		e.LockedUntil = nil
	}
	return nil
}
func (m *memStore) GetUserCIDRs(_ context.Context, id uuid.UUID) ([]string, error) {
	return m.cidrs[id], nil
}
func (m *memStore) SetUserCIDRs(_ context.Context, id uuid.UUID, c []string) error {
	m.cidrs[id] = c
	return nil
}
func (m *memStore) GetPolicy(context.Context) (*mfadom.Policy, error) {
	p := m.policy
	return &p, nil
}
func (m *memStore) SavePolicy(_ context.Context, p *mfadom.Policy, _ *uuid.UUID) error {
	m.policy = *p
	return nil
}

func newSvc(t *testing.T) (*Service, *memStore, *time.Time) {
	t.Helper()
	enc, err := secret.NewEncryptor(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	st := newMemStore()
	svc := New(st, enc, "Paca")
	clock := time.Unix(1700000000, 0)
	svc.now = func() time.Time { return clock }
	st.clock = svc.now
	return svc, st, &clock
}

func admin() *userdom.User {
	return &userdom.User{ID: uuid.New(), Username: "root", Roles: []roledom.Summary{{Name: "ADMIN"}}}
}

// enrol runs Begin+Confirm and returns the secret and recovery codes.
func enrol(t *testing.T, svc *Service, u *userdom.User) (string, []string) {
	t.Helper()
	sec, uri, err := svc.Begin(context.Background(), u)
	if err != nil || !strings.HasPrefix(uri, "otpauth://") {
		t.Fatalf("Begin: %v %q", err, uri)
	}
	code, _ := totp.Code(sec, svc.now())
	codes, err := svc.Confirm(context.Background(), u, code)
	if err != nil || len(codes) != recoveryCodeCount {
		t.Fatalf("Confirm: %v (%d codes)", err, len(codes))
	}
	return sec, codes
}

func TestCheckLogin_DefaultsUnrestricted(t *testing.T) {
	svc, _, _ := newSvc(t)
	if err := svc.CheckLogin(context.Background(), admin(), "", "203.0.113.9"); err != nil {
		t.Fatalf("existing installs must be unchanged: %v", err)
	}
}

func TestCheckLogin_MFAEnabledNeedsValidCode(t *testing.T) {
	svc, _, clock := newSvc(t)
	u := admin()
	sec, _ := enrol(t, svc, u)
	ctx := context.Background()

	if err := svc.CheckLogin(ctx, u, "", "1.1.1.1"); !errors.Is(err, mfadom.ErrRequired) {
		t.Fatalf("no code: got %v, want ErrRequired", err)
	}
	if err := svc.CheckLogin(ctx, u, "000000", "1.1.1.1"); !errors.Is(err, mfadom.ErrInvalidCode) {
		t.Fatalf("bad code: got %v, want ErrInvalidCode", err)
	}
	// The confirm step already consumed this time step, so a fresh one is needed.
	*clock = clock.Add(totp.Period)
	code, _ := totp.Code(sec, *clock)
	if err := svc.CheckLogin(ctx, u, code, "1.1.1.1"); err != nil {
		t.Fatalf("valid code: %v", err)
	}
	if err := svc.CheckLogin(ctx, u, code, "1.1.1.1"); !errors.Is(err, mfadom.ErrInvalidCode) {
		t.Fatalf("replayed code: got %v, want ErrInvalidCode", err)
	}
}

func TestCheckLogin_RecoveryCodeIsSingleUse(t *testing.T) {
	svc, _, _ := newSvc(t)
	u := admin()
	_, codes := enrol(t, svc, u)
	ctx := context.Background()
	if err := svc.CheckLogin(ctx, u, strings.ToUpper(codes[0]), "1.1.1.1"); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	if err := svc.CheckLogin(ctx, u, codes[0], "1.1.1.1"); !errors.Is(err, mfadom.ErrInvalidCode) {
		t.Fatalf("reused recovery code: got %v", err)
	}
}

func TestCheckLogin_PolicyRequiresMFAForPrivilegedOnly(t *testing.T) {
	svc, st, _ := newSvc(t)
	st.policy = mfadom.Policy{PrivilegedRoles: []string{"admin"}, RequireMFA: true}
	ctx := context.Background()

	if err := svc.CheckLogin(ctx, admin(), "", "1.1.1.1"); !errors.Is(err, mfadom.ErrSetupRequired) {
		t.Fatalf("privileged unenrolled: got %v, want ErrSetupRequired", err)
	}
	plain := &userdom.User{ID: uuid.New(), Username: "bob", Roles: []roledom.Summary{{Name: "USER"}}}
	if err := svc.CheckLogin(ctx, plain, "", "1.1.1.1"); err != nil {
		t.Fatalf("non-privileged must be unaffected: %v", err)
	}
}

func TestCheckLogin_AllRolesAppliesToEveryone(t *testing.T) {
	svc, st, _ := newSvc(t)
	st.policy = mfadom.Policy{PrivilegedRoles: []string{mfadom.AllRoles}, RequireMFA: true}
	roleless := &userdom.User{ID: uuid.New(), Username: "bob"}
	if err := svc.CheckLogin(context.Background(), roleless, "", "1.1.1.1"); !errors.Is(err, mfadom.ErrSetupRequired) {
		t.Fatalf("all-roles policy must cover any account: got %v", err)
	}
	// Saving collapses the wildcard so it can't be stored alongside names.
	p := &mfadom.Policy{PrivilegedRoles: []string{"ADMIN", mfadom.AllRoles}}
	if err := svc.SetPolicy(context.Background(), p, nil, "1.1.1.1"); err != nil || len(st.policy.PrivilegedRoles) != 1 {
		t.Fatalf("SetPolicy: %v %v", err, st.policy.PrivilegedRoles)
	}
}

func TestCheckLogin_IPAllowLists(t *testing.T) {
	svc, st, _ := newSvc(t)
	ctx := context.Background()
	u := admin()

	st.policy = mfadom.Policy{PrivilegedRoles: []string{"ADMIN"}, IPAllowList: []string{"10.0.0.0/8"}}
	if err := svc.CheckLogin(ctx, u, "", "10.1.2.3"); err != nil {
		t.Fatalf("inside instance list: %v", err)
	}
	if err := svc.CheckLogin(ctx, u, "", "192.168.1.1"); !errors.Is(err, mfadom.ErrIPNotAllowed) {
		t.Fatalf("outside instance list: got %v", err)
	}
	plain := &userdom.User{ID: uuid.New(), Username: "bob"}
	if err := svc.CheckLogin(ctx, plain, "", "192.168.1.1"); err != nil {
		t.Fatalf("instance list must not touch non-privileged: %v", err)
	}

	st.cidrs[plain.ID] = []string{"192.168.1.0/24"}
	if err := svc.CheckLogin(ctx, plain, "", "192.168.1.5"); err != nil {
		t.Fatalf("inside account list: %v", err)
	}
	if err := svc.CheckLogin(ctx, plain, "", "172.16.0.1"); !errors.Is(err, mfadom.ErrIPNotAllowed) {
		t.Fatalf("outside account list: got %v", err)
	}
	if err := svc.CheckLogin(ctx, plain, "", "not-an-ip"); !errors.Is(err, mfadom.ErrIPNotAllowed) {
		t.Fatalf("unparseable ip must fail closed: got %v", err)
	}
}

func TestDisableAndRegenerateNeedSecondFactor(t *testing.T) {
	svc, _, clock := newSvc(t)
	u := admin()
	sec, codes := enrol(t, svc, u)
	ctx := context.Background()

	if _, err := svc.RegenerateRecoveryCodes(ctx, u, "000000"); !errors.Is(err, mfadom.ErrInvalidCode) {
		t.Fatalf("regenerate bad code: %v", err)
	}
	fresh, err := svc.RegenerateRecoveryCodes(ctx, u, codes[0])
	if err != nil || len(fresh) != recoveryCodeCount {
		t.Fatalf("regenerate: %v", err)
	}
	if err := svc.CheckLogin(ctx, u, codes[1], "1.1.1.1"); !errors.Is(err, mfadom.ErrInvalidCode) {
		t.Fatalf("old recovery codes must stop working: %v", err)
	}
	*clock = clock.Add(totp.Period)
	code, _ := totp.Code(sec, *clock)
	if err := svc.Disable(ctx, u, code); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if en, _, _ := svc.Status(ctx, u); en {
		t.Fatal("still enabled after Disable")
	}
}

func TestNormalizeCIDRs(t *testing.T) {
	got, err := NormalizeCIDRs([]string{" 10.0.0.1 ", "", "2001:db8::/32", "::1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.1/32", "2001:db8::/32", "::1/128"}
	if m, err := NormalizeCIDRs([]string{"10.1.2.3/8"}); err != nil || m[0] != "10.0.0.0/8" {
		t.Fatalf("host bits not masked: %v %v", m, err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := NormalizeCIDRs([]string{"10.0.0.0/33"}); !errors.Is(err, mfadom.ErrInvalidCIDR) {
		t.Fatalf("bad prefix: %v", err)
	}
	if _, err := NormalizeCIDRs([]string{"nope"}); !errors.Is(err, mfadom.ErrInvalidCIDR) {
		t.Fatalf("bad ip: %v", err)
	}
}

func TestBeginRefusedWithoutEncryptionKey(t *testing.T) {
	svc := New(newMemStore(), nil, "")
	if _, _, err := svc.Begin(context.Background(), admin()); !errors.Is(err, mfadom.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestSetupRequired(t *testing.T) {
	svc, st, clock := newSvc(t)
	ctx := context.Background()
	u := admin()
	check := func(want bool, msg string) {
		t.Helper()
		if got, err := svc.SetupRequired(ctx, u); err != nil || got != want {
			t.Fatalf("%s: got %v, %v; want %v", msg, got, err, want)
		}
	}

	check(false, "policy off")
	st.policy = mfadom.Policy{PrivilegedRoles: []string{"ADMIN"}, RequireMFA: true}
	check(true, "privileged and unenrolled")

	st.policy.PrivilegedRoles = []string{"OTHER"}
	check(false, "role not covered")
	st.policy.PrivilegedRoles = []string{"ADMIN"}

	_, uri, err := svc.Begin(ctx, u)
	if err != nil || uri == "" {
		t.Fatal(err)
	}
	check(true, "pending enrollment still counts as unenrolled")

	enr, _ := st.GetEnrollment(ctx, u.ID)
	sec, _ := svc.enc.Decrypt(enr.SecretEnc)
	code, _ := totp.Code(sec, *clock)
	if _, err := svc.Confirm(ctx, u, code); err != nil {
		t.Fatal(err)
	}
	check(false, "already enrolled")
}

func TestRecoveryCodesUseOnlyTheAlphabetAndAreUnique(t *testing.T) {
	plain, hashes, err := newRecoveryCodes()
	if err != nil || len(plain) != recoveryCodeCount || len(hashes) != recoveryCodeCount {
		t.Fatalf("got %d/%d codes, %v", len(plain), len(hashes), err)
	}
	seen := map[string]bool{}
	for _, c := range plain {
		if len(c) != 11 || c[5] != '-' || seen[c] {
			t.Fatalf("bad or repeated code %q", c)
		}
		seen[c] = true
		for _, r := range strings.ReplaceAll(c, "-", "") {
			if !strings.ContainsRune(recoveryAlphabet, r) {
				t.Fatalf("%q has %q outside the alphabet", c, r)
			}
		}
	}
}

func TestWrongCodesLockTheAccount(t *testing.T) {
	svc, st, clock := newSvc(t)
	ctx := context.Background()
	u := admin()
	sec, _ := enrol(t, svc, u)
	*clock = clock.Add(time.Minute)

	for i := 0; i < maxFailedAttempts; i++ {
		err := svc.CheckLogin(ctx, u, "000000", "1.2.3.4")
		if i < maxFailedAttempts-1 && !errors.Is(err, mfadom.ErrInvalidCode) {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if i == maxFailedAttempts-1 && !errors.Is(err, mfadom.ErrLocked) {
			t.Fatalf("the attempt that trips the lock should say so: %v", err)
		}
	}
	good, _ := totp.Code(sec, svc.now())
	err := svc.CheckLogin(ctx, u, good, "1.2.3.4")
	var le *mfadom.LockedError
	if !errors.Is(err, mfadom.ErrLocked) || !errors.As(err, &le) || le.RetryAfter <= 0 || le.RetryAfter > lockoutDuration {
		t.Fatalf("locked account accepted a valid code or lacks a wait: %v", err)
	}
	*clock = clock.Add(lockoutDuration + time.Second)
	good, _ = totp.Code(sec, svc.now())
	if err := svc.CheckLogin(ctx, u, good, "1.2.3.4"); err != nil {
		t.Fatalf("after lockout: %v", err)
	}
	if st.fails[u.ID] != 0 || st.enr[u.ID].LockedUntil != nil {
		t.Fatal("success should clear failures")
	}
}

func TestConfirmLocksAfterRepeatedWrongCodes(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	u := admin()
	if _, _, err := svc.Begin(ctx, u); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxFailedAttempts; i++ {
		_, _ = svc.Confirm(ctx, u, "000000")
	}
	if _, err := svc.Confirm(ctx, u, "000000"); !errors.Is(err, mfadom.ErrLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckIPAppliesAllowLists(t *testing.T) {
	svc, st, _ := newSvc(t)
	u := admin()
	st.policy = mfadom.Policy{PrivilegedRoles: []string{"ADMIN"}, IPAllowList: []string{"10.0.0.0/8"}}
	if err := svc.CheckIP(context.Background(), u, "8.8.8.8"); !errors.Is(err, mfadom.ErrIPNotAllowed) {
		t.Fatalf("got %v", err)
	}
	if err := svc.CheckIP(context.Background(), u, "10.1.1.1"); err != nil {
		t.Fatal(err)
	}
}

func TestSetPolicyRefusesSelfLockout(t *testing.T) {
	svc, st, _ := newSvc(t)
	ctx := context.Background()
	p := &mfadom.Policy{PrivilegedRoles: []string{"*"}, IPAllowList: []string{"10.0.0.0/8"}}
	if err := svc.SetPolicy(ctx, p, nil, "8.8.8.8"); !errors.Is(err, mfadom.ErrSelfLockout) {
		t.Fatalf("got %v", err)
	}
	if err := svc.SetPolicy(ctx, p, nil, "10.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if len(st.policy.IPAllowList) != 1 {
		t.Fatal("policy not saved")
	}
	if err := svc.SetPolicy(ctx, &mfadom.Policy{PrivilegedRoles: []string{"*"}}, nil, "8.8.8.8"); err != nil {
		t.Fatalf("clearing the list must always be allowed: %v", err)
	}
}

func TestBeginReusesRecentPendingSecret(t *testing.T) {
	svc, st, clock := newSvc(t)
	ctx := context.Background()
	u := admin()
	a, _, err := svc.Begin(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	st.enr[u.ID].UpdatedAt = *clock
	b, _, err := svc.Begin(ctx, u)
	if err != nil || a != b {
		t.Fatalf("pending secret replaced: %v", err)
	}
	*clock = clock.Add(pendingReuseWindow + time.Second)
	if c, _, _ := svc.Begin(ctx, u); c == a {
		t.Fatal("stale pending secret should be replaced")
	}
}
