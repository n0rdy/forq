package services

import "forq/db"

type MonitoringService struct {
	repo *db.ForqRepo
}

func NewMonitoringService(repo *db.ForqRepo) *MonitoringService {
	return &MonitoringService{
		repo: repo,
	}
}

func (ms *MonitoringService) IsHealthy() bool {
	err := ms.repo.Ping()
	return err == nil
}
