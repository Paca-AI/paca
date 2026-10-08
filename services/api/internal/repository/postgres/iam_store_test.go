package postgres

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

const allowAllPolicy = `{"version":"1","statements":[{"effect":"Allow","actions":["*"],"resources":["*"]}]}`
const projAllowPolicy = `{"version":"1","statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/*"]}]}`

func grantRoleIDs(gs []iam.Grant) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.RoleID)
	}
	sort.Strings(out)
	return out
}

func TestIAMStoreListGrants(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	store := NewIAMStore(db)
	ctx := context.Background()
	p1, p2 := fx.project(), fx.project()

	t.Run("platform role for active user", func(t *testing.T) {
		u := fx.user(false)
		r := fx.role(allowAllPolicy)
		fx.attach(r, "user", u, nil)
		gs, err := store.ListGrants(ctx, iam.Principal{Type: "user", ID: u.String()})
		if err != nil || len(gs) != 1 {
			t.Fatalf("%v %v", gs, err)
		}
		if gs[0].RoleID != r.String() || gs[0].ProjectID != "" || gs[0].Policy == nil {
			t.Fatalf("%+v", gs[0])
		}
		res := iam.Evaluate(gs, iam.Request{Action: "users:read", Resource: "project/x"})
		if !res.Allowed {
			t.Fatal("platform * must allow")
		}
	})

	t.Run("project scoped needs active member", func(t *testing.T) {
		u := fx.user(false)
		r := fx.role(projAllowPolicy)
		fx.attach(r, "user", u, &p1)
		pr := iam.Principal{Type: "user", ID: u.String()}
		if gs, _ := store.ListGrants(ctx, pr); len(gs) != 0 {
			t.Fatalf("no membership => no grant, got %v", gs)
		}
		m := fx.member(p1, u, false, false)
		gs, err := store.ListGrants(ctx, pr)
		if err != nil || len(gs) != 1 || gs[0].ProjectID != p1.String() {
			t.Fatalf("%+v %v", gs, err)
		}
		// GHSA-hjcj class: role naming project/* never reaches p2.
		if iam.Evaluate(gs, iam.Request{Action: "tasks:read", Resource: "project/" + p2.String() + "/task/t"}).Allowed {
			t.Fatal("project-scoped attachment must not allow in another project")
		}
		if !iam.Evaluate(gs, iam.Request{Action: "tasks:read", Resource: "project/" + p1.String() + "/task/t"}).Allowed {
			t.Fatal("must allow in own project")
		}
		// soft-delete the membership
		pgExec(t, db, `UPDATE project_members SET deleted_at = NOW() WHERE id = $1`, m)
		if gs, _ := store.ListGrants(ctx, pr); len(gs) != 0 {
			t.Fatalf("soft-deleted member => no grant, got %v", gs)
		}
	})

	t.Run("membership in another project does not count", func(t *testing.T) {
		u := fx.user(false)
		fx.attach(fx.role(projAllowPolicy), "user", u, &p1)
		fx.member(p2, u, false, false)
		if gs, _ := store.ListGrants(ctx, iam.Principal{Type: "user", ID: u.String()}); len(gs) != 0 {
			t.Fatalf("got %v", gs)
		}
	})

	t.Run("soft-deleted user has no grants", func(t *testing.T) {
		u := fx.user(true)
		fx.attach(fx.role(allowAllPolicy), "user", u, nil)
		r2 := fx.role(projAllowPolicy)
		fx.attach(r2, "user", u, &p1)
		fx.member(p1, u, false, false)
		if gs, _ := store.ListGrants(ctx, iam.Principal{Type: "user", ID: u.String()}); len(gs) != 0 {
			t.Fatalf("got %v", gs)
		}
	})

	t.Run("agent principal", func(t *testing.T) {
		a := fx.agent(nil, false)
		pr := iam.Principal{Type: "agent", ID: a.String()}
		fx.attach(fx.role(allowAllPolicy), "agent", a, nil)
		fx.attach(fx.role(projAllowPolicy), "agent", a, &p1)
		gs, err := store.ListGrants(ctx, pr)
		if err != nil || len(gs) != 1 {
			t.Fatalf("platform only before membership: %v %v", gs, err)
		}
		fx.member(p1, a, true, false)
		if gs, _ = store.ListGrants(ctx, pr); len(gs) != 2 {
			t.Fatalf("want 2 after membership, got %v", gs)
		}
		// a user id never matches an agent attachment and vice versa
		if gs, _ = store.ListGrants(ctx, iam.Principal{Type: "user", ID: a.String()}); len(gs) != 0 {
			t.Fatalf("type confusion: %v", gs)
		}
	})

	t.Run("soft-deleted agent has no grants", func(t *testing.T) {
		a := fx.agent(nil, true)
		fx.attach(fx.role(allowAllPolicy), "agent", a, nil)
		if gs, _ := store.ListGrants(ctx, iam.Principal{Type: "agent", ID: a.String()}); len(gs) != 0 {
			t.Fatalf("got %v", gs)
		}
	})

	t.Run("project-owned role only counts in its own project", func(t *testing.T) {
		u := fx.user(false)
		fx.member(p1, u, false, false)
		fx.member(p2, u, false, false)
		owned := fx.role(projAllowPolicy)
		pgExec(t, db, `UPDATE roles SET project_id = $2 WHERE id = $1`, owned, p1)
		pr := iam.Principal{Type: "user", ID: u.String()}
		fx.attach(owned, "user", u, nil) // platform scope
		if gs, _ := store.ListGrants(ctx, pr); len(gs) != 0 {
			t.Fatalf("platform attachment of a project-owned role must grant nothing: %v", gs)
		}
		fx.attach(owned, "user", u, &p2) // another project
		if gs, _ := store.ListGrants(ctx, pr); len(gs) != 0 {
			t.Fatalf("attachment in another project must grant nothing: %v", gs)
		}
		fx.attach(owned, "user", u, &p1)
		if gs, _ := store.ListGrants(ctx, pr); len(gs) != 1 || gs[0].ProjectID != p1.String() {
			t.Fatalf("own project must grant: %v", gs)
		}
	})

	t.Run("invalid uuid is an error not a grant", func(t *testing.T) {
		gs, err := store.ListGrants(ctx, iam.Principal{Type: "user", ID: "not-a-uuid"})
		if err == nil || len(gs) != 0 {
			t.Fatalf("%v %v", gs, err)
		}
	})
}

