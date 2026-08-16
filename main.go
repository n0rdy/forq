package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/n0rdy/forq/api"
	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/configs"
	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/jobs/cleanup"
	"github.com/n0rdy/forq/jobs/maintenance"
	metricsJobs "github.com/n0rdy/forq/jobs/metrics"
	"github.com/n0rdy/forq/metrics"
	"github.com/n0rdy/forq/services"
	"github.com/n0rdy/forq/ui"

	_ "github.com/golang-migrate/migrate/v4/database/sqlite"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/rs/zerolog/log"
)

const (
	minAuthSecretLength = 32
)

func main() {
	env := getEnv()
	authSecret := getAuthSecret()
	metricsEnabled, metricsAuthSecret := getMetricsConfigs()
	queueTtlHours, dlqTtlHours := getTtlConfigs()
	apiAddr, uiAddr := getServerAddrs()
	trustProxyHeaders := getTrustProxyHeaders()

	dbPath := getDbPath()
	log.Info().Msgf("using database file at: %s", dbPath)

	runMigrations(dbPath)

	appConfigs := configs.NewAppConfig(metricsEnabled, queueTtlHours, dlqTtlHours)

	repo, err := db.NewSQLiteRepo(dbPath, appConfigs)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create SQLite repository")
	}
	defer repo.Close()

	monitoringService := services.NewMonitoringService(repo)
	metricsService := metrics.NewMetricsService(metricsEnabled)
	queuesService := services.NewQueuesService(repo)
	messagesService := services.NewMessagesService(metricsService, repo, appConfigs)
	sessionsService := services.NewSessionsService()
	defer sessionsService.Close()
	throttlingService := services.NewThrottlingService()
	defer throttlingService.Close()

	expiredMessagesCleanupJob := cleanup.NewExpiredMessagesCleanupJob(metricsService, repo, appConfigs.JobsIntervals.ExpiredMessagesCleanupMs)
	defer expiredMessagesCleanupJob.Close()
	expiredDlqMessagesCleanupJob := cleanup.NewExpiredDlqMessagesCleanupJob(metricsService, repo, appConfigs.JobsIntervals.ExpiredDlqMessagesCleanupMs)
	defer expiredDlqMessagesCleanupJob.Close()
	failedMessagesCleanupJob := cleanup.NewFailedMessagesCleanupJob(metricsService, repo, appConfigs.JobsIntervals.FailedMessagesCleanupMs)
	defer failedMessagesCleanupJob.Close()
	failedDlqMessagesCleanupJob := cleanup.NewFailedDlqMessagesCleanupJob(metricsService, repo, appConfigs.JobsIntervals.FailedDqlMessagesCleanupMs)
	defer failedDlqMessagesCleanupJob.Close()
	staleMessagesCleanupJob := cleanup.NewStaleMessagesCleanupJob(metricsService, repo, appConfigs.JobsIntervals.StaleMessagesCleanupMs)
	defer staleMessagesCleanupJob.Close()
	dbOptimizationJob := maintenance.NewDbOptimizationJob(repo, appConfigs.JobsIntervals.DbOptimizationMs, appConfigs.JobsIntervals.DbOptimizationMaxDurationMs)
	defer dbOptimizationJob.Close()

	if metricsEnabled {
		queuesDepthMetricsJob := metricsJobs.NewQueuesDepthMetricsJob(metricsService, repo, appConfigs.JobsIntervals.QueuesDepthMetricsMs)
		defer queuesDepthMetricsJob.Close()
	}

	// shutdownCtx is cancelled on SIGINT/SIGTERM. It is also the BaseContext of
	// both servers, so request contexts (incl. in-flight long polls) are cancelled
	// immediately on shutdown instead of holding Shutdown() for up to 30s.
	shutdownCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	serverFailedCh := make(chan struct{})
	var serverFailedOnce sync.Once

	apiRouter := api.NewRouter(monitoringService, messagesService, throttlingService, authSecret, metricsEnabled, metricsAuthSecret, env, trustProxyHeaders)

	var apiProtocols http.Protocols
	apiProtocols.SetUnencryptedHTTP2(true)
	apiProtocols.SetHTTP1(true)

	apiServer := &http.Server{
		Addr:              apiAddr,
		Handler:           http.TimeoutHandler(apiRouter.NewRouter(), appConfigs.ServerConfig.Timeouts.Handle, "timeout"),
		WriteTimeout:      appConfigs.ServerConfig.Timeouts.Write,
		ReadTimeout:       appConfigs.ServerConfig.Timeouts.Read,
		ReadHeaderTimeout: appConfigs.ServerConfig.Timeouts.ReadHeader,
		IdleTimeout:       appConfigs.ServerConfig.Timeouts.Idle,
		Protocols:         &apiProtocols,
		BaseContext:       func(net.Listener) context.Context { return shutdownCtx },
	}

	uiRouter := ui.NewRouter(messagesService, sessionsService, queuesService, throttlingService, authSecret, env, trustProxyHeaders)

	var uiProtocols http.Protocols
	uiProtocols.SetUnencryptedHTTP2(true)
	uiProtocols.SetHTTP1(true)

	uiServer := &http.Server{
		Addr:              uiAddr,
		Handler:           http.TimeoutHandler(uiRouter.NewRouter(), appConfigs.ServerConfig.Timeouts.Handle, "timeout"),
		WriteTimeout:      appConfigs.ServerConfig.Timeouts.Write,
		ReadTimeout:       appConfigs.ServerConfig.Timeouts.Read,
		ReadHeaderTimeout: appConfigs.ServerConfig.Timeouts.ReadHeader,
		IdleTimeout:       appConfigs.ServerConfig.Timeouts.Idle,
		Protocols:         &uiProtocols,
		BaseContext:       func(net.Listener) context.Context { return shutdownCtx },
	}

	// Start API server
	go func() {
		log.Info().Msgf("Starting API server on %s", apiAddr)
		err := apiServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn().Err(err).Msg("API server failed")
			serverFailedOnce.Do(func() { close(serverFailedCh) })
		}
	}()

	// Start UI server
	go func() {
		log.Info().Msgf("Starting UI server on %s", uiAddr)
		err := uiServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn().Err(err).Msg("UI server failed")
			serverFailedOnce.Do(func() { close(serverFailedCh) })
		}
	}()

	// Block until a shutdown signal arrives or one of the servers dies.
	select {
	case <-shutdownCtx.Done():
		log.Info().Msg("shutdown signal received")
	case <-serverFailedCh:
		log.Warn().Msg("server failure, shutting down")
	}

	// cancel shutdownCtx on BOTH trigger paths: on the server-failure path it
	// is not cancelled yet, and in-flight request contexts (incl. long polls
	// on the surviving server) hang off it via BaseContext - without this they
	// would hold Shutdown until its deadline instead of returning immediately
	stopSignals()

	// In-flight requests see their contexts cancelled via BaseContext, so this
	// deadline only needs to cover response writing, not the 30s long poll.
	gracefulCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := apiServer.Shutdown(gracefulCtx); err != nil {
		log.Warn().Err(err).Msg("graceful API server shutdown failed, closing forcefully")
		if err := apiServer.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close API server")
		}
	}
	if err := uiServer.Shutdown(gracefulCtx); err != nil {
		log.Warn().Err(err).Msg("graceful UI server shutdown failed, closing forcefully")
		if err := uiServer.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close UI server")
		}
	}

	log.Info().Msg("servers stopped, closing jobs and database")
	// jobs and repo are closed by the deferred Close() calls above (LIFO: jobs
	// first, repo last).
}

