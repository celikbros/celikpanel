//go:build linux

package main

import (
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
