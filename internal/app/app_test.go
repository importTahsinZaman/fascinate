package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestAppRunServesBeforeInitialReconcileCompletes(t *testing.T) {
	t.Parallel()

	reconcileStarted := make(chan struct{})
	reconcileRelease := make(chan struct{})
	listenerAddr := make(chan string, 1)

	app := &App{
		httpServer: &http.Server{
			Addr: "127.0.0.1:0",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "ok")
			}),
		},
		readiness: newStartupReadiness(),
		listen: func(network, address string) (net.Listener, error) {
			ln, err := net.Listen(network, address)
			if err == nil {
				listenerAddr <- ln.Addr().String()
			}
			return ln, err
		},
		initialReconcile: func(ctx context.Context) error {
			close(reconcileStarted)
			select {
			case <-reconcileRelease:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- app.Run(ctx)
	}()

	addr := <-listenerAddr
	<-reconcileStarted

	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get("http://" + addr)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200 while reconcile is blocked, got %d", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected server to respond before startup reconcile completed: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	ready, status := app.readiness.ReadinessStatus()
	if ready {
		t.Fatalf("expected readiness to stay false during startup reconcile")
	}
	if status != "startup recovery in progress" {
		t.Fatalf("unexpected readiness status %q", status)
	}

	close(reconcileRelease)

	deadline = time.Now().Add(2 * time.Second)
	for {
		ready, status = app.readiness.ReadinessStatus()
		if ready && status == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected readiness to flip after startup reconcile, got ready=%v status=%q", ready, status)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := <-runDone; err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}
}

func TestAppMarksReadyAfterInitialReconcileError(t *testing.T) {
	t.Parallel()

	app := &App{
		httpServer: &http.Server{
			Addr:    "127.0.0.1:0",
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		},
		readiness: newStartupReadiness(),
		initialReconcile: func(context.Context) error {
			return errors.New("recover failed")
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- app.Run(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		ready, status := app.readiness.ReadinessStatus()
		if ready && status == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected readiness to flip even after reconcile error")
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := <-runDone; err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}
}
