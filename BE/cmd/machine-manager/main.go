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
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/machine"
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	githubclient "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	configValue, err := config.Load()
	if err != nil {
		log.WithError(err).Fatal("load configuration")
	}
	if !configValue.DevMachine.Enabled {
		log.Fatal("DEV_MACHINES_ENABLED must be true for the machine manager")
	}
	database, err := repository.Open(configValue.DatabaseURL)
	if err != nil {
		log.WithError(err).Fatal("connect database")
	}
	defer database.Close()
	database.SetMaxOpenConns(16)
	database.SetMaxIdleConns(4)
	database.SetConnMaxLifetime(30 * time.Minute)

	runtime, err := machine.NewDockerRuntime(machine.DockerConfig{
		Host: configValue.DevMachine.DockerHost, GatewayContainerName: configValue.DevMachine.GatewayContainerName,
		SeccompProfile: configValue.DevMachine.SeccompProfile, AppArmorProfile: configValue.DevMachine.AppArmorProfile,
		PullImages: strings.EqualFold(os.Getenv("DEV_MACHINE_PULL_IMAGES"), "true"), ComposeImages: composeImages(), IngestURL: configValue.DevMachine.IngestURL,
		EgressAllowlist: configValue.DevMachine.EgressAllowlist, EgressDenylist: configValue.DevMachine.EgressDenylist,
		DiskQuotaMode:          configValue.DevMachine.EffectiveDiskQuotaMode(),
		HostDiskMinFreePercent: configValue.DevMachine.HostDiskMinFreePercent,
		HostDiskMinFreeGB:      configValue.DevMachine.HostDiskMinFreeGB,
		HostDiskPath:           configValue.DevMachine.HostDiskPath,
		DockerRuntime:          configValue.DevMachine.DockerRuntime,
		DockerSeccompProfile:   configValue.DevMachine.DockerSeccompProfile,
		DockerAppArmorProfile:  configValue.DevMachine.DockerAppArmorProfile,
	})
	if err != nil {
		log.WithError(err).Fatal("initialize Docker runtime")
	}
	runtimeCheckCtx, cancelRuntimeCheck := context.WithTimeout(context.Background(), 10*time.Second)
	if err := runtime.Ping(runtimeCheckCtx); err != nil {
		cancelRuntimeCheck()
		log.WithError(err).Fatal("verify Docker workspace quota support")
	}
	cancelRuntimeCheck()
	if configValue.DevMachine.MonitoredDiskQuota() {
		log.WithFields(log.Fields{
			"quota_mode":                 configValue.DevMachine.EffectiveDiskQuotaMode(),
			"host_disk_min_free_percent": configValue.DevMachine.HostDiskMinFreePercent,
			"host_disk_min_free_gb":      configValue.DevMachine.HostDiskMinFreeGB,
		}).Warn("workspace disk limits are monitored, not hard-enforced")
	}
	registry := agent.NewRegistry(
		agent.NewClaudeCodeProvider(configValue.DevMachine.ClaudeCodeImage),
		agent.NewOpenCodeProvider(configValue.DevMachine.OpenCodeImage),
		agent.NewCodexProvider(configValue.DevMachine.CodexImage),
		agent.NewQwenCodeProvider(configValue.DevMachine.HeadlessImage),
		agent.NewCrushProvider(configValue.DevMachine.HeadlessImage),
		agent.NewKimiProvider(configValue.DevMachine.HeadlessImage),
		agent.NewGooseProvider(configValue.DevMachine.HeadlessImage),
		agent.NewCustomCLIProvider(configValue.DevMachine.CustomImage),
	)
	hostname, _ := os.Hostname()
	var gitHubClient *githubclient.Client
	if configValue.GitHubApp.IsConfigured() {
		gitHubClient, err = githubclient.NewClient(configValue.GitHubApp.AppID, configValue.GitHubApp.PrivateKey)
		if err != nil {
			log.WithError(err).Fatal("initialize GitHub App client")
		}
	}
	manager := machine.NewManager(repository.NewDevMachineRepository(database), runtime, registry, gitHubClient,
		cryptoutil.DeriveKey(configValue.DevMachine.EncryptionKey), cryptoutil.DeriveKey(configValue.JWTSecret+":github"),
		hostname+":"+fmt.Sprint(os.Getpid()), machine.DiskQuotaConfig{
			Mode:               configValue.DevMachine.EffectiveDiskQuotaMode(),
			HostMinFreePercent: configValue.DevMachine.HostDiskMinFreePercent,
			HostMinFreeGB:      configValue.DevMachine.HostDiskMinFreeGB,
		})
	manager.SetCapacity(machine.CapacityConfig{ReservedCPUMillis: configValue.DevMachine.HostReservedCPUMillis, ReservedMemoryMB: configValue.DevMachine.HostReservedMemoryMB, CPUOvercommit: configValue.DevMachine.CPUOvercommit})
	manager.SetMachineDomain(configValue.DevMachine.Domain)
	manager.SetAgentGitIdentity(machine.GitIdentity{Name: configValue.DevMachine.AgentGitName, Email: configValue.DevMachine.AgentGitEmail})
	// Task images are read from the API's storage; without it runs get none.
	if files, err := storage.New(configValue.Storage); err == nil {
		manager.SetAttachmentStorage(files)
	} else {
		log.WithError(err).Warn("upload storage unavailable; task images will not be copied")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := metricsServer(manager, database)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Error("manager health server failed")
			stop()
		}
	}()
	if err := manager.Run(ctx); err != nil {
		log.WithError(err).Error("machine manager stopped")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func metricsServer(manager *machine.Manager, database *sqlx.DB) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/ready", func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := manager.Ready(ctx); err != nil {
			http.Error(writer, "docker unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := database.PingContext(ctx); err != nil {
			http.Error(writer, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/metrics", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
		for name, value := range manager.Metrics() {
			_, _ = fmt.Fprintf(writer, "kuayle_dev_machine_%s %d\n", name, value)
		}
	})
	port := os.Getenv("DEV_MACHINE_MANAGER_PORT")
	if port == "" {
		port = "8092"
	}
	return &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 32 * 1024}
}

// composeImages defaults to official Docker Hub images; operators can list other
// repositories or prefixes in DEV_MACHINE_COMPOSE_IMAGES (comma separated).
func composeImages() []string {
	value := strings.TrimSpace(os.Getenv("DEV_MACHINE_COMPOSE_IMAGES"))
	if value == "" {
		return []string{"docker.io/library"}
	}
	return strings.Split(value, ",")
}
