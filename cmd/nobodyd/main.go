// Command nobodyd is the Nobody Inc daemon.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/server"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("nobodyd stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	// Loopback by default: the daemon spawns processes and must not be exposed without auth.
	addr := os.Getenv("NOBODY_ADDR")
	if addr == "" {
		addr = "127.0.0.1:7420"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.NewHandler(version, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("nobodyd listening", "addr", addr, "version", version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
