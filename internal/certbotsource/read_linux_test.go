//go:build linux

package certbotsource

import (
	"bytes"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sourceFixture(t *testing.T, purpose string) (Reader, string, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("native root ownership required")
	}
	root := t.TempDir()
	lineage := "celikpanel-" + purpose + "-" + strings.Repeat("a", 24)
	for _, dir := range []string{"live", "archive"} {
		if err := os.MkdirAll(filepath.Join(root, dir, lineage), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, prefix := range []string{"fullchain", "privkey"} {
		if err := os.WriteFile(filepath.Join(root, "archive", lineage, prefix+"1.pem"), []byte(prefix+"-fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../../archive/"+lineage+"/"+prefix+"1.pem", filepath.Join(root, "live", lineage, prefix+".pem")); err != nil {
			t.Fatal(err)
		}
	}
	return Reader{Root: root, Openat2: unix.Openat2}, root, lineage
}
func TestNativeCertbotNamespacesShareReaderWithoutMutation(t *testing.T) {
	for _, purpose := range []string{"panel", "mail"} {
		t.Run(purpose, func(t *testing.T) {
			reader, root, lineage := sourceFixture(t, purpose)
			name := filepath.Join(root, "archive", lineage, "privkey1.pem")
			var before, after unix.Stat_t
			if err := unix.Lstat(name, &before); err != nil {
				t.Fatal(err)
			}
			cert, key, err := reader.ReadPair(lineage)
			if err != nil || !bytes.Equal(cert, []byte("fullchain-fixture")) || !bytes.Equal(key, []byte("privkey-fixture")) {
				t.Fatalf("read failed: %v", err)
			}
			if err := unix.Lstat(name, &after); err != nil {
				t.Fatal(err)
			}
			if before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid || before.Ctim != after.Ctim || before.Mtim != after.Mtim {
				t.Fatal("read normalized native material")
			}
		})
	}
}
func TestInvalidLineageAndMissingSecureResolverFailClosed(t *testing.T) {
	reader, _, lineage := sourceFixture(t, "mail")
	for _, name := range []string{"../" + lineage, "/" + lineage, lineage + "/x", "other-" + strings.Repeat("a", 24), strings.ToUpper(lineage)} {
		if _, _, err := reader.ReadPair(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	reader.Openat2 = nil
	if _, _, err := reader.ReadPair(lineage); err == nil {
		t.Fatal("missing secure resolver accepted")
	}
	if _, err := reader.OpenAt(-1, "x", 0, 0); err == nil {
		t.Fatal("nil resolver was not refused")
	}
}
func TestNativeGenerationMismatchAndOwnerKeyChangeRefused(t *testing.T) {
	t.Run("generation", func(t *testing.T) {
		reader, root, lineage := sourceFixture(t, "mail")
		link := filepath.Join(root, "live", lineage, "privkey.pem")
		if err := os.WriteFile(filepath.Join(root, "archive", lineage, "privkey2.pem"), []byte("other"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../../archive/"+lineage+"/privkey2.pem", link); err != nil {
			t.Fatal(err)
		}
		if _, _, err := reader.ReadPair(lineage); err == nil {
			t.Fatal("mixed revisions accepted")
		}
	})
	t.Run("owner", func(t *testing.T) {
		reader, root, lineage := sourceFixture(t, "mail")
		key := filepath.Join(root, "archive", lineage, "privkey1.pem")
		if err := os.Chmod(key, 0644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := reader.ReadPair(lineage); err == nil {
			t.Fatal("public private key accepted")
		}
		st, err := os.Stat(key)
		if err != nil || st.Mode().Perm() != 0644 {
			t.Fatal("owner change silently normalized")
		}
	})
}
