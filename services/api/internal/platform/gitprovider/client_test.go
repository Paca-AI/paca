package gitprovider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeDoer struct {
	status int
	resp   string
	err    error
	gotURL string
	gotReq []byte
}

func (f *fakeDoer) Do(_ context.Context, plugin, method, path string, body []byte) (int, []byte, error) {
	f.gotURL = plugin + " " + method + " " + path
	f.gotReq = body
	return f.status, []byte(f.resp), f.err
}

func TestClient_ListIssues(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `{"ok":true,"result":{"items":[{"number":"7","title":"A","state":"open","author":"x","url":"u"}],"next_cursor":"next"}}`}
	c := NewClient(d, "p")
	page, err := c.ListIssues(context.Background(), ListIssuesRequest{Target: Target{Identity: IdentityRef{ID: "i"}, Repo: RepoRef{FullPath: "a/b"}}, State: "closed", Cursor: "a+b"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Number != "7" || page.NextCursor != "next" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if d.gotURL != "p POST /_git/list_issues" {
		t.Fatalf("called %q", d.gotURL)
	}
	var sent ListIssuesRequest
	if json.Unmarshal(d.gotReq, &sent) != nil || sent.State != "closed" || sent.Cursor != "a+b" || sent.Target.Identity.ID != "i" || sent.Target.Repo.FullPath != "a/b" {
		t.Fatalf("bad request: %+v", sent)
	}
	d.status, d.resp = 422, `{"ok":false,"error":{"code":"unsupported","message":"unsupported"}}`
	if _, err := c.ListIssues(context.Background(), ListIssuesRequest{}); !IsCode(err, CodeUnsupported) {
		t.Fatalf("err=%v", err)
	}
}

func TestClient_CallsTheOperationRoute(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `{"ok":true,"result":{"username":"x","secret":"s"}}`}
	c := NewClient(d, "com.paca.github")
	cred, err := c.MintCredential(context.Background(), MintRequest{Access: AccessRead})
	if err != nil || cred.Secret != "s" {
		t.Fatalf("cred=%+v err=%v", cred, err)
	}
	if d.gotURL != "com.paca.github POST /_git/mint_credential" {
		t.Errorf("called %q", d.gotURL)
	}
	var sent MintRequest
	if json.Unmarshal(d.gotReq, &sent) != nil || sent.Access != AccessRead {
		t.Errorf("request body %s", d.gotReq)
	}
}

func TestClient_ProviderErrorsKeepTheirCode(t *testing.T) {
	d := &fakeDoer{status: 404, resp: `{"ok":false,"error":{"code":"not_found","message":"no such repo"}}`}
	_, err := NewClient(d, "p").ResolveRepository(context.Background(), ResolveRepoRequest{})
	if !IsCode(err, CodeNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestClient_RateLimitCarriesRetryHint(t *testing.T) {
	d := &fakeDoer{status: 429, resp: `{"ok":false,"error":{"code":"rate_limited","message":"slow","retry_after_seconds":30}}`}
	_, err := NewClient(d, "p").Describe(context.Background())
	var pe *Error
	if !errors.As(err, &pe) || pe.RetryAfterSeconds != 30 || !pe.Retryable() {
		t.Fatalf("got %+v", pe)
	}
}

func TestClient_TransportAndDecodingFailuresAreClassified(t *testing.T) {
	cases := map[string]struct {
		d    *fakeDoer
		want ErrorCode
	}{
		"plugin unavailable":        {&fakeDoer{err: errors.New("module trapped")}, CodeTransient},
		"not json":                  {&fakeDoer{status: 200, resp: `<html>`}, CodeInternal},
		"failure without reason":    {&fakeDoer{status: 500, resp: `{"ok":false}`}, CodeInternal},
		"success flag with 5xx":     {&fakeDoer{status: 502, resp: `{"ok":true,"result":{}}`}, CodeInternal},
		"result of the wrong shape": {&fakeDoer{status: 200, resp: `{"ok":true,"result":"nope"}`}, CodeInternal},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewClient(tc.d, "p").Describe(context.Background())
			if !IsCode(err, tc.want) {
				t.Fatalf("got %v, want code %s", err, tc.want)
			}
			if tc.d.err != nil {
				var pe *Error
				errors.As(err, &pe)
				if pe.Message == tc.d.err.Error() {
					t.Error("the underlying error text must not leak into the classified error")
				}
			}
		})
	}
}

