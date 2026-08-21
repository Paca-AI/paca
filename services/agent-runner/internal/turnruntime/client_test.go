package turnruntime

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestClientRetriesSameClaimAfterCommittedResponseIsLost(t *testing.T) {
	turnID, runID, claimToken := uuid.New(), uuid.New(), uuid.New()
	calls := 0
	var bodies []string
	client := &Client{baseURL: "http://runtime.invalid", token: "internal", http: &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			body, _ := io.ReadAll(request.Body)
			bodies = append(bodies, string(body))
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{"))}, nil
			}
			payload := `{"success":true,"data":{"turn_id":"` + turnID.String() +
				`","run_id":"` + runID.String() + `","claim_token":"` + claimToken.String() +
				`","conversation_id":"` + uuid.NewString() + `","project_id":"` + uuid.NewString() +
				`","agent_id":"` + uuid.NewString() + `","attempt":1,"status":"running"}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}, nil
		}),
	}}

	claim, err := client.Claim(context.Background(), turnID, "worker-one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(bodies) != 2 || bodies[0] != bodies[1] {
		t.Fatalf("claim retry calls=%d bodies=%q", calls, bodies)
	}
	if claim.RunID != runID || claim.ClaimToken == nil || *claim.ClaimToken != claimToken {
		t.Fatalf("claim = %+v", claim)
	}
}
