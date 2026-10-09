package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	annotationdom "github.com/Paca-AI/api/internal/domain/annotation"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// scopeRecordingAnnotationSvc records the scope the handler attached to the
// context it calls the service with.
type scopeRecordingAnnotationSvc struct {
	annotationdom.Service
	calls []string
	scope *iam.Node
	hasIt bool
}

func (s *scopeRecordingAnnotationSvc) record(ctx context.Context, call string) {
	s.calls = append(s.calls, call)
	s.scope, s.hasIt = iam.ScopeFrom(ctx, "annotation")
}

func (s *scopeRecordingAnnotationSvc) ListForPage(ctx context.Context, _, _, _ uuid.UUID, _ string) ([]*annotationdom.PageAnnotation, error) {
	s.record(ctx, "page")
	return nil, nil
}

func (s *scopeRecordingAnnotationSvc) ListForPortForward(ctx context.Context, _, _, _ uuid.UUID) ([]*annotationdom.PageAnnotation, error) {
	s.record(ctx, "port-forward")
	return nil, nil
}

func (s *scopeRecordingAnnotationSvc) SearchInProject(ctx context.Context, _ uuid.UUID, _ annotationdom.SearchFilter) ([]*annotationdom.PageAnnotation, bool, error) {
	s.record(ctx, "search")
	return nil, false, nil
}

func TestAnnotationLists_AttachScopeForTheRepository(t *testing.T) {
	userID, projectID, envID, pfID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	onlyOne := &iam.Node{Kind: iam.NodeCond, Key: "resource.id", Op: "In", Values: iam.ValueList{"a1"}}
	base := "/projects/" + projectID.String()
	paths := map[string]string{
		"page":         base + "/environments/" + envID.String() + "/port-forwards/" + pfID.String() + "/annotations/?page_path=/x",
		"port-forward": base + "/environments/" + envID.String() + "/port-forwards/" + pfID.String() + "/annotations/",
		"search":       base + "/annotations/",
	}
	for call, path := range paths {
		for _, c := range []struct {
			name       string
			scoper     *fakeScoper
			wantStatus int
			wantCalled bool
			wantNode   *iam.Node
		}{
			{"scope is handed to the service", &fakeScoper{node: onlyOne}, http.StatusOK, true, onlyOne},
			{"deny-all scope is handed over too", &fakeScoper{node: iam.False()}, http.StatusOK, true, iam.False()},
			{"authorizer error fails closed before querying", &fakeScoper{err: errors.New("db down")}, http.StatusInternalServerError, false, nil},
		} {
			t.Run(call+"/"+c.name, func(t *testing.T) {
				svc := &scopeRecordingAnnotationSvc{}
				h := handler.NewAnnotationHandler(svc).WithAnnotationListScoper(c.scoper)
				r := chi.NewRouter()
				r.Use(claimsMiddleware(userID.String()))
				r.Get("/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/", h.List)
				r.Get("/projects/{projectId}/annotations/", h.SearchInProject)

				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
				if rec.Code != c.wantStatus {
					t.Fatalf("status = %d, want %d: %s", rec.Code, c.wantStatus, rec.Body)
				}
				if called := len(svc.calls) > 0; called != c.wantCalled {
					t.Fatalf("service called = %v, want %v", called, c.wantCalled)
				}
				if c.scoper.gotAction != "annotations:read" || c.scoper.gotKind != "annotation" ||
					c.scoper.gotProject != projectID.String() || c.scoper.gotPrincipal != iam.User(userID.String()) {
					t.Fatalf("scoper asked %+v", c.scoper)
				}
				if c.wantCalled {
					if svc.calls[0] != call {
						t.Fatalf("service call = %v, want %s", svc.calls, call)
					}
					if !svc.hasIt || svc.scope != c.wantNode {
						t.Fatalf("service saw scope %+v (attached=%v)", svc.scope, svc.hasIt)
					}
				}
			})
		}
	}
}

func TestAnnotationLists_WithoutScoperLeaveContextUnscoped(t *testing.T) {
	svc := &scopeRecordingAnnotationSvc{}
	h := handler.NewAnnotationHandler(svc)
	r := chi.NewRouter()
	r.Get("/projects/{projectId}/annotations/", h.SearchInProject)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/annotations/", nil))
	if rec.Code != http.StatusOK || svc.hasIt {
		t.Fatalf("status %d, scope attached = %v", rec.Code, svc.hasIt)
	}
}

// scopeRecordingViewSvc records the task scope ListTaskPositions is called with.
type scopeRecordingViewSvc struct {
	sprintdom.ViewService
	called bool
	scope  *iam.Node
	hasIt  bool
}

func (s *scopeRecordingViewSvc) ListTaskPositions(ctx context.Context, _, _ uuid.UUID) ([]*sprintdom.ViewTaskPosition, error) {
	s.called = true
	s.scope, s.hasIt = iam.ScopeFrom(ctx, "task")
	return nil, nil
}

