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

	"database/sql"

	"github.com/Paca-AI/api/internal/config"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/cache"
	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/internal/platform/logger"
	"github.com/Paca-AI/api/internal/platform/messaging"
	oidcplatform "github.com/Paca-AI/api/internal/platform/oidc"
	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	"github.com/Paca-AI/api/internal/platform/secret"
	"github.com/Paca-AI/api/internal/platform/storage"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	redisRepo "github.com/Paca-AI/api/internal/repository/redis"
	agentsvc "github.com/Paca-AI/api/internal/service/agent"
	apikeysvc "github.com/Paca-AI/api/internal/service/apikey"
	attachmentsvc "github.com/Paca-AI/api/internal/service/attachment"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
	galaxyauthsvc "github.com/Paca-AI/api/internal/service/galaxyauth"
	globalrolesvc "github.com/Paca-AI/api/internal/service/globalrole"
	notificationsvc "github.com/Paca-AI/api/internal/service/notification"
	pluginsvc "github.com/Paca-AI/api/internal/service/plugin"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	workflowsvc "github.com/Paca-AI/api/internal/service/workflow"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/Paca-AI/api/internal/worker"
	"github.com/Paca-AI/api/migrations"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

// agentBotUserID is the fixed UUID of the built-in agent bot user seeded on
// startup.  The AI agent service authenticates as this user when it presents
// the AGENT_API_KEY configured in the SecurityConfig. It is also reused as
// the generic "system actor" for automated changes with no human actor (see
// userdom.SystemActorUserID).
var agentBotUserID = userdom.SystemActorUserID

// App holds the HTTP server and any resources that need graceful shutdown.
type App struct {
	server               *http.Server
	publisher            *messaging.Publisher
	activityConsumer     *worker.ActivityConsumer
	docActivityConsumer  *worker.DocActivityConsumer
	notificationConsumer *worker.NotificationConsumer
	pluginEventConsumer  *worker.PluginEventConsumer
	workflowConsumer     *worker.WorkflowConsumer
	log                  *slog.Logger
}

