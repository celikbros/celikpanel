package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNginxCommandErrorsPreserveOutputAndTimeoutCause(t *testing.T) {
	previousExecute := executeNginxCommand
	previousTimeout := nginxCommandTimeout
	previousNginxDir := nginxDir
	executeNginxCommand = func(
		ctx context.Context,
		name string,
		_ ...string,
	) ([]byte, error) {
		<-ctx.Done()
		return []byte("partial " + name + " output"), ctx.Err()
	}
	nginxCommandTimeout = 5 * time.Millisecond
	nginxDir = ""
	t.Cleanup(func() {
		executeNginxCommand = previousExecute
		nginxCommandTimeout = previousTimeout
		nginxDir = previousNginxDir
	})

	generator := &NginxGenerator{}
	tests := []struct {
		name       string
		run        func() error
		wantOutput string
	}{
		{
			name:       "validation",
			run:        generator.ValidateNginx,
			wantOutput: "partial nginx output",
		},
		{
			name:       "reload",
			run:        generator.ReloadNginx,
			wantOutput: "partial systemctl output",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil {
				t.Fatal("expected timeout")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout cause = %v, want context deadline exceeded", err)
			}
			for _, want := range []string{
				test.wantOutput,
				"timed out after",
				"context deadline exceeded",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}
