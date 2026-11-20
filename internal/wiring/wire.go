//go:build wireinject

package wiring

import (
	"context"
	"database/sql"

	"github.com/goforj/wire"
	"researchpilot/internal/app"
	"researchpilot/internal/evaluation"
	"researchpilot/internal/execution"
	"researchpilot/internal/repository"
	"researchpilot/internal/service"
)

func initializeDatabase(ctx context.Context) (*sql.DB, func(), error) {
	wire.Build(provideDatabase)
	return nil, nil, nil
}
func initializeAPI(db *sql.DB, cfg appConfig) (apiComponents, func(), error) {
	wire.Build(repository.New, provideRedis, provideLimiter, provideHealth, provideAuth, provideModelClient, provideModelService, provideModels, provideTaskService, provideTaskActions, provideEventStream, provideTasks, provideWeb, provideAPI)
	return apiComponents{}, nil, nil
}
func initializeWorker(db *sql.DB, cfg appConfig) (app.WorkerComponents, func(), error) {
	wire.Build(repository.New, execution.NewStubProvider, execution.NewStubTool, provideModelClient, provideToolsClient, provideToolRegistry, provideRunner, provideAsynqConfig, provideAsynqClient, provideAsynqServer, provideWorker)
	return app.WorkerComponents{}, nil, nil
}

func initializeEvaluation(db *sql.DB) (*evaluation.Evaluator, error) {
	wire.Build(repository.New, wire.Bind(new(evaluation.Store), new(*repository.Store)), evaluation.New)
	return nil, nil
}

func initializeKeyRotation(db *sql.DB) (service.KeyRotation, error) {
	wire.Build(repository.New, wire.Bind(new(service.KeyRotationStore), new(*repository.Store)), wire.Struct(new(service.KeyRotation), "Store"))
	return service.KeyRotation{}, nil
}
