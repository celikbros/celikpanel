package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoverypublication"
)

func materialTestArgs() []string {
	return []string{"prepare-recovery-material", "--snapshot", "20260914T120000Z-from-unknown-to-" + strings.Repeat("a", 40) + "-" + strings.Repeat("b", 32), "--snapshot-manifest", strings.Repeat("c", 64), "--candidate-root", "/var/backups/celikpanel/releases/aaaaaaaaaaaa-" + strings.Repeat("d", 24), "--candidate-manifest", strings.Repeat("e", 64)}
}
func TestMaterialDispatchClosedBoundary(t *testing.T) {
	args := materialTestArgs()
	deny := func(string, recoverypublication.Request) (string, error) {
		t.Fatal("unadmitted command executed")
		return "", nil
	}
	report := func(string) {}
	for _, valid := range [][]string{args, {"material-root", "--snapshot", args[2]}, {"verify-material-support", "--layout", "snapshot-name-sha256-v1"}} {
		if got := dispatchMaterial(valid, 1000, deny, io.Discard, report); got != exitNotOwner {
			t.Fatalf("owner gate: %d", got)
		}
	}
	for _, bad := range [][]string{nil, {"recover"}, {"material-root"}, {"material-root", "--snapshot", "../snapshot"}, {"material-root", "--snapshot", args[2], "--token", "supplied"}, {"verify-material-support"}, {"verify-material-support", "--layout", "token-sha256-v1"}, {"verify-material-support", "--force"}, append(append([]string{}, args...), "--output", "/tmp/material")} {
		if got := dispatchMaterial(bad, 0, deny, io.Discard, report); got != exitUsage {
			t.Fatalf("accepted %q: %d", bad, got)
		}
	}
	for _, change := range []struct {
		i int
		v string
	}{{1, "--force"}, {2, "../snapshot"}, {3, "--snapshot"}, {4, "invalid"}, {5, "--output"}, {6, "/tmp/a/../b"}, {7, "--token"}, {8, "invalid"}} {
		bad := append([]string{}, args...)
		bad[change.i] = change.v
		if got := dispatchMaterial(bad, 0, deny, io.Discard, report); got != exitUsage {
			t.Fatalf("accepted %+v: %d", change, got)
		}
	}
	called := 0
	got := dispatchMaterial(args, 0, func(command string, r recoverypublication.Request) (string, error) {
		called++
		expected := recoverypublication.Request{Snapshot: args[2], SnapshotManifest: args[4], CandidateRoot: args[6], CandidateManifest: args[8]}
		if command != args[0] || !reflect.DeepEqual(r, expected) {
			t.Fatal(command, r)
		}
		return "", nil
	}, io.Discard, report)
	if got != exitOK || called != 1 {
		t.Fatal(got, called)
	}
}
func TestMaterialDispatchOnlyTrustedAbsenceAllowsLegacyFallback(t *testing.T) {
	args := []string{"material-root", "--snapshot", materialTestArgs()[2]}
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{{"absent", recoverypublication.ErrMaterialAbsent, exitUnavailable}, {"wrapped absent", fmt.Errorf("absent: %w", recoverypublication.ErrMaterialAbsent), exitUnavailable}, {"corrupt", errors.New("private internal evidence"), exitOutput}, {"permission", errors.New("permission denied"), exitOutput}} {
		t.Run(tc.name, func(t *testing.T) {
			var output, report bytes.Buffer
			got := dispatchMaterial(args, 0, func(string, recoverypublication.Request) (string, error) { return "/ignored", tc.err }, &output, func(s string) { report.WriteString(s) })
			if got != tc.code || output.Len() != 0 || strings.Contains(report.String(), tc.err.Error()) {
				t.Fatal(got, output.String(), report.String())
			}
		})
	}
	if got := dispatchMaterial(materialTestArgs(), 0, func(string, recoverypublication.Request) (string, error) {
		return "", recoverypublication.ErrMaterialAbsent
	}, io.Discard, func(string) {}); got != exitOutput {
		t.Fatal("capture failure became legacy fallback", got)
	}
}

type materialFailWriter struct{}

