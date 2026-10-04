//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

func TestOwnerViewIndependentProcessHelper(t *testing.T) {
	if os.Getenv("CP_OWNER_VIEW_TEST") != "1" {
		return
	}
	port, err := strconv.Atoi(os.Getenv("CP_OWNER_VIEW_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The test invokes the actual listener/handler in a separate process. The
	// observation source is deliberately a fixture, not native acceptance.
	secret := []byte(strings.Repeat("b", 32))
	if err = serveOwnerView(ctx, ownerViewOptions{strings.Repeat("a", 32), port, "en"}, sha256.Sum256(secret), func(id string) recoveryobs.Status {
		return recoveryobs.Status{Schema: recoveryobs.StatusSchema, RequestID: id, Observation: "known", Phase: "recovery_required", TerminalProof: "none", Reason: "recovery_incomplete", AutomaticRecovery: "paused_retry_limit", ObservedAt: "2026-09-22T00:00:00Z"}
	}, func() {}); err != nil {
		t.Fatal(err)
	}
}
func TestOwnerViewSeparateProcessWithoutPanelDatabaseAgentOrTLS(t *testing.T) {
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	reservation.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerViewIndependentProcessHelper$")
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "CP_OWNER_VIEW_TEST=1", "CP_OWNER_VIEW_PORT=" + strconv.Itoa(port)}
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	defer func() {
		if !exited {
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	client := &http.Client{Timeout: time.Second}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	until := time.Now().Add(10 * time.Second)
	for {
		response, e := client.Get(base + "/")
		if e == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(until) {
			t.Fatal("view did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	response, err := client.Get(base + "/status")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("anonymous status exposed")
	}
	request, _ := http.NewRequest("GET", base+"/status", nil)
	request.Header.Set("Authorization", "Bearer "+hex.EncodeToString([]byte(strings.Repeat("b", 32))))
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(raw), "paused_retry_limit") {
		t.Fatalf("read failed: %d", response.StatusCode)
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	<-done
	exited = true
	if strings.Contains(output.String(), request.Header.Get("Authorization")) {
		t.Fatal("secret logged")
	}
}
func TestOwnerViewCancellationClosesListener(t *testing.T) {
	reservation, _ := net.Listen("tcp4", "127.0.0.1:0")
	port := reservation.Addr().(*net.TCPAddr).Port
	reservation.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serveOwnerView(ctx, ownerViewOptions{strings.Repeat("a", 32), port, "en"}, [32]byte{}, func(id string) recoveryobs.Status { return unavailableStatus(id) }, func() { close(ready) })
	}()
	<-ready
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not close view")
	}
}
