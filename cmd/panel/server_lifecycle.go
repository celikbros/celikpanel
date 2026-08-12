package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

const panelHTTPShutdownTimeout = 25 * time.Second

type panelHTTPStartupGate struct {
	ready atomic.Bool
	next  http.Handler
}

func newPanelHTTPStartupGate(next http.Handler) *panelHTTPStartupGate {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return &panelHTTPStartupGate{next: next}
}

func (gate *panelHTTPStartupGate) Open() {
	if gate != nil {
		gate.ready.Store(true)
	}
}

func (gate *panelHTTPStartupGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if gate == nil || !gate.ready.Load() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "panel startup recovery is still in progress", http.StatusServiceUnavailable)
		return
	}
	gate.next.ServeHTTP(w, r)
}

type runningPanelHTTPServer struct {
	server      *http.Server
	addr        net.Addr
	serveResult <-chan error
}

// startPanelHTTP binds and starts the listener before startup recovery. When
// TLS is configured, the certificate pair is validated before the socket is
// published and ServeTLS performs every accepted handshake. The startup gate
// in the handler keeps application traffic out until recovery is complete.
func startPanelHTTP(
	server *http.Server,
	certPath, keyPath string,
) (*runningPanelHTTPServer, error) {
	if server == nil {
		return nil, errors.New("panel HTTP server is nil")
	}
	tlsOn := certPath != "" || keyPath != ""
	if tlsOn {
		if certPath == "" || keyPath == "" {
			return nil, errors.New("panel TLS certificate pair is incomplete")
		}
		pair, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("load panel TLS certificate pair: %w", err)
		}
		tlsConfig := &tls.Config{}
		if server.TLSConfig != nil {
			tlsConfig = server.TLSConfig.Clone()
		}
		tlsConfig.Certificates = []tls.Certificate{pair}
		server.TLSConfig = tlsConfig
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return nil, err
	}
	serveResult := make(chan error, 1)
	go func() {
		if tlsOn {
			serveResult <- server.ServeTLS(listener, "", "")
			return
		}
		serveResult <- server.Serve(listener)
	}()

	return &runningPanelHTTPServer{
		server:      server,
		addr:        listener.Addr(),
		serveResult: serveResult,
	}, nil
}

func waitPanelHTTP(running *runningPanelHTTPServer) error {
	if running == nil || running.server == nil || running.serveResult == nil {
		return errors.New("panel HTTP server was not started")
	}
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	return waitStartedPanelHTTPUntil(
		ctx,
		running.server,
		running.serveResult,
	)
}

// servePanelHTTP keeps systemd SIGTERM graceful: stop accepting requests,
// allow in-flight handlers to finish, then return to main so deferred database
// and RPC cleanup can run.
func servePanelHTTP(server *http.Server, certPath, keyPath string) error {
	running, err := startPanelHTTP(server, certPath, keyPath)
	if err != nil {
		return err
	}
	return waitPanelHTTP(running)
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
	return waitStartedPanelHTTPUntil(ctx, server, serveResult)
}

func waitStartedPanelHTTPUntil(
	ctx context.Context,
	server *http.Server,
	serveResult <-chan error,
) error {
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
