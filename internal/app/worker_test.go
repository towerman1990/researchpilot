package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkerJobDoesNotBlockAnotherJob(t *testing.T) {
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()

	entered := make(chan struct{}, 1)
	released := make(chan error, 1)
	blockedDone := make(chan struct{})
	go func() {
		defer close(blockedDone)
		runWorkerJob(ctx, "blocked fixture", time.Millisecond, time.Second, func(job context.Context) error {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-job.Done()
			select {
			case released <- job.Err():
			default:
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("slow job did not start")
	}

	quickRan := make(chan struct{}, 1)
	fastDone := make(chan struct{})
	go func() {
		defer close(fastDone)
		runWorkerJob(ctx, "quick fixture", time.Millisecond, time.Second, func(context.Context) error {
			select {
			case quickRan <- struct{}{}:
			default:
			}
			return nil
		})
	}()
	select {
	case <-quickRan:
	case <-released:
		t.Fatal("quick job did not run before slow job reached its deadline")
	case <-ctx.Done():
		t.Fatal("quick job did not run")
	}

	select {
	case err := <-released:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected per-run deadline, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("slow job did not stop at its deadline")
	}
	stop()
	<-blockedDone
	<-fastDone
}
