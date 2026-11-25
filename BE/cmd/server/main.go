package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/buildinfo"
	"github.com/kuayle/kuayle-backend/internal/clientip"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/mail"
	"github.com/kuayle/kuayle-backend/pkg/storage"
)

func main() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	// Handle CLI subcommands
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(buildinfo.Version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		runMigrate(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		runBackupCommand(os.Args[2:])
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "vapid-keys" {
		privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", publicKey, privateKey)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Database
	db, err := repository.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Settings -> Instance configuration: sysadmin overrides of the variables
	// a running server reads per use (internal/config/registry.go). The
	// readers below ask it on every use, so a change needs no restart.
	//
	// The restart-only settings a sysadmin applied (KUA-311) are merged over
	// the environment here, before anything reads cfg. The API server also
	// owns the state of the apply that started it: it confirms the run once
	// healthy (below) or rolls it back. The automation worker only reads.
	instanceConfigRepo := repository.NewInstanceConfigRepository(db)
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	startup := service.ResolveStartup(startupCtx, instanceConfigRepo, cfg, !(len(os.Args) == 2 && os.Args[1] == "automation-worker"), time.Now, log.StandardLogger())
	cancelStartup()
	cfg = startup.Config
	// requestRestart makes the server exit; the supervisor starts it again.
	restartCtx, requestRestart := context.WithCancel(context.Background())
	defer requestRestart()
	instanceConfig := service.NewInstanceConfigService(cfg, instanceConfigRepo)
	instanceConfig.SetRestart(service.NewInstanceRestart(instanceConfigRepo, startup, cfg.RestartSupervised, requestRestart))

	workspaceRepo := repository.NewWorkspaceRepository(db)
	hub := realtime.NewHub(workspaceRepo.RealtimeAccess)
	conversationRepo := repository.NewConversationRepository(db)
	// Dev Machine control-plane store
	devMachineRepo := repository.NewDevMachineRepository(db)

	// Dev Machine agent registry
	devMachineAgentReg := agent.NewRegistry(
		agent.NewClaudeCodeProvider(cfg.DevMachine.ClaudeCodeImage),
		agent.NewOpenCodeProvider(cfg.DevMachine.OpenCodeImage),
		agent.NewCodexProvider(cfg.DevMachine.CodexImage),
		agent.NewPiProvider(cfg.DevMachine.HeadlessImage),
		agent.NewQwenCodeProvider(cfg.DevMachine.HeadlessImage),
		agent.NewCrushProvider(cfg.DevMachine.HeadlessImage),
		agent.NewKimiProvider(cfg.DevMachine.HeadlessImage),
		agent.NewGooseProvider(cfg.DevMachine.HeadlessImage),
		agent.NewCustomCLIProvider(cfg.DevMachine.CustomImage),
	)

	var devMachineEncKey []byte
	if cfg.DevMachine.EncryptionKey != "" {
		devMachineEncKey = crypto.DeriveKey(cfg.DevMachine.EncryptionKey)
	}
	devMachineSvc := service.NewDevMachineService(
		devMachineRepo, devMachineAgentReg, cfg.DevMachine.Enabled, cfg.DevMachine.Domain,
		devMachineEncKey, time.Duration(cfg.DevMachine.TicketTTLSeconds)*time.Second,
		service.DevMachineImages{
			IDE: cfg.DevMachine.IDEImage, Headless: cfg.DevMachine.HeadlessImage, Browser: cfg.DevMachine.BrowserImage,
			Collector: cfg.DevMachine.CollectorImage,
			Egress:    cfg.DevMachine.EgressImage,
			Docker:    cfg.DevMachine.DockerImage,
		}, cfg.FrontendURL,
	)
	devMachineSvc.SetModelDiscovery(cfg.AIAllowPrivateNetwork)
	devMachineSvc.SetDiskQuotaMode(cfg.DevMachine.EffectiveDiskQuotaMode())
	// Public demo mode keeps Dev Machines for SYSADMINS. The route guard
	// covers the Dev Machine routes; every service that reaches a machine
	// another way (assistant tools and approvals, custom tool runs, the task
	// service behind automations, follow-ups and workers) gets the same rule.
	machineAccess := service.NewMachineAccess(cfg.DemoDevMachineAllowed)
	devMachineSvc.SetMachineAccess(machineAccess)

	agentTaskSvc := service.NewAgentTaskService(repository.NewAgentTaskRepository(db), devMachineSvc, hub)
	// Tasks that pin a configurable workflow follow it (KUA-95).
	workflowRepo := repository.NewWorkflowRepository(db)
	agentTaskSvc.SetWorkflows(workflowRepo)
	agentTaskSvc.SetPipelineSwitch(instanceConfig.PipelinesEnabled)
	automationSvc := service.NewAgentAutomationService(repository.NewAgentAutomationRepository(db), conversationRepo, agentTaskSvc)
	if len(os.Args) == 2 && os.Args[1] == "automation-worker" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		automationSvc.Run(ctx)
		return
	}

	// Repositories
	userRepo := repository.NewUserRepository(db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	patRepo := repository.NewPersonalAccessTokenRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	issueRepo := repository.NewIssueRepository(db)
	labelRepo := repository.NewLabelRepository(db)
	commentRepo := repository.NewCommentRepository(db)
	projectRepo := repository.NewProjectRepository(db)
	notifRepo := repository.NewNotificationRepository(db)
	historyRepo := repository.NewIssueHistoryRepository(db)
	relationRepo := repository.NewIssueRelationRepository(db)
	templateRepo := repository.NewIssueTemplateRepository(db)
	viewRepo := repository.NewViewRepository(db)
	cycleRepo := repository.NewCycleRepository(db)
	teamStatusRepo := repository.NewTeamStatusRepository(db)
	visibilityRepo := repository.NewProjectStatusVisibilityRepository(db)
	favRepo := repository.NewFavoriteRepository(db)
	prefsRepo := repository.NewUserPreferencesRepository(db)
	assetRepo := repository.NewAssetRepository(db)
	aiSettingsRepo := repository.NewAISettingsRepository(db)
	inviteLinkRepo := repository.NewWorkspaceInviteLinkRepository(db)
	cliLinkRepo := repository.NewCLILinkRepository(db)
	ssoRepo := repository.NewSSORepository(db)

	// Services
	inviteLinkSvc := service.NewInviteLinkService(inviteLinkRepo, workspaceRepo)
	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg.JWTSecret,
		service.WithPublicDemoMode(cfg.PublicDemoMode),
		service.WithRegistrationSwitch(instanceConfig.RegistrationDisabled),
		service.WithInviteRedeemer(inviteLinkSvc),
		service.WithPasswordReset(repository.NewPasswordResetRepository(db), resetMailer(cfg), cfg.FrontendURL, cfg.Environment == "development"),
	)
	signInTokenRepo := repository.NewSignInTokenRepository(db)
	authSvc.SetSignInTokens(signInTokenRepo)
	ssoSvc := service.NewSSOService(
		ssoRepo, userRepo, workspaceRepo, authSvc,
		crypto.DeriveKey(cfg.JWTSecret+":sso"), cfg.FrontendURL, cfg.OIDCAllowPrivateNetwork,
		service.InstanceSSOConfig{
			WorkspaceSlug:  cfg.SSOInstanceConfig().WorkspaceSlug,
			IssuerURL:      cfg.SSOInstanceConfig().IssuerURL,
			ClientID:       cfg.SSOInstanceConfig().ClientID,
			ClientSecret:   cfg.SSOInstanceConfig().ClientSecret,
			AllowedDomains: cfg.SSOInstanceConfig().AllowedDomainList(),
			DefaultRole:    cfg.SSOInstanceConfig().DefaultRole,
			RequireSSO:     cfg.SSOInstanceConfig().RequireSSO,
		},
	)
	if err := ssoSvc.LoadSAMLSigningKey(cfg.SAMLSigningCertFile, cfg.SAMLSigningKeyFile); err != nil {
		log.Fatal(err)
	}
	// A missing SSO_OIDC_WORKSPACE_SLUG workspace only logs a warning.
	ssoCheckCtx, cancelSSOCheck := context.WithTimeout(context.Background(), 5*time.Second)
	ssoSvc.CheckInstanceWorkspace(ssoCheckCtx)
	cancelSSOCheck()
	// Password and passkey sign-in consult the per-workspace require-SSO rule.
	authSvc.SetSSOEnforcer(ssoSvc)
	workspaceSvc := service.NewWorkspaceService(workspaceRepo, userRepo, hub)
	teamSvc := service.NewTeamService(teamRepo, teamStatusRepo, hub)
	notifSvc := service.NewNotificationService(notifRepo)
	// Workspace webhooks receive issue and comment events from every write
	// path that changes them (KUA-109).
	webhookRepo := repository.NewWebhookRepository(db)
	webhookSvc := service.NewWebhookService(webhookRepo, cfg.JWTSecret)
	issueSvc := service.NewIssueService(issueRepo, teamRepo, teamStatusRepo, historyRepo, hub, notifSvc, projectRepo)
	issueSvc.SetDeleteGracePeriod(cfg.IssueDeleteGracePeriod)
	issueSvc.SetWebhooks(webhookSvc)
	labelSvc := service.NewLabelService(labelRepo, projectRepo)
	commentSvc := service.NewCommentService(commentRepo, issueRepo, hub, notifSvc)
	commentSvc.SetDeleteGracePeriod(cfg.IssueDeleteGracePeriod)
	commentSvc.SetWebhooks(webhookSvc)
	projectSvc := service.NewProjectService(projectRepo)
	relationSvc := service.NewIssueRelationService(relationRepo, issueRepo)
	templateSvc := service.NewIssueTemplateService(templateRepo)
	viewSvc := service.NewViewService(viewRepo, hub)
	cycleSvc := service.NewCycleService(cycleRepo, teamRepo, hub, notifSvc)
	cycleSvc.SetWebhooks(webhookSvc)
	teamStatusSvc := service.NewTeamStatusService(teamStatusRepo, visibilityRepo)
	favSvc := service.NewFavoriteService(favRepo)
	prefsSvc := service.NewPreferencesService(prefsRepo)
	instanceSettings := repository.NewInstanceSettingsRepository(db, crypto.DeriveKey(cfg.JWTSecret+":instance-settings"))
	pushSvc := service.NewPushService(cfg.VAPID, instanceSettings, repository.NewPushSubscriptionRepository(db), prefsSvc, workspaceRepo,
		ssoSvc.RequiresSSO, notifRepo, conversationRepo.Authorize)
	notifSvc.SetPush(pushSvc)
	tokenSvc := service.NewTokenService(patRepo, workspaceRepo)
	// Tokens and connection links refuse workspaces that require SSO unless
	// minted through that workspace's SSO sign-in.
	tokenSvc.SetSSORequirement(ssoSvc)
	cliLinkSvc := service.NewCLILinkService(cliLinkRepo, tokenSvc, cfg.FrontendURL)
	aiSettingsSvc := service.NewAISettingsService(aiSettingsRepo, workspaceRepo, issueRepo, crypto.DeriveKey(cfg.JWTSecret+":ai"), service.WithAIPrivateNetworkAccess(cfg.AIAllowPrivateNetwork))

	// Handlers
	healthH := handler.NewHealthHandler(db)
	loginThrottle := mw.NewLoginThrottle(5, 15*time.Minute)
	authH := handler.NewAuthHandler(authSvc, cfg.Environment != "development", loginThrottle, cfg.IsSysAdmin, cfg.DevMachine.AllowSameSite)
	authH.SetWorkspaceLookup(workspaceRepo)
	workspaceH := handler.NewWorkspaceHandler(workspaceSvc)
	inviteLinkH := handler.NewInviteLinkHandler(inviteLinkSvc, cfg.FrontendURL)
	passkeySvc, err := service.NewPasskeyService(authSvc, userRepo, repository.NewPasskeyRepository(db), passkeyRelyingParty(cfg))
	if err != nil {
		log.Fatalf("failed to set up passkeys: %v", err)
	}
	passkeyH := handler.NewPasskeyHandler(passkeySvc, authH)
	scimH := handler.NewSCIMHandler(service.NewSCIMService(repository.NewSCIMRepository(db), workspaceSvc), cfg.FrontendURL)
	ssoH := handler.NewSSOHandler(ssoSvc, authH, cfg.Environment != "development" || cfg.DevMachine.AllowSameSite)
	configH := handler.NewConfigHandler(false).WithRegistration(authSvc.PublicRegistrationEnabled).
		WithPasskeys(passkeySvc.Enabled()).
		WithSSOInstance(cfg.SSOInstanceConfig().Configured()).
		WithSSOAvailability(ssoSvc.SignInEnabled)
	teamH := handler.NewTeamHandler(teamSvc)
	issueH := handler.NewIssueHandler(issueSvc, commentSvc, userRepo, teamStatusRepo, projectRepo, cycleRepo, relationSvc)
	labelH := handler.NewLabelHandler(labelSvc)
	projectH := handler.NewProjectHandler(projectSvc)
	notifH := handler.NewNotificationHandler(notifSvc)
	wsH := handler.NewWSHandler(hub)
	relationH := handler.NewIssueRelationHandler(relationSvc)
	templateH := handler.NewIssueTemplateHandler(templateSvc)
	viewH := handler.NewViewHandler(viewSvc)
	cycleH := handler.NewCycleHandler(cycleSvc)
	teamStatusH := handler.NewTeamStatusHandler(teamStatusSvc)
	favH := handler.NewFavoriteHandler(favSvc)
	prefsH := handler.NewPreferencesHandler(prefsSvc)
	tokenH := handler.NewTokenHandler(tokenSvc)
	cliLinkH := handler.NewCLILinkHandler(cliLinkSvc)
	aiSettingsH := handler.NewAISettingsHandler(aiSettingsSvc)
	conversationSvc := service.NewConversationService(conversationRepo, aiSettingsSvc)
	conversationSvc.SetPreferences(prefsRepo)
	assistantTools := service.NewAssistantTools(issueRepo, teamRepo, workspaceRepo, issueSvc)
	assistantTools.SetIssueLabels(labelRepo)
	assistantTools.SetIssueFieldLookups(workspaceRepo, cycleRepo, projectRepo)
	assistantToolSettings := repository.NewAssistantToolSettingsRepository(db)
	assistantTools.SetSettings(assistantToolSettings)
	wikiRepo := repository.NewWikiRepository(db)
	wikiSvc := service.NewWikiService(wikiRepo, hub)
	wikiSvc.SetDeleteGracePeriod(cfg.IssueDeleteGracePeriod)
	wikiSvc.SetOverviewAI(aiSettingsSvc)
	wikiSvc.SetIssues(issueRepo)
	assistantTools.SetWiki(wikiSvc, projectRepo)
	assistantSkills := repository.NewAssistantSkillRepository(db)
	assistantTools.SetSkills(assistantSkills)
	assistantSkillH := handler.NewAssistantSkillHandler(service.NewAssistantSkillService(assistantSkills, workspaceRepo))
	assistantCustomTools := repository.NewAssistantCustomToolRepository(db)
	assistantTools.SetCustomTools(assistantCustomTools)
	assistantCustomToolSvc := service.NewAssistantCustomToolService(assistantCustomTools, workspaceRepo, issueRepo, repository.NewAgentTaskRepository(db))
	assistantCustomToolSvc.SetMachineAccess(machineAccess)
	assistantToolH := handler.NewAssistantCustomToolHandler(assistantCustomToolSvc)
	assistantTools.SetMachineAccess(machineAccess)
	// Research tools (KUA-93): web search with the workspace's provider key, and
	// research sub-tasks on the assistant's own provider.
	assistantResearch := service.NewAssistantResearchService(repository.NewAssistantResearchSettingsRepository(db), workspaceRepo, crypto.DeriveKey(cfg.JWTSecret+":assistant-research"))
	assistantResearchH := handler.NewAssistantResearchHandler(assistantResearch)
	searchProviders := []service.WebSearchProvider{service.NewBraveSearch(), service.NewTavilySearch()}
	if searxng := cfg.SearXNGURL(); searxng != nil {
		// Operator-configured SearXNG: the only private host web_search may dial.
		searchProviders = append(searchProviders, service.NewSearXNGSearch(searxng))
		log.WithField("host", searxng.Host).Info("Web search: SearXNG offered to workspaces")
	}
	assistantResearch.SetSearchProviders(searchProviders...)
	assistantTools.SetWebSearch(assistantResearch, searchProviders...)
	assistantTools.SetSubagents(true)
	assistantTools.SetPlanning(repository.NewIssueDependencyRepository(db), service.NewIssueRelationService(repository.NewIssueRelationRepository(db), issueRepo))
	conversationSvc.SetTools(assistantTools)
	conversationH := handler.NewConversationHandler(conversationSvc, cfg.FrontendURL)
	agentTaskSvc.SetWebhooks(webhookSvc)
	agentTaskSvc.SetRegistry(wikiSvc)
	// Finished, failed and review-ready tasks notify their creator (KUA-122).
	agentTaskSvc.SetNotifications(repository.NewAgentTaskRepository(db), notifSvc, hub)
	// A claimed task watch pings its watcher in their own chat (KUA-165).
	agentTaskSvc.SetWatchers(repository.NewAgentTaskRepository(db), conversationSvc.Repo)
	assistantTools.SetWorkflows(workflowRepo)
	workflowH := handler.NewWorkflowHandler(service.NewWorkflowService(workflowRepo, devMachineAgentReg, agentTaskSvc))
	// Workspace memory (KUA-134): approved lessons per repository. Putting
	// them in a task's prompt is WorkspaceMemoryService.ApprovedLessons.
	// Lessons are redacted before they are stored, with the machines' secrets.
	repository.ConfigureLessonRedaction(devMachineEncKey)
	memorySvc := service.NewWorkspaceMemoryService(repository.NewWorkspaceMemoryRepository(db), devMachineSvc.MachineSecretValues)
	// Approved lessons go into the prompt of every attempt on their repository.
	agentTaskSvc.SetMemory(memorySvc)
	memoryH := handler.NewWorkspaceMemoryHandler(memorySvc)
	assistantTools.SetAgentWork(repository.NewAgentTaskRepository(db))
	assistantTools.SetAgentTasks(agentTaskSvc)
	assistantTools.SetTaskWatchers(repository.NewAgentTaskRepository(db))
	assistantTools.SetRepositories(devMachineRepo)
	assistantTools.SetLiveChannel(repository.NewAgentTaskRepository(db))
	assistantApprovals := service.NewAssistantApprovals(conversationSvc.Repo, agentTaskSvc)
	assistantApprovals.SetMachineAccess(machineAccess)
	assistantApprovals.SetWorkflows(workflowRepo)
	assistantApprovals.SetNotifications(notifSvc, hub)
	assistantApprovals.SetLiveChannel(repository.NewAgentTaskRepository(db))
	assistantTools.SetMachines(repository.NewAgentTaskRepository(db), devMachineSvc)
	assistantApprovals.SetMachines(repository.NewAgentTaskRepository(db), devMachineSvc)
	assistantApprovals.SetToolSettings(assistantToolSettings)
	assistantApprovals.SetCustomTools(assistantCustomTools)
	// Commands on the user's machine or a temporary one (KUA-153).
	assistantCommands := repository.NewAssistantCommandRepository(db)
	assistantTools.SetCommands(assistantCommands, devMachineSvc)
	assistantApprovals.SetCommands(assistantCommands, devMachineSvc)
	// Named migration actions (KUA-181) are requested by name only.
	assistantTools.SetMigrations(assistantCommands)
	// Named migration actions (KUA-181) are configured through the machine
	// service, which encrypts their connection secret.
	devMachineSvc.SetMigrationActions(assistantCommands)
	assistantSystem := service.NewAssistantSystem(service.NewSystemUpdater(cfg.SystemUpdaterURL, cfg.SystemUpdaterToken), cfg.IsSysAdmin, buildinfo.Version)
	assistantTools.SetSystem(assistantSystem)
	assistantSystem.SetHealthCheck(func(ctx context.Context) error { return db.PingContext(ctx) })
	assistantApprovals.SetSystem(assistantSystem)
	conversationSvc.SetApprovals(assistantApprovals)
	agentTaskH := handler.NewAgentTaskHandler(agentTaskSvc)
	agentMessageSvc := service.NewAgentMessageService(repository.NewAgentMessageRepository(db), hub)
	agentMessageSvc.SetWebhooks(webhookSvc, commentRepo, issueRepo)
	agentMessageH := handler.NewAgentMessageHandler(agentMessageSvc)
	agentAutomationH := handler.NewAgentAutomationHandler(automationSvc)
	// AI agents as issue assignees (KUA-107): assignments and mentions start
	// tasks through the same task service as delegation and automations.
	agentAssigneeSvc := service.NewAgentAssigneeService(repository.NewAgentAssigneeRepository(db), conversationSvc.Repo, agentTaskSvc, agentTaskSvc.Repo, agentTaskSvc, hub)
	agentAssigneeSvc.SetMachinePolicy(agentTaskSvc.ProfilePolicy)
	agentAssigneeSvc.SetProfileSeeder(workflowRepo)
	commentSvc.SetAgentMentions(agentAssigneeSvc)
	issueH.SetAgentAssignees(agentAssigneeSvc)
	agentAssigneeH := handler.NewAgentAssigneeHandler(agentAssigneeSvc, issueSvc)
	telegramSvc := service.NewTelegramService(repository.NewTelegramRepository(db), workspaceRepo, aiSettingsSvc, crypto.DeriveKey(cfg.JWTSecret+":telegram"), cfg.FrontendURL)
	if err := telegramSvc.SetWebhookURL(cfg.TelegramWebhookURL); err != nil {
		log.Fatal(err)
	}
	telegramSvc.SetApprovals(conversationSvc)
	telegramH := handler.NewTelegramHandler(telegramSvc)
	devMachineH := handler.NewDevMachineHandler(devMachineSvc, cfg.DevMachine.EffectiveDiskQuotaMode())
	analyticsRepo := repository.NewAnalyticsRepository(db)
	analyticsH := handler.NewAnalyticsHandler(analyticsRepo)
	systemH := handler.NewSystemHandler(cfg.SystemUpdaterURL, cfg.SystemUpdaterToken, cfg.IsSysAdmin)
	systemH.OnUpdateStarted(func() { hub.BroadcastSystem(realtime.Event{Type: "system.update.started"}) })
	assistantSystem.OnUpdateStarted(func() { hub.BroadcastSystem(realtime.Event{Type: "system.update.started"}) })
	webhookH := handler.NewWebhookHandler(webhookSvc)
	sharedLinkRepo := repository.NewSharedLinkRepository(db)
	sharedLinkSvc := service.NewSharedLinkService(sharedLinkRepo, workspaceRepo, teamRepo, projectRepo, viewRepo, issueRepo, userRepo, teamStatusRepo, cfg.JWTSecret)
	sharedLinkH := handler.NewSharedLinkHandler(sharedLinkSvc, cfg.FrontendURL)
	store, err := storage.New(cfg.Storage)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	authH.SetAvatarStore(store)
	teamSvc.SetIconStore(store)
	workspaceSvc.SetTeamIconStore(store)
	workspaceSvc.SetLogoStore(store)
	uploadH := handler.NewUploadHandler(store, assetRepo, issueRepo, cfg.JWTSecret)
	backupDir, err := filepath.Abs(cfg.BackupDir)
	if err != nil {
		log.Fatalf("Invalid BACKUP_DIR: %v", err)
	}
	backupSvc := service.NewBackupService(repository.NewBackupRepository(db), notifSvc, service.BackupServiceConfig{
		Dir:                 backupDir,
		Timeout:             cfg.BackupTimeout,
		DatabaseURL:         cfg.DatabaseURL,
		AllowPrivateNetwork: cfg.BackupS3AllowPrivateNetwork,
		SecretKey:           crypto.DeriveKey(cfg.JWTSecret + ":backup"),
		Sysadmins:           cfg.SysadminIDs(),
		AppVersion:          buildinfo.Version,
	}, service.BackupUploadsFromBackend(store, cfg.Storage.LocalDir))
	backupSvc.SetTimeoutSource(instanceConfig.BackupTimeout)
	backupH := handler.NewBackupHandler(backupSvc, cfg.IsSysAdmin, backupDir)
	wikiPublishSvc := service.NewWikiPublishService(repository.NewWikiRepository(db), sharedLinkRepo, workspaceRepo, cfg.JWTSecret)
	uploadH.SetWikiPublicPages(wikiPublishSvc.PublicPageReachable)
	assistantTools.SetWikiPublishing(wikiPublishSvc, workspaceRepo)
	conversationImages := service.NewImageStore(store)
	conversationSvc.SetAttachments(conversationImages, assetRepo, commentSvc, workspaceRepo)
	telegramSvc.SetImages(conversationImages)
	var voiceClient *service.VoiceClient
	if cfg.VoiceURL != "" && cfg.VoiceInternalToken != "" {
		if voiceClient, err = service.NewVoiceClient(cfg.VoiceURL, cfg.VoiceInternalToken); err != nil {
			log.Fatal(err)
		}
	}
	voiceSvc := service.NewVoiceService(repository.NewVoiceRepository(db), workspaceRepo, conversationSvc.Repo, voiceClient)
	telegramSvc.SetVoice(voiceSvc)
	voiceH := handler.NewVoiceHandler(voiceSvc)
	workspaceTransferRepo := repository.NewWorkspaceTransferRepository(db)
	workspaceTransferSvc := service.NewWorkspaceTransferService(workspaceTransferRepo, store)
	workspaceTransferH := handler.NewWorkspaceTransferHandler(workspaceTransferSvc)

	importRepo := repository.NewImportJobRepository(db)
	importStore := service.NewSQLImportStore(teamRepo, teamStatusRepo, workspaceRepo, labelRepo, cycleRepo,
		projectRepo, issueRepo, commentRepo, relationRepo, assetRepo, importRepo, store, domain.MaxUploadSize)
	importSvc := service.NewImportService(importRepo, importStore,
		service.JiraSourceFactory(cfg.ImportAllowPrivateNetwork, domain.MaxUploadSize),
		crypto.DeriveKey(cfg.JWTSecret+":import"), domain.MaxUploadSize)
	importH := handler.NewImportJobHandler(importSvc)

	// GitHub integration
	var globalGitHubApp *service.GlobalGitHubAppConfig
	if cfg.GitHubApp.IsConfigured() {
		globalGitHubApp = &service.GlobalGitHubAppConfig{
			AppID:         cfg.GitHubApp.AppID,
			PrivateKey:    cfg.GitHubApp.PrivateKey,
			ClientID:      cfg.GitHubApp.ClientID,
			ClientSecret:  cfg.GitHubApp.ClientSecret,
			WebhookSecret: cfg.GitHubApp.WebhookSecret,
			Slug:          cfg.GitHubApp.Slug,
		}
		log.Info("Global GitHub App configured (SaaS mode)")
	}
	githubRepo := repository.NewGitHubRepository(db)
	githubSvc := service.NewGitHubService(
		githubRepo, issueRepo, teamRepo, teamStatusRepo, historyRepo,
		crypto.DeriveKey(cfg.JWTSecret+":github"), hub, cfg.FrontendURL, cfg.GitHubWebhookURL,
		globalGitHubApp,
	)
	githubSvc.SetTaskReviews(repository.NewAgentTaskRepository(db))
	githubSvc.SetWebhooks(webhookSvc)
	assistantApprovals.SetMerging(repository.NewAgentTaskRepository(db), workspaceRepo, githubSvc)
	assistantApprovals.SetWiki(wikiSvc, workspaceRepo)
	assistantApprovals.SetWikiPublishing(wikiPublishSvc, workspaceRepo)
	assistantApprovals.SetPullRequests(githubSvc, repository.NewAgentTaskRepository(db))
	assistantTools.SetPullRequests(githubSvc, repository.NewAgentTaskRepository(db))
	assistantTools.SetReleases(githubSvc, devMachineRepo, assistantSkills)
	// CI check nodes of configurable workflows confirm the commit's full
	// check list with GitHub before they pass (KUA-95).
	agentTaskSvc.Workflow().SetCIChecks(githubSvc)
	// Ship approvals merge the task's pull request, through the merge gate
	// the assistant's approvals use, and branches are brought up to date
	// before reviews and the ship approval (KUA-209).
	agentTaskSvc.Workflow().SetPullRequests(githubSvc, githubSvc)
	// Repository scans read temporary checkouts on this server, removed when
	// they expire and on shutdown.
	repositoryCheckouts := service.NewRepositoryCheckouts(githubSvc)
	assistantTools.SetRepositoryScans(repositoryCheckouts, devMachineRepo)
	// Live worktree scans queue a read-only operation on the task's machine.
	assistantTools.SetWorktreeScans(devMachineRepo)
	// A task's creator reads its files at a commit: published commits are
	// listed through GitHub's trees API (a directory at a time) and their
	// files read from the same checkouts, the rest with git on the task's machine.
	agentTaskSvc.SetTaskFiles(repositoryCheckouts, devMachineRepo)
	agentTaskSvc.SetTaskFileTrees(service.NewGitHubTrees(githubSvc))
	assistantApprovals.SetReleases(githubSvc)
	githubSvc.SetPullRequestSettings(repository.NewAgentTaskRepository(db))
	assistantApprovals.SetPullRequestHeads(githubSvc)
	assistantApprovals.SetGitHubAccess(githubSvc)
	assistantTools.SetGitHubAccess(service.NewCachedGitHubAccess(githubSvc))
	assistantTools.SetPullRequestHeads(githubSvc)
	githubSvc.SetPullRequestHeads(repository.NewAgentTaskRepository(db))
	assistantApprovals.SetReviews(agentTaskSvc)
	assistantApprovals.SetAccepting(agentTaskSvc)
	assistantApprovals.SetChecks(agentTaskSvc)
	githubSvc.SetReviewComments(repository.NewAgentTaskRepository(db))
	taskFeedback := service.NewAgentTaskFeedback(repository.NewAgentTaskRepository(db), agentTaskSvc)
	githubSvc.SetTaskSignals(taskFeedback)
	commentSvc.SetTaskFeedback(taskFeedback)
	// The workspace GitHub bot: identity, ability toggles and the mention trigger.
	githubBotRepo := repository.NewGitHubBotRepository(db)
	githubSvc.SetBotSettings(githubBotRepo)
	githubBotSvc := service.NewGitHubBotService(githubBotRepo, store, cfg.FrontendURL)
	githubBotSvc.SetAppSlug(githubSvc.AppSlug)
	githubSvc.SetMentionReviews(service.NewGitHubMentionReviews(repository.NewAgentTaskRepository(db), conversationSvc.Repo, agentTaskSvc))
	githubH := handler.NewGitHubHandler(githubSvc)
	slackSvc := service.NewSlackService(repository.NewSlackRepository(db), cfg.Slack, crypto.DeriveKey(cfg.JWTSecret+":slack"), cfg.FrontendURL)
	forgeRepo := repository.NewForgeRepository(db)
	forgeSvc := service.NewForgeService(
		forgeRepo, githubRepo, issueRepo, teamRepo, teamStatusRepo, historyRepo,
		crypto.DeriveKey(cfg.JWTSecret+":forge"), hub, cfg.FrontendURL, cfg.ForgeWebhookBaseURL, cfg.ForgeAllowPrivateNetwork,
	)
	forgeSvc.SetWebhooks(webhookSvc)
	forgeH := handler.NewForgeHandler(forgeSvc)

	// Background: clean up expired authentication tokens every hour
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := refreshRepo.DeleteExpired(context.Background()); err != nil {
				log.WithError(err).Warn("failed to clean up expired refresh tokens")
			} else {
				log.Info("expired refresh tokens cleaned up")
			}
			if err := signInTokenRepo.DeleteExpired(context.Background()); err != nil {
				log.WithError(err).Warn("failed to clean up expired sign-in tokens")
			}
		}
	}()

	// Background: purge deleted issues and wiki pages past their grace period every hour
	go purgeDeletedEvery(context.Background(), time.Hour, cfg.IssueDeleteGracePeriod, issueRepo, wikiRepo, commentRepo)

	// Echo
	e := echo.New()
	// Trust X-Forwarded-For only from the proxy networks; see internal/clientip.
	e.IPExtractor = clientip.FromRequest

	// Global middleware
	e.Use(mw.Recovery())
	e.Use(mw.Logging())
	e.Use(mw.CORS(cfg.FrontendURL))
	if cfg.DevMachine.AllowSameSite {
		log.Warn("Same-site Dev Machines enabled: browser site isolation is reduced; host-prefixed cookies and exact-origin checks are active")
		e.Use(mw.BrowserOrigin(cfg.FrontendURL))
	}
	e.Use(mw.SecureHeaders())

	registerRoutes(e, &appHandlers{
		health:            healthH,
		inviteLink:        inviteLinkH,
		config:            configH,
		workspaceTransfer: workspaceTransferH,
		importJob:         importH,
		auth:              authH,
		passkey:           passkeyH,
		sso:               ssoH,
		scim:              scimH,
		workspace:         workspaceH,
		team:              teamH,
		issue:             issueH,
		label:             labelH,
		project:           projectH,
		notif:             notifH,
		ws:                wsH,
		relation:          relationH,
		template:          templateH,
		view:              viewH,
		cycle:             cycleH,
		teamStatus:        teamStatusH,
		fav:               favH,
		prefs:             prefsH,
		push:              handler.NewPushHandler(pushSvc, cfg.IsSysAdmin),
		instanceConfig:    handler.NewInstanceConfigHandler(instanceConfig, cfg.IsSysAdmin),
		token:             tokenH,
		cliLink:           cliLinkH,
		aiSettings:        aiSettingsH,
		conversation:      conversationH,
		assistantSkill:    assistantSkillH,
		assistantTool:     assistantToolH,
		assistantResearch: assistantResearchH,
		agentTask:         agentTaskH,
		workflow:          workflowH,
		memory:            memoryH,
		agentAutomation:   agentAutomationH,
		agentAssignee:     agentAssigneeH,
		agentMessage:      agentMessageH,
		telegram:          telegramH,
		voice:             voiceH,
		devMachine:        devMachineH,
		mergeMethods:      handler.NewMergeMethodHandler(githubSvc),
		analytics:         analyticsH,
		system:            systemH,
		backup:            backupH,
		webhook:           webhookH,
		sharedLink:        sharedLinkH,
		upload:            uploadH,
		github:            githubH,
		githubBot:         handler.NewGitHubBotHandler(githubBotSvc),
		forge:             forgeH,
		slack:             handler.NewSlackHandler(slackSvc),
		wiki:              handler.NewWikiHandler(wikiSvc, aiSettingsSvc),
		wikiPublish:       handler.NewWikiPublishHandler(wikiPublishSvc, cfg.FrontendURL),
		openapi:           handler.NewOpenAPIHandler(buildinfo.Version),
	}, &appMiddleware{
		auth:                   mw.AuthWithSessionVersions(cfg.JWTSecret, patRepo, passkeySvc, refreshRepo, cfg.DevMachine.AllowSameSite),
		authRateLimit:          mw.RateLimit(5, 10),
		publicRateLimit:        mw.RateLimit(2, 5),
		publicAssetRateLimit:   mw.RateLimit(10, 20),
		workspaceMembership:    mw.WorkspaceMembership(workspaceRepo, ssoSvc),
		devMachineDemoGuard:    mw.DevMachineDemoGuard(cfg.DemoDevMachineAllowed),
		machineEventsRateLimit: mw.MachineTokenRateLimit(20, 40),
		machineLogsRateLimit:   mw.MachineTokenRateLimit(50, 100),
		// Per client address, like the other limits; tool calls are single requests.
		mcpRateLimit: mw.RateLimit(10, 20),
	})

	conversationCtx, stopConversations := context.WithCancel(context.Background())
	defer stopConversations()
	go conversationSvc.Run(conversationCtx)
	go taskFeedback.Run(conversationCtx)
	go agentTaskSvc.Run(conversationCtx)
	go agentTaskSvc.RunNotifications(conversationCtx)
	go pushSvc.Run(conversationCtx)
	go wikiSvc.RunOverviewSchedules(conversationCtx)
	go service.NewIssueTemplateRecurrenceWorker(templateRepo, issueRepo, issueSvc).Run(conversationCtx)
	go backupSvc.Run(conversationCtx)
	go importSvc.Run(conversationCtx)
	go agentMessageSvc.Run(conversationCtx, 10*time.Second)
	go listenWorkerNotifications(conversationCtx, cfg.DatabaseURL, hub)
	if !cfg.AutomationWorkerExternal {
		go automationSvc.Run(conversationCtx)
	}
	go agentAssigneeSvc.Run(conversationCtx)
	go githubSvc.RunMergeReconciliation(conversationCtx, 2*time.Minute)
	// Workspace webhook deliveries are stored: workers send the ones left
	// pending before a restart, and finished ones leave the log after a week.
	// On shutdown an in-flight delivery is aborted and stays pending.
	webhookSvc.Start()
	defer webhookSvc.Close()
	go webhookSvc.RunCleanup(conversationCtx, time.Hour)
	// Replay markers of GitHub webhook deliveries expire after a few days.
	go githubSvc.RunWebhookDeliveryCleanup(conversationCtx, time.Hour)
	go forgeSvc.RunWebhookDeliveryCleanup(conversationCtx, time.Hour)
	go githubSvc.RunReviewComments(conversationCtx, 15*time.Second)
	go telegramSvc.Run(conversationCtx)
	go slackSvc.Run(conversationCtx)
	// Removes expired scan checkouts (and roots a crashed process left), and
	// every checkout when the server stops.
	go repositoryCheckouts.Run(conversationCtx)
	defer repositoryCheckouts.Close()
	// Chains approved before a restart continue from their last recorded step.
	go assistantApprovals.ResumeShips(conversationCtx)
	// Approved merges whose branch Kuayle brought up to date merge once the
	// new head's checks pass (KUA-207); each wait is read at most once a minute.
	go assistantApprovals.RunMergeWaits(conversationCtx, 15*time.Second)
	go assistantApprovals.RunStoppedPublicationDecisions(conversationCtx, 5*time.Second)

	// Start
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Infof("Starting server on %s", addr)
	serverCtx, stopServer := signal.NotifyContext(restartCtx, os.Interrupt, syscall.SIGTERM)
	defer stopServer()
	// After an apply and restart this start has to prove itself: healthy
	// within the timeout, or the previous settings come back and the server
	// exits to start again with them.
	go service.WatchRestartHealth(serverCtx, instanceConfigRepo, startup.Run, apiHealthProbe(cfg.Port, db),
		service.RestartHealthTimeout, 2*time.Second, requestRestart, log.StandardLogger())
	start := echo.StartConfig{Address: addr, HideBanner: true, HidePort: true}
	if err := start.Start(serverCtx, e); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func runMigrate(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: server migrate [up|down|version|force <version>]")
		os.Exit(1)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	m, err := migrate.New("file://migrations", dbURL)
	if err != nil {
		log.Fatalf("Failed to create migrate instance: %v", err)
	}
	defer m.Close()

	switch args[0] {
	case "up":
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("Migration up failed: %v", err)
		}
		log.Info("Migrations applied successfully")
	case "down":
		if err := m.Steps(-1); err != nil {
			log.Fatalf("Migration down failed: %v", err)
		}
		log.Info("Rolled back one migration")
	case "force":
		// Recovery only: after a failed migration rolled back, mark the last
		// verified version clean. Inspect the schema before forcing a version.
		if len(args) != 2 {
			log.Fatal("Usage: server migrate force <version>")
		}
		version, err := strconv.Atoi(args[1])
		if err != nil || version < 0 {
			log.Fatalf("Invalid migration version: %s", args[1])
		}
		if err := m.Force(version); err != nil {
			log.Fatalf("Migration force failed: %v", err)
		}
		log.Infof("Marked migration version %d as clean", version)
	case "version":
		version, dirty, err := m.Version()
		if err != nil {
			log.Fatalf("Failed to get version: %v", err)
		}
		fmt.Printf("Version: %d, Dirty: %v\n", version, dirty)
	default:
		fmt.Printf("Unknown migrate command: %s\n", args[0])
		fmt.Println("Usage: server migrate [up|down|version|force <version>]")
		os.Exit(1)
	}
}

