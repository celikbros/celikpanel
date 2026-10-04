//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Rows copied verbatim from the native 2026-09-29 owner-inverse-critical
// evidence (target-started/owner-post-state.txt): PowerDNS serving on public
// addresses beside Debian 13's systemd-resolved stub and proxy listeners.
const nativePDNSWithResolvedStub = `udp UNCONN 0      0         192.0.2.10:53 0.0.0.0:* users:(("pdns_server",pid=9415,fd=6))
udp UNCONN 0      0          10.0.2.15:53 0.0.0.0:* users:(("pdns_server",pid=9415,fd=5))
udp UNCONN 0      0         127.0.0.54:53 0.0.0.0:* users:(("systemd-resolve",pid=372,fd=20))
udp UNCONN 0      0      127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=372,fd=18))
tcp LISTEN 0      4096   127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=372,fd=19))
tcp LISTEN 0      128        10.0.2.15:53 0.0.0.0:* users:(("pdns_server",pid=9415,fd=7))
tcp LISTEN 0      128       192.0.2.10:53 0.0.0.0:* users:(("pdns_server",pid=9415,fd=8))
tcp LISTEN 0      4096      127.0.0.54:53 0.0.0.0:* users:(("systemd-resolve",pid=372,fd=21))
`

// Rows of named from the same run while BIND served: loopback, IPv6 loopback
// and link-local sockets the public inventory skipped.
const nativeNamedLocalRows = `tcp LISTEN 0      10                         127.0.0.1:53 0.0.0.0:* users:(("named",pid=7461,fd=20))
tcp LISTEN 0      10                             [::1]:53    [::]:* users:(("named",pid=7461,fd=34))
tcp LISTEN 0      10     [fe80::5054:ff:fe13:10]%mgmt0:53    [::]:* users:(("named",pid=7461,fd=42))
udp UNCONN 0      0                          127.0.0.1:53 0.0.0.0:* users:(("named",pid=7461,fd=18))
`

func resolvedCgroup(paths map[uint64]string) ProcessCgroupReader {
	return func(_ context.Context, pid uint64) (string, error) {
		path, ok := paths[pid]
		if !ok {
			return "", errors.New("process gone")
		}
		return path, nil
	}
}

func fixedListeners(outputs ...string) BINDListenerRunner {
	calls := 0
	return func(context.Context) ([]byte, error) {
		output := outputs[len(outputs)-1]
		if calls < len(outputs) {
			output = outputs[calls]
		}
		calls++
		return []byte(output), nil
	}
}

func TestLocalDNSListenersAcceptSourceAndProvenResolverStub(t *testing.T) {
	cgroup := resolvedCgroup(map[uint64]string{372: "/system.slice/systemd-resolved.service"})
	// An ordinary host: the source PowerDNS owns only public sockets and the
	// resolver stub is proved by cgroup and address. It passes and is recorded.
	ctx, record := WithLocalDNSListenerRecord(context.Background())
	stubs, err := ProbeLocalDNSListeners(ctx, "pdns_server", 9415, fixedListeners(nativePDNSWithResolvedStub), cgroup)
	want := []string{
		"tcp|127.0.0.53|systemd-resolve|372|systemd-resolved.service",
		"tcp|127.0.0.54|systemd-resolve|372|systemd-resolved.service",
		"udp|127.0.0.53|systemd-resolve|372|systemd-resolved.service",
		"udp|127.0.0.54|systemd-resolve|372|systemd-resolved.service",
	}
	if err != nil || !reflect.DeepEqual(stubs, want) {
		t.Fatalf("ordinary resolver host refused: %q %v", stubs, err)
	}
	// A second proof under the same record adds nothing twice.
	if _, err := ProbeLocalDNSListeners(ctx, "", 0, fixedListeners(nativePDNSWithResolvedStub), cgroup); err != nil {
		t.Fatalf("stopped-source proof with only public PowerDNS rows and the stub refused: %v", err)
	}
	if got := record.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded stub listeners = %q", got)
	}
	if text := LocalDNSListenerRecordText(record.Entries()); !strings.Contains(text, "systemd-resolved stub resolver") ||
		!strings.Contains(text, "udp|127.0.0.53|systemd-resolve|372|systemd-resolved.service") {
		t.Fatalf("record text = %q", text)
	}
	if LocalDNSListenerRecordText(nil) != "" {
		t.Fatal("empty record produced text")
	}
	// The verified source daemon may hold loopback and link-local sockets.
	sourceLocal := strings.ReplaceAll(nativeNamedLocalRows, "named", "pdns_server")
	sourceLocal = strings.ReplaceAll(sourceLocal, "pid=7461", "pid=9415")
	if stubs, err := ProbeLocalDNSListeners(context.Background(), "pdns_server", 9415, fixedListeners(nativePDNSWithResolvedStub+sourceLocal), cgroup); err != nil || len(stubs) != 4 {
		t.Fatalf("source-owned local sockets refused: %q %v", stubs, err)
	}
	// No local listener at all passes with nothing recorded.
	if stubs, err := ProbeLocalDNSListeners(context.Background(), "", 0, fixedListeners(""), cgroup); err != nil || len(stubs) != 0 {
		t.Fatalf("empty inventory: %q %v", stubs, err)
	}
}

