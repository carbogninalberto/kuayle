package main

import (
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

	"github.com/carbon/carbon-backend/internal/config"
	"github.com/carbon/carbon-backend/internal/handler"
	mw "github.com/carbon/carbon-backend/internal/middleware"
	"github.com/carbon/carbon-backend/internal/realtime"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/carbon/carbon-backend/internal/service"
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

	// Services
	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg.JWTSecret)
	workspaceSvc := service.NewWorkspaceService(workspaceRepo, userRepo)
	teamSvc := service.NewTeamService(teamRepo, teamStatusRepo)
	issueSvc := service.NewIssueService(issueRepo, teamRepo, teamStatusRepo, historyRepo, hub)
	labelSvc := service.NewLabelService(labelRepo)
	commentSvc := service.NewCommentService(commentRepo)
	projectSvc := service.NewProjectService(projectRepo)
	notifSvc := service.NewNotificationService(notifRepo)
	relationSvc := service.NewIssueRelationService(relationRepo, issueRepo)
	templateSvc := service.NewIssueTemplateService(templateRepo)
	viewSvc := service.NewViewService(viewRepo)
	cycleSvc := service.NewCycleService(cycleRepo)
	teamStatusSvc := service.NewTeamStatusService(teamStatusRepo, visibilityRepo)
	favSvc := service.NewFavoriteService(favRepo)
	prefsSvc := service.NewPreferencesService(prefsRepo)

	// Handlers
	healthH := handler.NewHealthHandler(db)
	loginThrottle := mw.NewLoginThrottle(5, 15*time.Minute)
	authH := handler.NewAuthHandler(authSvc, cfg.Environment != "development", loginThrottle)
	workspaceH := handler.NewWorkspaceHandler(workspaceSvc)
	teamH := handler.NewTeamHandler(teamSvc)
	issueH := handler.NewIssueHandler(issueSvc, commentSvc, userRepo, teamStatusRepo)
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
	analyticsH := handler.NewAnalyticsHandler(db)
	webhookRepo := repository.NewWebhookRepository(db)
	webhookSvc := service.NewWebhookService(webhookRepo, cfg.JWTSecret)
	webhookH := handler.NewWebhookHandler(webhookSvc)
	uploadH := handler.NewUploadHandler("./uploads")

	// Echo
	e := echo.New()
	e.HideBanner = true
	e.Static("/uploads", "./uploads")

	// Global middleware
	e.Use(mw.Recovery())
	e.Use(mw.Logging())
	e.Use(mw.CORS(cfg.FrontendURL))
	e.Use(mw.SecureHeaders())

	// Health
	e.GET("/health", healthH.Health)
	e.GET("/ready", healthH.Ready)

	// Auth (public) — rate limited: 5 requests/sec, burst of 10
	auth := e.Group("/api/auth", mw.RateLimit(5, 10))
	auth.POST("/register", authH.Register)
	auth.POST("/login", authH.Login)
	auth.POST("/refresh", authH.Refresh)
	auth.POST("/logout", authH.Logout)

	// Authenticated routes
	api := e.Group("/api", mw.Auth(cfg.JWTSecret))

	// User
	api.GET("/auth/me", authH.Me)
	api.GET("/preferences", prefsH.Get)
	api.PATCH("/preferences", prefsH.Update)

	// Workspaces (no workspace context needed for list/create)
	api.GET("/workspaces", workspaceH.List)
	api.POST("/workspaces", workspaceH.Create)

	// Workspace-scoped routes
	ws := api.Group("/workspaces/:slug", mw.WorkspaceMembership(workspaceRepo))
	ws.GET("", workspaceH.Get)
	ws.PATCH("", workspaceH.Update, mw.RequirePermission("workspace:manage"))
	ws.POST("/invite", workspaceH.Invite, mw.RequirePermission("member:invite"))
	ws.GET("/members", workspaceH.ListMembers)
	ws.PATCH("/members/:userId", workspaceH.UpdateMemberRole, mw.RequirePermission("member:invite"))
	ws.DELETE("/members/:userId", workspaceH.RemoveMember, mw.RequirePermission("member:invite"))

	// Teams
	ws.GET("/teams", teamH.List)
	ws.POST("/teams", teamH.Create, mw.RequirePermission("team:manage"))
	ws.GET("/teams/:teamId", teamH.Get)
	ws.PATCH("/teams/:teamId", teamH.Update, mw.RequirePermission("team:manage"))

	// Team Statuses
	ws.GET("/teams/:teamId/statuses", teamStatusH.List)
	ws.POST("/teams/:teamId/statuses", teamStatusH.Create, mw.RequirePermission("team:manage"))
	ws.PATCH("/teams/:teamId/statuses/:statusId", teamStatusH.Update, mw.RequirePermission("team:manage"))
	ws.DELETE("/teams/:teamId/statuses/:statusId", teamStatusH.Delete, mw.RequirePermission("team:manage"))

	// Cycles (team-scoped)
	ws.GET("/teams/:teamId/cycles", cycleH.List)
	ws.POST("/teams/:teamId/cycles", cycleH.Create)
	ws.GET("/teams/:teamId/cycles/:cycleId", cycleH.Get)
	ws.PATCH("/teams/:teamId/cycles/:cycleId", cycleH.Update)
	ws.POST("/teams/:teamId/cycles/:cycleId/complete", cycleH.Complete)
	ws.DELETE("/teams/:teamId/cycles/:cycleId", cycleH.Delete)

	// Issues
	ws.GET("/issues", issueH.List)
	ws.POST("/issues", issueH.Create, mw.RequirePermission("issue:create"))
	ws.PATCH("/issues/bulk", issueH.BulkUpdate, mw.RequirePermission("issue:update"))
	ws.DELETE("/issues/bulk", issueH.BulkDelete, mw.RequirePermission("issue:delete"))
	ws.GET("/issues/:identifier", issueH.Get)
	ws.PATCH("/issues/:identifier", issueH.Update, mw.RequirePermission("issue:update"))
	ws.DELETE("/issues/:identifier", issueH.Delete, mw.RequirePermission("issue:delete"))
	ws.GET("/issues/:identifier/comments", issueH.ListComments)
	ws.POST("/issues/:identifier/comments", issueH.CreateComment, mw.RequirePermission("issue:create"))
	ws.POST("/issues/:identifier/comments/:commentId/resolve", issueH.ResolveComment, mw.RequirePermission("issue:update"))
	ws.POST("/issues/:identifier/comments/:commentId/reopen", issueH.ReopenComment, mw.RequirePermission("issue:update"))
	ws.GET("/issues/:identifier/sub-issues", issueH.ListSubIssues)
	ws.GET("/issues/:identifier/history", issueH.GetHistory)
	ws.POST("/issues/:identifier/triage/accept", issueH.TriageAccept, mw.RequirePermission("issue:update"))
	ws.POST("/issues/:identifier/triage/decline", issueH.TriageDecline, mw.RequirePermission("issue:update"))

	// Issue Relations
	ws.POST("/issues/:identifier/relations", relationH.Create, mw.RequirePermission("issue:update"))
	ws.GET("/issues/:identifier/relations", relationH.List)
	ws.DELETE("/issues/:identifier/relations/:relationId", relationH.Delete, mw.RequirePermission("issue:update"))

	// Issue Templates
	ws.GET("/issue-templates", templateH.List)
	ws.POST("/issue-templates", templateH.Create, mw.RequirePermission("issue:create"))
	ws.GET("/issue-templates/:id", templateH.Get)
	ws.PATCH("/issue-templates/:id", templateH.Update, mw.RequirePermission("issue:create"))
	ws.DELETE("/issue-templates/:id", templateH.Delete, mw.RequirePermission("issue:create"))

	// Labels
	ws.GET("/labels", labelH.List)
	ws.POST("/labels", labelH.Create, mw.RequirePermission("label:manage"))
	ws.PATCH("/labels/:id", labelH.Update, mw.RequirePermission("label:manage"))
	ws.DELETE("/labels/:id", labelH.Delete, mw.RequirePermission("label:manage"))

	// Projects
	ws.GET("/projects", projectH.List)
	ws.POST("/projects", projectH.Create, mw.RequirePermission("project:manage"))
	ws.GET("/projects/:id", projectH.Get)
	ws.PATCH("/projects/:id", projectH.Update, mw.RequirePermission("project:manage"))
	ws.DELETE("/projects/:id", projectH.Delete, mw.RequirePermission("project:manage"))
	ws.GET("/teams/:teamId/projects", projectH.ListByTeam)

	// Views
	ws.GET("/views", viewH.List)
	ws.POST("/views", viewH.Create)
	ws.GET("/views/:id", viewH.Get)
	ws.PATCH("/views/:id", viewH.Update)
	ws.DELETE("/views/:id", viewH.Delete)

	// Analytics
	ws.GET("/analytics/overview", analyticsH.Overview)
	ws.GET("/analytics/distribution", analyticsH.IssueDistribution)

	// Webhooks
	ws.GET("/webhooks", webhookH.List, mw.RequirePermission("workspace:manage"))
	ws.POST("/webhooks", webhookH.Create, mw.RequirePermission("workspace:manage"))
	ws.PATCH("/webhooks/:id", webhookH.Update, mw.RequirePermission("workspace:manage"))
	ws.DELETE("/webhooks/:id", webhookH.Delete, mw.RequirePermission("workspace:manage"))

	// Favorites
	ws.GET("/favorites", favH.List)
	ws.POST("/favorites", favH.Create)
	ws.DELETE("/favorites/:id", favH.Delete)

	// Uploads
	ws.POST("/upload", uploadH.Upload, mw.RequirePermission("issue:create"))

	// WebSocket
	ws.GET("/ws", wsH.Handle)

	// Notifications (user-scoped, not workspace-scoped)
	api.GET("/notifications", notifH.List)
	api.PATCH("/notifications/:id", notifH.Update)
	api.POST("/notifications/:id/snooze", notifH.Snooze)
	api.POST("/notifications/:id/archive", notifH.Archive)
	api.POST("/notifications/mark-all-read", notifH.MarkAllRead)

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