func (materialFailWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
func TestMaterialDispatchOutputIsSingleCanonicalPath(t *testing.T) {
	args := []string{"material-root", "--snapshot", materialTestArgs()[2]}
	expected := "/var/lib/celikpanel-release-state/recovery-material/v1/" + strings.Repeat("f", 64) + "/data"
	var out bytes.Buffer
	call := func(root string, writer io.Writer) int {
		return dispatchMaterial(args, 0, func(string, recoverypublication.Request) (string, error) { return root, nil }, writer, func(string) {})
	}
	if code := call(expected, &out); code != 0 || out.String() != expected+"\n" {
		t.Fatal(code, out.String())
	}
	for _, root := range []string{"", "relative", "/tmp/a/../b", "/tmp/a\nb", "/tmp/a\x00b"} {
		if code := call(root, io.Discard); code != exitOutput {
			t.Fatal(root, code)
		}
	}
	if code := call(expected, materialFailWriter{}); code != exitOutput {
		t.Fatal("write failure hidden", code)
	}
}

func TestMaterialCapabilityNamesExactLayout(t *testing.T) {
	called := false
	got := dispatchMaterial([]string{"verify-material-support", "--layout", "snapshot-name-sha256-v1"}, 0, func(command string, r recoverypublication.Request) (string, error) {
		called = true
		if command != "verify-material-support" || !reflect.DeepEqual(r, recoverypublication.Request{}) {
			t.Fatal(command, r)
		}
		return "", nil
	}, io.Discard, func(string) {})
	if got != exitOK || !called {
		t.Fatal(got, called)
	}
}

func TestCompletionMaterialCLIClosedCapabilityAndResultCodes(t *testing.T) {
	snapshot := materialTestArgs()[2]
	report := func(string) {}
	deny := func(string, recoverypublication.Request) (string, error) {
		t.Fatal("invalid command executed")
		return "", nil
	}
	valid := [][]string{{"verify-material-support", "--layout", "snapshot-name-sha256-v1", "--schema", recoverypublication.MaterialSchemaV2}, {"completion-material-root", "--snapshot", snapshot}, {"verify-installed-completion", "--snapshot", snapshot}}
	for _, args := range valid {
		if got := dispatchMaterial(args, 1000, deny, io.Discard, report); got != exitNotOwner {
			t.Fatal(got)
		}
		if !launcherDispatchCommand(args) {
			t.Fatal("missing fixed launcher dispatch", args)
		}
	}
	for _, args := range [][]string{{"verify-material-support", "--layout", "snapshot-name-sha256-v1", "--schema", recoverypublication.MaterialSchema}, {"verify-material-support", "--schema", recoverypublication.MaterialSchemaV2, "--layout", "snapshot-name-sha256-v1"}, {"completion-material-root", "--snapshot", "../snapshot"}, {"verify-installed-completion", "--snapshot", snapshot, "--repair"}} {
		if got := dispatchMaterial(args, 0, deny, io.Discard, report); got != exitUsage {
			t.Fatal(got, args)
		}
	}
	for _, args := range valid {
		called := false
		code := dispatchMaterial(args, 0, func(command string, r recoverypublication.Request) (string, error) {
			called = true
			if command != args[0] {
				t.Fatal(command)
			}
			return "/valid/data", nil
		}, io.Discard, report)
		if code != 0 || !called {
			t.Fatal(code, called)
		}
	}
	for _, command := range []string{"material-root", "completion-material-root", "verify-installed-completion", "prepare-recovery-material"} {
		args := []string{command, "--snapshot", snapshot}
		if command == "prepare-recovery-material" {
			args = materialTestArgs()
		}
		for _, e := range []error{recoverypublication.ErrMaterialAbsent, recoverypublication.ErrLegacyCompletionMaterial, errors.New("private evidence")} {
			var output, reported bytes.Buffer
			code := dispatchMaterial(args, 0, func(string, recoverypublication.Request) (string, error) { return "/ignored", e }, &output, func(s string) { reported.WriteString(s) })
			want := exitOutput
			if errors.Is(e, recoverypublication.ErrMaterialAbsent) && (command == "material-root" || command == "completion-material-root") {
				want = 3
			}
			if errors.Is(e, recoverypublication.ErrLegacyCompletionMaterial) && command == "completion-material-root" {
				want = 6
			}
			if code != want || output.Len() != 0 || strings.Contains(reported.String(), e.Error()) {
				t.Fatal(command, e, code, want, output.String(), reported.String())
			}
		}
	}
}
