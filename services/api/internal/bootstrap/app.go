// Package bootstrap wires up all application dependencies and exposes a
// runnable *App.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/config"
	userdom "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/authz"
	"github.com/paca/api/internal/platform/cache"
	"github.com/paca/api/internal/platform/database"
	"github.com/paca/api/internal/platform/logger"
	"github.com/paca/api/internal/platform/messaging"
	jwttoken "github.com/paca/api/internal/platform/token"
	pgRepo "github.com/paca/api/internal/repository/postgres"
	redisRepo "github.com/paca/api/internal/repository/redis"
	authsvc "github.com/paca/api/internal/service/auth"
	usersvc "github.com/paca/api/internal/service/user"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
	"golang.org/x/crypto/bcrypt"
)

// App holds the HTTP server and any resources that need graceful shutdown.
type App struct {
	server    *http.Server
	publisher *messaging.Publisher
	log       *slog.Logger
}

// New builds all dependencies and returns a ready-to-run App.
func New(cfg *config.Config) (*App, error) {
	log := logger.New(cfg.Env)

	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// --- Platform -----------------------------------------------------------
	db, err := database.Open(cfg.Database.DSN, log)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	redisClient, err := cache.NewClient(cfg.Redis.URL, log)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	publisher, err := messaging.NewPublisher(cfg.RabbitMQ.URL, "paca.events", log)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	tokenManager := jwttoken.New(cfg.JWT.Secret, cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL)
	policy := authz.NewPolicy()

	// --- Repositories -------------------------------------------------------
	userRepo := pgRepo.NewUserRepository(db)
	refreshStore := redisRepo.NewRefreshTokenStore(redisClient)

	// --- Admin seeding -------------------------------------------------------
	if err := seedAdmin(context.Background(), userRepo, cfg.Admin, log); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	// --- Services -----------------------------------------------------------
	authService := authsvc.New(userRepo, tokenManager, refreshStore, cfg.JWT.RefreshTTL, cfg.JWT.RefreshSessionTTL)
	userService := usersvc.New(userRepo)

	// --- Handlers -----------------------------------------------------------
	cookieCfg := handler.CookieConfig{
		Secure:            cfg.Server.CookieSecure,
		AccessTTL:         cfg.JWT.AccessTTL,
		RefreshTTL:        cfg.JWT.RefreshTTL,
		RefreshSessionTTL: cfg.JWT.RefreshSessionTTL,
	}

	deps := router.Deps{
		TokenManager: tokenManager,
		AuthzPolicy:  policy,
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, cookieCfg),
		User:         handler.NewUserHandler(userService),
		Log:          log,
	}

	engine := router.New(deps)

	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	_ = publisher // suppress unused; used for future event publishing

	return &App{server: srv, publisher: publisher, log: log}, nil
}

// Run starts the HTTP server.  It returns when the server stops.
func (a *App) Run() error {
	a.log.Info("starting server", "addr", a.server.Addr)
	return a.server.ListenAndServe()
}

// Shutdown gracefully stops the server with the given timeout.
func (a *App) Shutdown(ctx context.Context) error {
	a.log.Info("shutting down server")
	if a.publisher != nil {
		a.publisher.Close()
	}
	return a.server.Shutdown(ctx)
}

// seedAdmin ensures the default admin account exists in the database.
// If the account already exists it is left unchanged.
func seedAdmin(ctx context.Context, repo userdom.Repository, cfg config.AdminConfig, log *slog.Logger) error {
	_, err := repo.FindByUsername(ctx, cfg.Username)
	if err == nil {
		// Admin already exists — nothing to do.
		return nil
	}
	if !errors.Is(err, userdom.ErrNotFound) {
		return fmt.Errorf("seed admin: lookup: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("seed admin: hash password: %w", err)
	}

	now := time.Now()
	admin := &userdom.User{
		ID:           uuid.New(),
		Username:     cfg.Username,
		PasswordHash: string(hash),
		FullName:     "Admin",
		Role:         userdom.RoleAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := repo.Create(ctx, admin); err != nil {
		return fmt.Errorf("seed admin: create: %w", err)
	}

	log.Info("admin account created", "username", cfg.Username)
	return nil
}
