//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// nonRootMailTLSTestGID returns a real group on this host that is not root's.
// The production shape needs one: the shipped unit runs the agent as User=root
// with Group=celikpanel, so the group the agent writes with is never the group
// the managed /etc/ssl/celikpanel/_mail directory carries.
// nonRootMailTLSTestGID, bu makinede root'unki olmayan gercek bir grup dondurur.
func nonRootMailTLSTestGID(t *testing.T) uint64 {
	t.Helper()
	raw, err := os.ReadFile("/etc/group")
	if err != nil {
		t.Skipf("no group database on this host: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 3 {
			continue
		}
		gid, convErr := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 32)
		if convErr != nil || gid == 0 || gid > 60000 {
			continue
		}
		return gid
	}
	t.Skip("no non-root group on this host")
	return 0
}

// TestDefaultMailTLSPairSatisfiesItsOwnReadbackOnAManagedDirectory is R-046's
// first half. On a correctly installed host the managed mail TLS directory is
// root:root and the agent process's own group is celikpanel, so a certificate
// published without an explicit owner lands in the agent's group and the
// readback that follows it - the one that compares the file's group with the
// managed directory's - can never pass. The check is right; the write has to
// ask for the ownership the check demands.
//
// This test stages the same divergence without touching /etc/ssl: the managed
// owner's group is a real non-root group, while the test process writes as
// root. Publishing the pair and reading it back must agree.
//
// Bu test, ayni ayrismayi /etc/ssl'e dokunmadan kurar: yonetilen sahibin grubu
// root olmayan gercek bir gruptur, oysa surec root olarak yazar.
func TestDefaultMailTLSPairSatisfiesItsOwnReadbackOnAManagedDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("staging a managed directory group requires root")
	}
	gid := nonRootMailTLSTestGID(t)
	directory := t.TempDir()
	certPath := filepath.Join(directory, "default-cert.pem")
	keyPath := filepath.Join(directory, "default-key.pem")
	owner := mailTLSDirectoryOwner{uid: 0, gid: gid}
	if err := os.Chown(directory, 0, int(gid)); err != nil {
		t.Skipf("cannot stage a managed directory group: %v", err)
	}

	certPEM, keyPEM, err := generateDefaultMailCertPair(
		"mail.example.test", defaultMailTLSTestNow,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := secureWriteDefaultMailTLSFile(certPath, certPEM, 0o644, owner); err != nil {
		t.Fatalf("publish default mail certificate: %v", err)
	}
	if err := secureWriteDefaultMailTLSFile(keyPath, keyPEM, 0o600, owner); err != nil {
		t.Fatalf("publish default mail private key: %v", err)
	}
	for path, mode := range map[string]os.FileMode{certPath: 0o644, keyPath: 0o600} {
		if _, err := inspectDefaultMailTLSFile(path, owner, mode); err != nil {
			t.Fatalf("verify %s metadata: %v", path, err)
		}
		info, statErr := os.Lstat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		fileUID, uidKnown := mailTLSFileOwner(info)
		fileGID, gidKnown := mailTLSFileGroup(info)
		if !uidKnown || !gidKnown || fileUID != owner.uid || fileGID != owner.gid {
			t.Fatalf(
				"%s owner = %d:%d, want the managed directory's %d:%d",
				path, fileUID, fileGID, owner.uid, owner.gid,
			)
		}
	}
	if err := validateDefaultMailCertPair(
		certPath, keyPath, "mail.example.test", defaultMailTLSTestNow,
	); err != nil {
		t.Fatalf("published pair is invalid: %v", err)
	}
}

// TestEnsureDefaultMailCertPairPublishesTheDirectoryOwner is the same
// agreement end to end, on whatever identity this host gives the test.
// TestEnsureDefaultMailCertPairPublishesTheDirectoryOwner, ayni ortusmeyi
// uctan uca dogrular.
func TestEnsureDefaultMailCertPairPublishesTheDirectoryOwner(t *testing.T) {
	certPath, keyPath := defaultMailTLSTestPaths(t)
	if err := ensureDefaultMailCertPair(
		certPath, keyPath, "mail.example.test",
		defaultMailTLSTestNow, secureWriteDefaultMailTLSFile,
	); err != nil {
		t.Fatal(err)
	}
	owner, err := prepareDefaultMailTLSDirectory(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{certPath: 0o644, keyPath: 0o600} {
		if _, err := inspectDefaultMailTLSFile(path, owner, mode); err != nil {
			t.Fatalf("verify %s metadata: %v", path, err)
		}
	}
	requireDefaultMailTLSPair(t, certPath, keyPath)
}