func TestClient_WebhookEventsDecode(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `{"ok":true,"result":[{"id":"d1:comment","kind":"comment","action":"created","repo":{"host":"h","full_path":"a/b"},"actor":{"login":"u","kind":"user"},"addressed":[{"identity_id":"i","via":"mention"}]}]}`}
	evs, err := NewClient(d, "p").VerifyAndParseWebhook(context.Background(), WebhookRequest{Body: []byte("x")})
	if err != nil || len(evs) != 1 || evs[0].Addressed[0].Via != ViaMention {
		t.Fatalf("evs=%+v err=%v", evs, err)
	}
	// A provider that has nothing to report returns an empty list, not an error.
	d.resp = `{"ok":true,"result":[]}`
	evs, err = NewClient(d, "p").VerifyAndParseWebhook(context.Background(), WebhookRequest{})
	if err != nil || len(evs) != 0 {
		t.Fatalf("evs=%+v err=%v", evs, err)
	}
}

func TestClient_AccessGrantOperations(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `{"ok":true,"result":{"kind":"redirect","url":"https://github.com/apps/x/installations/new","state":"s1"}}`}
	c := NewClient(d, "com.paca.github")
	step, err := c.BeginAccessGrant(context.Background(), BeginAccessRequest{Identity: IdentityRef{ID: "app-1"}, Owner: "acme", ReturnURL: "https://p/r", CallbackURL: "https://p/cb"})
	if err != nil || step.State != "s1" || step.Kind != StepRedirect {
		t.Fatalf("begin: %+v %v", step, err)
	}
	if d.gotURL != "com.paca.github POST /_git/begin_access_grant" {
		t.Errorf("called %q", d.gotURL)
	}
	var sent BeginAccessRequest
	if json.Unmarshal(d.gotReq, &sent) != nil || sent.Owner != "acme" || sent.Identity.ID != "app-1" || sent.CallbackURL != "https://p/cb" {
		t.Errorf("request body %s", d.gotReq)
	}

	d.resp = `{"ok":true,"result":{"owner":"acme"}}`
	g, err := c.CompleteAccessGrant(context.Background(), CompleteAccessRequest{Identity: IdentityRef{ID: "app-1"}, State: "s1", Params: map[string]string{"installation_id": "9"}})
	if err != nil || g.Owner != "acme" || d.gotURL != "com.paca.github POST /_git/complete_access_grant" {
		t.Fatalf("complete: %+v %v %q", g, err, d.gotURL)
	}

	d.status, d.resp = 422, `{"ok":false,"error":{"code":"unsupported","message":"no access grants"}}`
	if _, err := c.BeginAccessGrant(context.Background(), BeginAccessRequest{}); !IsCode(err, CodeUnsupported) {
		t.Errorf("a provider without grants answers unsupported: %v", err)
	}
}

func TestClient_DescribeIdentity(t *testing.T) {
	d := &fakeDoer{status: 200, resp: `{"ok":true,"result":{"ref":{"id":"i1","handle":"bot"},"display_name":"Bot"}}`}
	c := NewClient(d, "com.paca.gitlab")
	id, err := c.DescribeIdentity(context.Background(), DescribeIdentityRequest{Identity: IdentityRef{ID: "i1"}})
	if err != nil || id.Ref.ID != "i1" || id.Ref.Handle != "bot" || id.DisplayName != "Bot" {
		t.Fatalf("describe: %+v %v", id, err)
	}
	if d.gotURL != "com.paca.gitlab POST /_git/describe_identity" {
		t.Errorf("called %q", d.gotURL)
	}
	var sent DescribeIdentityRequest
	if json.Unmarshal(d.gotReq, &sent) != nil || sent.Identity.ID != "i1" {
		t.Errorf("request body %s", d.gotReq)
	}
	d.status, d.resp = 404, `{"ok":false,"error":{"code":"not_found","message":"gone"}}`
	if _, err := c.DescribeIdentity(context.Background(), DescribeIdentityRequest{}); !IsCode(err, CodeNotFound) {
		t.Errorf("unknown identity is not_found: %v", err)
	}
}
