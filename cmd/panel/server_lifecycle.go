package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const panelHTTPShutdownTimeout = 25 * time.Second

// servePanelHTTP keeps systemd SIGTERM graceful: stop accepting requests,
// allow in-flight handlers to finish, then return to main so deferred database
// and RPC cleanup can run.
func servePanelHTTP(server *http.Server, certPath, keyPath string) error {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	serve := server.ListenAndServe
	if certPath != "" || keyPath != "" {
		serve = func() error {
			return server.ListenAndServeTLS(certPath, keyPath)
		}
	}
	return servePanelHTTPUntil(ctx, server, serve)
}

func servePanelHTTPUntil(
	ctx context.Context,
	server *http.Server,
	serve func() error,
) error {
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- serve()
	}()

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		panelHTTPShutdownTimeout,
	)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		// A stuck handler must not outlive systemd's stop budget.
		_ = server.Close()
		return fmt.Errorf("graceful panel shutdown: %w", err)
	}

	err := <-serveResult
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
