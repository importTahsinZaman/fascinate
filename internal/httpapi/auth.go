package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"fascinate/internal/browserauth"
	"fascinate/internal/config"
	"fascinate/internal/database"
)

type browserAuthService interface {
	Enabled() bool
	RequestCode(context.Context, string) error
	VerifyCode(context.Context, string, string, string, string) (browserauth.Session, error)
	Authenticate(context.Context, string) (database.User, database.WebSessionRecord, error)
	Logout(context.Context, string) error
}

type sessionContext struct {
	User   database.User
	Record database.WebSessionRecord
}

func readSessionFromRequest(ctx context.Context, r *http.Request, cfg config.Config, auth browserAuthService) (*sessionContext, error) {
	if auth == nil {
		return nil, database.ErrNotFound
	}
	cookie, err := r.Cookie(cfg.WebSessionCookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return nil, database.ErrNotFound
		}
		return nil, err
	}
	user, record, err := auth.Authenticate(ctx, strings.TrimSpace(cookie.Value))
	if err != nil {
		return nil, err
	}
	return &sessionContext{User: user, Record: record}, nil
}

func ownerEmailForRequest(ctx context.Context, r *http.Request, cfg config.Config, auth browserAuthService, explicit string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit, nil
	}
	session, err := readSessionFromRequest(ctx, r, cfg, auth)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return "", errors.New("authentication required")
		}
		return "", err
	}
	return session.User.Email, nil
}

func requireBrowserSession(ctx context.Context, r *http.Request, cfg config.Config, auth browserAuthService) (*sessionContext, error) {
	session, err := readSessionFromRequest(ctx, r, cfg, auth)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, errors.New("authentication required")
		}
		return nil, err
	}
	return session, nil
}

func setSessionCookie(w http.ResponseWriter, cfg config.Config, rawToken string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     cfg.WebSessionCookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, cfg config.Config) {
	http.SetCookie(w, &http.Cookie{
		Name:     cfg.WebSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func requestIPAddress(r *http.Request) string {
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if host != "" {
		parts := strings.Split(host, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
