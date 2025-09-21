package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"

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
	"github.com/n0rdy/forq/utils"

	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/golang-migrate/migrate/v4"
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

	dbPath, err := getDbPath()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get or create default database path")
		panic(err)
	}
	log.Info().Msgf("using database file at: %s", dbPath)

	runMigrations(dbPath)

	appConfigs := configs.NewAppConfig(metricsEnabled, queueTtlHours, dlqTtlHours)

	repo, err := db.NewSQLiteRepo(dbPath, appConfigs)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create SQLite repository")
		panic(err)
	}
	defer repo.Close()

	monirotingService := services.NewMonitoringService(repo)
	metricsService := metrics.NewMetricsService(metricsEnabled)
	queuesService := services.NewQueuesService(repo)
	messagesService := services.NewMessagesService(metricsService, repo, appConfigs)
	sessionsService := services.NewSessionsService()
	defer sessionsService.Close()

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

	shutdownCh := make(chan struct{})
	var shutdownOnce sync.Once

	apiRouter := api.NewRouter(monirotingService, messagesService, authSecret, metricsEnabled, metricsAuthSecret)

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
	}

	uiRouter := ui.NewRouter(messagesService, sessionsService, queuesService, authSecret, env)

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
	}

	// Start API server
	go func() {
		log.Info().Msgf("Starting API server on %s", apiAddr)
		err := apiServer.ListenAndServe()
		if err != nil {
			shutdownOnce.Do(func() { close(shutdownCh) })
			if errors.Is(err, http.ErrServerClosed) {
				log.Info().Msg("API server shutdown")
			} else {
				log.Warn().Err(err).Msg("API server failed")
			}
		}
	}()

	// Start UI server
	go func() {
		log.Info().Msgf("Starting UI server on %s", uiAddr)
		err := uiServer.ListenAndServe()
		if err != nil {
			shutdownOnce.Do(func() { close(shutdownCh) })
			if errors.Is(err, http.ErrServerClosed) {
				log.Info().Msg("UI server shutdown")
			} else {
				log.Warn().Err(err).Msg("UI server failed")
			}
		}
	}()

	for range shutdownCh {
		log.Info().Msg("server shutdown requested")

		// Shutdown API server
		err := apiServer.Shutdown(context.Background())
		if err != nil {
			err := apiServer.Close()
			if err != nil {
				log.Warn().Err(err).Msg("failed to close API server")
			}
		}

		// Shutdown UI server
		err = uiServer.Shutdown(context.Background())
		if err != nil {
			err := uiServer.Close()
			if err != nil {
				log.Warn().Err(err).Msg("failed to close UI server")
			}
		}
	}
}

func getEnv() string {
	env := os.Getenv("FORQ_ENV")
	if env == "" {
		env = common.ProEnv // Default to production environment
	}

	if !common.SupportedEnvs[env] {
		log.Fatal().Msgf("unsupported environment: %s", env)
		panic(fmt.Sprintf("unsupported environment: %s", env))
	}
	return env
}

func getAuthSecret() string {
	authSecret := os.Getenv("FORQ_AUTH_SECRET")
	if authSecret == "" {
		log.Fatal().Msg("auth secret is not provided: set FORQ_AUTH_SECRET environment variable")
		panic("auth secret is not provided: set FORQ_AUTH_SECRET environment variable")
	}
	if len(authSecret) < minAuthSecretLength {
		log.Fatal().Msgf("auth secret is too short: must be at least %d characters", minAuthSecretLength)
		panic(fmt.Sprintf("auth secret is too short: must be at least %d characters", minAuthSecretLength))
	}
	return authSecret
}

func getMetricsConfigs() (bool, string) {
	metricsEnabledEnv := os.Getenv("FORQ_METRICS_ENABLED")
	if metricsEnabledEnv == "" {
		return false, "" // Metrics disabled by default
	}

	metricsEnabled, err := strconv.ParseBool(metricsEnabledEnv)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to parse FORQ_METRICS_ENABLED env var")
		panic(err)
	}

	if !metricsEnabled {
		return false, ""
	}

	metricsAuthSecret := os.Getenv("FORQ_METRICS_AUTH_SECRET")
	if metricsAuthSecret == "" {
		log.Fatal().Msg("FORQ_METRICS_AUTH_SECRET env var is required when metrics are enabled")
		panic("FORQ_METRICS_AUTH_SECRET env var is required when metrics are enabled")
	}
	if len(metricsAuthSecret) < minAuthSecretLength {
		log.Fatal().Msgf("metrics auth secret is too short: must be at least %d characters", minAuthSecretLength)
		panic(fmt.Sprintf("metrics auth secret is too short: must be at least %d characters", minAuthSecretLength))
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
			panic(err)
		}
		if parsed < 1 {
			log.Fatal().Msg("FORQ_QUEUE_TTL_HOURS must be at least 1 hour")
			panic("FORQ_QUEUE_TTL_HOURS must be at least 1 hour")
		}
		queueTtlHours = parsed
	}

	if dlqTtlEnv != "" {
		parsed, err := strconv.Atoi(dlqTtlEnv)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to parse FORQ_DLQ_TTL_HOURS env var")
			panic(err)
		}
		if parsed < 1 {
			log.Fatal().Msg("FORQ_DLQ_TTL_HOURS must be at least 1 hour")
			panic("FORQ_DLQ_TTL_HOURS must be at least 1 hour")
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

func getDbPath() (string, error) {
	dbPath := os.Getenv("FORQ_DB_PATH")
	if dbPath == "" {
		return utils.GetOrCreateDefaultDBPath()
	}
	return dbPath, nil
}

func runMigrations(dbPath string) {
	// x-no-tx-wrap=true to disable transaction wrapping for PRAGMA statements, as otherwise it fails:
	// https://github.com/golang-migrate/migrate/issues/346
	dbURL := fmt.Sprintf("sqlite3://file:%s?cache=shared&mode=rwc&x-no-tx-wrap=true", dbPath)

	m, err := migrate.New("file://db/migrations", dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create migration instance")
		panic(err)
	}

	err = m.Up()
	if err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info().Msg("no migrations to run")
			return
		}
		log.Fatal().Err(err).Msg("failed to run migrations")
		panic(fmt.Errorf("failed to run migrations: %w", err))
	} else {
		log.Info().Msg("migrations applied successfully")
	}
}
