package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/obiente/support/internal/admin"
	"github.com/obiente/support/internal/domain"
	"github.com/obiente/support/internal/intake"
)

const adminCookieName = "obiente_admin_session"

var adminReportIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type loginWindow struct {
	count int
	start time.Time
}

type loginLimiter struct {
	mu     sync.Mutex
	values map[string]loginWindow
	now    func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{values: make(map[string]loginWindow), now: time.Now}
}

func (limiter *loginLimiter) allowed(key string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	window := limiter.values[key]
	if window.start.IsZero() || limiter.now().Sub(window.start) >= 15*time.Minute {
		delete(limiter.values, key)
		return true
	}
	return window.count < 5
}

func (limiter *loginLimiter) failed(key string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	window := limiter.values[key]
	if window.start.IsZero() || limiter.now().Sub(window.start) >= 15*time.Minute {
		window = loginWindow{start: limiter.now()}
	}
	window.count++
	limiter.values[key] = window
}

func (limiter *loginLimiter) succeeded(key string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	delete(limiter.values, key)
}

func (server *Server) adminLogin(response http.ResponseWriter, request *http.Request) {
	remote := remoteAddress(request)
	if !server.loginAttempts.allowed(remote) {
		response.Header().Set("Retry-After", "900")
		writeProblem(response, http.StatusTooManyRequests, "login_limited", "Too many sign-in attempts. Try again later.")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeAdminJSON(response, request, &body); err != nil {
		writeProblem(response, http.StatusBadRequest, "invalid_request", "The sign-in request is invalid.")
		return
	}
	credentials, err := server.admin.Login(request.Context(), strings.TrimSpace(body.Username), body.Password, remote)
	if errors.Is(err, admin.ErrUnauthorized) {
		server.loginAttempts.failed(remote)
		writeProblem(response, http.StatusUnauthorized, "invalid_credentials", "The username or password is incorrect.")
		return
	}
	if err != nil {
		server.logger.Error("admin login failed", "error", err)
		writeProblem(response, http.StatusInternalServerError, "temporary_failure", "Sign-in is temporarily unavailable.")
		return
	}
	server.loginAttempts.succeeded(remote)
	server.setAdminCookie(response, credentials.SessionToken, credentials.ExpiresAt)
	writeJSON(response, http.StatusOK, map[string]any{
		"contractVersion": domain.ContractVersion, "username": credentials.Username,
		"csrfToken": credentials.CSRFToken, "expiresAt": credentials.ExpiresAt,
	})
}

func (server *Server) adminSession(response http.ResponseWriter, request *http.Request) {
	token, session, ok := server.requireAdmin(response, request)
	if !ok {
		return
	}
	csrfToken, err := server.admin.RotateCSRF(request.Context(), token)
	if err != nil {
		server.logger.Error("rotate admin csrf token", "error", err)
		writeProblem(response, http.StatusInternalServerError, "temporary_failure", "The admin session could not be refreshed.")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"contractVersion": domain.ContractVersion, "username": session.Username,
		"csrfToken": csrfToken, "expiresAt": session.ExpiresAt,
	})
}

func (server *Server) adminLogout(response http.ResponseWriter, request *http.Request) {
	token, session, ok := server.requireAdminCSRF(response, request)
	if !ok {
		return
	}
	if err := server.admin.Logout(request.Context(), token, remoteAddress(request)); err != nil {
		server.clearAdminCookie(response)
		server.logger.Error("admin logout failed", "username", session.Username, "error", err)
		writeProblem(response, http.StatusInternalServerError, "temporary_failure", "Sign-out could not be completed.")
		return
	}
	server.clearAdminCookie(response)
	response.WriteHeader(http.StatusNoContent)
}

func (server *Server) adminReports(response http.ResponseWriter, request *http.Request) {
	_, session, ok := server.requireAdmin(response, request)
	if !ok {
		return
	}
	limit := boundedInteger(request.URL.Query().Get("limit"), 25, 1, 100)
	offset := boundedInteger(request.URL.Query().Get("offset"), 0, 0, 100000)
	var status *domain.ReportStatus
	if value := strings.TrimSpace(request.URL.Query().Get("status")); value != "" {
		parsed := domain.ReportStatus(value)
		if !parsed.Valid() {
			writeProblem(response, http.StatusBadRequest, "invalid_status", "The report status filter is invalid.")
			return
		}
		status = &parsed
	}
	if !server.auditAdmin(response, request, session.Username, "reports_listed", nil) {
		return
	}
	reports, total, err := server.intake.AdminList(request.Context(), status, limit, offset)
	if err != nil {
		server.writeAdminError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"contractVersion": domain.ContractVersion, "reports": reports,
		"total": total, "limit": limit, "offset": offset,
	})
}

func (server *Server) adminReport(response http.ResponseWriter, request *http.Request) {
	_, session, ok := server.requireAdmin(response, request)
	if !ok {
		return
	}
	id, ok := adminReportID(response, request)
	if !ok {
		return
	}
	if !server.auditAdmin(response, request, session.Username, "report_viewed", &id) {
		return
	}
	report, err := server.intake.AdminDetail(request.Context(), id)
	if err != nil {
		server.writeAdminError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, report)
}

