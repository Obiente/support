package admin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"

	"github.com/obiente/support/internal/domain"
	"github.com/obiente/support/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var ErrUnauthorized = errors.New("admin authentication failed")

const sessionLifetime = 12 * time.Hour

type Credentials struct {
	SessionToken string
	CSRFToken    string
	Username     string
	ExpiresAt    time.Time
}

type Service struct {
	store        store.AdminSessions
	username     string
	passwordHash []byte
	auditKey     []byte
	now          func() time.Time
}

func New(sessions store.AdminSessions, username, passwordHash string) *Service {
	auditKey := sha256.Sum256([]byte("obiente-support-admin-audit:" + passwordHash))
	return &Service{
		store: sessions, username: username, passwordHash: []byte(passwordHash), auditKey: auditKey[:],
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (service *Service) Login(ctx context.Context, username, password, remote string) (Credentials, error) {
	usernameMatches := subtle.ConstantTimeCompare([]byte(username), []byte(service.username)) == 1
	passwordMatches := bcrypt.CompareHashAndPassword(service.passwordHash, []byte(password)) == nil
	if !usernameMatches || !passwordMatches {
		_ = service.audit(ctx, service.username, "login_failed", nil, remote)
		return Credentials{}, ErrUnauthorized
	}
	sessionToken, err := randomToken()
	if err != nil {
		return Credentials{}, err
	}
	csrfToken, err := randomToken()
	if err != nil {
		return Credentials{}, err
	}
	now := service.now()
	credentials := Credentials{
		SessionToken: sessionToken, CSRFToken: csrfToken, Username: service.username,
		ExpiresAt: now.Add(sessionLifetime),
	}
	if err := service.store.CreateAdminSession(ctx, domain.AdminSession{
		TokenHash: digest(sessionToken), CSRFHash: digest(csrfToken), Username: service.username,
		CreatedAt: now, ExpiresAt: credentials.ExpiresAt,
	}); err != nil {
		return Credentials{}, err
	}
	_ = service.store.DeleteExpiredAdminSessions(ctx, now)
	if err := service.audit(ctx, service.username, "login_succeeded", nil, remote); err != nil {
		_ = service.store.DeleteAdminSession(ctx, digest(sessionToken))
		return Credentials{}, err
	}
	return credentials, nil
}

func (service *Service) Session(ctx context.Context, token string) (domain.AdminSession, error) {
	if !validToken(token) {
		return domain.AdminSession{}, ErrUnauthorized
	}
	session, err := service.store.AdminSessionByHash(ctx, digest(token), service.now())
	if errors.Is(err, store.ErrNotFound) {
		return domain.AdminSession{}, ErrUnauthorized
	}
	return session, err
}

func (service *Service) RotateCSRF(ctx context.Context, token string) (string, error) {
	if _, err := service.Session(ctx, token); err != nil {
		return "", err
	}
	csrfToken, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := service.store.RotateAdminCSRF(ctx, digest(token), digest(csrfToken)); err != nil {
		return "", err
	}
	return csrfToken, nil
}

func (service *Service) CheckCSRF(session domain.AdminSession, csrfToken string) bool {
	return validToken(csrfToken) && subtle.ConstantTimeCompare(session.CSRFHash, digest(csrfToken)) == 1
}

func (service *Service) Logout(ctx context.Context, token, remote string) error {
	session, err := service.Session(ctx, token)
	if err != nil {
		return err
	}
	if err := service.store.DeleteAdminSession(ctx, digest(token)); err != nil {
		return err
	}
	return service.audit(ctx, session.Username, "logout", nil, remote)
}

func (service *Service) Audit(ctx context.Context, username, action string, reportID *string, remote string) error {
	return service.audit(ctx, username, action, reportID, remote)
}

func (service *Service) audit(ctx context.Context, username, action string, reportID *string, remote string) error {
	hash := hmac.New(sha256.New, service.auditKey)
	hash.Write([]byte(remote))
	return service.store.RecordAdminAudit(ctx, domain.AdminAudit{
		Username: username, Action: action, ReportID: reportID, RemoteHash: hash.Sum(nil), CreatedAt: service.now(),
	})
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func validToken(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func digest(value string) []byte {
	valueHash := sha256.Sum256([]byte(value))
	return valueHash[:]
}
