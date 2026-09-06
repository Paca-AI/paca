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

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"

	"github.com/Paca-AI/api/internal/config"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/cache"
	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/internal/platform/logger"
	"github.com/Paca-AI/api/internal/platform/messaging"
	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	"github.com/Paca-AI/api/internal/platform/secret"
	"github.com/Paca-AI/api/internal/platform/storage"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	redisRepo "github.com/Paca-AI/api/internal/repository/redis"
	agentsvc "github.com/Paca-AI/api/internal/service/agent"
	annotationsvc "github.com/Paca-AI/api/internal/service/annotation"
	apikeysvc "github.com/Paca-AI/api/internal/service/apikey"
	attachmentsvc "github.com/Paca-AI/api/internal/service/attachment"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	automationsvc "github.com/Paca-AI/api/internal/service/automation"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
	environmentsvc "github.com/Paca-AI/api/internal/service/environment"
	globalrolesvc "github.com/Paca-AI/api/internal/service/globalrole"
	notificationsvc "github.com/Paca-AI/api/internal/service/notification"
	pluginsvc "github.com/Paca-AI/api/internal/service/plugin"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	settingssvc "github.com/Paca-AI/api/internal/service/settings"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/Paca-AI/api/internal/worker"
	"github.com/Paca-AI/api/migrations"
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
	environmentConsumer  *worker.EnvironmentCommandConsumer
	automationConsumer   *worker.AutomationConsumer
	agentQueueConsumer   *worker.AgentQueueConsumer
	dueDateScheduler     *worker.DueDateScheduler
	cronScheduler        *worker.CronScheduler
	waitScheduler        *worker.WaitScheduler
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
	settingsRepo := pgRepo.NewSettingsRepository(db)
	passwordSetTokenRepo := pgRepo.NewPasswordSetTokenRepository(db)
	rawAutomationRepo := pgRepo.NewAutomationRepository(db)
	// Wraps rawAutomationRepo with a cache for graph reads, invalidated on
	// writes — shared between automationService and automationConsumer
	// below so a node/edge edited via the API is visible to the very next
	// automation event. rawAutomationRepo itself is kept around only for
	// StatusUsedByAutomation, a Postgres-specific check outside the
	// automationdom.Repository interface this decorator implements.
	automationRepo := automationsvc.NewCachedRepository(rawAutomationRepo, cacheStore, cfg.Cache.ConfigTTL, log)

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
	userService := usersvc.New(userRepo, permissionStore, globalRoleRepo).
		WithPasswordSetTokenRepo(passwordSetTokenRepo).
		WithEventPublishing(publisher)
	agentRepo := pgRepo.NewAgentRepository(db)
	environmentRepo := pgRepo.NewEnvironmentRepository(db)
	annotationRepo := pgRepo.NewAnnotationRepository(db)
	globalRoleService := globalrolesvc.NewCachedService(globalrolesvc.New(globalRoleRepo, agentRepo), cacheStore, cfg.Cache.ConfigTTL, log)
	projectServiceBase := projectsvc.New(projectRepo, taskRepo, agentRepo)
	projectService := projectsvc.NewCachedService(projectServiceBase, cacheStore, cfg.Cache.ProjectTTL, cfg.Cache.ConfigTTL, log)
	taskService := tasksvc.NewCachedService(tasksvc.New(taskRepo).WithAutomationStatusChecker(rawAutomationRepo), cacheStore, cfg.Cache.ConfigTTL, log)
	sprintService := sprintsvc.NewCachedSprintService(sprintsvc.New(sprintRepo, taskRepo, publisher), cacheStore, cfg.Cache.SprintTTL, log)
	viewService := sprintsvc.NewCachedViewService(sprintsvc.NewViewService(viewRepo, sprintRepo, taskRepo, publisher), cacheStore, cfg.Cache.SprintTTL, log)
	notificationService := notificationsvc.New(notificationRepo, projectRepo, publisher).
		WithEventPublishing(userRepo, cfg.Server.PublicURL)
	agentService := agentsvc.New(agentRepo, projectService, publisher, pluginRepo)
	// environmentService calls agent-runner via the same AI_AGENT_URL/
	// AI_AGENT_INTERNAL_KEY pair AgentHandler already uses for its fast
	// calls (see environmentsvc.New's doc comment) — not a new config
	// surface — plus StreamAgentEnvironmentCommands (WithRedisClient) for
	// its 3 calls that wait on a Pod/container becoming ready.
	// agentService.WithEnvironmentService below wires it into
	// CreateAgent/UpdateAgent's default_environment_id validation and
	// StartChatSession/StartGlobalChatSession's environment-attach flow.
	environmentService := environmentsvc.New(environmentRepo, cfg.AIAgentURL, cfg.AIAgentInternalKey).
		WithPublisher(publisher).
		WithRedisClient(redisClient)
	agentService = agentService.WithEnvironmentService(environmentService)
	// Backs GetConversationForAgent's agents.read check (read_conversation
	// MCP tool) — see agentsvc.Service.authorizer's doc comment.
	agentService = agentService.WithAuthorizer(authorizer)
	settingsService := settingssvc.New(settingsRepo)
	if cfg.Security.EncryptionKey != "" {
		keyBytes, hexErr := secret.DecodeHexKey(cfg.Security.EncryptionKey)
		if hexErr != nil {
			log.Warn("agent LLM key encryption disabled: invalid ENCRYPTION_KEY", "error", hexErr)
		} else if enc, encErr := secret.NewEncryptor(keyBytes); encErr != nil {
			log.Warn("agent LLM key encryption disabled: encryptor init failed", "error", encErr)
		} else {
			agentService = agentService.WithEncryptor(enc)
			// Same Encryptor instance, reused verbatim — an environment's
			// secret_key_encrypted is encrypted at rest exactly like
			// agents.llm_api_key_secret (see migration 000042's own doc
			// comment on that column, and environmentsvc.Service.WithEncryptor).
			environmentService = environmentService.WithEncryptor(enc)
			log.Info("agent LLM API key at-rest encryption enabled")
		}
	} else {
		// Not fatal — deployments using only ACP-type agents and no plugins
		// with stored secrets have nothing to encrypt. But when this is
		// unintentional, the effect is silent: encryptKey() falls back to
		// storing the plaintext unchanged when no encryptor is configured, so
		// LLM API keys and plugin secrets end up in the database in plaintext
		// with no error or signal anywhere. Surface it once at startup.
		log.Warn("ENCRYPTION_KEY not set: agent LLM API keys and plugin secrets will be stored in plaintext, not encrypted")
	}
	activityService := tasksvc.NewActivityService(activityRepo, taskRepo, projectRepo, publisher).
		WithNotificationService(notificationService).
		WithAgentTrigger(agentService)
	notificationConsumer := worker.NewNotificationConsumer(redisClient, notificationService, log, projectRepo, agentService).
		WithActivityRecorder(activityService)
	activityConsumer := worker.NewActivityConsumer(redisClient, activityRepo, projectRepo, log)
	environmentConsumer := worker.NewEnvironmentCommandConsumer(redisClient, environmentService, log)
	docService := docsvc.New(docRepo, projectRepo)
	docActivityService := docsvc.NewActivityService(docRepo, docRepo, projectRepo, publisher).
		WithNotificationService(notificationService)
	docActivityConsumer := worker.NewDocActivityConsumer(redisClient, docRepo, projectRepo, log)
	automationService := automationsvc.New(automationRepo, taskRepo, projectRepo, publisher)
	automationConsumer := worker.NewAutomationConsumer(redisClient, automationRepo, taskRepo, taskService, activityService, publisher, log)
	dueDateScheduler := worker.NewDueDateScheduler(redisClient, automationConsumer, log)
	cronScheduler := worker.NewCronScheduler(redisClient, automationConsumer, log)
	waitScheduler := worker.NewWaitScheduler(redisClient, automationConsumer, log)

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

	attachmentService := attachmentsvc.New(attachmentRepo, attachmentsvc.NewTaskOwnerChecker(taskRepo), attachmentsvc.NewDocOwnerChecker(docRepo), storageClient, cfg.Storage.Bucket)
	// annotationService backs the Paca browser extension's on-page comments
	// (apps/extension) — attachmentRepo satisfies TaskAttachmentLinker
	// directly (its own CreateTaskAttachment), and taskService/
	// environmentService/storageClient are the same instances already
	// wired above, not new ones.
	annotationService := annotationsvc.New(annotationRepo, environmentService, taskService, attachmentRepo, attachmentRepo, storageClient, cfg.Storage.Bucket).
		WithPublicURL(cfg.Server.PublicURL)
	userService = userService.WithAvatarService(attachmentService)
	agentService = agentService.WithAvatarService(attachmentService)
	// Unlike userService/agentService above, this return value isn't
	// reassigned: projectService (the cached wrapper built from
	// projectServiceBase back at its construction) already holds this same
	// *Service pointer, and WithAvatarService mutates it in place, so the
	// config takes effect through projectService too. Reassigning here would
	// itself go unused (and trip staticcheck's SA4006) since projectServiceBase
	// is never read again after this line.
	projectServiceBase.WithAvatarService(attachmentService)
	settingsService.WithAvatarService(attachmentService)

	// --- API Key management -------------------------------------------------
	apiKeyRepo := pgRepo.NewAPIKeyRepository(db)
	apiKeyService := apikeysvc.New(apiKeyRepo)
	// Configure the static agent API key so the AI agent service can
	// authenticate without a database-stored key entry.
	if cfg.Security.AgentAPIKey != "" {
		apiKeyService.WithAgentKey(cfg.Security.AgentAPIKey, agentBotUserID)
	}
	// Wires the DB-backed X-Agent-ID / X-Actor-User-ID verification the
	// authn middleware needs (middleware.AgentIdentityVerifier) — without
	// this, agentRepo stays nil on apiKeyService and every agent/actor claim
	// fails closed (see apikeysvc.Service.FindAgentByID's nil-store branch).
	apiKeyService.WithAgentIdentityStore(agentRepo)

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

	// settingsReader resolves live branding for the paca.settings_get host
	// function so plugin-rendered content reflects an admin's logo/color
	// changes immediately, rather than a value baked in earlier — see
	// pluginrt.SettingsReader's doc comment.
	settingsReader := pluginrt.SettingsReaderFunc(func(ctx context.Context) (pluginrt.BrandingSnapshot, error) {
		ws, err := settingsService.Get(ctx)
		if err != nil {
			return pluginrt.BrandingSnapshot{}, err
		}
		var snap pluginrt.BrandingSnapshot
		if ws.BrandName != nil {
			snap.BrandName = *ws.BrandName
		}
		if ws.PrimaryColorLight != nil {
			snap.PrimaryColorLight = *ws.PrimaryColorLight
		}
		if ws.PrimaryColorDark != nil {
			snap.PrimaryColorDark = *ws.PrimaryColorDark
		}
		if logoURL, _ := attachmentService.ResolveAvatarURL(ctx, ws.LogoKey); logoURL != nil {
			snap.LogoURL = *logoURL
		}
		return snap, nil
	})

	// passwordSetTokenIssuer backs the paca.password_set_token_issue host
	// function with userService's existing token issuance — a plugin calls
	// this on demand, per user_id, rather than the raw token ever being
	// embedded in the user.created event payload every subscriber receives.
	passwordSetTokenIssuer := pluginrt.PasswordSetTokenIssuerFunc(userService.IssuePasswordSetToken)

	pluginRuntime := pluginrt.NewRuntime(pluginStore, pluginrt.HostServices{
		DB:                     sqlDB,
		Log:                    log,
		Publisher:              publisher,
		HTTPClient:             &http.Client{Timeout: 30 * time.Second},
		Authorizer:             authorizer,
		Cache:                  cacheStore,
		SettingsReader:         settingsReader,
		PasswordSetTokenIssuer: passwordSetTokenIssuer,
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
	pluginInstaller := pluginrt.NewInstaller(cfg.Plugins.WASMDir, cfg.Plugins.FrontendDir, cfg.Plugins.MCPDir, cfg.Plugins.SkillsDir, installerHTTPClient, log)

	pluginService := pluginsvc.New(pluginRepo).WithHostVersion(cfg.Release.Version)

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

	// Wire the plugin runtime into the automation engine so
	// plugin-contributed trigger/condition/action node types validate and
	// execute: automationService.WithPluginNodeResolver gates node
	// creation/update (rejecting unrecognized types), automationConsumer.
	// WithPluginRuntime dispatches plugin condition/action nodes mid-walk via
	// the same EvaluateCondition/RunAction WASM bridge HandleRequest uses.
	automationService.WithPluginNodeResolver(pluginRuntime)
	automationConsumer.WithPluginRuntime(pluginRuntime)
	// trigger_ai_agent starts an agent conversation without ever touching the
	// task's assignee: task-bound, it dispatches straight to
	// agentService.TriggerTaskAssigned; task-less (a cron/api_trigger/
	// predecessor_done trigger with no target task configured) it instead
	// fires a standalone message via agentService.TriggerDirectMessage.
	// Either way, projectRepo resolves the configured member to its AgentID.
	automationConsumer.WithAgentMessaging(projectRepo, agentService)
	// update_sprint/complete_sprint dispatch through sprintService (not
	// sprintRepo directly) so they get the same validation/event-publishing
	// side effects as an HTTP-driven change; sprintRepo alone backs
	// resolveSprintFor's task.SprintID lookup for a Task-triggered walk
	// reaching down into its own sprint.
	automationConsumer.WithSprintService(sprintRepo, sprintService)

	// Reads the same StreamAgentConversationStatus stream automationConsumer
	// does (its own independent consumer group), advancing an agent's
	// parallelism queue whenever one of its conversations reaches a terminal
	// status — see worker.AgentQueueConsumer's doc comment.
	agentQueueConsumer := worker.NewAgentQueueConsumer(redisClient, agentRepo, agentService, log)

	// Forward every recorded activity (task created/updated/deleted, comments,
	// links, etc.) to subscribed plugins. ActivitySvc appends to the
	// StreamPluginEvents Valkey stream; this consumer reads it back and
	// dispatches to the plugin runtime — the API never calls into the plugin
	// runtime directly when recording an activity.
	pluginEventConsumer := worker.NewPluginEventConsumer(redisClient, pluginRuntime, log)

	pluginHandler := handler.NewPluginHandler(pluginService, pluginRuntime, projectRepo).
		WithRouteAuth(tokenManager, apiKeyService, authorizer).
		WithMarketplace(marketplaceClient, pluginInstaller, pluginMigrationRunner)

	agentHandler := handler.NewAgentHandler(agentService, cfg.AIAgentURL, cfg.AIAgentInternalKey, cfg.Server.PublicURL).
		WithActivityRecorder(activityService).
		WithMemberRepo(projectRepo).
		WithGlobalPermissionReader(permissionStore).
		WithAvatarService(attachmentService).
		WithTaskChecker(attachmentsvc.NewTaskOwnerChecker(taskRepo))
	environmentHandler := handler.NewEnvironmentHandler(environmentService, cfg.AIAgentInternalKey).
		WithDeploymentConfig(cfg.SSHBastionHost, cfg.PortForwardHost)
	annotationHandler := handler.NewAnnotationHandler(annotationService).
		WithAvatarService(attachmentService).
		WithMemberRepo(projectRepo)
	convHandler := handler.NewConversationHandler(agentService).WithMemberRepo(projectRepo)
	automationHandler := handler.NewAutomationHandler(automationService).WithPluginRuntime(pluginRuntime)

	// --- Handlers -----------------------------------------------------------
	cookieCfg := handler.CookieConfig{
		Secure:            cfg.Server.CookieSecure,
		AccessTTL:         cfg.JWT.AccessTTL,
		RefreshTTL:        cfg.JWT.RefreshTTL,
		RefreshSessionTTL: cfg.JWT.RefreshSessionTTL,
	}

	deps := router.Deps{
		TokenManager:         tokenManager,
		APIKeyAuth:           apiKeyService,
		Authorizer:           authorizer,
		Health:               handler.NewHealthHandler(),
		Version:              handler.NewVersionHandler(cfg.Release, cacheStore, log),
		Auth:                 handler.NewAuthHandler(authService, cookieCfg),
		User:                 handler.NewUserHandler(userService, authService).WithAvatarService(attachmentService),
		GlobalRole:           handler.NewGlobalRoleHandler(globalRoleService),
		ProjectVisibilitySvc: projectService,
		Project: handler.NewProjectHandler(
			projectService,
			authorizer,
			handler.WithProjectDefaultViews(viewService, taskService),
			handler.WithProjectStatsServices(taskService, userService),
			handler.WithProjectAvatarService(attachmentService),
		),
		Task: handler.NewTaskHandler(taskService, viewService, activityService,
			handler.WithTaskPublisher(publisher),
			handler.WithTaskAssignedProjectService(projectService),
			handler.WithTaskAvatarService(attachmentService),
			handler.WithTaskNotificationService(notificationService)),
		Sprint: handler.NewSprintHandler(sprintService, viewService,
			handler.WithSprintDefaultTaskTypes(taskService),
			handler.WithSprintDefaultTaskStatuses(taskService),
		),
		View:       handler.NewViewHandler(viewService),
		Attachment: handler.NewAttachmentHandler(attachmentService),
		Document: handler.NewDocumentHandler(docService, docActivityService).
			WithDocAvatarService(attachmentService).
			WithDocNotificationService(notificationService),
		DocFile:            handler.NewDocFileHandler(attachmentService),
		Notification:       handler.NewNotificationHandler(notificationService, handler.WithNotificationAvatarService(attachmentService)),
		APIKey:             handler.NewAPIKeyHandler(apiKeyService),
		Skills:             handler.NewSkillsHandler(pluginService, cfg.Plugins.SkillsDir),
		Plugin:             pluginHandler,
		Agent:              agentHandler,
		Environment:        environmentHandler,
		Annotation:         annotationHandler,
		Conversation:       convHandler,
		Automation:         automationHandler,
		Settings:           handler.NewSettingsHandler(settingsService).WithAvatarService(attachmentService),
		Log:                log,
		CORSAllowedOrigins: cfg.Server.CORSAllowedOrigins,
	}

	engine := router.New(deps)

	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &App{server: srv, publisher: publisher, activityConsumer: activityConsumer, docActivityConsumer: docActivityConsumer, notificationConsumer: notificationConsumer, pluginEventConsumer: pluginEventConsumer, environmentConsumer: environmentConsumer, automationConsumer: automationConsumer, agentQueueConsumer: agentQueueConsumer, dueDateScheduler: dueDateScheduler, cronScheduler: cronScheduler, waitScheduler: waitScheduler, log: log}, nil
}

// Run starts the activity consumers and the HTTP server.
// It returns when the server stops.
func (a *App) Run() error {
	a.log.Info("starting server", "addr", a.server.Addr)
	a.activityConsumer.Start(context.Background())
	a.docActivityConsumer.Start(context.Background())
	a.notificationConsumer.Start(context.Background())
	a.pluginEventConsumer.Start(context.Background())
	a.environmentConsumer.Start(context.Background())
	a.automationConsumer.Start(context.Background())
	a.agentQueueConsumer.Start(context.Background())
	a.dueDateScheduler.Start(context.Background())
	a.cronScheduler.Start(context.Background())
	a.waitScheduler.Start(context.Background())
	return a.server.ListenAndServe()
}

// Shutdown gracefully stops the server with the given timeout.
func (a *App) Shutdown(ctx context.Context) error {
	a.log.Info("shutting down server")
	a.activityConsumer.Stop()
	a.docActivityConsumer.Stop()
	a.notificationConsumer.Stop()
	a.pluginEventConsumer.Stop()
	a.environmentConsumer.Stop()
	a.automationConsumer.Stop()
	a.agentQueueConsumer.Stop()
	a.dueDateScheduler.Stop()
	a.cronScheduler.Stop()
	a.waitScheduler.Stop()
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
