package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// scopeRecordingSprintSvc records the scope the handler attached to the
// context it calls the service with.
type scopeRecordingSprintSvc struct {
	*fakeSprintSvcH
	called bool
	scope  *iam.Node
	hasIt  bool
}

func (s *scopeRecordingSprintSvc) ListSprints(ctx context.Context, _ uuid.UUID) ([]*sprintdom.Sprint, error) {
	s.called = true
	s.scope, s.hasIt = iam.ScopeFrom(ctx, "sprint")
	return nil, nil
}

type fakeScoper struct {
	node *iam.Node
	err  error

	gotAction, gotProject, gotKind string
	gotPrincipal                   iam.Principal
}

func (f *fakeScoper) ListScope(_ context.Context, p iam.Principal, action, projectID, kind string) (*iam.Node, error) {
	f.gotPrincipal, f.gotAction, f.gotProject, f.gotKind = p, action, projectID, kind
	return f.node, f.err
}

func TestListSprints_AttachesScopeForTheRepository(t *testing.T) {
	userID, projectID := uuid.New(), uuid.New()
	onlyOne := &iam.Node{Kind: iam.NodeCond, Key: "resource.id", Op: "In", Values: iam.ValueList{"s1"}}

	cases := []struct {
		name       string
		scoper     *fakeScoper
		wantStatus int
		wantCalled bool
		wantNode   *iam.Node
	}{
		{"scope is handed to the service", &fakeScoper{node: onlyOne}, http.StatusOK, true, onlyOne},
		{"deny-all scope is handed over too", &fakeScoper{node: iam.False()}, http.StatusOK, true, iam.False()},
		{"authorizer error fails closed before querying", &fakeScoper{err: errors.New("db down")}, http.StatusInternalServerError, false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &scopeRecordingSprintSvc{fakeSprintSvcH: &fakeSprintSvcH{}}
			h := handler.NewSprintHandler(svc, nil, handler.WithSprintListScoper(c.scoper))
			r := chi.NewRouter()
			r.Use(claimsMiddleware(userID.String()))
			r.Get("/projects/{projectId}/sprints", h.ListSprints)

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/"+projectID.String()+"/sprints", nil))
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, c.wantStatus, rec.Body)
			}
			if svc.called != c.wantCalled {
				t.Fatalf("service called = %v, want %v", svc.called, c.wantCalled)
			}
			if c.scoper.gotAction != "sprints:read" || c.scoper.gotKind != "sprint" ||
				c.scoper.gotProject != projectID.String() || c.scoper.gotPrincipal != iam.User(userID.String()) {
				t.Fatalf("scoper asked %+v", c.scoper)
			}
			if c.wantCalled && (!svc.hasIt || svc.scope != c.wantNode) {
				t.Fatalf("service saw scope %+v (attached=%v)", svc.scope, svc.hasIt)
			}
		})
	}
}

func TestListSprints_WithoutScoperLeavesContextUnscoped(t *testing.T) {
	svc := &scopeRecordingSprintSvc{fakeSprintSvcH: &fakeSprintSvcH{}}
	h := handler.NewSprintHandler(svc, nil)
	r := chi.NewRouter()
	r.Get("/projects/{projectId}/sprints", h.ListSprints)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/"+uuid.NewString()+"/sprints", nil))
	if rec.Code != http.StatusOK || svc.hasIt {
		t.Fatalf("status %d, scope attached = %v", rec.Code, svc.hasIt)
	}
}