// New builds all dependencies and returns a ready-to-run App.
func New(cfg *config.Config) (*App, error) {
	log := logger.New(cfg.Env)

	// --- Platform -----------------------------------------------------------
	db, err := database.Open(database.Config{
		DSN: cfg.Database.DSN,
	}, log)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	redisClient, err := cache.NewClient(cfg.Redis.URL, log)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	cacheStore := cache.NewStore(redisClient, "paca:")

	publisher := messaging.NewPublisher(redisClient, log)

	tokenManager := jwttoken.New(cfg.JWT.Secret, cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL)
	permissionStore := pgRepo.NewAuthzPermissionStore(db)
	authorizer := authz.NewAuthorizer(permissionStore).WithAgentRoleResolver(permissionStore)

	// --- Repositories -------------------------------------------------------
	userRepo := pgRepo.NewUserRepository(db)
	globalRoleRepo := pgRepo.NewGlobalRoleRepository(db)
	projectRepo := pgRepo.NewProjectRepository(db)
	taskRepo := pgRepo.NewTaskRepository(db)
	activityRepo := pgRepo.NewTaskActivityRepository(db)
	notificationRepo := pgRepo.NewNotificationRepository(db)
	sprintRepo := pgRepo.NewSprintRepository(db)
	viewRepo := pgRepo.NewViewRepository(db)
	attachmentRepo := pgRepo.NewAttachmentRepository(db)
	docRepo := pgRepo.NewDocumentRepository(db)
	refreshStore := redisRepo.NewRefreshTokenStore(redisClient)
	pluginRepo := pgRepo.NewPluginRepository(db)
	rawWorkflowRepo := pgRepo.NewWorkflowRepository(db)
	// Wraps rawWorkflowRepo with a cache for status-rule reads, invalidated
	// on writes — shared between workflowService and workflowConsumer below
	// so a rule edited via the API is visible to the very next automation
	// event. rawWorkflowRepo itself is kept around only for
	// StatusUsedByWorkflow, a Postgres-specific check outside the
	// workflowdom.Repository interface this decorator implements.
	workflowRepo := workflowsvc.NewCachedRepository(rawWorkflowRepo, cacheStore, cfg.Cache.ConfigTTL, log)

	// --- Schema migration ---------------------------------------------------
	// All statements use CREATE TABLE IF NOT EXISTS / INSERT … ON CONFLICT so
	// they are idempotent and safe to re-run on every startup.
	if err := database.RunMigrationsFS(db.DB, migrations.FS); err != nil {
		return nil, fmt.Errorf("bootstrap: auto-migrate: %w", err)
	}
	log.Info("schema migrations applied")

	// --- Admin seeding -------------------------------------------------------
	// seedDefaultRoles must run first so the ADMIN global role exists before
	// seedAdmin tries to reference it by FK.
	if err := seedDefaultRoles(context.Background(), db, userRepo, globalRoleRepo, cfg.Admin.Username, log); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if err := seedAdmin(context.Background(), userRepo, globalRoleRepo, cfg.Admin, log); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if err := seedAgentBotUser(context.Background(), userRepo, globalRoleRepo, log); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	// --- Services -----------------------------------------------------------
	authService := authsvc.New(userRepo, tokenManager, refreshStore, cfg.JWT.RefreshTTL, cfg.JWT.RefreshSessionTTL)
	userService := usersvc.New(userRepo, permissionStore, globalRoleRepo)
	globalRoleService := globalrolesvc.NewCachedService(globalrolesvc.New(globalRoleRepo), cacheStore, cfg.Cache.ConfigTTL, log)
	projectService := projectsvc.NewCachedService(projectsvc.New(projectRepo, taskRepo), cacheStore, cfg.Cache.ProjectTTL, cfg.Cache.ConfigTTL, log)
	taskService := tasksvc.NewCachedService(tasksvc.New(taskRepo).WithWorkflowStatusChecker(rawWorkflowRepo), cacheStore, cfg.Cache.ConfigTTL, log)
	sprintService := sprintsvc.NewCachedSprintService(sprintsvc.New(sprintRepo, taskRepo), cacheStore, cfg.Cache.SprintTTL, log)
	viewService := sprintsvc.NewCachedViewService(sprintsvc.NewViewService(viewRepo), cacheStore, cfg.Cache.SprintTTL, log)
	notificationService := notificationsvc.New(notificationRepo, projectRepo, publisher)
	agentRepo := pgRepo.NewAgentRepository(db)
	agentService := agentsvc.New(agentRepo, projectService, publisher, pluginRepo)
	if cfg.Security.EncryptionKey != "" {
		keyBytes, hexErr := secret.DecodeHexKey(cfg.Security.EncryptionKey)
		if hexErr != nil {
			log.Warn("agent LLM key encryption disabled: invalid ENCRYPTION_KEY", "error", hexErr)
		} else if enc, encErr := secret.NewEncryptor(keyBytes); encErr != nil {
			log.Warn("agent LLM key encryption disabled: encryptor init failed", "error", encErr)
		} else {
			agentService = agentService.WithEncryptor(enc)
			log.Info("agent LLM API key at-rest encryption enabled")
		}
	}
	activityService := tasksvc.NewActivityService(activityRepo, projectRepo, publisher).
		WithNotificationService(notificationService).
		WithAgentTrigger(agentService)
	notificationConsumer := worker.NewNotificationConsumer(redisClient, notificationService, log, projectRepo, agentService).
		WithActivityRecorder(activityService)
	activityConsumer := worker.NewActivityConsumer(redisClient, activityRepo, projectRepo, log)
	docService := docsvc.New(docRepo, projectRepo)
	docActivityService := docsvc.NewActivityService(docRepo, projectRepo, publisher).
		WithNotificationService(notificationService)
	docActivityConsumer := worker.NewDocActivityConsumer(redisClient, docRepo, projectRepo, log)
	workflowService := workflowsvc.New(workflowRepo, taskRepo, projectRepo, publisher)
	workflowConsumer := worker.NewWorkflowConsumer(redisClient, workflowRepo, taskRepo, taskService, activityService, publisher, log)

	// Object storage — defaults to MinIO; switches to AWS S3 when STORAGE_PROVIDER=s3.
	storageClient, err := storage.NewS3Client(context.Background(), storage.S3Config{
		Endpoint:        cfg.Storage.Endpoint,
		PublicURL:       cfg.Storage.PublicURL,
		Region:          cfg.Storage.Region,
		Bucket:          cfg.Storage.Bucket,
		AccessKeyID:     cfg.Storage.AccessKeyID,
		SecretAccessKey: cfg.Storage.SecretAccessKey,
		UseSSL:          cfg.Storage.UseSSL,
		ForcePathStyle:  cfg.Storage.Provider != "s3", // MinIO requires path-style
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap: storage client: %w", err)
	}
	if cfg.Storage.Provider != "s3" {
		if err := storageClient.EnsureBucket(context.Background(), cfg.Storage.Bucket); err != nil {
			return nil, fmt.Errorf("bootstrap: ensure storage bucket: %w", err)
		}
	}

	attachmentService := attachmentsvc.New(attachmentRepo, attachmentsvc.NewTaskOwnerChecker(taskRepo), storageClient, cfg.Storage.Bucket)

	// --- API Key management -------------------------------------------------
	apiKeyRepo := pgRepo.NewAPIKeyRepository(db)
	apiKeyService := apikeysvc.New(apiKeyRepo)
	// Configure the static agent API key so the AI agent service can
	// authenticate without a database-stored key entry.
	if cfg.Security.AgentAPIKey != "" {
		apiKeyService.WithAgentKey(cfg.Security.AgentAPIKey, agentBotUserID)
	}
	// Legacy AGENT_API_KEY + X-Agent-ID header impersonation is disabled by
	// default (ADR-038); requests presenting the header are rejected with 401
	// unless AGENT_HEADER_IMPERSONATION=enabled.
	apiKeyService.WithAgentHeaderImpersonation(cfg.Security.AgentHeaderImpersonation)
	if cfg.Security.AgentHeaderImpersonation {
		log.Warn("legacy X-Agent-ID header impersonation is ENABLED — ADR-038 recommends the Vortex bearer act_as contract instead")
	}

	// --- Plugin infrastructure ----------------------------------------------
	// sqlx.DB embeds *sql.DB; plugin infrastructure uses the raw driver interface.
	sqlDB := db.DB

	pluginStore, err := pluginrt.NewStore(context.Background(), pluginrt.StoreConfig{
		Store:    cfg.Plugins.Store,
		WASMDir:  cfg.Plugins.WASMDir,
		S3Bucket: cfg.Storage.Bucket,
		S3Prefix: cfg.Plugins.S3Prefix,
		S3Region: cfg.Storage.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap: plugin store: %w", err)
	}

	pluginMigrationRunner := pluginrt.NewMigrationRunner(sqlDB, pluginStore, log)

	pluginRuntime := pluginrt.NewRuntime(pluginStore, pluginrt.HostServices{
		DB:         sqlDB,
		Log:        log,
		Publisher:  publisher,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Authorizer: authorizer,
		Config: map[string]string{
			"ENCRYPTION_KEY": cfg.Security.EncryptionKey,
			"PUBLIC_URL":     cfg.Server.PublicURL,
		},
	}, pluginrt.ResourceLimits{
		MaxCallDuration:     cfg.Plugins.Limits.MaxCallDuration,
		MaxMemoryPages:      cfg.Plugins.Limits.MaxMemoryPages,
		MaxRequestBodyBytes: cfg.Plugins.Limits.MaxRequestBodyBytes,
	}, log)
	marketplaceClient := pluginrt.NewMarketplaceClient(cfg.Plugins.MarketplaceCatalogURL, cfg.Plugins.MarketplaceTimeout)
	installerHTTPClient := &http.Client{Timeout: cfg.Plugins.MarketplaceTimeout}
	pluginInstaller := pluginrt.NewInstaller(cfg.Plugins.WASMDir, cfg.Plugins.FrontendDir, cfg.Plugins.MCPDir, installerHTTPClient, log)

	pluginService := pluginsvc.New(pluginRepo)

	// Load all enabled plugins from the DB into the WASM runtime.
	installedPlugins, err := pluginService.ListPlugins(context.Background())
	if err != nil {
		return nil, fmt.Errorf("bootstrap: plugin: list: %w", err)
	}
	// Run per-plugin DB migrations before loading WASM modules.
	for _, p := range installedPlugins {
		if !p.Enabled {
			continue
		}
		if err := pluginMigrationRunner.Run(context.Background(), p.Name); err != nil {
			log.Error("plugin: migration failed", "name", p.Name, "error", err)
		}
	}
	if err := pluginRuntime.LoadAll(context.Background(), installedPlugins); err != nil {
		log.Error("plugin: some plugins failed to load", "error", err)
	}

	// Forward every recorded activity (task created/updated/deleted, comments,
	// links, etc.) to subscribed plugins. ActivitySvc appends to the
	// StreamPluginEvents Valkey stream; this consumer reads it back and
	// dispatches to the plugin runtime — the API never calls into the plugin
	// runtime directly when recording an activity.
	pluginEventConsumer := worker.NewPluginEventConsumer(redisClient, pluginRuntime, log)

	pluginHandler := handler.NewPluginHandler(pluginService, pluginRuntime, projectRepo).
		WithRouteAuth(tokenManager, apiKeyService, authorizer).
		WithMarketplace(marketplaceClient, pluginInstaller, pluginMigrationRunner)

	agentHandler := handler.NewAgentHandler(agentService, cfg.AIAgentURL).
		WithActivityRecorder(activityService).
		WithMemberRepo(projectRepo)
	convHandler := handler.NewConversationHandler(agentService)
	workflowHandler := handler.NewWorkflowHandler(workflowService)

	// --- Handlers -----------------------------------------------------------
	cookieCfg := handler.CookieConfig{
		Secure:            cfg.Server.CookieSecure,
		AccessTTL:         cfg.JWT.AccessTTL,
		RefreshTTL:        cfg.JWT.RefreshTTL,
		RefreshSessionTTL: cfg.JWT.RefreshSessionTTL,
	}

	authHandler := handler.NewAuthHandler(authService, cookieCfg)

	// Galaxy chat dock (ADR-038 P3.2): advertise the dock bundle URL on the
	// public /auth/config endpoint so the SPA mounts it after login.
	if cfg.GalaxyDockSrc != "" {
		authHandler = authHandler.WithGalaxyDock(cfg.GalaxyDockSrc)
	}

	// --- Galaxy identity (ADR-038) -------------------------------------------
	// OIDC SSO login against the Vortex identity provider. Off unless
	// OIDC_ISSUER is set; discovery/JWKS are fetched lazily on first login.
	var oidcHandler *handler.OIDCHandler
	if cfg.OIDC.Enabled() {
		authHandler = authHandler.WithOIDC(cfg.OIDC.ButtonLabel)
		oidcProvider := oidcplatform.NewProvider(cfg.OIDC.Issuer)
		galaxyAuthService := galaxyauthsvc.New(userRepo, globalRoleRepo, cfg.OIDC.AutoCreateUsers, cfg.OIDC.DefaultRole, log)
		oidcHandler = handler.NewOIDCHandler(
			oidcProvider,
			handler.OIDCOptions{
				ClientID:     cfg.OIDC.ClientID,
				ClientSecret: cfg.OIDC.ClientSecret,
				RedirectURL:  cfg.OIDC.RedirectURL,
				Scopes:       cfg.OIDC.Scopes,
			},
			galaxyAuthService,
			authService,
			authHandler,
			[]byte(cfg.JWT.Secret),
			log,
		)
		log.Info("OIDC SSO login enabled", "issuer", cfg.OIDC.Issuer, "auto_create_users", cfg.OIDC.AutoCreateUsers)
	}

	// Trusted-issuer RS256 bearer auth: platform tokens (with act_as) map to
	// local users via users.oidc_sub; unknown principals are rejected.
	var galaxyBearer httpmw.GalaxyBearerAuthenticator
	if cfg.Security.GalaxyTrustedIssuer != "" {
		// Audience + scope enforcement (PACA-C1): the aud claim is checked
		// against the configured resource id, and scope-bearing tokens must
		// target Paca (foreign-resource tokens rejected; read-only tokens
		// denied writes).
		galaxyBearer = galaxyauthsvc.NewBearerAuthenticator(
			oidcplatform.NewProviderWithIssuerClaims(
				cfg.Security.GalaxyTrustedIssuer, cfg.Security.GalaxyTrustedIssuerClaims),
			userRepo, log).
			WithResourceAudience(cfg.Security.GalaxyBearerAudience).
			WithResourceScopePrefix(cfg.Security.GalaxyResourceScopePrefix)
		log.Info("Galaxy trusted-issuer bearer auth enabled",
			"issuer", cfg.Security.GalaxyTrustedIssuer,
			"extra_issuer_claims", cfg.Security.GalaxyTrustedIssuerClaims,
			"audience_enforced", cfg.Security.GalaxyBearerAudience != "",
			"resource_scope_prefix", cfg.Security.GalaxyResourceScopePrefix)
	}

	deps := router.Deps{
		TokenManager:         tokenManager,
		APIKeyAuth:           apiKeyService,
		GalaxyBearer:         galaxyBearer,
		Authorizer:           authorizer,
		Health:               handler.NewHealthHandler(),
		Auth:                 authHandler,
		OIDC:                 oidcHandler,
		User:                 handler.NewUserHandler(userService, authService),
		GlobalRole:           handler.NewGlobalRoleHandler(globalRoleService, authorizer),
		ProjectVisibilitySvc: projectService,
		Project:              handler.NewProjectHandler(projectService, authorizer, handler.WithProjectDefaultViews(viewService, taskService)),
		Task: handler.NewTaskHandler(taskService, viewService, activityService,
			handler.WithTaskPublisher(publisher)),
		Sprint: handler.NewSprintHandler(sprintService, viewService,
			handler.WithSprintDefaultTaskTypes(taskService),
			handler.WithSprintDefaultTaskStatuses(taskService),
		),
		View:         handler.NewViewHandler(viewService),
		Attachment:   handler.NewAttachmentHandler(attachmentService),
		Document:     handler.NewDocumentHandler(docService, docActivityService),
		DocFile:      handler.NewDocFileHandler(attachmentService),
		Notification: handler.NewNotificationHandler(notificationService),
		APIKey:       handler.NewAPIKeyHandler(apiKeyService),
		Plugin:       pluginHandler,
		Agent:        agentHandler,
		Conversation: convHandler,
		Workflow:     workflowHandler,
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

	return &App{server: srv, publisher: publisher, activityConsumer: activityConsumer, docActivityConsumer: docActivityConsumer, notificationConsumer: notificationConsumer, pluginEventConsumer: pluginEventConsumer, workflowConsumer: workflowConsumer, log: log}, nil
}

// Run starts the activity consumers and the HTTP server.
// It returns when the server stops.
func (a *App) Run() error {
	a.log.Info("starting server", "addr", a.server.Addr)
	a.activityConsumer.Start(context.Background())
	a.docActivityConsumer.Start(context.Background())
	a.notificationConsumer.Start(context.Background())
	a.pluginEventConsumer.Start(context.Background())
	a.workflowConsumer.Start(context.Background())
	return a.server.ListenAndServe()
}

// Shutdown gracefully stops the server with the given timeout.
func (a *App) Shutdown(ctx context.Context) error {
	a.log.Info("shutting down server")
	a.activityConsumer.Stop()
	a.docActivityConsumer.Stop()
	a.notificationConsumer.Stop()
	a.pluginEventConsumer.Stop()
	a.workflowConsumer.Stop()
	if a.publisher != nil {
		a.publisher.Close()
	}
	return a.server.Shutdown(ctx)
}

// seedAdmin ensures the default admin account exists in the database.
// It must be called after seedDefaultRoles so the ADMIN global role exists.
// If the account already exists it is left unchanged.
func seedAdmin(ctx context.Context, repo userdom.Repository, globalRoleRepo *pgRepo.GlobalRoleRepository, cfg config.AdminConfig, log *slog.Logger) error {
	_, err := repo.FindByUsernameIncludingDeleted(ctx, cfg.Username)
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

	// Immediately assign the SUPER_ADMIN global role via users.role_id so the
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

// seedAgentBotUser ensures the built-in agent bot user exists in the database.
// This user has the SUPER_ADMIN global role and is used as the identity for
// requests authenticated via AGENT_API_KEY.  The bot can never log in with a
// password because its password_hash is set to an invalid value.
func seedAgentBotUser(ctx context.Context, repo userdom.Repository, globalRoleRepo *pgRepo.GlobalRoleRepository, log *slog.Logger) error {
	_, err := repo.FindByUsernameIncludingDeleted(ctx, "_paca_agent_bot")
	if err == nil {
		// Already exists — nothing to do.
		return nil
	}
	if !errors.Is(err, userdom.ErrNotFound) {
		return fmt.Errorf("seed agent bot: lookup: %w", err)
	}

	superAdminRole, err := globalRoleRepo.FindByName(ctx, "SUPER_ADMIN")
	if err != nil {
		return fmt.Errorf("seed agent bot: find SUPER_ADMIN role: %w", err)
	}

	now := time.Now()
	bot := &userdom.User{
		ID:           agentBotUserID,
		Username:     "_paca_agent_bot",
		PasswordHash: "!", // intentionally invalid — bot cannot log in with a password
		FullName:     "Paca Agent Bot",
		RoleID:       superAdminRole.ID,
		Role:         superAdminRole.Name,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.Create(ctx, bot); err != nil {
		return fmt.Errorf("seed agent bot: create: %w", err)
	}
	if err := globalRoleRepo.ReplaceUserRoles(ctx, bot.ID, []uuid.UUID{superAdminRole.ID}); err != nil {
		return fmt.Errorf("seed agent bot: assign SUPER_ADMIN: %w", err)
	}

	log.Info("agent bot user created")
	return nil
}

// projectTaskLookup implements githubsvc.TaskLookup using the project and task
// repositories.  It is used by the GitHub service to resolve a task-ID-prefix
// pattern (e.g. "PROJ-42") found in a branch name to the corresponding task.
type projectTaskLookup struct {
	projectRepo *pgRepo.ProjectRepository
	taskRepo    *pgRepo.TaskRepository
}

func (l *projectTaskLookup) FindTaskByProjectPrefixAndNumber(ctx context.Context, prefix string, number int64) (uuid.UUID, uuid.UUID, error) {
	project, err := l.projectRepo.FindByTaskIDPrefix(ctx, prefix)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	task, err := l.taskRepo.FindTaskByNumber(ctx, project.ID, number)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return task.ID, task.ProjectID, nil
}

func seedDefaultRoles(
	ctx context.Context,
	db *sqlx.DB,
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

	// Under the single-role schema users.role_id holds exactly one role.
	// Check whether the admin already has SUPER_ADMIN; if not, assign it (replacing whatever role they have).
	existingRoles, err := globalRoleRepo.ListUserRoles(ctx, adminUser.ID)
	if err != nil {
		return fmt.Errorf("seed global roles: list admin user roles: %w", err)
	}
	hasSuperAdmin := false
	for _, role := range existingRoles {
		if role.ID == superAdminRole.ID {
			hasSuperAdmin = true
			break
		}
	}
	if !hasSuperAdmin {
		if err := globalRoleRepo.ReplaceUserRoles(ctx, adminUser.ID, []uuid.UUID{superAdminRole.ID}); err != nil {
			return fmt.Errorf("seed global roles: assign SUPER_ADMIN: %w", err)
		}
		log.Info("assigned SUPER_ADMIN role to admin user", "username", adminUsername)
	}

	return nil
}

func seedDefaultProjectRoleTemplates(ctx context.Context, db *sqlx.DB) error {
	for _, def := range authz.DefaultProjectRoles() {
		permissionsRaw, err := json.Marshal(permissionMap(def.Permissions))
		if err != nil {
			return fmt.Errorf("seed project roles: marshal %s permissions: %w", def.Name, err)
		}

		var existingID string
		err = db.QueryRowContext(ctx,
			`SELECT id FROM project_roles WHERE project_id IS NULL AND role_name = $1`,
			def.Name,
		).Scan(&existingID)

		if errors.Is(err, sql.ErrNoRows) {
			now := time.Now()
			_, err = db.ExecContext(ctx,
				`INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at)
				 VALUES ($1, NULL, $2, $3, $4, $5)`,
				uuid.NewString(), def.Name, permissionsRaw, now, now,
			)
			if err != nil {
				return fmt.Errorf("seed project roles: create template %s: %w", def.Name, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("seed project roles: find template %s: %w", def.Name, err)
		}

		_, err = db.ExecContext(ctx,
			`UPDATE project_roles SET permissions = $1, updated_at = $2 WHERE id = $3`,
			permissionsRaw, time.Now(), existingID,
		)
		if err != nil {
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
