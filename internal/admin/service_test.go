package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/obiente/support/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginCreatesBoundedSessionAndRotatesCSRF(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	memory := store.NewMemory()
	service := New(memory, "maintainer", string(passwordHash))
	service.now = func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }

	if _, err := service.Login(context.Background(), "maintainer", "wrong", "127.0.0.1"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("invalid login error = %v", err)
	}
	credentials, err := service.Login(context.Background(), "maintainer", "correct horse battery staple", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Session(context.Background(), credentials.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if !service.CheckCSRF(session, credentials.CSRFToken) {
		t.Fatal("issued CSRF token was rejected")
	}
	rotated, err := service.RotateCSRF(context.Background(), credentials.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	rotatedSession, err := service.Session(context.Background(), credentials.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if service.CheckCSRF(rotatedSession, credentials.CSRFToken) || !service.CheckCSRF(rotatedSession, rotated) {
		t.Fatal("CSRF rotation did not revoke the previous token")
	}
	if err := service.Logout(context.Background(), credentials.SessionToken, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Session(context.Background(), credentials.SessionToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("logged-out session error = %v", err)
	}
}
