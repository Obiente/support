package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	objects, err := store.NewFileObjects(configuration.ObjectRoot, box)
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
	registry := products.Default()
	intakeService := intake.New(reports, objects, registry, box, configuration.PublicURL)
	if err := intakeService.PurgeExpired(startupContext, 250); err != nil {
		logger.Warn("initial private-data retention purge did not complete", "error", err)
	}
	retentionContext, retentionCancel := context.WithCancel(context.Background())
	defer retentionCancel()
	go runRetention(retentionContext, intakeService, logger)
	webServer, err := web.New(intakeService, registry, logger, configuration.WebRoot)
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

func runRetention(ctx context.Context, intakeService *intake.Service, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purgeContext, cancel := context.WithTimeout(ctx, 5*time.Minute)
			if err := intakeService.PurgeExpired(purgeContext, 250); err != nil {
				logger.Warn("private-data retention purge did not complete", "error", err)
			}
			cancel()
		}
	}
}
