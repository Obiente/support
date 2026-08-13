package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/obiente/support/internal/domain"
	"github.com/obiente/support/internal/intake"
	"github.com/obiente/support/internal/products"
)

const maxRequestBytes = domain.MaxDiagnosticArchiveBytes + domain.MaxMetadataBytes + 256*1024

type Server struct {
	intake   *intake.Service
	products *products.Registry
	logger   *slog.Logger
	webRoot  string
}

func New(intakeService *intake.Service, registry *products.Registry, logger *slog.Logger, webRoot string) (*Server, error) {
	absoluteRoot, err := filepath.Abs(webRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve support web root: %w", err)
	}
	if _, err := os.Stat(filepath.Join(absoluteRoot, "index.html")); err != nil {
		return nil, fmt.Errorf("support web build is unavailable: %w", err)
	}
	return &Server{intake: intakeService, products: registry, logger: logger, webRoot: absoluteRoot}, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /api/v1/products", server.listProducts)
	mux.HandleFunc("POST /api/v1/reports", server.createReport)
	mux.HandleFunc("GET /api/v1/receipts", server.reconcileReceipt)
	mux.HandleFunc("GET /api/v1/reports/{capability}", server.reportStatus)
	mux.HandleFunc("DELETE /api/v1/reports/{capability}", server.deleteReport)
	mux.Handle("GET /", server.spa())
	return securityHeaders(mux)
}

func (server *Server) spa() http.Handler {
	files := http.FileServer(http.Dir(server.webRoot))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cleaned := filepath.Clean(strings.TrimPrefix(request.URL.Path, "/"))
		if cleaned != "." && !strings.HasPrefix(cleaned, "..") {
			if info, err := os.Stat(filepath.Join(server.webRoot, cleaned)); err == nil && !info.IsDir() {
				files.ServeHTTP(response, request)
				return
			}
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		http.ServeFileFS(response, request, os.DirFS(server.webRoot), "index.html")
	})
}

func (server *Server) health(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	response.Write([]byte(`{"status":"ok"}`))
}

func (server *Server) listProducts(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"contractVersion": domain.ContractVersion,
		"products":        server.products.List(),
	})
}

func (server *Server) createReport(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
	reader, err := request.MultipartReader()
	if err != nil {
		writeProblem(response, http.StatusBadRequest, "invalid_request", "The report form could not be read.")
		return
	}
	var metadataBytes, archive []byte
	archiveType := ""
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			writeProblem(response, http.StatusBadRequest, "invalid_request", "The report upload was interrupted.")
			return
		}
		name := part.FormName()
		switch name {
		case "metadata":
			if metadataBytes != nil {
				part.Close()
				writeProblem(response, http.StatusBadRequest, "invalid_request", "The report contains duplicate metadata.")
				return
			}
			metadataBytes, err = readBounded(part, domain.MaxMetadataBytes)
		case "diagnostics":
			if archive != nil {
				part.Close()
				writeProblem(response, http.StatusBadRequest, "invalid_request", "The report contains duplicate diagnostics.")
				return
			}
			archiveType, _, _ = mime.ParseMediaType(part.Header.Get("Content-Type"))
			archive, err = readBounded(part, domain.MaxDiagnosticArchiveBytes)
		default:
			err = fmt.Errorf("unknown multipart field")
		}
		part.Close()
		if err != nil {
			writeProblem(response, http.StatusBadRequest, "invalid_request", "The report exceeds its allowed size or shape.")
			return
		}
	}
	if len(metadataBytes) == 0 {
		writeProblem(response, http.StatusBadRequest, "invalid_request", "Report details are required.")
		return
	}
	var metadata domain.ReportMetadata
	decoder := json.NewDecoder(strings.NewReader(string(metadataBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		writeProblem(response, http.StatusBadRequest, "invalid_request", "The report details are invalid.")
		return
	}
	receipt, err := server.intake.Submit(request.Context(), intake.Submission{
		Metadata: metadata, Archive: archive, ArchiveType: archiveType,
		IdempotencyKey: request.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		server.writeIntakeError(response, err)
		return
	}
	response.Header().Set("Location", receipt.StatusURL)
	writeJSON(response, http.StatusCreated, receipt)
}

func (server *Server) reconcileReceipt(response http.ResponseWriter, request *http.Request) {
	receipt, err := server.intake.Reconcile(request.Context(), request.Header.Get("Idempotency-Key"))
	if err != nil {
		server.writeIntakeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, receipt)
}

func (server *Server) reportStatus(response http.ResponseWriter, request *http.Request) {
	status, err := server.intake.Status(request.Context(), request.PathValue("capability"))
	if err != nil {
		server.writeIntakeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, status)
}

func (server *Server) deleteReport(response http.ResponseWriter, request *http.Request) {
	status, err := server.intake.Delete(request.Context(), request.PathValue("capability"))
	if err != nil {
		server.writeIntakeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, status)
}

func (server *Server) writeIntakeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, intake.ErrInvalid):
		writeProblem(response, http.StatusBadRequest, "invalid_report", "Check the report details and try again.")
	case errors.Is(err, intake.ErrKeyReused):
		writeProblem(response, http.StatusConflict, "idempotency_conflict", "This retry identifier belongs to different report content.")
	case errors.Is(err, intake.ErrNotFound):
		writeProblem(response, http.StatusNotFound, "not_found", "This private report link is not available.")
	default:
		server.logger.Error("support intake failed", "error", err)
		writeProblem(response, http.StatusInternalServerError, "temporary_failure", "Support intake is temporarily unavailable. Your report was not automatically retried.")
	}
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum {
		return nil, errors.New("content exceeds its limit")
	}
	return content, nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeProblem(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{
		"contractVersion": domain.ContractVersion, "code": code, "message": message,
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		response.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=(), payment=(), usb=()")
		if strings.HasPrefix(request.URL.Path, "/api/") || strings.HasPrefix(request.URL.Path, "/r/") {
			response.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		}
		next.ServeHTTP(response, request)
	})
}

func HTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: address, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second,
	}
}
