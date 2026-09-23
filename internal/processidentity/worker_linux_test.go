//go:build linux

package processidentity

import (
	"os"
	"testing"
)

func TestWorkerStartIdentityMatchesOnlyExactProcess(t *testing.T) {
	pid := os.Getpid()
	token, err := StartToken(pid)
	if err != nil || token == "" {
		t.Fatalf("start token: %q, %v", token, err)
	}
	match, err := Matches(pid, token)
	if err != nil || !match {
		t.Fatalf("same worker not matched: %v, %v", match, err)
	}
	match, err = Matches(pid, "0")
	if err != nil || match {
		t.Fatalf("different start token matched: %v, %v", match, err)
	}
	if _, err := Matches(0, token); err == nil {
		t.Fatal("invalid PID accepted")
	}
	if _, err := Matches(pid, ""); err == nil {
		t.Fatal("empty token accepted")
	}
}
