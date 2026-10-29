package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/obiente/support/internal/cryptobox"
	"github.com/obiente/support/internal/domain"
)

func TestAdminDeleteRequiresSessionCSRFAndAudit(t *testing.T) {
	fixture := newAdminFixture(t)
	path := "/api/v1/admin/reports/" + fixture.reportID

	unauthenticated := httptest.NewRecorder()
	fixture.handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodDelete, path, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("delete without session = %d", unauthenticated.Code)
	}

	cookie, csrfToken := adminSignIn(t, fixture.handler)
	withoutCSRF := httptest.NewRequest(http.MethodDelete, path, nil)
	withoutCSRF.AddCookie(cookie)
	forbidden := httptest.NewRecorder()
	fixture.handler.ServeHTTP(forbidden, withoutCSRF)
	if forbidden.Code != http.StatusForbidden || len(fixture.objects.Values) != 1 {
		t.Fatalf("delete without csrf = %d with %d objects left", forbidden.Code, len(fixture.objects.Values))
	}
	if audited(fixture, "report_deletion_requested") != 0 {
		t.Fatal("rejected deletion was audited as requested")
	}

	deleted := adminRequest(t, fixture.handler, http.MethodDelete, path, cookie, csrfToken)
	if deleted.Code != http.StatusNoContent || deleted.Body.Len() != 0 {
		t.Fatalf("admin delete = %d, body = %q", deleted.Code, deleted.Body.String())
	}
	if len(fixture.objects.Values) != 0 {
		t.Fatalf("private object count after admin delete = %d", len(fixture.objects.Values))
	}
	if audited(fixture, "report_deletion_requested") != 1 {
		t.Fatalf("admin deletion audit entries = %#v", fixture.reports.AdminAudits())
	}
	for _, audit := range fixture.reports.AdminAudits() {
		if audit.Action == "report_deletion_requested" && (audit.ReportID == nil || *audit.ReportID != fixture.reportID || audit.Username != "maintainer") {
			t.Fatalf("admin deletion audit = %#v", audit)
		}
	}

	detail := adminRequest(t, fixture.handler, http.MethodGet, path, cookie, "")
	if detail.Code != http.StatusNotFound {
		t.Fatalf("detail after admin delete = %d", detail.Code)
	}
	status := httptest.NewRecorder()
	fixture.handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/reports/"+fixture.capability, nil))
	if status.Code != http.StatusNotFound {
		t.Fatalf("capability status after admin delete = %d", status.Code)
	}
	if again := adminRequest(t, fixture.handler, http.MethodDelete, path, cookie, csrfToken); again.Code != http.StatusNotFound {
		t.Fatalf("repeated admin delete = %d", again.Code)
	}
	unknown := adminRequest(t, fixture.handler, http.MethodDelete, "/api/v1/admin/reports/00000000-0000-4000-8000-00000000abcd", cookie, csrfToken)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown admin delete = %d", unknown.Code)
	}
	malformed := adminRequest(t, fixture.handler, http.MethodDelete, "/api/v1/admin/reports/not-a-report", cookie, csrfToken)
	if malformed.Code != http.StatusNotFound {
		t.Fatalf("malformed admin delete = %d", malformed.Code)
	}
}