func (server *Server) adminUpdateReport(response http.ResponseWriter, request *http.Request) {
	_, session, ok := server.requireAdminCSRF(response, request)
	if !ok {
		return
	}
	var body struct {
		Status domain.ReportStatus `json:"status"`
	}
	if err := decodeAdminJSON(response, request, &body); err != nil || !body.Status.Valid() {
		writeProblem(response, http.StatusBadRequest, "invalid_status", "Choose a valid report status.")
		return
	}
	id, ok := adminReportID(response, request)
	if !ok {
		return
	}
	if !server.auditAdmin(response, request, session.Username, "report_status_change_requested:"+string(body.Status), &id) {
		return
	}
	report, err := server.intake.AdminUpdateStatus(request.Context(), id, body.Status)
	if err != nil {
		server.writeAdminError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, report)
}

func (server *Server) adminReportMessage(response http.ResponseWriter, request *http.Request) {
	_, session, ok := server.requireAdminCSRF(response, request)
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := decodeAdminJSON(response, request, &body); err != nil {
		writeProblem(response, http.StatusBadRequest, "invalid_message", "Enter a message to send to the reporter.")
		return
	}
	id, ok := adminReportID(response, request)
	if !ok {
		return
	}
	if !server.auditAdmin(response, request, session.Username, "report_message_requested", &id) {
		return
	}
	report, err := server.intake.AdminMessage(request.Context(), id, body.Body)
	if err != nil {
		server.writeAdminError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, report)
}

func (server *Server) adminDiagnostics(response http.ResponseWriter, request *http.Request) {
	_, session, ok := server.requireAdmin(response, request)
	if !ok {
		return
	}
	id, ok := adminReportID(response, request)
	if !ok {
		return
	}
	if !server.auditAdmin(response, request, session.Username, "diagnostics_downloaded", &id) {
		return
	}
	content, filename, err := server.intake.AdminDiagnostics(request.Context(), id)
	if err != nil {
		server.writeAdminError(response, err)
		return
	}
	response.Header().Set("Content-Type", "application/zip")
	response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(content)
}

func (server *Server) requireAdmin(response http.ResponseWriter, request *http.Request) (string, domain.AdminSession, bool) {
	cookie, err := request.Cookie(adminCookieName)
	if err != nil {
		writeProblem(response, http.StatusUnauthorized, "authentication_required", "Sign in to continue.")
		return "", domain.AdminSession{}, false
	}
	session, err := server.admin.Session(request.Context(), cookie.Value)
	if err != nil {
		server.clearAdminCookie(response)
		writeProblem(response, http.StatusUnauthorized, "authentication_required", "Your admin session is not available. Sign in again.")
		return "", domain.AdminSession{}, false
	}
	return cookie.Value, session, true
}

func (server *Server) requireAdminCSRF(response http.ResponseWriter, request *http.Request) (string, domain.AdminSession, bool) {
	token, session, ok := server.requireAdmin(response, request)
	if !ok {
		return "", domain.AdminSession{}, false
	}
	if !server.admin.CheckCSRF(session, request.Header.Get("X-CSRF-Token")) {
		writeProblem(response, http.StatusForbidden, "invalid_csrf", "Refresh the admin page and try again.")
		return "", domain.AdminSession{}, false
	}
	return token, session, true
}

func (server *Server) setAdminCookie(response http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(response, &http.Cookie{
		Name: adminCookieName, Value: token, Path: "/", Expires: expires,
		MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: server.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (server *Server) clearAdminCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{
		Name: adminCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: server.secureCookies, SameSite: http.SameSiteStrictMode,
	})
}

func (server *Server) writeAdminError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, intake.ErrInvalid):
		writeProblem(response, http.StatusBadRequest, "invalid_request", "The admin request is invalid.")
	case errors.Is(err, intake.ErrNotFound):
		writeProblem(response, http.StatusNotFound, "not_found", "The private report is not available.")
	default:
		server.logger.Error("admin request failed", "error", err)
		writeProblem(response, http.StatusInternalServerError, "temporary_failure", "The admin request could not be completed.")
	}
}

func decodeAdminJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, 8*1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func boundedInteger(raw string, fallback, minimum, maximum int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return fallback
	}
	return value
}

func remoteAddress(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func adminReportID(response http.ResponseWriter, request *http.Request) (string, bool) {
	id := request.PathValue("id")
	if !adminReportIDPattern.MatchString(id) {
		writeProblem(response, http.StatusNotFound, "not_found", "The private report is not available.")
		return "", false
	}
	return id, true
}

func (server *Server) auditAdmin(response http.ResponseWriter, request *http.Request, username, action string, reportID *string) bool {
	if err := server.admin.Audit(request.Context(), username, action, reportID, remoteAddress(request)); err != nil {
		server.logger.Error("admin audit failed", "action", action, "error", err)
		writeProblem(response, http.StatusInternalServerError, "audit_unavailable", "Private report access is unavailable because it could not be audited.")
		return false
	}
	return true
}
