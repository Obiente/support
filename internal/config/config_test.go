package config

import (
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestProductionRequiresHTTPSAndBcryptAdminCredentials(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("test password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUPPORT_ENVIRONMENT", "production")
	t.Setenv("SUPPORT_PUBLIC_URL", "https://support.example")
	t.Setenv("DATABASE_URL", "postgres://support:test@database/support")
	t.Setenv("SUPPORT_DATA_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("SUPPORT_ADMIN_USERNAME", "maintainer")
	t.Setenv("SUPPORT_ADMIN_PASSWORD_HASH", string(passwordHash))

	configuration, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.SecureCookies {
		t.Fatal("production HTTPS did not enable secure admin cookies")
	}

	t.Setenv("SUPPORT_PUBLIC_URL", "http://support.example")
	if _, err := FromEnvironment(); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("HTTP production error = %v", err)
	}
	t.Setenv("SUPPORT_PUBLIC_URL", "https://support.example")
	t.Setenv("SUPPORT_ADMIN_PASSWORD_HASH", "plaintext")
	if _, err := FromEnvironment(); err == nil || !strings.Contains(err.Error(), "bcrypt") {
		t.Fatalf("plaintext admin password error = %v", err)
	}
}
