//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const panelTLSReceiptWriterFixtureEnv = "CELIKPANEL_TEST_TLS_RECEIPT_WRITER_ROOT"

// The production writers have fixed paths. Chroot only this child process so
// the exact issuance and renewal writers can run without touching the host.
// Üretim yazıcıları sabit yollar kullanır. Gerçek sertifika ve yenileme kodunu
// ana sisteme dokunmadan çalıştırmak için yalnız bu alt süreç chroot kullanır.
func TestPanelTLSReceiptWriterFixtureProcess(t *testing.T) {
	root := os.Getenv(panelTLSReceiptWriterFixtureEnv)
	if root == "" {
		return
	}
	if os.Geteuid() != 0 || !filepath.IsAbs(root) || root == "/" {
		t.Fatal("receipt writer fixture requires its isolated root")
	}
	if err := unix.Chroot(root); err != nil {
		t.Fatalf("isolate production TLS writer: %v", err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	const domain = "panel.example.test"
	writeMaterial := func() ([]byte, []byte, []byte) {
		pair, leaf := testPanelCertificate(t, domain)
		privateKey, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf}),
			pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}), leaf
	}
	certificate, key, leaf := writeMaterial()
	receipt, err := newPanelCertificateIssueReceipt(
		testMutationRequestID, testPanelCertificateIssueQualifier(t), domain, leaf,
	)
	if err != nil {
		t.Fatal(err)
	}
	stage, err := stagePanelCertificateIssueMaterial(domain, managedPanelTLSDir, certificate, key, receipt)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.close()
	if err := stage.publish(); err != nil {
		t.Fatal(err)
	}
	if err := stage.close(); err != nil {
		t.Fatal(err)
	}
	certificate, key, _ = writeMaterial()
	if err := installPanelCertMaterial(domain, managedPanelTLSDir, certificate, key); err != nil {
		t.Fatal(err)
	}
}

type panelTLSReceiptSnapshotFixture struct {
	root, tls, receipt, snapshot, helper, oldVersion, currentVersion string
}

