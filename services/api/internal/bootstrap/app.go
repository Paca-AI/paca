// Package bootstrap wires up all application dependencies and exposes a
// runnable *App.
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/config"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
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
	globalrolesvc "github.com/paca/api/internal/service/globalrole"
	usersvc "github.com/paca/api/internal/service/user"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
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
	permissionStore := pgRepo.NewAuthzPermissionStore(db)
	authorizer := authz.NewAuthorizer(permissionStore)

	// --- Repositories -------------------------------------------------------
	userRepo := pgRepo.NewUserRepository(db)
	globalRoleRepo := pgRepo.NewGlobalRoleRepository(db)
	refreshStore := redisRepo.NewRefreshTokenStore(redisClient)

	if err := db.AutoMigrate(
		&userModel{},
		&projectModel{},
		&projectRoleModel{},
		&projectMemberModel{},
		&globalRoleModel{},
		&userGlobalRoleModel{},
	); err != nil {
		return nil, fmt.Errorf("bootstrap: migrate role tables: %w", err)
	}

	// --- Admin seeding -------------------------------------------------------
	// seedDefaultRoles must run first so the ADMIN global role exists before
	// seedAdmin tries to reference it by FK.
	if err := seedDefaultRoles(context.Background(), db, userRepo, globalRoleRepo, cfg.Admin.Username, log); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if err := seedAdmin(context.Background(), userRepo, globalRoleRepo, cfg.Admin, log); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	// --- Services -----------------------------------------------------------
	authService := authsvc.New(userRepo, tokenManager, refreshStore, cfg.JWT.RefreshTTL, cfg.JWT.RefreshSessionTTL)
	userService := usersvc.New(userRepo, permissionStore, globalRoleRepo)
	globalRoleService := globalrolesvc.New(globalRoleRepo)

	// --- Handlers -----------------------------------------------------------
	cookieCfg := handler.CookieConfig{
		Secure:            cfg.Server.CookieSecure,
		AccessTTL:         cfg.JWT.AccessTTL,
		RefreshTTL:        cfg.JWT.RefreshTTL,
		RefreshSessionTTL: cfg.JWT.RefreshSessionTTL,
	}

	deps := router.Deps{
		TokenManager: tokenManager,
		Authorizer:   authorizer,
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, cookieCfg),
		User:         handler.NewUserHandler(userService, authService),
		GlobalRole:   handler.NewGlobalRoleHandler(globalRoleService),
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

