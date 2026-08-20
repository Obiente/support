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

	invalidPublicURLs := []struct {
		name      string
		publicURL string
		message   string
	}{
		{name: "omitted", message: "required"},
		{name: "HTTP", publicURL: "http://support.example", message: "HTTPS"},
		{name: "user information", publicURL: "https://operator@support.example", message: "user information"},
		{name: "non-root path", publicURL: "https://support.example/private", message: "non-root path"},
		{name: "query", publicURL: "https://support.example?source=app", message: "query"},
		{name: "empty query", publicURL: "https://support.example?", message: "query"},
		{name: "fragment", publicURL: "https://support.example#receipt", message: "fragment"},
	}
	for _, test := range invalidPublicURLs {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SUPPORT_PUBLIC_URL", test.publicURL)
			if _, err := FromEnvironment(); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("production public URL error = %v, want %q", err, test.message)
			}
		})
	}
	t.Setenv("SUPPORT_PUBLIC_URL", "https://support.example")
	t.Setenv("SUPPORT_ADMIN_PASSWORD_HASH", "plaintext")
	if _, err := FromEnvironment(); err == nil || !strings.Contains(err.Error(), "bcrypt") {
		t.Fatalf("plaintext admin password error = %v", err)
	}
}
