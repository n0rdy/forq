package services

import (
	"context"

	"github.com/n0rdy/forq/db"
)

type MonitoringService struct {
	repo *db.ForqRepo
}

func NewMonitoringService(repo *db.ForqRepo) *MonitoringService {
	return &MonitoringService{
		repo: repo,
	}
}

func (ms *MonitoringService) IsHealthy(ctx context.Context) bool {
	err := ms.repo.Ping(ctx)
	return err == nil
}
