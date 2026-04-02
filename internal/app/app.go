package app

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"fascinate/internal/browserauth"
	"fascinate/internal/browserterm"
	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
	"fascinate/internal/email"
	"fascinate/internal/httpapi"
	"fascinate/internal/runtime/cloudhypervisor"
	"fascinate/internal/toolauth"
)

const runtimeReconcileInterval = 30 * time.Second
const initialRuntimeReconcileTimeout = 2 * time.Minute

type App struct {
	cfg        config.Config
	db         *database.Store
	control    *controlplane.Service
	httpServer *http.Server
	readiness  *startupReadiness

	listen           func(network, address string) (net.Listener, error)
	initialReconcile func(context.Context) error
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

	runtimeClient, err := cloudhypervisor.New(cfg)
	if err != nil {
		store.Close()
		return nil, err
	}
	toolAuthStore, err := toolauth.NewStore(cfg)
	if err != nil {
		store.Close()
		return nil, err
	}
	toolAuthManager, err := toolauth.NewManager(
		toolAuthStore,
		runtimeClient,
		toolauth.ClaudeSubscriptionAdapter{},
		toolauth.CodexChatGPTAdapter{},
		toolauth.GitHubCLIAdapter{},
	)
	if err != nil {
		store.Close()
		return nil, err
	}
	controlPlane := controlplane.New(cfg, store, runtimeClient, toolAuthManager)
	hostCtx, hostCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if _, err := controlPlane.EnsureLocalHost(hostCtx); err != nil {
		hostCancel()
		store.Close()
		return nil, err
	}
	if err := controlPlane.HeartbeatLocalHost(hostCtx); err != nil {
		log.Printf("fascinate: initial host heartbeat: %v", err)
	}
	hostCancel()
	emailClient := email.NewResendClient(cfg.ResendBaseURL, cfg.ResendAPIKey, cfg.EmailFrom)
	browserAuth := browserauth.New(cfg, store, emailClient)
	terminalGateway := browserterm.New(cfg, controlPlane)
	readiness := newStartupReadiness()
	handler := httpapi.New(cfg, store, runtimeClient, controlPlane, browserAuth, terminalGateway, readiness)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           logRequests(handler),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{
		cfg:        cfg,
		db:         store,
		control:    controlPlane,
		httpServer: httpServer,
		readiness:  readiness,
		listen:     net.Listen,
		initialReconcile: func(ctx context.Context) error {
			return controlPlane.ReconcileRuntimeState(ctx)
		},
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	listen := a.listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", a.httpServer.Addr)
	if err != nil {
		return err
	}
	log.Printf("fascinate listening on %s", listener.Addr().String())

	go func() {
		err := a.httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	go a.runInitialRuntimeReconcile(ctx)
	go a.runToolAuthSyncLoop(ctx)
	go a.runRuntimeReconcileLoop(ctx)
	go a.runHostHeartbeatLoop(ctx)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (a *App) runInitialRuntimeReconcile(ctx context.Context) {
	if a == nil || a.readiness == nil {
		return
	}
	reconcile := a.initialReconcile
	if reconcile == nil {
		a.readiness.MarkReady()
		return
	}

	reconcileCtx, cancel := context.WithTimeout(ctx, initialRuntimeReconcileTimeout)
	defer cancel()
	if err := reconcile(reconcileCtx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("fascinate: initial runtime reconcile: %v", err)
	}
	if ctx.Err() == nil {
		a.readiness.MarkReady()
	}
}

func (a *App) runToolAuthSyncLoop(ctx context.Context) {
	if a == nil || a.control == nil || a.cfg.ToolAuthSyncInterval <= 0 {
		return
	}

	ticker := time.NewTicker(a.cfg.ToolAuthSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ToolAuthSyncInterval)
			if err := a.control.SyncRunningToolAuth(syncCtx); err != nil {
				log.Printf("fascinate: sync tool auth: %v", err)
			}
			cancel()
		}
	}
}

func (a *App) runRuntimeReconcileLoop(ctx context.Context) {
	if a == nil || a.control == nil || runtimeReconcileInterval <= 0 {
		return
	}
	if a.readiness != nil {
		select {
		case <-ctx.Done():
			return
		case <-a.readiness.Done():
		}
	}

	ticker := time.NewTicker(runtimeReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcileCtx, cancel := context.WithTimeout(context.Background(), runtimeReconcileInterval)
			if err := a.control.ReconcileRuntimeState(reconcileCtx); err != nil {
				log.Printf("fascinate: reconcile runtime: %v", err)
			}
			cancel()
		}
	}
}

func (a *App) runHostHeartbeatLoop(ctx context.Context) {
	if a == nil || a.control == nil || a.cfg.HostHeartbeatInterval <= 0 {
		return
	}

	ticker := time.NewTicker(a.cfg.HostHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeatCtx, cancel := context.WithTimeout(context.Background(), a.cfg.HostHeartbeatInterval)
			if err := a.control.HeartbeatLocalHost(heartbeatCtx); err != nil {
				log.Printf("fascinate: local host heartbeat: %v", err)
			}
			cancel()
		}
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

type startupReadiness struct {
	mu     sync.RWMutex
	ready  bool
	status string
	done   chan struct{}
}

func newStartupReadiness() *startupReadiness {
	return &startupReadiness{
		status: "startup recovery in progress",
		done:   make(chan struct{}),
	}
}

func (r *startupReadiness) MarkReady() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ready {
		return
	}
	r.ready = true
	r.status = "ready"
	close(r.done)
}

func (r *startupReadiness) ReadinessStatus() (bool, string) {
	if r == nil {
		return true, "ready"
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ready, r.status
}

func (r *startupReadiness) Done() <-chan struct{} {
	if r == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return r.done
}

func isAdminEmail(adminEmails []string, email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, candidate := range adminEmails {
		if strings.ToLower(strings.TrimSpace(candidate)) == email {
			return true
		}
	}

	return false
}
