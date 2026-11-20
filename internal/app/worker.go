package app

import (
	"context"
	"log"
	taskservice "researchpilot/internal/task"
	"sync"
	"time"

	"github.com/hibiken/asynq"
)

func runWorker(ctx context.Context, deps WorkerComponents) error {
	store, client, queue, r := deps.Store, deps.Client, deps.Queue, deps.Runner
	server := deps.Server
	mux := asynq.NewServeMux()
	mux.HandleFunc(taskservice.RunTaskName, r.Execute)
	if err := server.Start(mux); err != nil {
		return err
	}
	defer server.Shutdown()

	var jobs sync.WaitGroup
	jobs.Go(func() {
		runWorkerJob(ctx, "outbox relay", time.Second, 10*time.Second, func(ctx context.Context) error {
			return taskservice.RelayOutbox(ctx, store, client, queue)
		})
	})
	jobs.Go(func() {
		var after string
		runWorkerJob(ctx, "task recovery", 5*time.Second, 30*time.Second, func(ctx context.Context) error {
			next, err := r.Recover(ctx, after)
			after = next
			return err
		})
	})
	jobs.Go(func() {
		runWorkerJob(ctx, "event retention", time.Hour, 30*time.Second, func(ctx context.Context) error {
			return taskservice.PruneEvents(ctx, store)
		})
	})
	jobs.Go(func() {
		runWorkerJob(ctx, "research schedules", 5*time.Second, 10*time.Second, func(ctx context.Context) error {
			return taskservice.RunDueSchedules(ctx, store, time.Now())
		})
	})
	log.Printf("worker listening on %s", queue)
	<-ctx.Done()
	jobs.Wait()
	return nil
}

func runWorkerJob(ctx context.Context, name string, interval, timeout time.Duration, run func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jobCtx, cancel := context.WithTimeout(ctx, timeout)
			err := run(jobCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Printf("%s: %v", name, err)
			}
		}
	}
}
