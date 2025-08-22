package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"forq/api"
	"forq/configs"
	"forq/db"
	"forq/jobs/cleanup"
	"forq/services"
	"forq/utils"
	"net/http"
	"os"

	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/golang-migrate/migrate/v4"
	"github.com/rs/zerolog/log"
)

func main() {
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

	forqRouter := api.NewForqRouter(messagesService, sessionsService, authSecret)

	// enforcing HTTP2 only
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	protocols.SetHTTP1(false)

	forqServer := &http.Server{
		Addr:              "localhost:8080",
		Handler:           http.TimeoutHandler(forqRouter.NewRouter(), appConfigs.ServerConfig.Timeouts.Handle, "timeout"),
		WriteTimeout:      appConfigs.ServerConfig.Timeouts.Write,
		ReadTimeout:       appConfigs.ServerConfig.Timeouts.Read,
		ReadHeaderTimeout: appConfigs.ServerConfig.Timeouts.ReadHeader,
		IdleTimeout:       appConfigs.ServerConfig.Timeouts.Idle,
		Protocols:         &protocols,
	}

	go func() {
		err := forqServer.ListenAndServe()
		if err != nil {
			close(shutdownCh)
			if errors.Is(err, http.ErrServerClosed) {
				log.Info().Msg("server shutdown")
			} else {
				log.Warn().Err(err).Msg("server failed")
			}
		}
	}()

	for range shutdownCh {
		log.Info().Msg("server shutdown requested")
		err := forqServer.Shutdown(context.Background())
		if err != nil {
			err := forqServer.Close()
			if err != nil {
				log.Warn().Err(err).Msg("failed to close server")
				return
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
