package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/machine"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	configValue, err := config.LoadMachineGateway()
	if err != nil {
		log.WithError(err).Fatal("load configuration")
	}
	database, err := sqlx.Connect("pgx", configValue.DatabaseURL)
	if err != nil {
		log.WithError(err).Fatal("connect database")
	}
	defer database.Close()
	database.SetMaxOpenConns(50)
	database.SetMaxIdleConns(10)
	database.SetConnMaxLifetime(30 * time.Minute)
	if strings.EqualFold(configValue.Environment, "production") {
		roleCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		role, roleErr := inspectGatewayDatabaseRole(roleCtx, database)
		cancel()
		if roleErr != nil {
			log.WithError(roleErr).Fatal("inspect gateway database role")
		}
		if roleErr = validateGatewayDatabaseRole(role); roleErr != nil {
			log.WithError(roleErr).Fatal("reject unrestricted gateway database role")
		}
	}
	gateway, err := machine.NewGateway(repository.NewDevMachineRepository(database), configValue.DevMachine.Domain,
		time.Duration(configValue.DevMachine.SessionTTLMinutes)*time.Minute, configValue.FrontendURL)
	if err != nil {
		log.WithError(err).Fatal("initialize machine gateway")
	}
	gateway.SetDemoRestriction(configValue.PublicDemoMode, configValue.IsSysAdmin)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/ready", func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := database.PingContext(ctx); err != nil {
			http.Error(writer, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.Handle("/", gateway)
	port := os.Getenv("DEV_MACHINE_GATEWAY_PORT")
	if port == "" {
		port = "8090"
	}
	server := &http.Server{
		Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 0, WriteTimeout: 0, MaxHeaderBytes: 64 * 1024,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.WithFields(log.Fields{"port": port, "machine_domain": configValue.DevMachine.Domain, "event_type": "gateway.started"}).Info("machine gateway started")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.WithError(err).Fatal(fmt.Sprintf("machine gateway failed on port %s", port))
	}
}

type gatewayDatabaseRole struct {
	Username          string `db:"username"`
	Superuser         bool   `db:"superuser"`
	CreateRole        bool   `db:"create_role"`
	CreateDatabase    bool   `db:"create_database"`
	Replication       bool   `db:"replication"`
	BypassRLS         bool   `db:"bypass_rls"`
	DatabaseCreate    bool   `db:"database_create"`
	PublicCreate      bool   `db:"public_create"`
	HasRoleMembership bool   `db:"has_role_membership"`
	OwnsObjects       bool   `db:"owns_objects"`
}

func inspectGatewayDatabaseRole(ctx context.Context, database *sqlx.DB) (gatewayDatabaseRole, error) {
	var role gatewayDatabaseRole
	err := database.GetContext(ctx, &role, `SELECT current_user AS username,
		r.rolsuper AS superuser, r.rolcreaterole AS create_role, r.rolcreatedb AS create_database,
		r.rolreplication AS replication, r.rolbypassrls AS bypass_rls,
		has_database_privilege(current_user, current_database(), 'CREATE') AS database_create,
		has_schema_privilege(current_user, 'public', 'CREATE') AS public_create,
		EXISTS (SELECT 1 FROM pg_auth_members membership WHERE membership.member=r.oid) AS has_role_membership,
		EXISTS (SELECT 1 FROM pg_class object WHERE object.relowner=r.oid
			UNION ALL SELECT 1 FROM pg_proc proc WHERE proc.proowner=r.oid
			UNION ALL SELECT 1 FROM pg_type typ WHERE typ.typowner=r.oid) AS owns_objects
		FROM pg_roles r WHERE r.rolname=current_user`)
	return role, err
}

func validateGatewayDatabaseRole(role gatewayDatabaseRole) error {
	if role.Username == "" {
		return fmt.Errorf("database did not identify the current gateway role")
	}
	if role.Superuser || role.CreateRole || role.CreateDatabase || role.Replication || role.BypassRLS || role.DatabaseCreate || role.PublicCreate || role.HasRoleMembership || role.OwnsObjects {
		return fmt.Errorf("database role %q has administrative, object-creation, or inherited privileges", role.Username)
	}
	return nil
}
