package sso

import (
	"context"
	"errors"
	"testing"

	ssodom "github.com/Paca-AI/api/internal/domain/sso"
)

func TestGroupDenialReason(t *testing.T) {
	allowed := []string{"g-admins", "g-eng"}
	cases := []struct {
		name   string
		claims map[string]any
		path   string
		deny   bool
	}{
		{"array with allowed value", map[string]any{"groups": []any{"x", "g-eng"}}, "groups", false},
		{"single string value", map[string]any{"groups": "g-admins"}, "groups", false},
		{"nested path", map[string]any{"realm_access": map[string]any{"roles": []any{"g-eng"}}}, "realm_access.roles", false},
		{"claim name containing dots", map[string]any{"https://idp.example/groups": []any{"g-eng"}}, "https://idp.example/groups", false},
		{"no allowed value", map[string]any{"groups": []any{"x", "y"}}, "groups", true},
		{"matching is exact", map[string]any{"groups": []any{"G-ENG", "g-eng "}}, "groups", true},
		{"claim absent", map[string]any{"sub": "s"}, "groups", true},
		{"empty array", map[string]any{"groups": []any{}}, "groups", true},
		{"null claim", map[string]any{"groups": nil}, "groups", true},
		{"wrong type: number", map[string]any{"groups": 7.0}, "groups", true},
		{"wrong type: object", map[string]any{"groups": map[string]any{"g-eng": true}}, "groups", true},
		{"malformed: non-string element", map[string]any{"groups": []any{"g-eng", 3.0}}, "groups", true},
		{"nested path through a non-object", map[string]any{"realm_access": "g-eng"}, "realm_access.roles", true},
		{"nested path missing leaf", map[string]any{"realm_access": map[string]any{}}, "realm_access.roles", true},
		{"entra group overage", map[string]any{
			"_claim_names":   map[string]any{"groups": "src1"},
			"_claim_sources": map[string]any{"src1": map[string]any{"endpoint": "https://graph.example/users/x/getMemberObjects"}},
		}, "groups", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := groupDenialReason(tc.claims, tc.path, allowed)
			if (reason != "") != tc.deny {
				t.Fatalf("reason = %q, deny = %v", reason, tc.deny)
			}
		})
	}
}

