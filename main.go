package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"forq/api"
	"forq/common"
	"forq/configs"
	"forq/db"
	"forq/jobs/cleanup"
	"forq/services"
	"forq/ui"
	"forq/utils"
	"net/http"
	"os"
	"sync"

	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/golang-migrate/migrate/v4"
	"github.com/rs/zerolog/log"
)

func main() {
	env := getEnv()
	if !common.SupportedEnvs[env] {
		log.Fatal().Msgf("unsupported environment: %s", env)
		panic(fmt.Sprintf("unsupported environment: %s", env))
	}

	authSecret := getAuthSecret()
	if authSecret == "" {
		log.Fatal().Msg("auth secret is not provided: either set FORQ_AUTH_SECRET environment variable or pass it as a command line argument --auth-secret")
		panic("auth secret is not provided: either set FORQ_AUTH_SECRET environment variable or pass it as a command line argument --auth-secret")
	}

	dbPath, err := utils.GetOrCreateDefaultDBPath()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to get or create default database path")
		panic(err)
	}

	runMigrations(dbPath)

	appConfigs := configs.NewAppConfig()

	repo, err := db.NewSQLiteRepo(dbPath, appConfigs)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create SQLite repository")
		panic(err)
	}
	defer repo.Close()

	queuesService := services.NewQueuesService(repo)
	messagesService := services.NewMessagesService(repo, appConfigs)
	sessionsService := services.NewSessionsService()
	defer sessionsService.Close()

	expiredMessagesCleanupJob := cleanup.NewExpiredMessagesCleanupJob(repo, appConfigs.JobsIntervals.ExpiredMessagesCleanupMs)
	defer expiredMessagesCleanupJob.Close()
	expiredDlqMessagesCleanupJob := cleanup.NewExpiredDlqMessagesCleanupJob(repo, appConfigs.JobsIntervals.ExpiredDlqMessagesCleanupMs)
	defer expiredDlqMessagesCleanupJob.Close()
	failedMessagesCleanupJob := cleanup.NewFailedMessagesCleanupJob(repo, appConfigs.JobsIntervals.FailedMessagesCleanupMs)
	defer failedMessagesCleanupJob.Close()
	failedDlqMessagesCleanupJob := cleanup.NewFailedDlqMessagesCleanupJob(repo, appConfigs.JobsIntervals.FailedDqlMessagesCleanupMs)
	defer failedDlqMessagesCleanupJob.Close()
	staleMessagesCleanupJob := cleanup.NewStaleMessagesCleanupJob(repo, appConfigs.JobsIntervals.StaleMessagesCleanupMs)
	defer staleMessagesCleanupJob.Close()

	shutdownCh := make(chan struct{})
	var shutdownOnce sync.Once

	// Create API router (HTTP/2 only)
	apiRouter := api.NewRouter(messagesService, authSecret)

	// API server protocols - HTTP/2 only
	var apiProtocols http.Protocols
	apiProtocols.SetUnencryptedHTTP2(true)
	apiProtocols.SetHTTP1(false)

	apiServer := &http.Server{
		Addr:              "localhost:8080",
		Handler:           http.TimeoutHandler(apiRouter.NewRouter(), appConfigs.ServerConfig.Timeouts.Handle, "timeout"),
		WriteTimeout:      appConfigs.ServerConfig.Timeouts.Write,
		ReadTimeout:       appConfigs.ServerConfig.Timeouts.Read,
		ReadHeaderTimeout: appConfigs.ServerConfig.Timeouts.ReadHeader,
		IdleTimeout:       appConfigs.ServerConfig.Timeouts.Idle,
		Protocols:         &apiProtocols,
	}

	// Create UI router (HTTP/1.1 + HTTP/2)
	uiRouter := ui.NewRouter(messagesService, sessionsService, queuesService, authSecret, env)

	// UI server protocols - HTTP/1.1 + HTTP/2 for browser compatibility
	var uiProtocols http.Protocols
	uiProtocols.SetUnencryptedHTTP2(true)
	uiProtocols.SetHTTP1(true)

	uiServer := &http.Server{
		Addr:              "localhost:8081",
		Handler:           http.TimeoutHandler(uiRouter.NewRouter(), appConfigs.ServerConfig.Timeouts.Handle, "timeout"),
		WriteTimeout:      appConfigs.ServerConfig.Timeouts.Write,
		ReadTimeout:       appConfigs.ServerConfig.Timeouts.Read,
		ReadHeaderTimeout: appConfigs.ServerConfig.Timeouts.ReadHeader,
		IdleTimeout:       appConfigs.ServerConfig.Timeouts.Idle,
		Protocols:         &uiProtocols,
	}

	// Start API server
	go func() {
		log.Info().Msg("Starting API server on :8080 (HTTP/2 only)")
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
		log.Info().Msg("Starting UI server on :8081 (HTTP/1.1 + HTTP/2)")
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

func getAuthSecret() string {
	authSecret := os.Getenv("FORQ_AUTH_SECRET")
	if authSecret != "" {
		return authSecret
	}

	var flagAuthSecret string
	flag.StringVar(&flagAuthSecret, "auth-secret", "", "Authentication secret")
	flag.Parse()

	return flagAuthSecret
}

func getEnv() string {
	env := os.Getenv("FORQ_ENV")
	if env != "" {
		return env
	}

	var flagEnv string
	flag.StringVar(&flagEnv, "env", common.ProEnv, "Application environment ("+common.LocalEnv+"|"+common.ProEnv+")")
	flag.Parse()
	return flagEnv
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
