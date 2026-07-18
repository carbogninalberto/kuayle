package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
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
	if gatewayDatabaseURL := os.Getenv("DEV_MACHINE_GATEWAY_DATABASE_URL"); gatewayDatabaseURL != "" {
		configValue.DatabaseURL = gatewayDatabaseURL
	}
	database, err := sqlx.Connect("pgx", configValue.DatabaseURL)
	if err != nil {
		log.WithError(err).Fatal("connect database")
	}
	defer database.Close()
	database.SetMaxOpenConns(50)
	database.SetMaxIdleConns(10)
	database.SetConnMaxLifetime(30 * time.Minute)
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
