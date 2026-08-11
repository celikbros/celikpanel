package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunPanelCertCommandReportsTimeoutWithOutputAndCause(t *testing.T) {
	previous := executePanelCertCommand
	executePanelCertCommand = func(
		ctx context.Context,
		_ string,
		_ ...string,
	) ([]byte, error) {
		<-ctx.Done()
		return []byte("Error: partial certbot output"), ctx.Err()
	}
	t.Cleanup(func() { executePanelCertCommand = previous })

	output, err := runPanelCertCommand(5*time.Millisecond, "certbot", "renew")
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout cause = %v, want context deadline exceeded", err)
	}
	detail := panelCertCommandError("certbot renew", output, err).Error()
	for _, want := range []string{
		"partial certbot output",
		"timed out after",
		"context deadline exceeded",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("error %q does not contain %q", detail, want)
		}
	}
}

func TestMailTLSCommandErrorPreservesOutputAndCause(t *testing.T) {
	cause := errors.New("command timed out")
	err := mailTLSCommandError(
		"postmap -F",
		[]byte("partial postmap output"),
		cause,
	)
	if !errors.Is(err, cause) {
		t.Fatalf("error cause = %v, want %v", err, cause)
	}
	for _, want := range []string{"partial postmap output", "command timed out"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestRunMailTLSMutationCommandRejectsCanceledLease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runMailTLSMutationCommand(ctx, "command-that-must-not-run")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled mutation command error = %v, want context canceled", err)
	}
}

func TestValidatePanelCertTLSDirAllowsOnlyManagedDirectory(t *testing.T) {
	if got, err := validatePanelCertTLSDir(managedPanelTLSDir); err != nil ||
		got != managedPanelTLSDir {
		t.Fatalf("managed directory = %q, %v", got, err)
	}

	for _, candidate := range []string{
		"",
		"/tmp/tls",
		managedPanelTLSDir + "/nested",
		managedPanelTLSDir + "; touch /tmp/pwned",
		managedPanelTLSDir + " ",
	} {
		if _, err := validatePanelCertTLSDir(candidate); err == nil {
			t.Fatalf("unsafe TLS directory %q was accepted", candidate)
		}
	}
}