// resetMailer returns nil when SMTP is not configured.
func resetMailer(cfg *config.Config) mail.Sender {
	if !cfg.SMTP.Enabled() {
		return nil
	}
	return mail.NewSMTPSender(cfg.SMTP)
}

// passkeyRelyingParty returns the instance's WebAuthn relying party, or nil
// when passkeys are switched off or cannot work at the configured address. A
// plain-http address outside localhost is the common case: the instance keeps
// running with password sign-in only.
func passkeyRelyingParty(cfg *config.Config) *service.PasskeyRelyingParty {
	if !cfg.PasskeysEnabled {
		log.Info("passkeys are disabled (PASSKEYS_ENABLED=false)")
		return nil
	}
	// Demo accounts are shared and their passwords public: a visitor's passkey
	// would be a private way into an account everyone uses.
	if cfg.PublicDemoMode {
		log.Info("passkeys are disabled in public demo mode")
		return nil
	}
	rp, err := service.NewPasskeyRelyingParty(cfg.PasskeyRPID, cfg.PasskeyRPOrigins, cfg.PasskeyRPDisplayName, cfg.FrontendURL)
	if err != nil {
		log.Warnf("passkeys are disabled: %v", err)
		return nil
	}
	log.Infof("passkeys enabled for relying party %s (origins %v)", rp.ID, rp.Origins)
	return rp
}
