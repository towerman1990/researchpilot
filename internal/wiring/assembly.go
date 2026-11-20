package wiring

import (
	"context"
	"database/sql"

	"github.com/cloudwego/hertz/pkg/route"

	"researchpilot/internal/app"
	"researchpilot/internal/common/envconfig"
	"researchpilot/internal/evaluation"
	"researchpilot/internal/persistence/migrations"
)

type assembly struct{ db *sql.DB }

// Open creates the connection pool for the selected command.
func Open(ctx context.Context, requiresSchema bool) (app.Assembly, func(), error) {
	db, cleanup, err := initializeDatabase(ctx)
	if err != nil {
		return nil, nil, err
	}
	if requiresSchema {
		if err := migrations.CheckReady(ctx, db); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	return assembly{db: db}, cleanup, nil
}

func (a assembly) API() (func(route.IRouter), func(), error) {
	components, cleanup, err := initializeAPI(a.db, readConfig())
	if err != nil {
		return nil, nil, err
	}
	return components.register, cleanup, nil
}
func (a assembly) Worker() (app.WorkerComponents, func(), error) {
	return initializeWorker(a.db, readConfig())
}
func (a assembly) Evaluation() (*evaluation.Evaluator, error) { return initializeEvaluation(a.db) }
func (a assembly) Migrate(ctx context.Context) error {
	return migrations.Migrate(ctx, a.db, envconfig.String("MIGRATIONS_DIR", "migrations"))
}
func (a assembly) RotateModelKeys(ctx context.Context) error {
	rotation, err := initializeKeyRotation(a.db)
	if err != nil {
		return err
	}
	return rotation.Run(ctx)
}
