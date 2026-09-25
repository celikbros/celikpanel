//go:build linux

package dnsenginerecovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

func TestPDNSAdoptionConfigProofRejectsOwnerAndPathDrift(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned native config fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	etc := filepath.Join(root, "etc")
	dir := filepath.Join(etc, "powerdns")
	nested := filepath.Join(dir, "pdns.d")
	for _, path := range []string{etc, dir, nested} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const serviceGID = 12345
	mainPath := filepath.Join(dir, "pdns.conf")
	managedPath := filepath.Join(nested, "celikpanel.conf")
	if err := os.WriteFile(mainPath, []byte("launch=gsqlite3\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(mainPath, 0, serviceGID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedPath, []byte("local-address=0.0.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := dnsengineartifact.JournalPolicy{
		PDNSMainPath:    "/etc/powerdns/pdns.conf",
		PDNSManagedPath: "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath: "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
	}
	snapshots := []dnsengineartifact.FileSnapshot{
		{Path: policy.PDNSMainPath, Exists: true, Mode: 0o640, OwnerKnown: true, UID: 0, GID: serviceGID, Data: []byte("launch=gsqlite3\n")},
		{Path: policy.PDNSClusterPath},
		{Path: policy.PDNSManagedPath, Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 0, Data: []byte("local-address=0.0.0.0\n")},
	}
	for i := range snapshots {
		if snapshots[i].Exists {
			snapshots[i].SHA256 = dnsengineartifact.DigestBytes(snapshots[i].Data)
		}
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	probe := func() error {
		return ProbePDNSAdoptionConfigsAt(context.Background(), fd, policy, snapshots, serviceGID)
	}
	if err := probe(); err != nil {
		t.Fatalf("exact native files rejected: %v", err)
	}
	if err := ProbePDNSAdoptionConfigsAt(context.Background(), fd, policy, snapshots, serviceGID+1); err == nil {
		t.Fatal("foreign local service group accepted")
	}
	if err := os.WriteFile(managedPath, []byte("local-address=127.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := probe(); err == nil {
		t.Fatal("owner edit accepted")
	}
	if err := os.WriteFile(managedPath, snapshots[2].Data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(managedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", managedPath); err != nil {
		t.Fatal(err)
	}
	if err := probe(); err == nil {
		t.Fatal("symlinked managed config accepted")
	}
	if err := os.Remove(managedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedPath, snapshots[2].Data, 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(nested, "alias.conf")
	if err := os.Link(managedPath, alias); err != nil {
		t.Fatal(err)
	}
	if err := probe(); err == nil {
		t.Fatal("hard-linked managed config accepted")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	clusterPath := filepath.Join(nested, "celikpanel-cluster.conf")
	if err := os.WriteFile(clusterPath, []byte("unexpected=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := probe(); err == nil {
		t.Fatal("unexpected cluster config accepted")
	}
	if err := os.Remove(clusterPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nested, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := probe(); err == nil {
		t.Fatal("writable config parent accepted")
	}
}