func getEnv() string {
	env := os.Getenv("FORQ_ENV")
	if env == "" {
		env = common.ProEnv // Default to production environment
	}

	if !common.SupportedEnvs[env] {
		log.Fatal().Msgf("unsupported environment: %s", env)
	}
	return env
}

func getAuthSecret() string {
	authSecret := os.Getenv("FORQ_AUTH_SECRET")
	if authSecret == "" {
		log.Fatal().Msg("auth secret is not provided: set FORQ_AUTH_SECRET environment variable")
	}
	if len(authSecret) < minAuthSecretLength {
		log.Fatal().Msgf("auth secret is too short: must be at least %d characters", minAuthSecretLength)
	}
	return authSecret
}

func getTrustProxyHeaders() bool {
	v := os.Getenv("FORQ_TRUST_PROXY_HEADERS")
	if v == "" {
		return false
	}
	trust, err := strconv.ParseBool(v)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to parse FORQ_TRUST_PROXY_HEADERS env var")
	}
	if trust {
		log.Warn().Msg("FORQ_TRUST_PROXY_HEADERS=true: client IP will be read from X-Forwarded-For. " +
			"Forq MUST be behind a reverse proxy that strips or replaces incoming X-Forwarded-For from clients, " +
			"otherwise IPs are spoofable and throttling can be bypassed.")
	}
	return trust
}

