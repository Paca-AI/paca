package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Removing a member deletes that principal's role attachments scoped to the
// project (never those in other projects or platform-wide), for every removal
// path: by user, by member id, and an agent leaving the project.
func TestRemoveMemberDeletesProjectScopedAttachments(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewProjectRepository(db)
	ctx := context.Background()
	role := fx.role(`{"version":"2026-10-01","statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/*"]}]}`)

	count := func(ptype string, principal uuid.UUID, project *uuid.UUID) int {
		var n int
		q := `SELECT count(*) FROM role_attachments WHERE principal_type = $1 AND principal_id = $2 AND project_id IS NULL`
		args := []any{ptype, principal}
		if project != nil {
			q = `SELECT count(*) FROM role_attachments WHERE principal_type = $1 AND principal_id = $2 AND project_id = $3`
			args = append(args, *project)
		}
		if err := db.Get(&n, q, args...); err != nil {
			t.Fatal(err)
		}
		return n
	}
	setup := func(ptype string, principal uuid.UUID) (p, other uuid.UUID, memberID uuid.UUID) {
		p, other = fx.project(), fx.project()
		memberID = fx.member(p, principal, ptype == "agent", false)
		fx.member(other, principal, ptype == "agent", false)
		fx.attach(role, ptype, principal, &p)
		fx.attach(role, ptype, principal, &other)
		fx.attach(role, ptype, principal, nil)
		return p, other, memberID
	}
	check := func(name, ptype string, principal, p, other uuid.UUID) {
		t.Helper()
		if n := count(ptype, principal, &p); n != 0 {
			t.Errorf("%s: %d attachments left in the project", name, n)
		}
		if n := count(ptype, principal, &other); n != 1 {
			t.Errorf("%s: other project's attachment touched (%d)", name, n)
		}
		if n := count(ptype, principal, nil); n != 1 {
			t.Errorf("%s: platform attachment touched (%d)", name, n)
		}
	}

	u := fx.user(false)
	p, other, _ := setup("user", u)
	if err := repo.RemoveMember(ctx, p, u); err != nil {
		t.Fatal(err)
	}
	check("RemoveMember", "user", u, p, other)

	u2 := fx.user(false)
	p, other, memberID := setup("user", u2)
	if err := repo.RemoveMemberByMemberID(ctx, memberID); err != nil {
		t.Fatal(err)
	}
	check("RemoveMemberByMemberID", "user", u2, p, other)

	a := fx.agent(nil, false)
	p, other, _ = setup("agent", a)
	if err := repo.RemoveAgentMember(ctx, p, a); err != nil {
		t.Fatal(err)
	}
	check("RemoveAgentMember", "agent", a, p, other)

	// Not a member: nothing removed, the not-found contract is unchanged.
	if err := repo.RemoveMember(ctx, fx.project(), fx.user(false)); err == nil {
		t.Error("removing a non-member must fail")
	}
}
