package app

import (
	"context"
	"errors"
	"strings"

	"researchpilot/internal/common/envconfig"
	"researchpilot/internal/transport/httpserver"
)

type startupCommand struct {
	Name string
	Run  func() error
}

func dispatchCommand(args []string, commands []startupCommand) error {
	if len(args) == 1 {
		for _, cmd := range commands {
			if cmd.Name == args[0] {
				return cmd.Run()
			}
		}
	}
	names := make([]string, 0, len(commands))
	for _, cmd := range commands {
		names = append(names, cmd.Name)
	}
	return errors.New("usage: researchpilot " + strings.Join(names, "|"))
}

type startupHandler func(context.Context, Assembly) error

func startupCommands(open OpenAssembly) []startupCommand {
	bind := func(requiresSchema bool, handler startupHandler) func() error {
		return func() error { return run(open, requiresSchema, handler) }
	}
	return []startupCommand{
		{Name: "api", Run: bind(true, serveAPI)},
		{Name: "worker", Run: bind(true, serveWorker)},
		{Name: "migrate", Run: bind(false, migrateDatabase)},
		{Name: "rotate-model-keys", Run: bind(true, rotateKeys)},
		{Name: "evaluate", Run: bind(true, evaluateResearch)},
	}
}

func serveAPI(ctx context.Context, assembly Assembly) error {
	register, cleanup, err := assembly.API()
	if err != nil {
		return err
	}
	defer cleanup()
	return httpserver.Serve(ctx, envconfig.String("HTTP_ADDR", ":8080"), register)
}

func serveWorker(ctx context.Context, assembly Assembly) error {
	worker, cleanup, err := assembly.Worker()
	if err != nil {
		return err
	}
	defer cleanup()
	return runWorker(ctx, worker)
}

func migrateDatabase(ctx context.Context, assembly Assembly) error { return assembly.Migrate(ctx) }

func rotateKeys(ctx context.Context, assembly Assembly) error { return assembly.RotateModelKeys(ctx) }

func evaluateResearch(ctx context.Context, assembly Assembly) error {
	evaluator, err := assembly.Evaluation()
	if err != nil {
		return err
	}
	return evaluator.Run(ctx)
}
