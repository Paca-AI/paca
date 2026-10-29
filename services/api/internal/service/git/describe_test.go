package gitsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

func describingSetup(t *testing.T, capable bool) (*Service, *fakeStore, *fakeProv) {
	t.Helper()
	svc, st, pr := setup(t)
	pr.info.ID = "p"
	pr.info.Capabilities.IdentityDescription = capable
	return svc, st, pr
}

func TestDescribeNameless_FillsHandleAndPersists(t *testing.T) {
	svc, st, pr := describingSetup(t, true)
	exp := time.Now().Add(24 * time.Hour).UTC()
	id := st.addIdentity("", "tok-1")
	pr.describe = func(r gitprovider.DescribeIdentityRequest) (gitprovider.Identity, error) {
		return gitprovider.Identity{Ref: gitprovider.IdentityRef{ID: r.Identity.ID, Handle: "paca-bot"}, DisplayName: "Paca Bot", ExpiresAt: exp}, nil
	}
	for name, list := range map[string]func() ([]*gitdom.Identity, error){
		"identities":  func() ([]*gitdom.Identity, error) { return svc.ListIdentities(context.Background()) },
		"connections": func() ([]*gitdom.Identity, error) { return svc.ListConnections(context.Background(), uuid.New()) },
	} {
		id.Handle, id.DisplayName, id.ExpiresAt = "", "", nil
		pr.describeLog = nil
		got, err := list()
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: %v %v", name, got, err)
		}
		if got[0].Handle != "paca-bot" || got[0].DisplayName != "Paca Bot" || got[0].ExpiresAt == nil || !got[0].ExpiresAt.Equal(exp) {
			t.Errorf("%s: response not filled: %+v", name, got[0])
		}
		if len(pr.describeLog) != 1 || pr.describeLog[0].Identity.ID != "tok-1" {
			t.Errorf("%s: describe calls = %+v", name, pr.describeLog)
		}
	}
	if st.profileUpdates != 2 {
		t.Errorf("profile saved %d times, want 2", st.profileUpdates)
	}
	// Once named, a later listing does not ask again.
	pr.describeLog = nil
	if _, err := svc.ListIdentities(context.Background()); err != nil || len(pr.describeLog) != 0 {
		t.Errorf("named identity described again: %v %d", err, len(pr.describeLog))
	}
}

func TestDescribeNameless_ProviderErrorKeepsRequestOK(t *testing.T) {
	svc, st, pr := describingSetup(t, true)
	id := st.addIdentity("", "tok-1")
	pr.describe = func(gitprovider.DescribeIdentityRequest) (gitprovider.Identity, error) {
		return gitprovider.Identity{}, errors.New("platform down")
	}
	got, err := svc.ListIdentities(context.Background())
	if err != nil || len(got) != 1 || id.Handle != "" || st.profileUpdates != 0 {
		t.Fatalf("got=%v err=%v handle=%q saves=%d", got, err, id.Handle, st.profileUpdates)
	}
}

func TestDescribeNameless_SkipsWhenNotNeeded(t *testing.T) {
	t.Run("handle present", func(t *testing.T) {
		svc, st, pr := describingSetup(t, true)
		st.addIdentity("already", "tok-1")
		if _, err := svc.ListIdentities(context.Background()); err != nil || len(pr.describeLog) != 0 {
			t.Fatalf("err=%v calls=%d", err, len(pr.describeLog))
		}
	})
	t.Run("provider without the capability", func(t *testing.T) {
		svc, st, pr := describingSetup(t, false)
		st.addIdentity("", "tok-1")
		if _, err := svc.ListConnections(context.Background(), uuid.New()); err != nil || len(pr.describeLog) != 0 {
			t.Fatalf("err=%v calls=%d", err, len(pr.describeLog))
		}
	})
}

func TestDescribeNameless_IgnoresUnusableAnswers(t *testing.T) {
	for name, answer := range map[string]gitprovider.Identity{
		"other identity": {Ref: gitprovider.IdentityRef{ID: "someone-else", Handle: "intruder"}, DisplayName: "X"},
		"empty handle":   {Ref: gitprovider.IdentityRef{ID: "tok-1"}, DisplayName: "X"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, st, pr := describingSetup(t, true)
			id := st.addIdentity("", "tok-1")
			pr.describe = func(gitprovider.DescribeIdentityRequest) (gitprovider.Identity, error) { return answer, nil }
			if _, err := svc.ListIdentities(context.Background()); err != nil {
				t.Fatal(err)
			}
			if id.Handle != "" || id.DisplayName != "" || st.profileUpdates != 0 {
				t.Errorf("stored an unusable answer: %+v saves=%d", id, st.profileUpdates)
			}
		})
	}
}

func TestDescribeNameless_BacksOffAfterFailure(t *testing.T) {
	svc, st, pr := describingSetup(t, true)
	now := time.Now()
	svc.now = func() time.Time { return now }
	id := st.addIdentity("", "tok-1")
	fail := true
	pr.describe = func(r gitprovider.DescribeIdentityRequest) (gitprovider.Identity, error) {
		if fail {
			return gitprovider.Identity{}, errors.New("revoked")
		}
		return gitprovider.Identity{Ref: gitprovider.IdentityRef{ID: r.Identity.ID, Handle: "bot"}}, nil
	}
	list := func() { _, _ = svc.ListIdentities(context.Background()) }

	list()
	list()
	if len(pr.describeLog) != 1 {
		t.Fatalf("calls within the backoff = %d, want 1", len(pr.describeLog))
	}
	now = now.Add(describeBackoff + time.Second)
	list()
	if len(pr.describeLog) != 2 {
		t.Fatalf("calls after the backoff = %d, want 2", len(pr.describeLog))
	}
	fail = false
	now = now.Add(describeBackoff + time.Second)
	list()
	if id.Handle != "bot" || len(svc.describeFailed) != 0 {
		t.Errorf("success: handle=%q failures=%v", id.Handle, svc.describeFailed)
	}
}
