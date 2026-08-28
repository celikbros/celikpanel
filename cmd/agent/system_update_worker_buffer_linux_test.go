//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBoundedSystemUpdateBufferRetainsFailureTail(t *testing.T) {
	var buffer boundedSystemUpdateBuffer
	prefix := strings.Repeat(`p`, 4090)
	tail := `final updater failure`
	_, _ = buffer.Write([]byte(prefix))
	written, err := buffer.Write([]byte(tail))
	if err != nil || written != len(tail) {
		t.Fatalf(`tail write = %d, %v`, written, err)
	}
	if len(buffer.raw) != 4096 || !strings.HasSuffix(string(buffer.raw), tail) {
		t.Fatal(`buffer did not retain the bounded failure tail`)
	}

	oversized := []byte(strings.Repeat(`x`, 5000) + `terminal detail`)
	written, err = buffer.Write(oversized)
	if err != nil || written != len(oversized) {
		t.Fatalf(`oversized write = %d, %v`, written, err)
	}
	if len(buffer.raw) != 4096 || !strings.HasSuffix(string(buffer.raw), `terminal detail`) {
		t.Fatal(`oversized write did not retain its bounded tail`)
	}
}

func TestRunSystemUpdateInstallerReportsTerminalFailureFromBoundedTail(t *testing.T) {
	oldResolver := systemUpdateExecutableResolver
	oldRunner := systemUpdateCommandRunner
	t.Cleanup(func() {
		systemUpdateExecutableResolver = oldResolver
		systemUpdateCommandRunner = oldRunner
	})
	systemUpdateExecutableResolver = func(candidates ...string) (string, error) {
		return candidates[0], nil
	}

	var output boundedSystemUpdateBuffer
	const inner = "Prepare BIND generation root under external lock: committed BIND ownership differs from its exact active state"
	const wrapper = "!! installed agent could not prepare the managed BIND generation root"
	stream := strings.Repeat("historical updater progress\n", 300) +
		"celikpanel archive: OK\r\n" +
		"2026/08/28 10:11:12 " + inner + "\r\n" +
		wrapper + "\r\n" +
		"!! Update transaction remains active; both services were left stopped for exact recovery.\r\n" +
		"!! Verified snapshot: /var/backups/celikpanel/update-snapshots/example\r\n\r\n"
	if len(stream) <= 4096 {
		t.Fatal("test stream does not exercise bounded tail retention")
	}
	if _, err := output.Write([]byte(stream)); err != nil {
		t.Fatal(err)
	}
	systemUpdateCommandRunner = func(
		context.Context, string, []string,
	) ([]byte, error) {
		return append([]byte(nil), output.raw...), errors.New("exit status 1")
	}

	err := runSystemUpdateInstaller(
		context.Background(),
		linuxSystemUpdateTestState(strings.Repeat("9", 32)),
		"41",
	)
	if err == nil || !strings.Contains(err.Error(), wrapper) ||
		!strings.Contains(err.Error(), inner) {
		t.Fatalf("installer error lost wrapper or exact inner failure: %v", err)
	}
	if strings.Contains(err.Error(), "archive: OK") ||
		strings.Contains(err.Error(), "historical updater progress") ||
		strings.Contains(err.Error(), "Verified snapshot") {
		t.Fatalf("installer reported a stale output line: %v", err)
	}
}
