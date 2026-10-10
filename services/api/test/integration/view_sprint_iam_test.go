package integration_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/platform/database"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	rolesvc "github.com/Paca-AI/api/internal/service/role"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/Paca-AI/api/migrations"
)

// newViewAPIEnv is the roles API stack plus the real view endpoints (real
// repositories, service and list scoping) behind the real router and gates.
func newViewAPIEnv(t *testing.T) *roleAPIEnv {
	t.Helper()
	sqlDB := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(sqlDB, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	db := sqlx.NewDb(sqlDB, "pgx")
	authz := pgRepo.NewIAMAuthorizer(db)
	roleRepo := pgRepo.NewRoleRepository(db)
	roleSvc := rolesvc.New(roleRepo, authz, authz, authz.Registry(), authz.Schema())
	viewSvc := sprintsvc.NewViewService(pgRepo.NewViewRepository(db), pgRepo.NewSprintRepository(db), nil, nil)
	tokens := jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour)
	srv := router.New(router.Deps{
		TokenManager:         tokens,
		IAM:                  authz,
		ProjectVisibilitySvc: publicProjects{},
		Role:                 handler.NewRoleHandler(roleSvc),
		RoleAttachments:      httpmw.NewRoleServiceAttachments(roleSvc),
		View:                 handler.NewViewHandler(viewSvc).WithViewListScoper(authz),
		Health:               handler.NewHealthHandler(),
		Log:                  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return &roleAPIEnv{t: t, db: db, authz: authz, server: srv, tokens: tokens}
}

// The worked example from the docs: a member limited to the views of one
// sprint through view.sprint_id.
func TestViewsOfOneSprint_EndToEnd(t *testing.T) {
	e := newViewAPIEnv(t)
	p, u := e.project(), e.user()
	e.member(p, u)
	s5, s6 := uuid.New(), uuid.New()
	for _, s := range []uuid.UUID{s5, s6} {
		e.exec(`INSERT INTO sprints (id, project_id, name) VALUES ($1, $2, $3)`, s, p, "S-"+s.String()[:6])
	}
	a1, a2, b1, bl := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for i, v := range []struct{ id, sprint uuid.UUID }{{a1, s5}, {a2, s5}, {b1, s6}} {
		e.exec(`INSERT INTO sprint_views (id, sprint_id, project_id, name, view_context, position) VALUES ($1,$2,$3,'v','sprint',$4)`, v.id, v.sprint, p, i)
	}
	e.exec(`INSERT INTO sprint_views (id, project_id, name, view_context) VALUES ($1,$2,'b','backlog')`, bl, p)

	policy := fmt.Sprintf(`{"version":"2026-10-01","statements":[
		{"sid":"Project","effect":"Allow","actions":["views:read","views:write"],"resources":["project/%[1]s"]},
		{"sid":"ViewsOfThisSprint","effect":"Allow","actions":["views:read","views:write"],"resources":["project/%[1]s/view/*"],
		 "conditions":{"StringEquals":{"view.sprint_id":"%[2]s"}}}]}`, p, s5)
	e.attach(e.role("views-s5", policy, &p), u, &p)

	base := "/projects/" + p.String() + "/views"
	listIDs := func(q string) []string {
		t.Helper()
		res := e.call(u, "GET", base+"?"+q, nil)
		if res.code != http.StatusOK {
			t.Fatalf("list %s: %d %s", q, res.code, res.body)
		}
		var d struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		res.data(t, &d)
		out := []string{}
		for _, it := range d.Items {
			out = append(out, it.ID)
		}
		return out
	}
	if got := listIDs("context=sprint&sprint_id=" + s5.String()); len(got) != 2 {
		t.Fatalf("allowed sprint lists %v, want 2 views", got)
	}
	if got := listIDs("context=sprint&sprint_id=" + s6.String()); len(got) != 0 {
		t.Fatalf("other sprint lists %v, want none", got)
	}
	if got := listIDs("context=backlog"); len(got) != 0 {
		t.Fatalf("backlog lists %v, want none", got)
	}

	for name, c := range map[string]struct {
		method, path string
		body         any
		want         int
	}{
		"read a view of the sprint":      {"GET", base + "/" + a1.String(), nil, 200},
		"read a view of another sprint":  {"GET", base + "/" + b1.String(), nil, 403},
		"read a backlog view":            {"GET", base + "/" + bl.String(), nil, 403},
		"rename a view of the sprint":    {"PATCH", base + "/" + a1.String(), map[string]any{"name": "x"}, 200},
		"rename a view of another":       {"PATCH", base + "/" + b1.String(), map[string]any{"name": "x"}, 403},
		"create in the sprint":           {"POST", base + "?context=sprint&sprint_id=" + s5.String(), map[string]any{"name": "n"}, 201},
		"create in another sprint":       {"POST", base + "?context=sprint&sprint_id=" + s6.String(), map[string]any{"name": "n"}, 403},
		"create a backlog view":          {"POST", base + "?context=backlog", map[string]any{"name": "n"}, 403},
		"reorder naming another sprint":  {"PUT", base + "/positions?context=sprint&sprint_id=" + s6.String(), map[string]any{"view_ids": []string{b1.String()}}, 403},
		"reorder mixing sprints":         {"PUT", base + "/positions?context=sprint&sprint_id=" + s5.String(), map[string]any{"view_ids": []string{a1.String(), b1.String()}}, 403},
		"delete a view of another":       {"DELETE", base + "/" + b1.String(), nil, 403},
		"delete a view of the sprint":    {"DELETE", base + "/" + a2.String(), nil, 204},
		"read the other sprint's detail": {"GET", base + "/" + b1.String(), nil, 403},
	} {
		if res := e.call(u, c.method, c.path, c.body); res.code != c.want {
			t.Errorf("%s: got %d want %d: %s", name, res.code, c.want, res.body)
		}
	}
}