func TestLocalDNSListenersRefuseOtherOwners(t *testing.T) {
	good := map[uint64]string{372: "/system.slice/systemd-resolved.service"}
	for _, tc := range []struct {
		name          string
		output        string
		sourceProcess string
		sourcePID     uint64
		cgroups       map[uint64]string
		want          string
	}{
		{"named-local-beside-stopped-source", nativePDNSWithResolvedStub + nativeNamedLocalRows, "", 0, good, "named process (PID 7461) holds local port-53 listener"},
		{"named-local-beside-pdns-source", nativeNamedLocalRows, "pdns_server", 9415, good, "outside the verified DNS source"},
		{"pdns-local-other-pid", `udp UNCONN 0 0 127.0.0.1:53 0.0.0.0:* users:(("pdns_server",pid=9999,fd=5))`, "pdns_server", 9415, good, "pdns_server process (PID 9999)"},
		{"pdns-local-beside-stopped-source", `udp UNCONN 0 0 [::1]:53 [::]:* users:(("pdns_server",pid=9415,fd=5))`, "", 0, good, "outside the verified DNS source"},
		{"unknown-local", `udp UNCONN 0 0 127.0.1.1:53 0.0.0.0:* users:(("dnsmasq",pid=800,fd=4))`, "", 0, good, "unrecognized process \"dnsmasq\""},
		{"resolver-name-wrong-cgroup", `udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=372,fd=18))`, "", 0,
			map[uint64]string{372: "/system.slice/other.service"}, "is not in systemd-resolved.service"},
		{"resolver-name-cgroup-unknown", `udp UNCONN 0 0 127.0.0.54:53 0.0.0.0:* users:(("systemd-resolve",pid=373,fd=18))`, "", 0, good, "process gone"},
		{"resolver-name-wrong-address", `udp UNCONN 0 0 127.0.0.1:53 0.0.0.0:* users:(("systemd-resolve",pid=372,fd=18))`, "", 0, good, "unrecognized process"},
		{"resolver-name-link-local", `udp UNCONN 0 0 [fe80::1]%eth0:53 [::]:* users:(("systemd-resolve",pid=372,fd=18))`, "", 0, good, "unrecognized process"},
		{"malformed", "udp UNCONN 0 0 127.0.0.53:53\n", "", 0, good, "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, record := WithLocalDNSListenerRecord(context.Background())
			_, err := ProbeLocalDNSListeners(ctx, tc.sourceProcess, tc.sourcePID, fixedListeners(tc.output), resolvedCgroup(tc.cgroups))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			if len(record.Entries()) != 0 {
				t.Fatal("refused proof recorded listeners")
			}
		})
	}
	// Two reads must classify the same inventory.
	changed := fixedListeners(nativePDNSWithResolvedStub, strings.SplitN(nativePDNSWithResolvedStub, "\n", 4)[3])
	if _, err := ProbeLocalDNSListeners(context.Background(), "pdns_server", 9415, changed, resolvedCgroup(good)); err == nil ||
		!strings.Contains(err.Error(), "changed during observation") {
		t.Fatalf("changing inventory accepted: %v", err)
	}
	if _, err := ProbeLocalDNSListeners(context.Background(), "pdns_server", 0, fixedListeners(""), resolvedCgroup(good)); err == nil {
		t.Fatal("half source identity accepted")
	}
}

func TestProcessUnifiedCgroupIsBoundToOneProcess(t *testing.T) {
	root := t.TempDir()
	write := func(content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, "372"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "372", "cgroup"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	verify := func(string) error { return nil }
	start := func(uint64) (string, error) { return "4242", nil }
	write("0::/system.slice/systemd-resolved.service\n")
	if path, err := processUnifiedCgroup(context.Background(), root, 372, verify, start); err != nil || path != "/system.slice/systemd-resolved.service" {
		t.Fatalf("v2 cgroup = %q %v", path, err)
	}
	write("12:pids:/system.slice/systemd-resolved.service\n1:name=systemd:/system.slice/systemd-resolved.service\n0::/system.slice/systemd-resolved.service\n")
	if path, err := processUnifiedCgroup(context.Background(), root, 372, verify, start); err != nil || path != "/system.slice/systemd-resolved.service" {
		t.Fatalf("hybrid cgroup = %q %v", path, err)
	}
	for _, bad := range []string{"1:name=systemd:/system.slice/systemd-resolved.service\n", "0::/a\n0::/b\n", "garbage\n"} {
		write(bad)
		if _, err := processUnifiedCgroup(context.Background(), root, 372, verify, start); err == nil {
			t.Fatalf("cgroup record %q accepted", bad)
		}
	}
	write("0::/system.slice/systemd-resolved.service\n")
	tokens := []string{"1", "2"}
	reused := func(uint64) (string, error) {
		token := tokens[0]
		tokens = tokens[1:]
		return token, nil
	}
	if _, err := processUnifiedCgroup(context.Background(), root, 372, verify, reused); err == nil {
		t.Fatal("PID reuse during the read accepted")
	}
	if _, err := processUnifiedCgroup(context.Background(), root, 372, func(string) error { return errors.New("not procfs") }, start); err == nil {
		t.Fatal("unverified procfs accepted")
	}
	// The installed reader reads this test process's own cgroup record.
	if path, err := NativeProcessUnifiedCgroup(context.Background(), uint64(os.Getpid())); err == nil && !strings.HasPrefix(path, "/") {
		t.Fatalf("native cgroup path = %q", path)
	}
}
