// Package bootstrap wires up all application dependencies and exposes a
// runnable *App.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paca/api/internal/config"
	"github.com/paca/api/internal/platform/authz"
	"github.com/paca/api/internal/platform/cache"
	"github.com/paca/api/internal/platform/database"
	"github.com/paca/api/internal/platform/logger"
	"github.com/paca/api/internal/platform/messaging"
	"github.com/paca/api/internal/platform/token"
	pgRepo "github.com/paca/api/internal/repository/postgres"
	redisRepo "github.com/paca/api/internal/repository/redis"
	authsvc "github.com/paca/api/internal/service/auth"
	usersvc "github.com/paca/api/internal/service/user"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
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

	tokenManager := token.New(cfg.JWT.Secret, cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL)
	policy := authz.NewPolicy()

	// --- Repositories -------------------------------------------------------
	userRepo := pgRepo.NewUserRepository(db)
	blacklist := redisRepo.NewTokenBlacklist(redisClient)

	// --- Services -----------------------------------------------------------
	authService := authsvc.New(userRepo, tokenManager, blacklist, cfg.JWT.RefreshTTL)
	userService := usersvc.New(userRepo)

	// --- Handlers -----------------------------------------------------------
	deps := router.Deps{
		TokenManager: tokenManager,
		AuthzPolicy:  policy,
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService),
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