func TestValidClaimPath(t *testing.T) {
	for path, want := range map[string]bool{
		"groups":                     true,
		"realm_access.roles":         true,
		"https://idp.example/groups": true,
		"":                           false,
		"a b":                        false,
		".roles":                     false,
		"realm_access.":              false,
		"a..b":                       false,
	} {
		if got := validClaimPath(path); got != want {
			t.Errorf("validClaimPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestCreateProvider_GroupPolicyValidation(t *testing.T) {
	svc, _, _ := newTestService(t, newFakeUsers())
	base := ssodom.ProviderInput{
		Slug: "corp", DisplayName: "Corp", IssuerURL: "https://idp.example", ClientID: "c",
		GroupPolicyEnabled: true, GroupClaim: "groups", AllowedGroups: []string{"g1"},
	}
	cases := map[string]func(*ssodom.ProviderInput){
		"no claim":   func(in *ssodom.ProviderInput) { in.GroupClaim = " " },
		"bad claim":  func(in *ssodom.ProviderInput) { in.GroupClaim = "realm_access..roles" },
		"no groups":  func(in *ssodom.ProviderInput) { in.AllowedGroups = []string{" ", ""} },
		"nil groups": func(in *ssodom.ProviderInput) { in.AllowedGroups = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := base
			mutate(&in)
			if _, err := svc.CreateProvider(context.Background(), in); !errors.Is(err, ssodom.ErrInvalidProvider) {
				t.Fatalf("err = %v, want ErrInvalidProvider", err)
			}
		})
	}

	in := base
	in.GroupClaim = " realm_access.roles "
	in.AllowedGroups = []string{" g1 ", "g1", "g2"}
	p, err := svc.CreateProvider(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !p.GroupPolicyEnabled || p.GroupClaim != "realm_access.roles" || len(p.AllowedGroups) != 2 {
		t.Fatalf("normalized provider = %+v", p)
	}

	// A disabled policy is not validated, so a half-filled form can be saved.
	off := base
	off.GroupPolicyEnabled, off.GroupClaim, off.AllowedGroups = false, "", nil
	off.Slug = "corp-off"
	if _, err := svc.CreateProvider(context.Background(), off); err != nil {
		t.Fatalf("disabled policy rejected: %v", err)
	}
}

func TestCompleteLogin_GroupPolicy(t *testing.T) {
	identity := func(extra map[string]any) map[string]any {
		c := map[string]any{"sub": "abc", "email": "jane@corp.example", "email_verified": true, "preferred_username": "jane"}
		for k, v := range extra {
			c[k] = v
		}
		return c
	}
	enable := func(in *ssodom.ProviderInput) {
		in.GroupPolicyEnabled, in.GroupClaim, in.AllowedGroups = true, "groups", []string{"g-eng"}
	}
	login := func(t *testing.T, svc *Service, idp *fakeIDP) error {
		t.Helper()
		ctx := context.Background()
		authURL, binding, err := svc.BeginLogin(ctx, "corp", "https://paca.example", false, "")
		if err != nil {
			t.Fatal(err)
		}
		state := idp.authorize(t, authURL)
		_, _, err = svc.CompleteLogin(ctx, "corp", "https://paca.example", "good-code", state, binding, "203.0.113.9")
		return err
	}

	t.Run("allowed group signs in", func(t *testing.T) {
		idp := newFakeIDP(t, identity(map[string]any{"groups": []string{"other", "g-eng"}}))
		svc, _, sessions := newTestService(t, newFakeUsers())
		createCorp(t, svc, idp.srv.URL, enable)
		if err := login(t, svc, idp); err != nil {
			t.Fatalf("err = %v", err)
		}
		if sessions.issuedFor == nil {
			t.Fatal("no session issued")
		}
	})

	denied := map[string]map[string]any{
		"no matching group": {"groups": []string{"other"}},
		"claim absent":      {},
		"empty list":        {"groups": []string{}},
		"malformed claim":   {"groups": map[string]any{"g-eng": true}},
		"entra overage": {
			"_claim_names":   map[string]any{"groups": "src1"},
			"_claim_sources": map[string]any{"src1": map[string]any{"endpoint": "https://graph.example"}},
		},
	}
	for name, extra := range denied {
		t.Run("denied: "+name, func(t *testing.T) {
			users := newFakeUsers()
			idp := newFakeIDP(t, identity(extra))
			svc, repo, sessions := newTestService(t, users)
			createCorp(t, svc, idp.srv.URL, enable)
			if err := login(t, svc, idp); !errors.Is(err, ssodom.ErrGroupNotAllowed) {
				t.Fatalf("err = %v, want ErrGroupNotAllowed", err)
			}
			if len(users.byID) != 0 || len(repo.identities) != 0 || sessions.issuedFor != nil {
				t.Fatalf("denied sign-in left state behind: %d users, %d identities", len(users.byID), len(repo.identities))
			}
		})
	}

	t.Run("returning user who lost the group is denied", func(t *testing.T) {
		idp := newFakeIDP(t, identity(map[string]any{"groups": []string{"g-eng"}}))
		svc, _, _ := newTestService(t, newFakeUsers())
		createCorp(t, svc, idp.srv.URL, enable)
		if err := login(t, svc, idp); err != nil {
			t.Fatal(err)
		}
		idp.claims = identity(map[string]any{"groups": []string{"other"}})
		if err := login(t, svc, idp); !errors.Is(err, ssodom.ErrGroupNotAllowed) {
			t.Fatalf("err = %v, want ErrGroupNotAllowed", err)
		}
	})

	t.Run("disabled policy ignores groups", func(t *testing.T) {
		idp := newFakeIDP(t, identity(nil))
		svc, _, _ := newTestService(t, newFakeUsers())
		createCorp(t, svc, idp.srv.URL, func(in *ssodom.ProviderInput) {
			in.GroupClaim, in.AllowedGroups = "groups", []string{"g-eng"}
		})
		if err := login(t, svc, idp); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}
