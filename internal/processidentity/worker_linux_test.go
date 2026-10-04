//go:build linux

package processidentity

import (
	"errors"
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

func TestRecordedWorkerGoneRequiresKernelProof(t *testing.T) {
	pid := os.Getpid()
	token, err := StartToken(pid)
	if err != nil {
		t.Fatal(err)
	}
	if gone, err := RecordedWorkerGone(pid, token); err != nil || gone {
		t.Fatalf("live worker was excluded: gone=%v err=%v", gone, err)
	}
	if gone, err := RecordedWorkerGone(pid, "1"); err != nil || !gone {
		t.Fatalf("reused PID was not distinguished: gone=%v err=%v", gone, err)
	}
	for _, invalid := range []string{"invalid", "000123", "0"} {
		if gone, err := RecordedWorkerGone(pid, invalid); err == nil || gone {
			t.Fatalf("invalid start identity %q admitted: gone=%v err=%v", invalid, gone, err)
		}
	}
	if gone, err := RecordedWorkerGone(0, token); err == nil || gone {
		t.Fatalf("unregistered worker admitted: gone=%v err=%v", gone, err)
	}
	if gone, err := RecordedWorkerGone(int(^uint(0)>>1), token); err != nil || !gone {
		t.Fatalf("missing worker was not proven gone: gone=%v err=%v", gone, err)
	}
}

func TestRecordedWorkerGoneFailsClosedOnUnknownProcState(t *testing.T) {
	unreadable := errors.New("process identity unreadable")
	for _, tc := range []struct {
		name     string
		verify   func() error
		read     func(int) (string, error)
		wantGone bool
		wantErr  bool
	}{
		{"procfs unavailable", func() error { return unreadable }, func(int) (string, error) {
			t.Fatal("must not read an unverified procfs")
			return "", nil
		}, false, true},
		{"process unreadable", func() error { return nil }, func(int) (string, error) {
			return "", unreadable
		}, false, true},
		{"process missing", func() error { return nil }, func(int) (string, error) {
			return "", os.ErrNotExist
		}, true, false},
		{"PID replaced", func() error { return nil }, func(int) (string, error) {
			return "456", nil
		}, true, false},
		{"worker still alive", func() error { return nil }, func(int) (string, error) {
			return "123", nil
		}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gone, err := recordedWorkerGone(100, "123", tc.verify, tc.read)
			if gone != tc.wantGone || (err != nil) != tc.wantErr {
				t.Fatalf("gone=%v err=%v; want gone=%v error=%v", gone, err, tc.wantGone, tc.wantErr)
			}
		})
	}
}

func TestRecordedWorkerGoneRejectsProcfsChangeDuringMissingProcess(t *testing.T) {
	checks := 0
	verify := func() error {
		checks++
		if checks == 2 {
			return errors.New("procfs changed")
		}
		return nil
	}
	gone, err := recordedWorkerGone(100, "123", verify, func(int) (string, error) {
		return "", os.ErrNotExist
	})
	if gone || err == nil || checks != 2 {
		t.Fatalf("missing process with changed procfs admitted: gone=%v err=%v checks=%d", gone, err, checks)
	}
}
