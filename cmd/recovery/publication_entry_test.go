package main

import (
	"errors"
	"github.com/alicelik/celikpanel/internal/recoverypublication"
	"strings"
	"testing"
)

func TestPublicationDispatchClosedOwnerBoundary(t *testing.T) {
	snapshot := "20260914T120000Z-from-unknown-to-" + strings.Repeat("a", 40) + "-" + strings.Repeat("b", 32)
	args := []string{"restore-resource", "--resource", "web", "--snapshot", snapshot, "--snapshot-manifest", strings.Repeat("c", 64), "--candidate-root", "/var/backups/celikpanel/releases/aaaaaaaaaaaa-" + strings.Repeat("d", 24), "--candidate-manifest", strings.Repeat("e", 64)}
	for _, command := range []string{"restore-resource", "publish-resource"} {
		args[0] = command
		called := false
		got := dispatchPublication(args, 0, func(apply bool, r recoverypublication.Request) error {
			called = true
			if apply != (command == "publish-resource") || r.Resource != "web" || r.Snapshot != snapshot || r.CandidateRoot != args[8] {
				t.Fatal("tuple changed")
			}
			return nil
		}, func(string) {})
		if got != exitOK || !called {
			t.Fatal(got, called)
		}
	}
	mustNotRun := func(bool, recoverypublication.Request) error { t.Fatal("unadmitted mutation"); return nil }
	if dispatchPublication(args, 1000, mustNotRun, func(string) {}) != exitNotOwner {
		t.Fatal("owner gate")
	}
	for _, replacement := range []struct {
		index int
		value string
	}{{1, "--force"}, {2, "database"}, {4, "../../snapshot"}, {6, "invalid"}, {8, "/tmp/a/../b"}, {10, "invalid"}} {
		bad := append([]string{}, args...)
		bad[replacement.index] = replacement.value
		if dispatchPublication(bad, 0, mustNotRun, func(string) {}) != exitUsage {
			t.Fatal(replacement)
		}
	}
	if dispatchPublication(append(args, "--token", "secret"), 0, mustNotRun, func(string) {}) != exitUsage {
		t.Fatal("caller token accepted")
	}
	if dispatchPublication(args, 0, func(bool, recoverypublication.Request) error { return errors.New("owner changed") }, func(string) {}) != exitUnavailable {
		t.Fatal("failure hidden")
	}
}
