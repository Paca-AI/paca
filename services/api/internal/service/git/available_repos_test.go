package gitsvc

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

func TestListAvailableRepositories_PassesQueryCursorAndIdentity(t *testing.T) {
	svc, st, pr := setup(t)
	project := uuid.New()
	conn := st.addProjectConnection(project, "mine", "own-token")
	pr.list = func(r gitprovider.ListReposRequest) (gitprovider.RepositoryPage, error) {
		return gitprovider.RepositoryPage{
			Items:      []gitprovider.Repository{{Ref: gitprovider.RepoRef{FullPath: "acme/web"}, Private: true}},
			NextCursor: "c2",
		}, nil
	}
	got, err := svc.ListAvailableRepositories(context.Background(), project, conn.ID, "web", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Supported || len(got.Items) != 1 || got.NextCursor != "c2" {
		t.Fatalf("page = %+v", got)
	}
	if len(pr.listReqs) != 1 {
		t.Fatalf("provider called %d times", len(pr.listReqs))
	}
	req := pr.listReqs[0]
	if req.Query != "web" || req.Cursor != "c1" || req.Identity.ID != "own-token" || req.PageSize != DefaultAvailablePageSize {
		t.Fatalf("request = %+v", req)
	}
}

func TestListAvailableRepositories_WorkspaceConnectionIsVisible(t *testing.T) {
	svc, st, _ := setup(t)
	ws := st.addIdentity("ws", "ws-app")
	got, err := svc.ListAvailableRepositories(context.Background(), uuid.New(), ws.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Supported || got.Items == nil {
		t.Fatalf("page = %+v, want a supported, non-nil empty page", got)
	}
}

func TestListAvailableRepositories_OtherProjectsConnectionIsNotFound(t *testing.T) {
	svc, st, pr := setup(t)
	project, other := uuid.New(), uuid.New()
	theirs := st.addProjectConnection(other, "theirs", "other-token")
	_, err := svc.ListAvailableRepositories(context.Background(), project, theirs.ID, "", "")
	if !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(pr.listReqs) != 0 {
		t.Fatal("the provider must not be asked about a connection the project cannot use")
	}
}

func TestListAvailableRepositories_AgentConnectionIsNotFound(t *testing.T) {
	svc, st, _ := setup(t)
	agent := &gitdom.Identity{ID: uuid.New(), ProviderID: "p", ProviderIdentityID: "agent-app", Scope: gitdom.ScopeAgent}
	st.byID[agent.ID] = agent
	_, err := svc.ListAvailableRepositories(context.Background(), uuid.New(), agent.ID, "", "")
	if !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListAvailableRepositories_UnsupportedFallsBackQuietly(t *testing.T) {
	svc, st, pr := setup(t)
	ws := st.addIdentity("ws", "ws-app")
	pr.list = func(gitprovider.ListReposRequest) (gitprovider.RepositoryPage, error) {
		return gitprovider.RepositoryPage{}, &gitprovider.Error{Code: gitprovider.CodeUnsupported, Message: "no listing"}
	}
	got, err := svc.ListAvailableRepositories(context.Background(), uuid.New(), ws.ID, "", "")
	if err != nil {
		t.Fatalf("unsupported must not be an error: %v", err)
	}
	if got.Supported || len(got.Items) != 0 || got.Items == nil {
		t.Fatalf("page = %+v, want empty and unsupported", got)
	}
}

func TestListAvailableRepositories_ProviderErrorsPropagate(t *testing.T) {
	svc, st, pr := setup(t)
	ws := st.addIdentity("ws", "ws-app")
	boom := &gitprovider.Error{Code: gitprovider.CodeForbidden, Message: "token lacks read_api"}
	pr.list = func(gitprovider.ListReposRequest) (gitprovider.RepositoryPage, error) {
		return gitprovider.RepositoryPage{}, boom
	}
	_, err := svc.ListAvailableRepositories(context.Background(), uuid.New(), ws.ID, "", "")
	if !gitprovider.IsCode(err, gitprovider.CodeForbidden) {
		t.Fatalf("err = %v, want the provider's forbidden error", err)
	}
}