// userModel is used only for startup AutoMigrate to ensure the users table
// has the role_id and must_change_password columns in development without
// running SQL migrations.
type userModel struct {
	ID                 string `gorm:"primarykey;type:uuid"`
	Username           string `gorm:"uniqueIndex;not null"`
	PasswordHash       string `gorm:"not null"`
	FullName           string `gorm:"column:full_name"`
	RoleID             string `gorm:"column:role_id;type:uuid"`
	MustChangePassword bool   `gorm:"column:must_change_password;not null;default:false"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          gorm.DeletedAt `gorm:"index"`
}

func (userModel) TableName() string { return "users" }

// globalRoleModel is used only for startup AutoMigrate to ensure the planned
// admin tables exist in development environments.
type globalRoleModel struct {
	ID          string `gorm:"primarykey;type:uuid"`
	Name        string `gorm:"uniqueIndex;not null"`
	Permissions []byte `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (globalRoleModel) TableName() string { return "global_roles" }

// userGlobalRoleModel keeps the user-role relationship table aligned.
type userGlobalRoleModel struct {
	UserID string `gorm:"primaryKey;type:uuid;column:user_id"`
	RoleID string `gorm:"primaryKey;type:uuid;column:role_id"`
}

func (userGlobalRoleModel) TableName() string { return "user_global_roles" }

type projectModel struct {
	ID          string `gorm:"primarykey;type:uuid"`
	Name        string `gorm:"not null"`
	Description string
	Settings    []byte `gorm:"type:jsonb;not null"`
	CreatedBy   *string
	CreatedAt   time.Time
}

func (projectModel) TableName() string { return "projects" }

type projectRoleModel struct {
	ID          string  `gorm:"primarykey;type:uuid"`
	ProjectID   *string `gorm:"type:uuid;column:project_id;index"`
	RoleName    string  `gorm:"column:role_name;not null"`
	Permissions []byte  `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (projectRoleModel) TableName() string { return "project_roles" }

type projectMemberModel struct {
	ID            string `gorm:"primarykey;type:uuid"`
	ProjectID     string `gorm:"type:uuid;column:project_id;index"`
	UserID        string `gorm:"type:uuid;column:user_id;index"`
	ProjectRoleID string `gorm:"type:uuid;column:project_role_id;index"`
}

func (projectMemberModel) TableName() string { return "project_members" }

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
// It must be called after seedDefaultRoles so the ADMIN global role exists.
// If the account already exists it is left unchanged.
func seedAdmin(ctx context.Context, repo userdom.Repository, globalRoleRepo *pgRepo.GlobalRoleRepository, cfg config.AdminConfig, log *slog.Logger) error {
	_, err := repo.FindByUsername(ctx, cfg.Username)
	if err == nil {
		// Admin already exists — nothing to do.
		return nil
	}
	if !errors.Is(err, userdom.ErrNotFound) {
		return fmt.Errorf("seed admin: lookup: %w", err)
	}

	// Resolve the ADMIN global role FK.
	adminRole, err := globalRoleRepo.FindByName(ctx, "ADMIN")
	if err != nil {
		return fmt.Errorf("seed admin: find ADMIN role: %w", err)
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
		RoleID:       adminRole.ID,
		Role:         adminRole.Name,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := repo.Create(ctx, admin); err != nil {
		return fmt.Errorf("seed admin: create: %w", err)
	}

	// Immediately grant the SUPER_ADMIN global role via user_global_roles so the
	// admin user has full permissions from the first request.
	superAdminRole, err := globalRoleRepo.FindByName(ctx, "SUPER_ADMIN")
	if err != nil {
		return fmt.Errorf("seed admin: find SUPER_ADMIN role: %w", err)
	}
	if err := globalRoleRepo.ReplaceUserRoles(ctx, admin.ID, []uuid.UUID{superAdminRole.ID}); err != nil {
		return fmt.Errorf("seed admin: assign SUPER_ADMIN: %w", err)
	}

	log.Info("admin account created", "username", cfg.Username)
	return nil
}

func seedDefaultRoles(
	ctx context.Context,
	db *gorm.DB,
	userRepo userdom.Repository,
	globalRoleRepo *pgRepo.GlobalRoleRepository,
	adminUsername string,
	log *slog.Logger,
) error {
	for _, def := range authz.DefaultGlobalRoles() {
		role, err := globalRoleRepo.FindByName(ctx, def.Name)
		if err != nil {
			if !errors.Is(err, globalroledom.ErrNotFound) {
				return fmt.Errorf("seed global roles: find %s: %w", def.Name, err)
			}
			now := time.Now()
			if err := globalRoleRepo.Create(ctx, &globalroledom.GlobalRole{
				ID:          uuid.New(),
				Name:        def.Name,
				Permissions: permissionMap(def.Permissions),
				CreatedAt:   now,
				UpdatedAt:   now,
			}); err != nil {
				return fmt.Errorf("seed global roles: create %s: %w", def.Name, err)
			}
			continue
		}

		role.Permissions = permissionMap(def.Permissions)
		role.UpdatedAt = time.Now()
		if err := globalRoleRepo.Update(ctx, role); err != nil {
			return fmt.Errorf("seed global roles: update %s: %w", def.Name, err)
		}
	}

	if err := seedDefaultProjectRoleTemplates(ctx, db); err != nil {
		return err
	}

	adminUser, err := userRepo.FindByUsername(ctx, adminUsername)
	if err != nil {
		if errors.Is(err, userdom.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("seed global roles: load admin user: %w", err)
	}

	superAdminRole, err := globalRoleRepo.FindByName(ctx, "SUPER_ADMIN")
	if err != nil {
		return fmt.Errorf("seed global roles: load SUPER_ADMIN role: %w", err)
	}

	existingRoles, err := globalRoleRepo.ListUserRoles(ctx, adminUser.ID)
	if err != nil {
		return fmt.Errorf("seed global roles: list admin user roles: %w", err)
	}
	roleIDs := make([]uuid.UUID, 0, len(existingRoles)+1)
	hasSuperAdmin := false
	for _, role := range existingRoles {
		roleIDs = append(roleIDs, role.ID)
		if role.ID == superAdminRole.ID {
			hasSuperAdmin = true
		}
	}
	if !hasSuperAdmin {
		roleIDs = append(roleIDs, superAdminRole.ID)
		if err := globalRoleRepo.ReplaceUserRoles(ctx, adminUser.ID, roleIDs); err != nil {
			return fmt.Errorf("seed global roles: assign SUPER_ADMIN: %w", err)
		}
		log.Info("assigned SUPER_ADMIN role to admin user", "username", adminUsername)
	}

	return nil
}

func seedDefaultProjectRoleTemplates(ctx context.Context, db *gorm.DB) error {
	for _, def := range authz.DefaultProjectRoles() {
		permissionsRaw, err := json.Marshal(permissionMap(def.Permissions))
		if err != nil {
			return fmt.Errorf("seed project roles: marshal %s permissions: %w", def.Name, err)
		}

		var existing projectRoleModel
		find := db.WithContext(ctx).
			Where("project_id IS NULL AND role_name = ?", def.Name).
			First(&existing)

		if errors.Is(find.Error, gorm.ErrRecordNotFound) {
			now := time.Now()
			projectRoleID := uuid.NewString()
			if err := db.WithContext(ctx).Create(&projectRoleModel{
				ID:          projectRoleID,
				ProjectID:   nil,
				RoleName:    def.Name,
				Permissions: permissionsRaw,
				CreatedAt:   now,
				UpdatedAt:   now,
			}).Error; err != nil {
				return fmt.Errorf("seed project roles: create template %s: %w", def.Name, err)
			}
			continue
		}
		if find.Error != nil {
			return fmt.Errorf("seed project roles: find template %s: %w", def.Name, find.Error)
		}

		if err := db.WithContext(ctx).
			Model(&projectRoleModel{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{"permissions": permissionsRaw, "updated_at": time.Now()}).Error; err != nil {
			return fmt.Errorf("seed project roles: update template %s: %w", def.Name, err)
		}
	}

	return nil
}

func permissionMap(permissions []authz.Permission) map[string]any {
	out := make(map[string]any, len(permissions))
	for _, p := range permissions {
		out[string(p)] = true
	}
	return out
}