func TestIAMStorePolicyCache(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	store := NewIAMStore(db)
	ctx := context.Background()
	u := fx.user(false)
	pr := iam.Principal{Type: "user", ID: u.String()}
	r := fx.role(allowAllPolicy)
	fx.attach(r, "user", u, nil)

	gs, _ := store.ListGrants(ctx, pr)
	if !iam.Evaluate(gs, iam.Request{Action: "users:read", Resource: "*"}).Allowed {
		t.Fatal("initial allow expected")
	}
	pgExec(t, db, `UPDATE roles SET policy = '{"version":"1","statements":[]}'::jsonb WHERE id = $1`, r)
	gs, _ = store.ListGrants(ctx, pr)
	if !iam.Evaluate(gs, iam.Request{Action: "users:read", Resource: "*"}).Allowed {
		t.Fatal("without Invalidate the cached policy must still be served")
	}
	store.Invalidate(r.String())
	gs, _ = store.ListGrants(ctx, pr)
	if iam.Evaluate(gs, iam.Request{Action: "users:read", Resource: "*"}).Allowed {
		t.Fatal("after Invalidate the new policy must be read")
	}
	// Invalidate through the Authorizer
	a := iam.NewAuthorizer(store, iam.NewRegistry(), iam.NewAttributeSchema())
	pgExec(t, db, `UPDATE roles SET policy = $2::jsonb WHERE id = $1`, r, allowAllPolicy)
	a.Invalidate(r.String())
	if res, err := a.Authorize(ctx, pr, "users:read", "*"); err != nil || !res.Allowed {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestIAMStoreUnparsablePolicy(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	store := NewIAMStore(db)
	ctx := context.Background()
	u := fx.user(false)
	pr := iam.Principal{Type: "user", ID: u.String()}
	for _, bad := range []string{`{"version":"1","statements":[{"effect":"Maybe","actions":["*"],"resources":["*"]}]}`, `"just a string"`, `null`, `{"nope":1}`, `[]`} {
		r := fx.role(bad)
		fx.attach(r, "user", u, nil)
	}
	gs, err := store.ListGrants(ctx, pr)
	if err != nil || len(gs) != 5 {
		t.Fatalf("%v %v", gs, err)
	}
	for _, g := range gs {
		if g.Policy != nil {
			t.Fatalf("unparsable policy must be nil, got %+v", g)
		}
	}
	a := iam.NewAuthorizer(store, iam.NewRegistry(), iam.NewAttributeSchema())
	res, err := a.Authorize(ctx, pr, "users:read", "user/x")
	if err != nil || res.Allowed {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestIAMStoreConcurrentUse(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	store := NewIAMStore(db)
	u := fx.user(false)
	r := fx.role(allowAllPolicy)
	fx.attach(r, "user", u, nil)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%4 == 0 {
				store.Invalidate(r.String())
			}
			if _, err := store.ListGrants(context.Background(), iam.Principal{Type: "user", ID: u.String()}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
}
