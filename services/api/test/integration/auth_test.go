package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/authz"
	jwttoken "github.com/paca/api/internal/platform/token"
	authsvc "github.com/paca/api/internal/service/auth"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
	"golang.org/x/crypto/bcrypt"
)

// -- fakes -------------------------------------------------------------------

type fakeUserRepo struct {
	byEmail map[string]*userdom.User
	byID    map[uuid.UUID]*userdom.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byEmail: make(map[string]*userdom.User),
		byID:    make(map[uuid.UUID]*userdom.User),
	}
}

func (r *fakeUserRepo) FindByID(_ context.Context, id uuid.UUID) (*userdom.User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, userdom.ErrNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (*userdom.User, error) {
	u, ok := r.byEmail[email]
	if !ok {
		return nil, userdom.ErrNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) Create(_ context.Context, u *userdom.User) error {
	r.byEmail[u.Email] = u
	r.byID[u.ID] = u
	return nil
}

func (r *fakeUserRepo) Update(_ context.Context, u *userdom.User) error {
	r.byEmail[u.Email] = u
	r.byID[u.ID] = u
	return nil
}

func (r *fakeUserRepo) Delete(_ context.Context, id uuid.UUID) error {
	if u, ok := r.byID[id]; ok {
		delete(r.byEmail, u.Email)
		delete(r.byID, id)
	}
	return nil
}

type fakeBlacklist struct{ revoked map[string]bool }

func newFakeBlacklist() *fakeBlacklist { return &fakeBlacklist{revoked: map[string]bool{}} }

func (b *fakeBlacklist) Revoke(_ context.Context, jti string, _ time.Duration) error {
	b.revoked[jti] = true
	return nil
}

func (b *fakeBlacklist) IsRevoked(_ context.Context, jti string) (bool, error) {
	return b.revoked[jti], nil
}

// -- helpers -----------------------------------------------------------------

const testSecret = "test-secret-that-is-at-least-32-chars"

func buildTestRouter(repo *fakeUserRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	bl := newFakeBlacklist()
	authService := authsvc.New(repo, tm, bl, 168*time.Hour)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager: tm,
		AuthzPolicy:  authz.NewPolicy(),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService),
		User:         handler.NewUserHandler(nil),
		Log:          log,
	})
}

// -- tests -------------------------------------------------------------------

func TestLoginSuccess(t *testing.T) {
	repo := newFakeUserRepo()

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	u := &userdom.User{ID: uuid.New(), Email: "test@example.com", PasswordHash: string(hash), Role: userdom.RoleUser}
	_ = repo.Create(context.Background(), u)

	r := buildTestRouter(repo)

	body, _ := json.Marshal(map[string]string{"email": "test@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoginWrongPassword(t *testing.T) {
	repo := newFakeUserRepo()

	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	u := &userdom.User{ID: uuid.New(), Email: "test@example.com", PasswordHash: string(hash), Role: userdom.RoleUser}
	_ = repo.Create(context.Background(), u)

	r := buildTestRouter(repo)

	body, _ := json.Marshal(map[string]string{"email": "test@example.com", "password": "wrong-password"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