// Released contract version 1 clients require retentionUntil to be a timestamp
// exactly within 30 days of createdAt. The service keeps reports until deletion,
// so the field is informational only and must never cause a purge.
func TestContractV1RetentionUntilStaysCreatedAtPlus30Days(t *testing.T) {
	fixture := newAdminFixture(t)
	cookie, csrfToken := adminSignIn(t, fixture.handler)

	idempotencyKey := strings.Repeat("R", 43)
	created := httptest.NewRecorder()
	fixture.handler.ServeHTTP(created, multipartReport(t, idempotencyKey))
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d, body = %s", created.Code, created.Body.String())
	}
	requireV1RetentionUntil(t, "receipt", created.Body.Bytes())

	reconcile := httptest.NewRequest(http.MethodGet, "/api/v1/receipts", nil)
	reconcile.Header.Set("Idempotency-Key", idempotencyKey)
	reconciled := httptest.NewRecorder()
	fixture.handler.ServeHTTP(reconciled, reconcile)
	if reconciled.Code != http.StatusOK {
		t.Fatalf("reconcile = %d", reconciled.Code)
	}
	requireV1RetentionUntil(t, "reconciled receipt", reconciled.Body.Bytes())

	legacyCapability, legacyKey := seedLegacyReport(t, fixture)
	if err := fixture.intake.PurgeDeleted(context.Background(), 100); err != nil {
		t.Fatal(err)
	}

	for _, capability := range []string{fixture.capability, legacyCapability} {
		status := httptest.NewRecorder()
		fixture.handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/reports/"+capability, nil))
		if status.Code != http.StatusOK {
			t.Fatalf("private status after purge = %d", status.Code)
		}
		requireV1RetentionUntil(t, "private status", status.Body.Bytes())
	}
	legacyReconcile := httptest.NewRequest(http.MethodGet, "/api/v1/receipts", nil)
	legacyReconcile.Header.Set("Idempotency-Key", legacyKey)
	legacyReconciled := httptest.NewRecorder()
	fixture.handler.ServeHTTP(legacyReconciled, legacyReconcile)
	if legacyReconciled.Code != http.StatusOK {
		t.Fatalf("legacy reconcile after purge = %d", legacyReconciled.Code)
	}
	legacyReceipt := requireV1RetentionUntil(t, "legacy receipt", legacyReconciled.Body.Bytes())
	if !legacyReceipt.Before(time.Now()) {
		t.Fatalf("legacy retentionUntil %v should already have passed", legacyReceipt)
	}

	reply := httptest.NewRecorder()
	fixture.handler.ServeHTTP(reply, httptest.NewRequest(http.MethodPost, "/api/v1/reports/"+legacyCapability+"/messages",
		strings.NewReader(`{"body":"The old report still needs attention."}`)))
	if reply.Code != http.StatusCreated {
		t.Fatalf("reply to legacy report = %d", reply.Code)
	}
	requireV1RetentionUntil(t, "reply status", reply.Body.Bytes())

	listed := adminRequest(t, fixture.handler, http.MethodGet, "/api/v1/admin/reports", cookie, "")
	var list struct {
		Total   int               `json:"total"`
		Reports []json.RawMessage `json:"reports"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil || list.Total != 3 {
		t.Fatalf("admin list after purge = %d reports, %v; want 3", list.Total, err)
	}
	for _, report := range list.Reports {
		requireV1RetentionUntil(t, "admin summary", report)
	}
	detail := adminRequest(t, fixture.handler, http.MethodGet, "/api/v1/admin/reports/"+fixture.reportID, cookie, "")
	requireV1RetentionUntil(t, "admin detail", detail.Body.Bytes())
	updated := adminRequest(t, fixture.handler, http.MethodPatch, "/api/v1/admin/reports/"+fixture.reportID, cookie, csrfToken, `{"status":"resolved"}`)
	requireV1RetentionUntil(t, "admin update", updated.Body.Bytes())
}

func requireV1RetentionUntil(t *testing.T, name string, body []byte) time.Time {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("%s is not a JSON object: %v", name, err)
	}
	var createdAt, retentionUntil string
	if err := json.Unmarshal(fields["createdAt"], &createdAt); err != nil {
		t.Fatalf("%s createdAt is not a string: %v", name, err)
	}
	raw, present := fields["retentionUntil"]
	if !present || string(raw) == "null" {
		t.Fatalf("%s retentionUntil is missing or null", name)
	}
	if err := json.Unmarshal(raw, &retentionUntil); err != nil {
		t.Fatalf("%s retentionUntil is not a string: %s", name, raw)
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t.Fatalf("%s createdAt = %q: %v", name, createdAt, err)
	}
	retained, err := time.Parse(time.RFC3339Nano, retentionUntil)
	if err != nil {
		t.Fatalf("%s retentionUntil = %q: %v", name, retentionUntil, err)
	}
	if !retained.Equal(created.Add(30 * 24 * time.Hour)) {
		t.Fatalf("%s retentionUntil = %v, want createdAt + 30 days (%v)", name, retained, created.Add(30*24*time.Hour))
	}
	return retained
}

func seedLegacyReport(t *testing.T, fixture adminFixture) (string, string) {
	t.Helper()
	box, err := cryptobox.NewFromBase64(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000001"
	capability := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	idempotencyKey := strings.Repeat("L", 43)
	payload, err := json.Marshal(domain.PrivatePayload{
		Title: "Legacy synthetic report", Description: "Submitted long before the retention change.", Source: "web",
		Release: domain.ReleaseMetadata{Platform: "web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sealedPayload, err := box.Seal(payload, []byte(id+":payload"))
	if err != nil {
		t.Fatal(err)
	}
	sealedCapability, err := box.Seal([]byte(capability), []byte(id+":capability"))
	if err != nil {
		t.Fatal(err)
	}
	capabilityHash := sha256.Sum256([]byte(capability))
	idempotencyHash := sha256.Sum256([]byte(idempotencyKey))
	longAgo := time.Date(2016, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := fixture.reports.Create(context.Background(), domain.Report{
		ID: id, SupportCode: "OBI-LEGAC-Y2345", ProductID: "synthetic-product", RequestType: domain.RequestSupport,
		Status: domain.StatusResolved, PrivatePayload: sealedPayload, CapabilityCiphertext: sealedCapability,
		IdempotencyHash: idempotencyHash[:], RequestHash: idempotencyHash[:], CapabilityHash: capabilityHash[:],
		CreatedAt: longAgo, UpdatedAt: longAgo,
	}); err != nil {
		t.Fatal(err)
	}
	return capability, idempotencyKey
}

func multipartReport(t *testing.T, idempotencyKey string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata, err := writer.CreateFormFile("metadata", "metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := metadata.Write([]byte(`{"contractVersion":1,"productId":"synthetic-product","requestType":"support",` +
		`"title":"Contract fixture","description":"Synthetic contract fixture report.","source":"web",` +
		`"release":{"version":"","platform":"web"},"privacyAccepted":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/reports", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Idempotency-Key", idempotencyKey)
	return request
}

func adminSignIn(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/admin/login",
		strings.NewReader(`{"username":"maintainer","password":"test password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d", login.Code)
	}
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	return login.Result().Cookies()[0], session.CSRFToken
}

func adminRequest(t *testing.T, handler http.Handler, method, path string, cookie *http.Cookie, csrfToken string, body ...string) *httptest.ResponseRecorder {
	t.Helper()
	var request *http.Request
	if len(body) > 0 {
		request = httptest.NewRequest(method, path, strings.NewReader(body[0]))
	} else {
		request = httptest.NewRequest(method, path, nil)
	}
	request.AddCookie(cookie)
	if csrfToken != "" {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func audited(fixture adminFixture, action string) int {
	count := 0
	for _, audit := range fixture.reports.AdminAudits() {
		if audit.Action == action {
			count++
		}
	}
	return count
}
