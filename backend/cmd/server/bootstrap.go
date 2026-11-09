package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/logging"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 20 * time.Second
	idleTimeout       = 120 * time.Second
	apiShutdownGrace  = 25 * time.Second
)

// loadConfig reads the environment and returns the config plus a process logger.
func loadConfig(process string) (config.Config, *slog.Logger, error) {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return config.Config{}, nil, err
	}
	return cfg, logging.New(os.Stdout, cfg.LogLevel, process), nil
}

// serve runs srv until ctx is cancelled, then shuts it down gracefully.
func serve(ctx context.Context, srv *http.Server, grace time.Duration, log *slog.Logger) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return fmt.Errorf("listen on %s: %w", srv.Addr, err)
	case <-ctx.Done():
	}
	log.Info("shutting down", slog.String("addr", srv.Addr))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	err := srv.Shutdown(shutdownCtx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}
