package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
	"fascinate/internal/email"
	"fascinate/internal/httpapi"
	"fascinate/internal/runtime/cloudhypervisor"
	"fascinate/internal/signup"
	"fascinate/internal/sshfrontdoor"
)

type App struct {
	cfg        config.Config
	db         *database.Store
	httpServer *http.Server
	sshServer  *sshfrontdoor.Server
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
	controlPlane := controlplane.New(cfg, store, runtimeClient)
	handler := httpapi.New(cfg, store, runtimeClient, controlPlane)
	emailClient := email.NewResendClient(cfg.ResendBaseURL, cfg.ResendAPIKey, cfg.EmailFrom)
	signupService := signup.New(cfg, store, emailClient)
	sshServer, err := sshfrontdoor.New(cfg, store, controlPlane, signupService)
	if err != nil {
		store.Close()
		return nil, err
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           logRequests(handler),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{
		cfg:        cfg,
		db:         store,
		httpServer: httpServer,
		sshServer:  sshServer,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 2)

	go func() {
		err := a.httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	go func() {
		errCh <- a.sshServer.Run(ctx)
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

func SeedSSHKey(ctx context.Context, cfg config.Config, email, name, publicKey string) (database.SSHKeyRecord, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return database.SSHKeyRecord{}, err
	}

	store, err := database.Open(ctx, cfg.DBPath)
	if err != nil {
		return database.SSHKeyRecord{}, err
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		return database.SSHKeyRecord{}, err
	}

	user, err := store.UpsertUser(ctx, email, isAdminEmail(cfg.AdminEmails, email))
	if err != nil {
		return database.SSHKeyRecord{}, err
	}

	authorizedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return database.SSHKeyRecord{}, err
	}

	return store.CreateSSHKey(ctx, database.CreateSSHKeyParams{
		UserID:      user.ID,
		Name:        name,
		PublicKey:   strings.TrimSpace(publicKey),
		Fingerprint: ssh.FingerprintSHA256(authorizedKey),
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
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
