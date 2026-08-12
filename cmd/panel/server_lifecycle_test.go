package main

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPanelHTTPStartupGateReturns503UntilOpened(t *testing.T) {
	var calls atomic.Int32
	gate := newPanelHTTPStartupGate(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		calls.Add(1)
		_, _ = io.WriteString(w, "ready")
	}))

	recorder := httptest.NewRecorder()
	gate.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable ||
		recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf(
			"closed gate status=%d retry-after=%q",
			recorder.Code,
			recorder.Header().Get("Retry-After"),
		)
	}
	if calls.Load() != 0 {
		t.Fatalf("closed gate invoked application handler %d times", calls.Load())
	}

	gate.Open()
	recorder = httptest.NewRecorder()
	gate.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("open gate status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("open gate application calls=%d", calls.Load())
	}
}

func TestStartPanelHTTPServesTLSWhileApplicationGateIsClosed(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "panel.crt")
	keyPath := filepath.Join(dir, "panel.key")
	if err := generateSelfSigned(certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	gate := newPanelHTTPStartupGate(http.HandlerFunc(func(
		w http.ResponseWriter,
		_ *http.Request,
	) {
		calls.Add(1)
		_, _ = io.WriteString(w, "ready")
	}))
	server := newPanelHTTPServer("127.0.0.1:0", gate)
	running, err := startPanelHTTP(server, certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		select {
		case <-running.serveResult:
		case <-time.After(5 * time.Second):
			t.Error("TLS server did not stop")
		}
	})

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Test certificate is intentionally self-signed.
		}},
	}
	response, err := client.Get("https://" + running.addr.String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	if response.TLS == nil || len(response.TLS.PeerCertificates) != 1 {
		_ = response.Body.Close()
		t.Fatalf("startup TLS peer certificates=%v", response.TLS)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || calls.Load() != 0 {
		t.Fatalf(
			"closed TLS gate status=%d application calls=%d",
			response.StatusCode,
			calls.Load(),
		)
	}

	gate.Open()
	response, err = client.Get("https://" + running.addr.String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusOK || string(body) != "ready" ||
		calls.Load() != 1 {
		t.Fatalf(
			"open TLS gate status=%d body=%q application calls=%d",
			response.StatusCode,
			body,
			calls.Load(),
		)
	}
}

func TestMainStartupLifecycleOrdering(t *testing.T) {
	contents, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	if strings.Count(source, "startPanelHTTP(server, certPath, keyPath)") != 1 {
		t.Fatal("main must start exactly one panel listener")
	}
	if strings.Count(source, "waitPanelHTTP(runningServer)") != 1 {
		t.Fatal("main must wait on exactly one already-started listener")
	}
	if strings.Contains(source, "servePanelHTTP(server,") {
		t.Fatal("main must not bind a second listener after startup recovery")
	}

	markers := []string{
		"runningServer, err := startPanelHTTP(server, certPath, keyPath)",
		"panel.recoverInterruptedServiceOperations(context.Background())",
		"panel.serviceMutationMu.Lock()",
		"panel.serviceMutationMu.Unlock()",
		"panel.reconcileCertificateRuntimeAtStartup()",
		"panel.recoverVPNProvisioningState(recoveryCtx)",
		"panel.wireMailFiltersSynchronouslyAtStartup()",
		"frontendHandler(webRoot)",
		"startupGate.Open()",
		"panel.startBackupScheduler()",
		"panel.startCertRenewalScheduler()",
		"panel.startVPNEntitlementReconciler()",
		"waitPanelHTTP(runningServer)",
	}
	previous := -1
	for _, marker := range markers {
		index := strings.Index(source, marker)
		if index < 0 {
			t.Fatalf("main startup marker %q is missing", marker)
		}
		if index <= previous {
			t.Fatalf("main startup marker %q is out of order", marker)
		}
		previous = index
	}
}

func TestServePanelHTTPUntilGracefullyStopsListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	server := &http.Server{
		Addr: listener.Addr().String(),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			closeOnce(started)
			_, _ = io.WriteString(w, "ok")
		}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- servePanelHTTPUntil(
			ctx,
			server,
			func() error { return server.Serve(listener) },
		)
	}()

	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	<-started
	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("graceful serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("graceful serve did not return")
	}
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
