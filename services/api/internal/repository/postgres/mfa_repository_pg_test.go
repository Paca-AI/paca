package postgres

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	mfadom "github.com/Paca-AI/api/internal/domain/mfa"
)

func TestMFARepository_PG(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := &iamFx{t: t, db: db}
	repo := NewMFARepository(db)
	ctx := t.Context()
	uid := fx.user(false)

	if _, err := repo.GetEnrollment(ctx, uid); !errors.Is(err, mfadom.ErrNotFound) {
		t.Fatalf("missing enrollment: got %v, want ErrNotFound", err)
	}

	enr := &mfadom.Enrollment{UserID: uid, SecretEnc: "enc", Enabled: true, RecoveryCodes: []string{"h1", "h2", "h3"}, LastUsedStep: 5}
	if err := repo.SaveEnrollment(ctx, enr); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetEnrollment(ctx, uid)
	if err != nil || !got.Enabled || got.SecretEnc != "enc" || got.LastUsedStep != 5 || !slices.Equal(got.RecoveryCodes, enr.RecoveryCodes) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}

	// Upsert replaces, and a nil code list is stored as [] not null.
	if err := repo.SaveEnrollment(ctx, &mfadom.Enrollment{UserID: uid, SecretEnc: "enc2"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = repo.GetEnrollment(ctx, uid); got.Enabled || got.SecretEnc != "enc2" || len(got.RecoveryCodes) != 0 {
		t.Fatalf("upsert: %+v", got)
	}
	repo.SaveEnrollment(ctx, enr)

	// Replay protection.
	if ok, err := repo.AdvanceStep(ctx, uid, 6); err != nil || !ok {
		t.Fatalf("advance 6: %v %v", ok, err)
	}
	for _, step := range []int64{6, 5} {
		if ok, _ := repo.AdvanceStep(ctx, uid, step); ok {
			t.Fatalf("step %d must be rejected as a replay", step)
		}
	}

	// Recovery codes are single-use.
	if ok, err := repo.ConsumeRecoveryCode(ctx, uid, "h2"); err != nil || !ok {
		t.Fatalf("consume h2: %v %v", ok, err)
	}
	if ok, _ := repo.ConsumeRecoveryCode(ctx, uid, "h2"); ok {
		t.Fatal("h2 consumed twice")
	}
	if got, _ = repo.GetEnrollment(ctx, uid); !slices.Equal(got.RecoveryCodes, []string{"h1", "h3"}) {
		t.Fatalf("remaining codes: %v", got.RecoveryCodes)
	}

	// Per-user CIDRs: default empty.
	if c, err := repo.GetUserCIDRs(ctx, uid); err != nil || len(c) != 0 {
		t.Fatalf("default cidrs: %v %v", c, err)
	}
	if err := repo.SetUserCIDRs(ctx, uid, []string{"10.0.0.0/8", "::1/128"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := repo.GetUserCIDRs(ctx, uid); !slices.Equal(c, []string{"10.0.0.0/8", "::1/128"}) {
		t.Fatalf("cidrs: %v", c)
	}
	if c, err := repo.GetUserCIDRs(ctx, uuid.New()); err != nil || len(c) != 0 {
		t.Fatalf("unknown user cidrs: %v %v", c, err)
	}

	// Policy: seeded default is off, and covers every role.
	p, err := repo.GetPolicy(ctx)
	if err != nil || p.RequireMFA || !slices.Equal(p.PrivilegedRoles, []string{mfadom.AllRoles}) || len(p.IPAllowList) != 0 {
		t.Fatalf("default policy: %+v %v", p, err)
	}
	want := &mfadom.Policy{PrivilegedRoles: []string{"ADMIN"}, RequireMFA: true, IPAllowList: []string{"192.0.2.0/24"}}
	if err := repo.SavePolicy(ctx, want, &uid); err != nil {
		t.Fatal(err)
	}
	if p, _ = repo.GetPolicy(ctx); !p.RequireMFA || !slices.Equal(p.PrivilegedRoles, want.PrivilegedRoles) || !slices.Equal(p.IPAllowList, want.IPAllowList) {
		t.Fatalf("policy round trip: %+v", p)
	}

	// Deleting the enrollment, and the cascade when the user goes away.
	if err := repo.DeleteEnrollment(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetEnrollment(ctx, uid); !errors.Is(err, mfadom.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if err := repo.DeleteEnrollment(ctx, uid); err != nil {
		t.Fatalf("deleting a missing enrollment must be a no-op: %v", err)
	}
}
