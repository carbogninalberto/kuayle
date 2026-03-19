package main

import (
	"fmt"
	"os"

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

	// Services
	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg.JWTSecret)
	workspaceSvc := service.NewWorkspaceService(workspaceRepo, userRepo)
	teamSvc := service.NewTeamService(teamRepo)
	issueSvc := service.NewIssueService(issueRepo, teamRepo, historyRepo, hub)
	labelSvc := service.NewLabelService(labelRepo)
	commentSvc := service.NewCommentService(commentRepo)
	projectSvc := service.NewProjectService(projectRepo)
	notifSvc := service.NewNotificationService(notifRepo)
	relationSvc := service.NewIssueRelationService(relationRepo, issueRepo)
	templateSvc := service.NewIssueTemplateService(templateRepo)
	viewSvc := service.NewViewService(viewRepo)

	// Handlers
	healthH := handler.NewHealthHandler(db)
	authH := handler.NewAuthHandler(authSvc)
	workspaceH := handler.NewWorkspaceHandler(workspaceSvc)
	teamH := handler.NewTeamHandler(teamSvc)
	issueH := handler.NewIssueHandler(issueSvc, commentSvc)
	labelH := handler.NewLabelHandler(labelSvc)
	projectH := handler.NewProjectHandler(projectSvc)
	notifH := handler.NewNotificationHandler(notifSvc)
	wsH := handler.NewWSHandler(hub)
	relationH := handler.NewIssueRelationHandler(relationSvc)
	templateH := handler.NewIssueTemplateHandler(templateSvc)
	viewH := handler.NewViewHandler(viewSvc)

	// Echo
	e := echo.New()
	e.HideBanner = true

	// Global middleware
	e.Use(mw.Recovery())
	e.Use(mw.Logging())
	e.Use(mw.CORS(cfg.FrontendURL))

	// Health
	e.GET("/health", healthH.Health)
	e.GET("/ready", healthH.Ready)

	// Auth (public)
	auth := e.Group("/api/auth")
	auth.POST("/register", authH.Register)
	auth.POST("/login", authH.Login)
	auth.POST("/refresh", authH.Refresh)
	auth.POST("/logout", authH.Logout)

	// Authenticated routes
	api := e.Group("/api", mw.Auth(cfg.JWTSecret))

	// User
	api.GET("/auth/me", authH.Me)

	// Workspaces (no workspace context needed for list/create)
	api.GET("/workspaces", workspaceH.List)
	api.POST("/workspaces", workspaceH.Create)

	// Workspace-scoped routes
	ws := api.Group("/workspaces/:slug", mw.WorkspaceMembership(workspaceRepo))
	ws.GET("", workspaceH.Get)
	ws.PATCH("", workspaceH.Update, mw.RequirePermission("workspace:manage"))
	ws.POST("/invite", workspaceH.Invite, mw.RequirePermission("member:invite"))
	ws.GET("/members", workspaceH.ListMembers)

	// Teams
	ws.GET("/teams", teamH.List)
	ws.POST("/teams", teamH.Create, mw.RequirePermission("team:manage"))
	ws.GET("/teams/:teamId", teamH.Get)
	ws.PATCH("/teams/:teamId", teamH.Update, mw.RequirePermission("team:manage"))

	// Issues
	ws.GET("/issues", issueH.List)
	ws.POST("/issues", issueH.Create, mw.RequirePermission("issue:create"))
	ws.GET("/issues/:identifier", issueH.Get)
	ws.PATCH("/issues/:identifier", issueH.Update, mw.RequirePermission("issue:update"))
	ws.DELETE("/issues/:identifier", issueH.Delete, mw.RequirePermission("issue:delete"))
	ws.GET("/issues/:identifier/comments", issueH.ListComments)
	ws.POST("/issues/:identifier/comments", issueH.CreateComment, mw.RequirePermission("issue:create"))
	ws.GET("/issues/:identifier/sub-issues", issueH.ListSubIssues)
	ws.GET("/issues/:identifier/history", issueH.GetHistory)

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

	// Views
	ws.GET("/views", viewH.List)
	ws.POST("/views", viewH.Create)
	ws.GET("/views/:id", viewH.Get)
	ws.PATCH("/views/:id", viewH.Update)
	ws.DELETE("/views/:id", viewH.Delete)

	// WebSocket
	ws.GET("/ws", wsH.Handle)

	// Notifications (user-scoped, not workspace-scoped)
	api.GET("/notifications", notifH.List)
	api.PATCH("/notifications/:id", notifH.Update)
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