func newPanelTLSReceiptSnapshotFixture(t *testing.T) panelTLSReceiptSnapshotFixture {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("real TLS ownership, chroot, and snapshot integration requires root")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatal("TLS snapshot integration requires bash")
	}
	fixtureRoot := t.TempDir()
	for _, dir := range []string{"etc", "var/lib/celikpanel"} {
		if err := os.MkdirAll(filepath.Join(fixtureRoot, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for name, contents := range map[string]string{
		"passwd":        "root:x:0:0:root:/root:/bin/sh\ncelikpanel:x:65533:65533:panel:/nonexistent:/bin/false\n",
		"group":         "root:x:0:\ncelikpanel:x:65533:\n",
		"nsswitch.conf": "passwd: files\ngroup: files\n",
	} {
		if err := os.WriteFile(filepath.Join(fixtureRoot, "etc", name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestPanelTLSReceiptWriterFixtureProcess$", "-test.count=1")
	command.Env = append(os.Environ(), panelTLSReceiptWriterFixtureEnv+"="+fixtureRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("production certificate writers failed: %v\n%s", err, output)
	}
	root := filepath.Join(fixtureRoot, "var/lib/celikpanel")
	f := panelTLSReceiptSnapshotFixture{root: root, tls: filepath.Join(root, "tls"), snapshot: filepath.Join(root, "snapshot")}
	f.helper, err = filepath.Abs(filepath.Join("..", "..", "deploy", "panel-tls-snapshot.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if candidate := os.Getenv("CELIKPANEL_TEST_TLS_SNAPSHOT_HELPER"); candidate != "" {
		f.helper, err = filepath.Abs(candidate)
		if err != nil {
			t.Fatal(err)
		}
	}
	f.currentVersion, err = os.Readlink(filepath.Join(f.tls, "current"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(f.tls)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("issuance and renewal must leave two versions plus current, got %d entries", len(entries))
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != f.currentVersion {
			f.oldVersion = entry.Name()
		}
	}
	f.receipt = filepath.Join(f.tls, f.oldVersion, panelCertificateIssueReceiptName)
	for version, wantCount := range map[string]int{f.oldVersion: 4, f.currentVersion: 3} {
		files, err := os.ReadDir(filepath.Join(f.tls, version))
		if err != nil || len(files) != wantCount {
			t.Fatalf("real writer version %q: files=%v err=%v, want %d files", version, files, err, wantCount)
		}
	}
	oldDir, err := os.Open(filepath.Join(f.tls, f.oldVersion))
	if err != nil {
		t.Fatal(err)
	}
	defer oldDir.Close()
	if _, found, err := readPanelCertificateIssueReceiptAt(int(oldDir.Fd())); err != nil || !found {
		t.Fatalf("real issuance receipt is invalid: found=%v err=%v", found, err)
	}
	for _, dir := range []string{"agent", "etc/letsencrypt/renewal-hooks/deploy", "snapshot"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f panelTLSReceiptSnapshotFixture) shell(t *testing.T, action string) ([]byte, error) {
	t.Helper()
	// Only absent scheduler answers are simulated. File validation, copies,
	// manifests, comparisons, and restoration execute the production helper.
	// Yalnız bulunmayan zamanlayıcı yanıtları taklit edilir. Dosya doğrulama,
	// kopyalama, manifest ve geri yükleme üretim yardımcısını çalıştırır.
	script := `set -euo pipefail
umask 077
systemctl() {
    case "$1" in
        show) printf 'not-found\n' ;;
        is-enabled) printf 'not-found\n'; return 1 ;;
        is-active) printf 'inactive\n'; return 3 ;;
        *) printf 'unexpected scheduler action\n' >&2; return 99 ;;
    esac
}
source "$1"
export CELIKPANEL_TLS_SNAPSHOT_TEST_ROOT="$2"
tls="$2/tls"
snapshot="$2/snapshot"
pending="$2/agent/panel-certificate-activation.json"
hook="$2/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert"
` + action
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "bash", "-c", script, "tls-receipt-integration", f.helper, f.root).CombinedOutput()
}

type panelTLSReceiptFileState struct {
	bytes    []byte
	mode     os.FileMode
	uid, gid uint32
	links    uint64
	mtime    int64
	inode    uint64
}

func panelTLSReceiptState(t *testing.T, path string) panelTLSReceiptFileState {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata unix.Stat_t
	if err := unix.Lstat(path, &metadata); err != nil {
		t.Fatal(err)
	}
	return panelTLSReceiptFileState{contents, os.FileMode(metadata.Mode), metadata.Uid, metadata.Gid,
		uint64(metadata.Nlink), metadata.Mtim.Nano(), metadata.Ino}
}

func TestPanelTLSSnapshotPreservesRealIssuedReceiptAfterRenewal(t *testing.T) {
	f := newPanelTLSReceiptSnapshotFixture(t)
	before := panelTLSReceiptState(t, f.receipt)
	if before.mode.Perm() != 0o600 || before.uid != 0 || before.gid != 0 || before.links != 1 {
		t.Fatalf("unexpected production receipt metadata: %+v", before)
	}
	if output, err := f.shell(t, `panel_tls_normalize_legacy_self_signed "$tls" 65533 65533`); err != nil {
		t.Fatalf("valid issued receipt rejected during normalization: %v\n%s", err, output)
	}
	if got := panelTLSReceiptState(t, f.receipt); !reflect.DeepEqual(got, before) {
		t.Fatal("normalizing managed issuance and renewal changed receipt bytes or metadata")
	}
	if output, err := f.shell(t, `panel_tls_snapshot_capture "$snapshot" "$tls" "$pending" "$hook"
panel_tls_snapshot_validate "$snapshot"
panel_tls_snapshot_assert_source_unchanged "$snapshot" "$tls" "$pending" "$hook" quiesced`); err != nil {
		t.Fatalf("real writer tree snapshot failed: %v\n%s", err, output)
	}
	copyPath := filepath.Join(f.snapshot, "managed", f.oldVersion, panelCertificateIssueReceiptName)
	copied := panelTLSReceiptState(t, copyPath)
	copied.inode = before.inode
	if !reflect.DeepEqual(copied, before) {
		t.Fatal("snapshot did not preserve receipt bytes and metadata")
	}
	manifest, err := os.ReadFile(filepath.Join(f.snapshot, "managed.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	wantRecord := fmt.Sprintf("F\t%s/%s\t0\t0\t600\t%d\t%d\t%x\n",
		f.oldVersion, panelCertificateIssueReceiptName, before.mtime/int64(time.Second), len(before.bytes), sha256.Sum256(before.bytes))
	if !bytes.Contains(manifest, []byte(wantRecord)) {
		t.Fatalf("receipt integrity record missing from manifest: want %q", wantRecord)
	}
	if err := os.WriteFile(f.receipt, append(append([]byte(nil), before.bytes...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.shell(t, `panel_tls_snapshot_assert_source_unchanged "$snapshot" "$tls" "$pending" "$hook" quiesced`); err == nil {
		t.Fatal("receipt content drift was not detected")
	}
	if output, err := f.shell(t, `panel_tls_restore_snapshot "$snapshot" "$tls" "$pending" "$hook"
panel_tls_snapshot_assert_source_unchanged "$snapshot" "$tls" "$pending" "$hook" quiesced`); err != nil {
		t.Fatalf("real writer receipt restore failed: %v\n%s", err, output)
	}
	restored := panelTLSReceiptState(t, f.receipt)
	restored.inode = before.inode
	if !reflect.DeepEqual(restored, before) {
		t.Fatal("restore did not preserve receipt bytes and metadata")
	}
	current, err := os.Readlink(filepath.Join(f.tls, "current"))
	if err != nil || current != f.currentVersion {
		t.Fatalf("restoration replaced renewal with old issuance: current=%q err=%v", current, err)
	}
}

func TestPanelTLSSnapshotRejectsUnsafeIssuedReceipt(t *testing.T) {
	cases := map[string]func(panelTLSReceiptSnapshotFixture) error{
		"owner": func(f panelTLSReceiptSnapshotFixture) error { return os.Chown(f.receipt, 65533, 0) },
		"group": func(f panelTLSReceiptSnapshotFixture) error { return os.Chown(f.receipt, 0, 65533) },
		"mode":  func(f panelTLSReceiptSnapshotFixture) error { return os.Chmod(f.receipt, 0o640) },
		"hardlink": func(f panelTLSReceiptSnapshotFixture) error {
			return os.Link(f.receipt, filepath.Join(f.root, "receipt-alias"))
		},
		"unknown-file": func(f panelTLSReceiptSnapshotFixture) error {
			return os.WriteFile(filepath.Join(f.tls, f.oldVersion, "unexpected"), []byte("unexpected"), 0o600)
		},
		"empty": func(f panelTLSReceiptSnapshotFixture) error { return os.WriteFile(f.receipt, nil, 0o600) },
		"oversized": func(f panelTLSReceiptSnapshotFixture) error {
			return os.WriteFile(f.receipt, []byte(strings.Repeat("x", panelCertificateIssueReceiptMaxSize+1)), 0o600)
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newPanelTLSReceiptSnapshotFixture(t)
			if err := corrupt(f); err != nil {
				t.Fatal(err)
			}
			before := panelTLSReceiptState(t, f.receipt)
			if output, err := f.shell(t, `panel_tls_normalize_legacy_self_signed "$tls" 65533 65533`); err == nil {
				t.Fatalf("normalization accepted unsafe issuance receipt: %s", output)
			}
			if output, err := f.shell(t, `panel_tls_snapshot_capture "$snapshot" "$tls" "$pending" "$hook"`); err == nil {
				t.Fatalf("snapshot accepted unsafe issuance receipt: %s", output)
			}
			if after := panelTLSReceiptState(t, f.receipt); !reflect.DeepEqual(before, after) {
				t.Fatal("rejected receipt was silently repaired or changed")
			}
		})
	}
}
