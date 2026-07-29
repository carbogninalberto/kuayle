package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/storage"
)

func main() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	// Handle CLI subcommands
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		runMigrate(os.Args[2:])
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Database
	db, err := sqlx.Connect("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Realtime hub
	hub := realtime.NewHub()

	// Repositories
	userRepo := repository.NewUserRepository(db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	patRepo := repository.NewPersonalAccessTokenRepository(db)
	workspaceRepo := repository.NewWorkspaceRepository(db)
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

	// Dev Machine control-plane store
	devMachineRepo := repository.NewDevMachineRepository(db)

	// Services
	inviteLinkSvc := service.NewInviteLinkService(inviteLinkRepo, workspaceRepo)
	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg.JWTSecret,
		service.WithRegistrationDisabled(cfg.DisableRegistration),
		service.WithInviteRedeemer(inviteLinkSvc),
	)
	workspaceSvc := service.NewWorkspaceService(workspaceRepo, userRepo)
	teamSvc := service.NewTeamService(teamRepo, teamStatusRepo)
	notifSvc := service.NewNotificationService(notifRepo)
	issueSvc := service.NewIssueService(issueRepo, teamRepo, teamStatusRepo, historyRepo, hub, notifSvc, projectRepo)
	labelSvc := service.NewLabelService(labelRepo)
	commentSvc := service.NewCommentService(commentRepo, issueRepo, hub, notifSvc)
	projectSvc := service.NewProjectService(projectRepo)
	relationSvc := service.NewIssueRelationService(relationRepo, issueRepo)
	templateSvc := service.NewIssueTemplateService(templateRepo)
	viewSvc := service.NewViewService(viewRepo, hub)
	cycleSvc := service.NewCycleService(cycleRepo, teamRepo, hub, notifSvc)
	teamStatusSvc := service.NewTeamStatusService(teamStatusRepo, visibilityRepo)
	favSvc := service.NewFavoriteService(favRepo)
	prefsSvc := service.NewPreferencesService(prefsRepo)
	tokenSvc := service.NewTokenService(patRepo, workspaceRepo)
	aiSettingsSvc := service.NewAISettingsService(aiSettingsRepo, workspaceRepo, issueRepo, crypto.DeriveKey(cfg.JWTSecret+":ai"))

	// Dev Machine agent registry
	devMachineAgentReg := agent.NewRegistry(
		agent.NewClaudeCodeProvider(cfg.DevMachine.ClaudeCodeImage),
		agent.NewOpenCodeProvider(cfg.DevMachine.OpenCodeImage),
		agent.NewCodexProvider(cfg.DevMachine.CodexImage),
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
			IDE: cfg.DevMachine.IDEImage, Browser: cfg.DevMachine.BrowserImage,
			Collector: cfg.DevMachine.CollectorImage,
			Egress:    cfg.DevMachine.EgressImage,
		}, cfg.FrontendURL,
	)

	// Handlers
	healthH := handler.NewHealthHandler(db)
	loginThrottle := mw.NewLoginThrottle(5, 15*time.Minute)
	authH := handler.NewAuthHandler(authSvc, cfg.Environment != "development", loginThrottle, cfg.IsSysAdmin)
	workspaceH := handler.NewWorkspaceHandler(workspaceSvc)
	inviteLinkH := handler.NewInviteLinkHandler(inviteLinkSvc, cfg.FrontendURL)
	configH := handler.NewConfigHandler(!cfg.DisableRegistration)
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
	aiSettingsH := handler.NewAISettingsHandler(aiSettingsSvc)
	devMachineH := handler.NewDevMachineHandler(devMachineSvc)
	analyticsRepo := repository.NewAnalyticsRepository(db)
	analyticsH := handler.NewAnalyticsHandler(analyticsRepo)
	systemH := handler.NewSystemHandler(cfg.SystemUpdaterURL, cfg.SystemUpdaterToken, cfg.IsSysAdmin)
	webhookRepo := repository.NewWebhookRepository(db)
	webhookSvc := service.NewWebhookService(webhookRepo, cfg.JWTSecret)
	webhookH := handler.NewWebhookHandler(webhookSvc)
	sharedLinkRepo := repository.NewSharedLinkRepository(db)
	sharedLinkSvc := service.NewSharedLinkService(sharedLinkRepo, workspaceRepo, teamRepo, projectRepo, viewRepo, issueRepo, userRepo, teamStatusRepo, cfg.JWTSecret)
	sharedLinkH := handler.NewSharedLinkHandler(sharedLinkSvc, cfg.FrontendURL)
	store, err := storage.New(cfg.Storage)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	uploadH := handler.NewUploadHandler(store, assetRepo, issueRepo, cfg.JWTSecret)
	workspaceTransferRepo := repository.NewWorkspaceTransferRepository(db)
	workspaceTransferSvc := service.NewWorkspaceTransferService(workspaceTransferRepo, store)
	workspaceTransferH := handler.NewWorkspaceTransferHandler(workspaceTransferSvc)

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
	githubH := handler.NewGitHubHandler(githubSvc)

	// Background: clean up expired refresh tokens every hour
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := refreshRepo.DeleteExpired(context.Background()); err != nil {
				log.WithError(err).Warn("failed to clean up expired refresh tokens")
			} else {
				log.Info("expired refresh tokens cleaned up")
			}
		}
	}()

	// Echo
	e := echo.New()
	e.HideBanner = true

	// Global middleware
	e.Use(mw.Recovery())
	e.Use(mw.Logging())
	e.Use(mw.CORS(cfg.FrontendURL))
	e.Use(mw.SecureHeaders())

	registerRoutes(e, &appHandlers{
		health:            healthH,
		inviteLink:        inviteLinkH,
		workspaceTransfer: workspaceTransferH,
		auth:              authH,
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
		token:             tokenH,
		aiSettings:        aiSettingsH,
		devMachine:        devMachineH,
		analytics:         analyticsH,
		system:            systemH,
		webhook:           webhookH,
		sharedLink:        sharedLinkH,
		upload:            uploadH,
		github:            githubH,
	}, &appMiddleware{
		auth:                   mw.Auth(cfg.JWTSecret, patRepo),
		authRateLimit:          mw.RateLimit(5, 10),
		publicRateLimit:        mw.RateLimit(2, 5),
		publicAssetRateLimit:   mw.RateLimit(10, 20),
		workspaceMembership:    mw.WorkspaceMembership(workspaceRepo),
		devMachineDemoGuard:    mw.DevMachineDemoGuard(cfg.DemoDevMachineAllowed),
		machineEventsRateLimit: mw.MachineTokenRateLimit(20, 40),
		machineLogsRateLimit:   mw.MachineTokenRateLimit(50, 100),
	})

	// Start
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Infof("Starting server on %s", addr)
	if err := e.Start(addr); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func runMigrate(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: server migrate [up|down|version]")
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
	case "version":
		version, dirty, err := m.Version()
		if err != nil {
			log.Fatalf("Failed to get version: %v", err)
		}
		fmt.Printf("Version: %d, Dirty: %v\n", version, dirty)
	default:
		fmt.Printf("Unknown migrate command: %s\n", args[0])
		fmt.Println("Usage: server migrate [up|down|version]")
		os.Exit(1)
	}
}
