//go:build linux

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestWebmailTransportUsesOnlyConfiguredUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "webmail.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != webmailPublicPath {
			t.Errorf("upstream path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	transport := newWebmailUnixTransport(socketPath)
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: time.Second}
	if !webmailEndpointReady(
		context.Background(), client,
		"http://tcp-name-that-must-never-resolve.invalid"+webmailPublicPath,
	) {
		t.Fatal("Unix socket backend was not reported ready")
	}
}

func TestWebmailTransportNeverFallsBackToReachableTCP(t *testing.T) {
	tcpReached := false
	tcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tcpReached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer tcpServer.Close()

	transport := newWebmailUnixTransport(filepath.Join(t.TempDir(), "missing.sock"))
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: time.Second}
	if webmailEndpointReady(context.Background(), client, tcpServer.URL+webmailPublicPath) {
		t.Fatal("missing Unix socket fell back to TCP")
	}
	if tcpReached {
		t.Fatal("reachable TCP server received a Unix-only webmail request")
	}
}
