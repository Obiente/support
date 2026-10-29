package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/obiente/support/internal/admin"
	"github.com/obiente/support/internal/config"
	"github.com/obiente/support/internal/cryptobox"
	"github.com/obiente/support/internal/intake"
	"github.com/obiente/support/internal/products"
	"github.com/obiente/support/internal/store"
	"github.com/obiente/support/internal/web"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if err := checkHealth(os.Getenv("SUPPORT_ADDRESS")); err != nil {
			logger.Error("support health check failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := run(logger); err != nil {
		logger.Error("support service stopped", "error", err)
		os.Exit(1)
	}
}

func checkHealth(address string) error {
	probeURL, err := healthcheckURL(address)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(probeURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("support health endpoint returned a non-success status")
	}
	return nil
}

func healthcheckURL(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		address = ":8080"
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}

func run(logger *slog.Logger) error {
	configuration, err := config.FromEnvironment()
	if err != nil {
		return err
	}
	box, err := cryptobox.NewFromBase64(configuration.DataKey)
	if err != nil {
		return err
	}
	objects, err := openPrivateObjects(configuration.ObjectRoot, box)
	if err != nil {
		return err
	}
	startupContext, startupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer startupCancel()
	reports, err := store.OpenPostgres(startupContext, configuration.DatabaseURL)
	if err != nil {
		return err
	}
	defer reports.Close()
	if err := reports.Migrate(startupContext); err != nil {
		return err
	}
	if err := checkStorageIdentity(startupContext, logger, configuration.ObjectRoot, reports, configuration.StorageAdopt); err != nil {
		return err
	}
	registry := products.Default()
	intakeService := intake.New(reports, objects, registry, box, configuration.PublicURL)
	adminService := admin.New(reports, configuration.AdminUsername, configuration.AdminPasswordHash)
	inventoryContext, inventoryCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	reportDiagnosticInventory(inventoryContext, logger, configuration.ObjectRoot, intakeService)
	inventoryCancel()
	if err := intakeService.PurgeDeleted(startupContext, 250); err != nil {
		logger.Warn("initial deleted-report purge did not complete", "error", err)
	}
	purgeContext, purgeCancel := context.WithCancel(context.Background())
	defer purgeCancel()
	go runDeletionPurge(purgeContext, intakeService, logger)
	webServer, err := web.New(intakeService, adminService, registry, logger, configuration.WebRoot, configuration.SecureCookies)
	if err != nil {
		return err
	}
	server := web.HTTPServer(configuration.Address, webServer.Handler())
	shutdown, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serverError := make(chan error, 1)
	go func() {
		logger.Info("support service listening", "address", configuration.Address, "environment", configuration.Environment)
		serverError <- server.ListenAndServe()
	}()
	select {
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdown.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	}
}

// openPrivateObjects opens the private object root and refuses to continue
// unless a probe file can be created and removed there.
func openPrivateObjects(root string, box *cryptobox.Box) (*store.FileObjects, error) {
	objects, err := store.NewFileObjects(root, box)
	if err == nil {
		err = store.VerifyObjectRoot(root)
	}
	if err != nil {
		return nil, fmt.Errorf("refusing to start without writable persistent private storage: %w", err)
	}
	return objects, nil
}

// checkStorageIdentity refuses to continue when the object root is not the
// volume the database was written against, so purges and uploads never run
// against a replacement volume. SUPPORT_STORAGE_ADOPT is a recovery-only
// override that rebinds the database to the mounted volume.
func checkStorageIdentity(ctx context.Context, logger *slog.Logger, objectRoot string, identities store.StorageIdentities, adopt bool) error {
	check, err := store.EnsureStorageIdentity(ctx, objectRoot, identities, adopt, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("refusing to start: %w", err)
	}
	switch check.Binding {
	case store.StorageRebound:
		logger.Error("SUPPORT_STORAGE_ADOPT rebound the database to a different private object volume; reports whose objects are not on this volume stay unavailable. Remove SUPPORT_STORAGE_ADOPT after this start",
			"object_root", objectRoot, "storage_id", check.StorageID, "previous_storage_id", check.PreviousID)
	case store.StorageMatched:
		if adopt {
			logger.Warn("SUPPORT_STORAGE_ADOPT is set but the private object volume already matches; remove the override",
				"object_root", objectRoot, "storage_id", check.StorageID)
		} else {
			logger.Info("private object volume matches the database", "object_root", objectRoot, "storage_id", check.StorageID)
		}
	default:
		logger.Info("private object volume bound to the database",
			"object_root", objectRoot, "storage_id", check.StorageID, "binding", string(check.Binding))
	}
	return nil
}

// reportDiagnosticInventory logs how many live reports reference a diagnostic
// object that is missing from the private object root, and how many stored
// objects no report references. Missing objects usually mean the persistent
// volume was not reattached; orphaned objects usually mean deletions ran
// against a different volume. It only counts: it never changes or deletes a
// report or an object, and never logs object keys.
func reportDiagnosticInventory(ctx context.Context, logger *slog.Logger, objectRoot string, intakeService *intake.Service) {
	inventory, err := intakeService.InventoryDiagnostics(ctx)
	if err != nil {
		logger.Warn("private diagnostic object inventory did not complete",
			"object_root", objectRoot, "attached", inventory.Attached, "missing", inventory.Missing,
			"unknown", inventory.Unknown, "orphaned", inventory.Orphaned, "error", err)
		return
	}
	if inventory.Missing > 0 || inventory.Unknown > 0 {
		logger.Warn("reports reference unavailable private diagnostic objects; restore the persistent volume or its backup",
			"object_root", objectRoot, "attached", inventory.Attached, "missing", inventory.Missing,
			"unknown", inventory.Unknown, "orphaned", inventory.Orphaned)
		return
	}
	if inventory.Orphaned > 0 {
		logger.Warn("private object root holds diagnostic objects that no report references",
			"object_root", objectRoot, "attached", inventory.Attached, "missing", 0, "orphaned", inventory.Orphaned)
		return
	}
	logger.Info("private diagnostic objects verified",
		"object_root", objectRoot, "attached", inventory.Attached, "missing", 0, "orphaned", 0)
}

// runDeletionPurge completes reporter, cancellation, and maintainer deletions.
// Reports are never purged because of their age.
func runDeletionPurge(ctx context.Context, intakeService *intake.Service, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purgeContext, cancel := context.WithTimeout(ctx, 5*time.Minute)
			if err := intakeService.PurgeDeleted(purgeContext, 250); err != nil {
				logger.Warn("deleted-report purge did not complete", "error", err)
			}
			cancel()
		}
	}
}