func getMetricsConfigs() (bool, string) {
	metricsEnabledEnv := os.Getenv("FORQ_METRICS_ENABLED")
	if metricsEnabledEnv == "" {
		return false, "" // Metrics disabled by default
	}

	metricsEnabled, err := strconv.ParseBool(metricsEnabledEnv)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to parse FORQ_METRICS_ENABLED env var")
	}

	if !metricsEnabled {
		return false, ""
	}

	metricsAuthSecret := os.Getenv("FORQ_METRICS_AUTH_SECRET")
	if metricsAuthSecret == "" {
		log.Fatal().Msg("FORQ_METRICS_AUTH_SECRET env var is required when metrics are enabled")
	}
	if len(metricsAuthSecret) < minAuthSecretLength {
		log.Fatal().Msgf("metrics auth secret is too short: must be at least %d characters", minAuthSecretLength)
	}
	return true, metricsAuthSecret
}

func getTtlConfigs() (int, int) {
	queueTtlEnv := os.Getenv("FORQ_QUEUE_TTL_HOURS")
	dlqTtlEnv := os.Getenv("FORQ_DLQ_TTL_HOURS")

	// Default values
	queueTtlHours := 24   // 1 day
	dlqTtlHours := 7 * 24 // 7 days

	if queueTtlEnv != "" {
		parsed, err := strconv.Atoi(queueTtlEnv)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to parse FORQ_QUEUE_TTL_HOURS env var")
		}
		if parsed < 1 {
			log.Fatal().Msg("FORQ_QUEUE_TTL_HOURS must be at least 1 hour")
		}
		queueTtlHours = parsed
	}

	if dlqTtlEnv != "" {
		parsed, err := strconv.Atoi(dlqTtlEnv)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to parse FORQ_DLQ_TTL_HOURS env var")
		}
		if parsed < 1 {
			log.Fatal().Msg("FORQ_DLQ_TTL_HOURS must be at least 1 hour")
		}
		dlqTtlHours = parsed
	}

	return queueTtlHours, dlqTtlHours
}

func getServerAddrs() (string, string) {
	apiAddr := os.Getenv("FORQ_API_ADDR")
	if apiAddr == "" {
		apiAddr = "localhost:8080" // safe default localhost only
	}

	uiAddr := os.Getenv("FORQ_UI_ADDR")
	if uiAddr == "" {
		uiAddr = "localhost:8081" // safe default localhost only
	}

	return apiAddr, uiAddr
}

func getDbPath() string {
	dbPath := os.Getenv("FORQ_DB_PATH")
	if dbPath == "" {
		log.Fatal().Msg("database path is not provided: set FORQ_DB_PATH environment variable")
	}

	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		log.Fatal().Err(err).Msgf("failed to resolve absolute path for FORQ_DB_PATH=%s", dbPath)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		log.Fatal().Err(err).Msgf("failed to create directory for database file at %s", absPath)
	}

	return absPath
}

func runMigrations(dbPath string) {
	// x-no-tx-wrap=true to disable transaction wrapping for PRAGMA statements, as otherwise it fails:
	// https://github.com/golang-migrate/migrate/issues/346
	dbURL := fmt.Sprintf("sqlite://file:%s?cache=shared&mode=rwc&x-no-tx-wrap=true", dbPath)

	source, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create migrations source")
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create migration instance")
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			log.Warn().Err(srcErr).Msg("failed to close migration source")
		}
		if dbErr != nil {
			log.Warn().Err(dbErr).Msg("failed to close migration database connection")
		}
	}()

	err = m.Up()
	if err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info().Msg("no migrations to run")
			return
		}
		log.Fatal().Err(err).Msg("failed to run migrations")
	} else {
		log.Info().Msg("migrations applied successfully")
	}
}
