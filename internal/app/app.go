package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
	"fascinate/internal/httpapi"
	"fascinate/internal/runtime/incus"
)

type App struct {
	cfg        config.Config
	db         *database.Store
	httpServer *http.Server
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}

	store, err := database.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}

	if err := store.Migrate(ctx); err != nil {
		store.Close()
		return nil, err
	}

	runtimeClient := incus.NewCLI(cfg.IncusBinary)
	controlPlane := controlplane.New(cfg, store, runtimeClient)
	handler := httpapi.New(cfg, store, runtimeClient, controlPlane)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           logRequests(handler),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{
		cfg:        cfg,
		db:         store,
		httpServer: httpServer,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		err := a.httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (a *App) Close() error {
	return a.db.Close()
}

func RunMigrations(ctx context.Context, cfg config.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}

	store, err := database.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	return store.Migrate(ctx)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
