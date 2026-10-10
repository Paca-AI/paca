// Package bootstrap wires up all application dependencies and exposes a
// runnable *App.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/bootstrap/defaultroles"
	"github.com/Paca-AI/api/internal/config"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/platform/cache"
	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/internal/platform/logger"
	"github.com/Paca-AI/api/internal/platform/messaging"
	"github.com/Paca-AI/api/internal/platform/netguard"
	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	"github.com/Paca-AI/api/internal/platform/secret"
	"github.com/Paca-AI/api/internal/platform/storage"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	redisRepo "github.com/Paca-AI/api/internal/repository/redis"
	activitysvc "github.com/Paca-AI/api/internal/service/activity"
	agentsvc "github.com/Paca-AI/api/internal/service/agent"
	annotationsvc "github.com/Paca-AI/api/internal/service/annotation"
	apikeysvc "github.com/Paca-AI/api/internal/service/apikey"
	attachmentsvc "github.com/Paca-AI/api/internal/service/attachment"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	automationsvc "github.com/Paca-AI/api/internal/service/automation"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
	environmentsvc "github.com/Paca-AI/api/internal/service/environment"
	exportsvc "github.com/Paca-AI/api/internal/service/export"
	notificationsvc "github.com/Paca-AI/api/internal/service/notification"
	pluginsvc "github.com/Paca-AI/api/internal/service/plugin"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	rolesvc "github.com/Paca-AI/api/internal/service/role"
	settingssvc "github.com/Paca-AI/api/internal/service/settings"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	ssosvc "github.com/Paca-AI/api/internal/service/sso"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/Paca-AI/api/internal/transport/triggergate"
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
	server                 *http.Server
	publisher              *messaging.Publisher
	activityConsumer       *worker.ActivityConsumer
	notificationConsumer   *worker.NotificationConsumer
	pluginEventConsumer    *worker.PluginEventConsumer
	environmentConsumer    *worker.EnvironmentCommandConsumer
	projectExportConsumer  *worker.ProjectExportConsumer
	automationConsumer     *worker.AutomationConsumer
	taskAutofillConsumer   *worker.TaskAutofillConsumer
	taskAutoAssignConsumer *worker.TaskAutoAssignConsumer
	agentQueueConsumer     *worker.AgentQueueConsumer
	dueDateScheduler       *worker.DueDateScheduler
	cronScheduler          *worker.CronScheduler
	waitScheduler          *worker.WaitScheduler
	log                    *slog.Logger
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
	// Every authorization decision (route gates, plugin permission checks,
	// effective-permission listings) goes through the IAM authorizer.
	authorizer := pgRepo.NewIAMAuthorizer(db)
	// The "my platform actions" listings, answered by the same engine.
	permissionStore := platformActionsReader{a: authorizer}

	// --- Repositories -------------------------------------------------------
	userRepo := pgRepo.NewUserRepository(db)
	// IAM roles and attachments. The service invalidates the authorizer's
	// cached policies after every committed change, and simulates through the
	// same engine and action registry the gates use.
	roleRepo := pgRepo.NewRoleRepository(db)
	roleService := rolesvc.New(roleRepo, authorizer, authorizer, authorizer.Registry(), authorizer.Schema())
	projectRepo := pgRepo.NewProjectRepository(db)
	taskRepo := pgRepo.NewTaskRepository(db)
	// The activity log: one repository and one service behind every
	// entity's timeline, the project feed, the agent tab and comments, and
	// the one Recorder every domain service records its changes through.
	activityRecorder := activitysvc.NewRecorder(publisher)
	activityLog := activitysvc.New(pgRepo.NewActivityRepository(db), projectRepo, activityRecorder)
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

	// --- Role and admin seeding ---------------------------------------------
	// The shipped platform roles must exist (as system roles) before the
	// bootstrap accounts are given SUPER_ADMIN. Every step is idempotent and
	// touches only the shipped roles and the bootstrap accounts.
	roleSeeder := pgRepo.NewRoleSeedRepository(db)
	shippedRoles, err := seedPlatformRoles(context.Background(), roleSeeder, authorizer, log)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	superAdminRoleID := shippedRoles[defaultroles.SuperAdmin]
	if err := seedAdmin(context.Background(), userRepo, roleSeeder, superAdminRoleID, cfg.Admin, log); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if err := seedAgentBotUser(context.Background(), userRepo, roleSeeder, superAdminRoleID, log); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	// --- Services -----------------------------------------------------------
	authService := authsvc.New(userRepo, tokenManager, refreshStore, cfg.JWT.RefreshTTL, cfg.JWT.RefreshSessionTTL)
	userService := usersvc.New(userRepo, permissionStore).
		WithPasswordSetTokenRepo(passwordSetTokenRepo).
		WithEventPublishing(publisher)
	agentRepo := pgRepo.NewAgentRepository(db)
	environmentRepo := pgRepo.NewEnvironmentRepository(db)
	annotationRepo := pgRepo.NewAnnotationRepository(db)
	projectServiceBase := projectsvc.New(projectRepo, taskRepo, agentRepo).WithActivityRecorder(activityRecorder)
	projectServiceBase = projectServiceBase.WithRoleInvalidator(authorizer)
	projectService := projectsvc.NewCachedService(projectServiceBase, cacheStore, cfg.Cache.ProjectTTL, log)
	// Member lists show each member's roles, so changing them drops the cache.
	roleService = roleService.WithMembersCache(projectService)
	taskService := tasksvc.NewCachedService(tasksvc.New(taskRepo).WithAutomationStatusChecker(rawAutomationRepo).WithActivityRecorder(activityRecorder), cacheStore, cfg.Cache.ConfigTTL, log)
	sprintService := sprintsvc.NewCachedSprintService(sprintsvc.New(sprintRepo, taskRepo, publisher), cacheStore, cfg.Cache.SprintTTL, log)
	viewService := sprintsvc.NewCachedViewService(sprintsvc.NewViewService(viewRepo, sprintRepo, taskRepo, publisher), cacheStore, cfg.Cache.SprintTTL, log)
	notificationService := notificationsvc.New(notificationRepo, projectRepo, publisher).
		WithEventPublishing(userRepo, cfg.Server.PublicURL).
		WithTitleLookup(taskRepo, docRepo)
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
	// Every agent run started without an HTTP request of its own (task
	// assignment, automation, comment mention) and the description-write
	// trigger go through this gate — see package triggergate. The agent
	// service itself carries no authorization for them.
	triggerGate := triggergate.New(agentService, authorizer, projectRepo, httpmw.AgentRepoLookups{Repo: agentRepo})
	settingsService := settingssvc.New(settingsRepo)
	// encryptor is reused, verbatim, for every at-rest secret in this
	// codebase (agent LLM keys, environment secrets, and — see below —
	// each project's own Jev API key): a nil encryptor (ENCRYPTION_KEY
	// unset) is a valid, non-fatal state that every WithEncryptor-style
	// caller falls back to storing/reading plaintext for.
	var encryptor *secret.Encryptor
	if cfg.Security.EncryptionKey != "" {
		keyBytes, hexErr := secret.DecodeHexKey(cfg.Security.EncryptionKey)
		if hexErr != nil {
			log.Warn("agent LLM key encryption disabled: invalid ENCRYPTION_KEY", "error", hexErr)
		} else if enc, encErr := secret.NewEncryptor(keyBytes); encErr != nil {
			log.Warn("agent LLM key encryption disabled: encryptor init failed", "error", encErr)
		} else {
			encryptor = enc
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
		// LLM API keys, plugin secrets, and per-project Jev API keys end up
		// in the database in plaintext with no error or signal anywhere.
		// Surface it once at startup.
		log.Warn("ENCRYPTION_KEY not set: agent LLM API keys, plugin secrets, and project Jev API keys will be stored in plaintext, not encrypted")
	}
	// Reassigned (unlike agentService/environmentService above) because
	// projectServiceBase is a local var read again below when wiring
	// ProjectHandler's Jev-config option — WithEncryptor mutates in place,
	// so this is belt-and-suspenders, not load-bearing, but keeps the
	// pointer's provenance obvious at every read site.
	projectServiceBase = projectServiceBase.WithEncryptor(encryptor)
	ssoService := ssosvc.New(pgRepo.NewSSORepository(db), redisRepo.NewSSOStateStore(redisClient, 10*time.Minute),
		userRepo, userService, authService, log).WithEncryptor(encryptor)
	activityService := tasksvc.NewActivityService(activityLog, taskRepo, projectRepo).
		WithNotificationService(notificationService).
		WithAgentTrigger(triggerGate)
	notificationConsumer := worker.NewNotificationConsumer(redisClient, notificationService, log, projectRepo, triggerGate).
		WithActivityRecorder(activityService)
	activityConsumer := worker.NewActivityConsumer(redisClient, pgRepo.NewActivityRepository(db), projectRepo, log)
	environmentConsumer := worker.NewEnvironmentCommandConsumer(redisClient, environmentService, log)
	docService := docsvc.New(docRepo, projectRepo)
	docActivityService := docsvc.NewActivityService(activityLog, docRepo)
	automationService := automationsvc.New(automationRepo, taskRepo, projectRepo, publisher)
	automationConsumer := worker.NewAutomationConsumer(redisClient, automationRepo, taskRepo, taskService, activityService, publisher, log)
	taskAutofillConsumer := worker.NewTaskAutofillConsumer(redisClient, taskService, taskRepo, projectService, activityService, encryptor, log)
	taskAutoAssignConsumer := worker.NewTaskAutoAssignConsumer(redisClient, taskService, projectRepo, projectService, activityService, encryptor, log)
	dueDateScheduler := worker.NewDueDateScheduler(redisClient, automationConsumer, log)
	cronScheduler := worker.NewCronScheduler(redisClient, automationConsumer, log)
	waitScheduler := worker.NewWaitScheduler(redisClient, automationConsumer, log)

	// Object storage — defaults to RustFS; switches to AWS S3 when STORAGE_PROVIDER=s3.
	storageClient, err := storage.NewS3Client(context.Background(), storage.S3Config{
		Endpoint:        cfg.Storage.Endpoint,
		PublicURL:       cfg.Storage.PublicURL,
		Region:          cfg.Storage.Region,
		Bucket:          cfg.Storage.Bucket,
		AccessKeyID:     cfg.Storage.AccessKeyID,
		SecretAccessKey: cfg.Storage.SecretAccessKey,
		UseSSL:          cfg.Storage.UseSSL,
		ForcePathStyle:  cfg.Storage.Provider != "s3", // self-hosted S3-compatible stores require path-style
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
	// projectExportService builds asynchronous project exports (a zip of tasks,
	// task comments/activities and docs):
	// the handler only queues them on a Valkey stream, projectExportConsumer
	// runs them and uploads the file to the same object store attachments use.
	projectExportService := exportsvc.New(pgRepo.NewProjectExportRepository(db), projectRepo, projectRepo, taskRepo, sprintRepo, docRepo, pgRepo.NewActivityRepository(db), storageClient, cfg.Storage.Bucket, publisher, log).WithPublicURL(cfg.Server.PublicURL)
	projectExportConsumer := worker.NewProjectExportConsumer(redisClient, projectExportService, log)
	// annotationService backs the Paca browser extension's on-page comments
	// (apps/extension) — attachmentRepo satisfies TaskAttachmentLinker
	// directly (its own CreateTaskAttachment), and taskService/
	// environmentService/storageClient are the same instances already
	// wired above, not new ones.
	annotationService := annotationsvc.New(annotationRepo, environmentService, taskService, attachmentRepo, attachmentRepo, storageClient, cfg.Storage.Bucket).
		WithPublicURL(cfg.Server.PublicURL).
		WithActivityRecorder(activityService).
		WithActivityLog(activityRecorder)
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
		DB:        sqlDB,
		Log:       log,
		Publisher: publisher,
		// netguard.NewSafeHTTPClient pins the dial to the exact IP validated
		// by isAllowedFetchDomain (runtime.go), closing a DNS-rebinding gap:
		// a plain client re-resolves DNS independently at dial time, so an
		// answer that differs from (or changes after) the check bypasses it
		// entirely — the same vulnerability class as GHSA-cj3q-c44j-q8p9,
		// just a different call site. marketplace.go and installer.go
		// already use netguard for their own clients; this one (paca.fetch,
		// used by every installed plugin's outbound calls) was the one
		// netguard's own package doc says it was built for but never got
		// wired up.
		HTTPClient:             netguard.NewSafeHTTPClient(30 * time.Second),
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

	pluginService := pluginsvc.New(pluginRepo).WithHostVersion(cfg.Release.Version).WithActionRegistry(authorizer.Registry())

	// Make the actions installed plugins declare known to role validation
	// (a clash between two plugins is logged; the rest are still registered).
	if err := pluginService.SyncActions(context.Background()); err != nil {
		log.Error("plugin: registering declared actions", "error", err)
	}

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
	automationConsumer.WithJevProjectService(projectService, encryptor)
	// trigger_ai_agent starts an agent conversation without ever touching the
	// task's assignee: task-bound, it dispatches straight to
	// agentService.TriggerTaskAssigned; task-less (a cron/api_trigger/
	// predecessor_done trigger with no target task configured) it instead
	// fires a standalone message via agentService.TriggerDirectMessage.
	// Either way, projectRepo resolves the configured member to its AgentID.
	automationConsumer.WithAgentMessaging(projectRepo, triggerGate)
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

	agentHandler := handler.NewAgentHandler(triggerGate.WrapService(agentService), cfg.AIAgentURL, cfg.AIAgentInternalKey, cfg.Server.PublicURL).
		WithActivityRecorder(activityService).
		WithActivityLister(activityLog).
		WithMemberRepo(projectRepo).
		WithListScoper(authorizer).
		WithGlobalPermissionReader(permissionStore).
		WithAvatarService(attachmentService).
		WithTaskChecker(attachmentsvc.NewTaskOwnerChecker(taskRepo)).
		WithJevProjectService(projectService, encryptor)
	environmentHandler := handler.NewEnvironmentHandler(environmentService, cfg.AIAgentInternalKey).
		WithListScoper(authorizer).
		WithDeploymentConfig(cfg.SSHBastionHost, cfg.PortForwardHost)
	annotationHandler := handler.NewAnnotationHandler(annotationService).
		WithAnnotationListScoper(authorizer).
		WithAvatarService(attachmentService).
		WithMemberRepo(projectRepo)
	convHandler := handler.NewConversationHandler(agentService).WithMemberRepo(projectRepo).WithConversationListScoper(authorizer)
	automationHandler := handler.NewAutomationHandler(automationService).WithPluginRuntime(pluginRuntime).WithAutomationListScoper(authorizer)

	// --- Handlers -----------------------------------------------------------
	cookieCfg := handler.CookieConfig{
		Secure:            cfg.Server.CookieSecure,
		AccessTTL:         cfg.JWT.AccessTTL,
		RefreshTTL:        cfg.JWT.RefreshTTL,
		RefreshSessionTTL: cfg.JWT.RefreshSessionTTL,
	}

	authHandler := handler.NewAuthHandler(authService, cookieCfg)
	deps := router.Deps{
		TokenManager:         tokenManager,
		APIKeyAuth:           apiKeyService,
		IAM:                  authorizer,
		AgentEnvironments:    httpmw.AgentRepoLookups{Repo: agentRepo},
		MemberPrincipals:     httpmw.MemberRepoLookup{Repo: projectRepo},
		TaskNumbers:          taskRepo,
		SessionEnvironments:  httpmw.AgentRepoLookups{Repo: agentRepo},
		Health:               handler.NewHealthHandler(),
		Version:              handler.NewVersionHandler(cfg.Release, cacheStore, log),
		Auth:                 authHandler,
		SSO:                  handler.NewSSOHandler(ssoService, authHandler, cfg.Server.PublicURL),
		User:                 handler.NewUserHandler(userService, authService).WithAvatarService(attachmentService),
		Role:                 handler.NewRoleHandler(roleService),
		RoleAttachments:      httpmw.NewRoleServiceAttachments(roleService),
		ProjectVisibilitySvc: projectService,
		ProjectActivity:      handler.NewProjectActivityHandler(activityLog, attachmentService),
		ProjectExport:        handler.NewProjectExportHandler(projectExportService),
		Project: handler.NewProjectHandler(
			projectService,
			authorizer,
			handler.WithProjectDefaultViews(viewService, taskService),
			handler.WithProjectStatsServices(taskService, userService),
			handler.WithProjectTaskScoper(authorizer),
			handler.WithProjectAvatarService(attachmentService),
			// projectService, the cached wrapper — not projectServiceBase.
			// UpdateJevConfig writes credentials that every read path then
			// reads back off a cached *projectdom.Project (this handler's own
			// TestJevConfig/GetProject, and the Jev-dependent workers), so a
			// write through the base service would leave save-then-test
			// exercising the credentials the caller just replaced. The
			// wrapper's UpdateJevConfig delegates and drops the cache entry;
			// it picks up the method by assertion, so projectdom.Service
			// still doesn't carry it and its mocks are unaffected (see
			// WithProjectJevConfigService's doc comment).
			handler.WithProjectJevConfigService(projectService, encryptor),
		),
		Task: handler.NewTaskHandler(taskService, viewService, activityService,
			handler.WithTaskListScoper(authorizer),
			handler.WithTaskPublisher(publisher),
			handler.WithTaskAssignedProjectService(projectService),
			handler.WithTaskAvatarService(attachmentService),
			handler.WithTaskNotificationService(notificationService),
			handler.WithTaskAutofillRepository(taskRepo)),
		Sprint: handler.NewSprintHandler(sprintService, viewService,
			handler.WithSprintListScoper(authorizer),
			handler.WithSprintDefaultTaskTypes(taskService),
			handler.WithSprintDefaultTaskStatuses(taskService),
		),
		View:       handler.NewViewHandler(viewService).WithViewListScoper(authorizer),
		Attachment: handler.NewAttachmentHandler(attachmentService),
		Document: handler.NewDocumentHandler(docService, docActivityService).
			WithDocListScoper(authorizer).
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
		AuthRateLimit:      cfg.Server.AuthRateLimit,
	}

	engine := router.New(deps)

	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &App{server: srv, publisher: publisher, activityConsumer: activityConsumer, notificationConsumer: notificationConsumer, pluginEventConsumer: pluginEventConsumer, environmentConsumer: environmentConsumer, projectExportConsumer: projectExportConsumer, automationConsumer: automationConsumer, taskAutofillConsumer: taskAutofillConsumer, taskAutoAssignConsumer: taskAutoAssignConsumer, agentQueueConsumer: agentQueueConsumer, dueDateScheduler: dueDateScheduler, cronScheduler: cronScheduler, waitScheduler: waitScheduler, log: log}, nil
}

// platformActionsReader lists a user's or agent's effective platform-level
// IAM actions (iam.Authorizer.EffectiveActions with no project) for the
// /users/me and /agents/me global-permissions endpoints.
type platformActionsReader struct{ a *iam.Authorizer }

func (r platformActionsReader) list(ctx context.Context, p iam.Principal) ([]iam.Action, error) {
	acts, err := r.a.EffectiveActions(ctx, p, "")
	if err != nil {
		return nil, err
	}
	out := make([]iam.Action, len(acts))
	for i, a := range acts {
		out[i] = iam.Action(a)
	}
	return out, nil
}

// ListGlobalPermissions lists a user's platform-level actions.
func (r platformActionsReader) ListGlobalPermissions(ctx context.Context, userID uuid.UUID) ([]iam.Action, error) {
	return r.list(ctx, iam.User(userID.String()))
}

// ListAgentGlobalPermissions lists an agent's platform-level actions.
func (r platformActionsReader) ListAgentGlobalPermissions(ctx context.Context, agentID uuid.UUID) ([]iam.Action, error) {
	return r.list(ctx, iam.Agent(agentID.String()))
}

// Run starts the activity consumers and the HTTP server.
// It returns when the server stops.
func (a *App) Run() error {
	a.log.Info("starting server", "addr", a.server.Addr)
	a.activityConsumer.Start(context.Background())
	a.notificationConsumer.Start(context.Background())
	a.pluginEventConsumer.Start(context.Background())
	a.environmentConsumer.Start(context.Background())
	a.projectExportConsumer.Start(context.Background())
	a.automationConsumer.Start(context.Background())
	a.taskAutofillConsumer.Start(context.Background())
	a.taskAutoAssignConsumer.Start(context.Background())
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
	a.notificationConsumer.Stop()
	a.pluginEventConsumer.Stop()
	a.environmentConsumer.Stop()
	a.projectExportConsumer.Stop()
	a.automationConsumer.Stop()
	a.taskAutofillConsumer.Stop()
	a.taskAutoAssignConsumer.Stop()
	a.agentQueueConsumer.Stop()
	a.dueDateScheduler.Stop()
	a.cronScheduler.Stop()
	a.waitScheduler.Stop()
	if a.publisher != nil {
		a.publisher.Close()
	}
	return a.server.Shutdown(ctx)
}