func TestListTaskPositions_AttachesTaskScopeForTheRepository(t *testing.T) {
	userID, projectID, viewID := uuid.New(), uuid.New(), uuid.New()
	onlyS5 := &iam.Node{Kind: iam.NodeCond, Key: "task.sprint_id", Op: "In", Values: iam.ValueList{"s5"}}
	for _, c := range []struct {
		name       string
		scoper     *fakeScoper
		wantStatus int
		wantCalled bool
		wantNode   *iam.Node
	}{
		{"scope is handed to the service", &fakeScoper{node: onlyS5}, http.StatusOK, true, onlyS5},
		{"deny-all scope is handed over too", &fakeScoper{node: iam.False()}, http.StatusOK, true, iam.False()},
		{"authorizer error fails closed before querying", &fakeScoper{err: errors.New("db down")}, http.StatusInternalServerError, false, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc := &scopeRecordingViewSvc{}
			h := handler.NewViewHandler(svc).WithViewListScoper(c.scoper)
			r := chi.NewRouter()
			r.Use(claimsMiddleware(userID.String()))
			r.Get("/projects/{projectId}/views/{viewId}/task-positions", h.ListTaskPositions)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+projectID.String()+"/views/"+viewID.String()+"/task-positions", nil))
			if rec.Code != c.wantStatus || svc.called != c.wantCalled {
				t.Fatalf("status = %d (want %d), service called = %v (want %v): %s", rec.Code, c.wantStatus, svc.called, c.wantCalled, rec.Body)
			}
			// The positions name tasks, so the TASK read scope applies, not the view's.
			if c.scoper.gotAction != "tasks:read" || c.scoper.gotKind != "task" || c.scoper.gotProject != projectID.String() {
				t.Fatalf("scoper asked %+v", c.scoper)
			}
			if c.wantCalled && (!svc.hasIt || svc.scope != c.wantNode) {
				t.Fatalf("service saw scope %+v (attached=%v)", svc.scope, svc.hasIt)
			}
		})
	}
}

// fakeProjectsScoper answers ListScopes from a fixed map.
type fakeProjectsScoper struct {
	byProject map[string]*iam.Node
	err       error

	gotAction, gotKind string
	gotProjects        []string
	gotPrincipal       iam.Principal
}

func (f *fakeProjectsScoper) ListScopes(_ context.Context, p iam.Principal, action string, projectIDs []string, kind string) (map[string]*iam.Node, error) {
	f.gotPrincipal, f.gotAction, f.gotProjects, f.gotKind = p, action, projectIDs, kind
	return f.byProject, f.err
}

func TestGetWorkspaceStats_OpenTaskCountRunsUnderThePerProjectTaskScopes(t *testing.T) {
	proj1, proj2 := uuid.New(), uuid.New()
	onlyS5 := &iam.Node{Kind: iam.NodeCond, Key: "task.sprint_id", Op: "In", Values: iam.ValueList{"s5"}}
	run := func(scoper *fakeProjectsScoper) (code int, counted bool, seen map[string]*iam.Node, ok bool) {
		projectSvc := &mockProjectSvc{
			list: func(_ context.Context, _, _ int) ([]*projectdom.Project, int64, error) {
				return []*projectdom.Project{{ID: proj1}, {ID: proj2}}, 2, nil
			},
			countDistinctAgents: func(context.Context, []uuid.UUID) (int64, error) { return 0, nil },
		}
		taskSvc := &mockTaskStatsSvc{countOpenTasksByProjects: func(ctx context.Context, _ []uuid.UUID) (int64, error) {
			counted = true
			seen, ok = iam.ProjectScopesFrom(ctx, "task")
			return 3, nil
		}}
		opts := []handler.ProjectHandlerOption{handler.WithProjectStatsServices(taskSvc, &mockUserStatsSvc{countUsers: func(context.Context) (int64, error) { return 1, nil }})}
		if scoper != nil {
			opts = append(opts, handler.WithProjectTaskScoper(scoper))
		}
		r := chi.NewRouter()
		r.Use(adminClaimsMiddleware())
		r.Get("/projects/workspace-stats", handler.NewProjectHandler(projectSvc, adminAuthorizer(), opts...).GetWorkspaceStats)
		return do(t, r, http.MethodGet, "/projects/workspace-stats", nil).Code, counted, seen, ok
	}

	t.Run("each project's scope reaches the count", func(t *testing.T) {
		scoper := &fakeProjectsScoper{byProject: map[string]*iam.Node{proj1.String(): onlyS5, proj2.String(): iam.False()}}
		code, counted, seen, ok := run(scoper)
		if code != http.StatusOK || !counted || !ok {
			t.Fatalf("status %d, counted %v, scopes attached %v", code, counted, ok)
		}
		if seen[proj1.String()] != onlyS5 || !seen[proj2.String()].DeniesAll() {
			t.Fatalf("count saw scopes %+v", seen)
		}
		if scoper.gotAction != "tasks:read" || scoper.gotKind != "task" || len(scoper.gotProjects) != 2 || scoper.gotPrincipal.Type != "user" {
			t.Fatalf("scoper asked %+v", scoper)
		}
	})
	t.Run("a scoping failure fails closed without counting", func(t *testing.T) {
		code, counted, _, _ := run(&fakeProjectsScoper{err: errors.New("db down")})
		if code != http.StatusInternalServerError || counted {
			t.Fatalf("status %d, counted %v: want 500 and no count", code, counted)
		}
	})
	t.Run("without a scoper the count is unscoped", func(t *testing.T) {
		code, counted, _, ok := run(nil)
		if code != http.StatusOK || !counted || ok {
			t.Fatalf("status %d, counted %v, scopes attached %v", code, counted, ok)
		}
	})
}
