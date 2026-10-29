package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/obiente/support/internal/domain"
)

// These tests need a disposable PostgreSQL database. Set
// SUPPORT_TEST_DATABASE_URL to a postgres:// URL to run them; each test uses
// its own schema and drops it afterwards.
func testPostgres(t *testing.T) (*Postgres, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("SUPPORT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SUPPORT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	schema := "support_test_" + hex.EncodeToString(suffix)
	admin, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(context.Background())
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	postgres, err := OpenPostgres(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(postgres.Close)
	return postgres, ctx
}

func applyMigrationsBefore(t *testing.T, ctx context.Context, postgres *Postgres, first string) {
	t.Helper()
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	for _, entry := range entries {
		if entry.Name() >= first {
			return
		}
		migration, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := postgres.pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
	}
}

func insertLegacyReport(t *testing.T, ctx context.Context, postgres *Postgres, id string, createdAt, retentionUntil time.Time, deletedAt *time.Time) {
	t.Helper()
	objectKey := strings.Repeat(id[len(id)-2:], 32) + ".enc"
	_, err := postgres.pool.Exec(ctx, `INSERT INTO support_reports (
		id, support_code, product_id, request_type, status, private_payload, capability_ciphertext,
		diagnostic_object_key, idempotency_hash, request_hash, capability_hash, created_at, updated_at,
		retention_until, deleted_at
	) VALUES ($1, $2, 'synthetic-product', 'bug', 'new', '\x00', '\x00', $3, $4, $4, $5, $6, $6, $7, $8)`,
		id, "OBI-TEST"+id[len(id)-1:]+"-23456", objectKey, digest("idempotency-"+id), digest("capability-"+id),
		createdAt, retentionUntil, deletedAt)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMigrationClearsRetentionDeadlinesAndPurgesOnlyDeletedReports(t *testing.T) {
	postgres, ctx := testPostgres(t)
	applyMigrationsBefore(t, ctx, postgres, "005")

	longAgo := time.Date(2016, 1, 2, 3, 4, 5, 0, time.UTC)
	deletedAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	const expired, future, revoked = "00000000-0000-4000-8000-0000000000a1", "00000000-0000-4000-8000-0000000000a2", "00000000-0000-4000-8000-0000000000a3"
	insertLegacyReport(t, ctx, postgres, expired, longAgo, longAgo.Add(30*24*time.Hour), nil)
	insertLegacyReport(t, ctx, postgres, future, time.Now().UTC(), time.Now().UTC().Add(30*24*time.Hour), nil)
	insertLegacyReport(t, ctx, postgres, revoked, longAgo, longAgo.Add(30*24*time.Hour), &deletedAt)

	for run := 0; run < 2; run++ {
		if err := postgres.Migrate(ctx); err != nil {
			t.Fatalf("migration run %d: %v", run+1, err)
		}
	}
	var withDeadline int
	var nullable string
	if err := postgres.pool.QueryRow(ctx, `SELECT count(*) FROM support_reports WHERE retention_until IS NOT NULL`).Scan(&withDeadline); err != nil {
		t.Fatal(err)
	}
	if err := postgres.pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'support_reports' AND column_name = 'retention_until'`).Scan(&nullable); err != nil {
		t.Fatal(err)
	}
	if withDeadline != 0 || nullable != "YES" {
		t.Fatalf("after migration: %d rows keep a deadline, nullable = %s", withDeadline, nullable)
	}

	deleted, err := postgres.Deleted(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].ID != revoked || deleted[0].RetentionUntil != nil {
		t.Fatalf("purge candidates = %#v, want only the revoked report", deleted)
	}
	if err := postgres.PurgeDeleted(ctx, expired); !errors.Is(err, ErrNotFound) {
		t.Fatalf("purging a long-expired live report = %v, want ErrNotFound", err)
	}
	if err := postgres.PurgeDeleted(ctx, revoked); err != nil {
		t.Fatal(err)
	}
	live, total, err := postgres.AdminList(ctx, nil, 10, 0)
	if err != nil || total != 2 || len(live) != 2 {
		t.Fatalf("live reports after purge = %d (%d), %v; want 2", len(live), total, err)
	}
	references, err := postgres.DiagnosticReferences(ctx, FirstReportID, 1)
	if err != nil || len(references) != 1 || references[0].ReportID != expired {
		t.Fatalf("first diagnostic reference page = %#v, %v", references, err)
	}
	references, err = postgres.DiagnosticReferences(ctx, references[0].ReportID, 10)
	if err != nil || len(references) != 1 || references[0].ReportID != future {
		t.Fatalf("second diagnostic reference page = %#v, %v", references, err)
	}
}

func TestPostgresCreatesReportsWithoutDeadlineAndAdminDeleteRevokes(t *testing.T) {
	postgres, ctx := testPostgres(t)
	if err := postgres.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	report := domain.Report{
		ID: "00000000-0000-4000-8000-0000000000b1", SupportCode: "OBI-NEWRP-23456", ProductID: "synthetic-product",
		RequestType: domain.RequestBug, Status: domain.StatusNew, PrivatePayload: []byte{1}, CapabilityCiphertext: []byte{1},
		IdempotencyHash: digest("idempotency-new"), RequestHash: digest("request-new"), CapabilityHash: digest("capability-new"),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := postgres.Create(ctx, report); err != nil {
		t.Fatal(err)
	}
	stored, err := postgres.AdminByID(ctx, report.ID)
	if err != nil || stored.RetentionUntil != nil {
		t.Fatalf("stored report deadline = %v, %v; want none", stored.RetentionUntil, err)
	}
	revoked, err := postgres.AdminDelete(ctx, report.ID, now)
	if err != nil || revoked.DeletedAt == nil {
		t.Fatalf("admin delete = %#v, %v", revoked, err)
	}
	if _, err := postgres.AdminDelete(ctx, report.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated admin delete = %v, want ErrNotFound", err)
	}
	if _, err := postgres.AdminByID(ctx, report.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("admin read after delete = %v, want ErrNotFound", err)
	}
	if err := postgres.PurgeDeleted(ctx, report.ID); err != nil {
		t.Fatal(err)
	}
}

// digest returns a 32-byte SHA-256 value, matching the hashes the service stores.
func digest(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func TestPostgresStorageIdentityAndObjectReferences(t *testing.T) {
	postgres, ctx := testPostgres(t)
	if err := postgres.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.StorageID(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unbound storage ID error = %v, want ErrNotFound", err)
	}
	first, second := strings.Repeat("a", 64), strings.Repeat("b", 64)
	now := time.Now().UTC()
	if bound, err := postgres.BindStorageID(ctx, first, now); err != nil || bound != first {
		t.Fatalf("first bind = %q, %v", bound, err)
	}
	if bound, err := postgres.BindStorageID(ctx, second, now); err != nil || bound != first {
		t.Fatalf("second bind = %q, %v; want the first ID kept", bound, err)
	}
	if err := postgres.RebindStorageID(ctx, second, now); err != nil {
		t.Fatal(err)
	}
	if bound, err := postgres.StorageID(ctx); err != nil || bound != second {
		t.Fatalf("rebound ID = %q, %v", bound, err)
	}
	if err := postgres.RebindStorageID(ctx, "not-hex", now); err == nil {
		t.Fatal("malformed storage ID was stored")
	}

	deletedAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	const live, revoked = "00000000-0000-4000-8000-0000000000c1", "00000000-0000-4000-8000-0000000000c2"
	insertLegacyReport(t, ctx, postgres, live, now, now, nil)
	insertLegacyReport(t, ctx, postgres, revoked, now, now, &deletedAt)
	liveKey, revokedKey, orphanKey := strings.Repeat("c1", 32)+".enc", strings.Repeat("c2", 32)+".enc", strings.Repeat("ff", 32)+".enc"
	referenced, err := postgres.ReferencedObjectKeys(ctx, []string{liveKey, revokedKey, orphanKey})
	if err != nil {
		t.Fatal(err)
	}
	if !referenced[liveKey] || !referenced[revokedKey] || referenced[orphanKey] {
		t.Fatalf("referenced keys = %v", referenced)
	}
}
