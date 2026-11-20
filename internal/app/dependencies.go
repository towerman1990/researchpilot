package app

import (
	"context"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/hibiken/asynq"
	"researchpilot/internal/evaluation"
	taskexecution "researchpilot/internal/execution"
	"researchpilot/internal/repository"
)

type WorkerComponents struct {
	Store  *repository.Store
	Runner taskexecution.Runner
	Client *asynq.Client
	Server *asynq.Server
	Queue  string
}
type Assembly interface {
	API() (func(route.IRouter), func(), error)
	Worker() (WorkerComponents, func(), error)
	Evaluation() (*evaluation.Evaluator, error)
	Migrate(context.Context) error
	RotateModelKeys(context.Context) error
}

// OpenAssembly initializes the dependencies for the selected command.
type OpenAssembly func(ctx context.Context, requiresSchema bool) (Assembly, func(), error)
