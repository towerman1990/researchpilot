package app

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func Main(open OpenAssembly) {
	if err := dispatchCommand(os.Args[1:], startupCommands(open)); err != nil {
		log.Fatal(err)
	}
}

func run(open OpenAssembly, requiresSchema bool, handler startupHandler) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	assembly, cleanup, err := open(ctx, requiresSchema)
	if err != nil {
		return err
	}
	defer cleanup()
	return handler(ctx, assembly)
}
