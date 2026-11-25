package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/Paca-AI/api/internal/apierr"
	searchdom "github.com/Paca-AI/api/internal/domain/search"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// searchSource runs the workspace-wide search's queries (see
// postgres.SearchRepository). It is told which projects to look in; it does
// not decide what the caller may see.
type searchSource interface {
	SearchProjects(ctx context.Context, projectIDs []uuid.UUID, q string, limit int) ([]searchdom.ProjectHit, error)
	SearchTasks(ctx context.Context, projectIDs []uuid.UUID, q string, limit int) ([]searchdom.TaskHit, error)
	SearchPeople(ctx context.Context, memberProjectIDs []uuid.UUID, allUsers bool, q string, limit int) ([]searchdom.PersonHit, error)
}

// WithProjectSearch wires the source behind GET /search.
func WithProjectSearch(src searchSource) ProjectHandlerOption {
	return func(h *ProjectHandler) { h.search = src }
}

const (
	searchDefaultLimit = 5
	searchMaxLimit     = 20
	searchMaxQueryLen  = 200
)

// searchAccess is what the caller may search: the projects they may read,
// the projects whose member list they may read, and whether they may find
// every user.
type searchAccess struct {
	readable, memberLists []uuid.UUID
	allUsers              bool
}

// searchAccessFor answers, with one fetch of the caller's grants, which of
// projectIDs they hold projects:read and project.members:read on, and whether
// they hold the platform-wide users:write. An authorizer error denies.
//
// Finding people outside one's teams takes users:write (managing users), not
// users:read: the built-in default role gives every user users:read, which
// would make the team limit apply to nobody.
func (h *ProjectHandler) searchAccessFor(r *http.Request, projectIDs []uuid.UUID) (searchAccess, error) {
	var access searchAccess
	principal, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		return access, err
	}
	queries := make([]iam.Query, 0, 2*len(projectIDs)+1)
	for _, id := range projectIDs {
		res := "project/" + id.String()
		queries = append(queries,
			iam.Query{Action: string(iam.ActionProjectsRead), Resource: res},
			iam.Query{Action: string(iam.ActionProjectMembersRead), Resource: res})
	}
	queries = append(queries, iam.Query{
		Action:   string(iam.ActionUsersWrite),
		Resource: iam.PlatformRootFor(string(iam.ActionUsersWrite)),
	})
	results, err := h.authorizer.AuthorizeMany(r.Context(), principal, queries)
	if err != nil {
		return access, err
	}
	for i, id := range projectIDs {
		if results[2*i].Allowed {
			access.readable = append(access.readable, id)
		}
		if results[2*i+1].Allowed {
			access.memberLists = append(access.memberLists, id)
		}
	}
	access.allUsers = results[len(results)-1].Allowed
	return access, nil
}

// Search handles GET /search?q=...&limit=N, the workspace-wide search behind
// the app header. It returns up to limit (default 5, at most 20) projects,
// tasks and people matching q, each limited to what the caller may see:
//
//   - projects: those of the caller's projects (every project with the
//     platform-wide projects:read, otherwise the ones they are a member of)
//     they hold projects:read on;
//   - tasks: of those same projects, the ones the caller may read, per
//     project (the tasks:read scope, as for the workspace open-task count);
//   - people: members of the projects whose member list the caller may read
//     (project.members:read), i.e. their teams; every user for a user admin
//     (users:write).
//
// The route itself is open to any authenticated caller; a caller who may see
// nothing gets empty lists.
func (h *ProjectHandler) Search(w http.ResponseWriter, r *http.Request) {
	accountID, ok := requireAccountID(w, r)
	if !ok {
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > searchMaxQueryLen {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest,
			fmt.Sprintf("q must be at most %d characters", searchMaxQueryLen)))
		return
	}
	limit := searchDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > searchMaxLimit {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest,
				fmt.Sprintf("limit must be an integer between 1 and %d", searchMaxLimit)))
			return
		}
		limit = n
	}

	resp := dto.SearchResponse{
		Query:    q,
		Projects: []dto.SearchProject{},
		Tasks:    []dto.SearchTask{},
		People:   []dto.SearchPerson{},
	}
	// No authorizer means deny-all: nothing is searchable.
	if q == "" || h.search == nil || h.authorizer == nil {
		presenter.OK(w, r, resp)
		return
	}

	projectIDs, err := h.workspaceProjectIDs(r, accountID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	access, err := h.searchAccessFor(r, projectIDs)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var (
		projects []searchdom.ProjectHit
		tasks    []searchdom.TaskHit
		people   []searchdom.PersonHit
	)
	g, gctx := errgroup.WithContext(r.Context())
	g.Go(func() error {
		var err error
		projects, err = h.search.SearchProjects(gctx, access.readable, q, limit)
		return err
	})
	g.Go(func() error {
		var err error
		people, err = h.search.SearchPeople(gctx, access.memberLists, access.allUsers, q, limit)
		return err
	})
	taskCtx, err := h.workspaceTaskScope(gctx, r, projectIDs)
	if err != nil {
		_ = g.Wait()
		presenter.Error(w, r, err)
		return
	}
	g.Go(func() error {
		var err error
		tasks, err = h.search.SearchTasks(taskCtx, projectIDs, q, limit)
		return err
	})
	if err := g.Wait(); err != nil {
		presenter.Error(w, r, err)
		return
	}

	ctx := r.Context()
	for _, p := range projects {
		resp.Projects = append(resp.Projects, dto.SearchProject{
			ID:             p.ID.String(),
			Name:           p.Name,
			Description:    p.Description,
			TaskIDPrefix:   p.TaskIDPrefix,
			AvatarThumbURL: h.resolveAvatar(ctx, p.AvatarThumbKey),
		})
	}
	for _, t := range tasks {
		key := "#" + strconv.Itoa(t.TaskNumber)
		if t.TaskIDPrefix != "" {
			key = strings.ToUpper(t.TaskIDPrefix) + "-" + strconv.Itoa(t.TaskNumber)
		}
		resp.Tasks = append(resp.Tasks, dto.SearchTask{
			ID:             t.ID.String(),
			ProjectID:      t.ProjectID.String(),
			ProjectName:    t.ProjectName,
			TaskKey:        key,
			TaskNumber:     t.TaskNumber,
			Title:          t.Title,
			StatusName:     t.StatusName,
			StatusCategory: t.StatusCategory,
			TypeName:       t.TypeName,
			TypeIcon:       t.TypeIcon,
			TypeColor:      t.TypeColor,
		})
	}
	for _, p := range people {
		person := dto.SearchPerson{
			ID:             p.ID.String(),
			Name:           p.Name,
			Username:       p.Username,
			AvatarThumbURL: h.resolveAvatar(ctx, p.AvatarThumbKey),
		}
		if p.ProjectID != nil {
			id := p.ProjectID.String()
			person.ProjectID = &id
		}
		resp.People = append(resp.People, person)
	}
	presenter.OK(w, r, resp)
}

// resolveAvatar turns an avatar object key into a display URL, or nil when
// there is no avatar, no avatar service, or the URL cannot be made.
func (h *ProjectHandler) resolveAvatar(ctx context.Context, key *string) *string {
	if h.avatarSvc == nil || key == nil {
		return nil
	}
	url, err := h.avatarSvc.ResolveAvatarURL(ctx, key)
	if err != nil {
		return nil
	}
	return url
}
