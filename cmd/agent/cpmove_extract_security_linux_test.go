//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cpmoveTestMember struct {
	name     string
	typeflag byte
	mode     int64
	content  string
}

func configureCpmoveTestRoots(t *testing.T) (string, string) {
	t.Helper()
	archiveRoot := t.TempDir()
	siteHome := filepath.Join(t.TempDir(), "site-home")
	if err := os.MkdirAll(filepath.Join(siteHome, "public_html"), 0o755); err != nil {
		t.Fatal(err)
	}

	previousArchiveRoot := cpmoveArchiveRoot
	previousOwner := cpmoveArchiveOwnerUID
	previousSiteHome := cpmoveSiteHome
	cpmoveArchiveRoot = archiveRoot
	cpmoveArchiveOwnerUID = uint32(os.Geteuid())
	cpmoveSiteHome = func(subscriptionID, domainID int) (string, error) {
		if subscriptionID != 41 || domainID != 73 {
			return "", os.ErrPermission
		}
		return siteHome, nil
	}
	t.Cleanup(func() {
		cpmoveArchiveRoot = previousArchiveRoot
		cpmoveArchiveOwnerUID = previousOwner
		cpmoveSiteHome = previousSiteHome
	})
	return archiveRoot, siteHome
}

func writeCpmoveSiteArchive(t *testing.T, archiveRoot string, members []cpmoveTestMember) string {
	t.Helper()
	archivePath := filepath.Join(archiveRoot, "cpmove-user.tar.gz")
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, member := range members {
		mode := member.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{
			Name:     member.name,
			Mode:     mode,
			Typeflag: member.typeflag,
		}
		if member.typeflag == tar.TypeReg || member.typeflag == tar.TypeRegA {
			header.Size = int64(len(member.content))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := io.WriteString(tarWriter, member.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(archivePath, 0o600); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func runCpmoveExtraction(t *testing.T, archivePath string) (*CpmoveExtractResponse, error) {
	t.Helper()
	response := &CpmoveExtractResponse{}
	err := extractCpmoveFilesSecure(&CpmoveExtractRequest{
		Path:           archivePath,
		SubscriptionID: 41,
		DomainID:       73,
	}, response)
	return response, err
}

func assertNoCpmoveStage(t *testing.T, siteHome string) {
	t.Helper()
	entries, err := os.ReadDir(siteHome)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".cpmove-") {
			t.Fatalf("temporary cpmove entry survived: %s", entry.Name())
		}
	}
}

func TestExtractCpmoveFilesAtomicallyReplacesDocumentRoot(t *testing.T) {
	archiveRoot, siteHome := configureCpmoveTestRoots(t)
	oldPath := filepath.Join(siteHome, "public_html", "old.txt")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := writeCpmoveSiteArchive(t, archiveRoot, []cpmoveTestMember{
		{name: "cpmove-user/homedir/public_html/assets", typeflag: tar.TypeDir, mode: 0o755},
		{name: "cpmove-user/homedir/public_html/index.html", typeflag: tar.TypeReg, content: "new"},
		{name: "cpmove-user/homedir/public_html/assets/app.js", typeflag: tar.TypeReg, content: "asset"},
	})

	response, err := runCpmoveExtraction(t, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Complete || response.Files != 2 || response.Bytes != 8 {
		t.Fatalf("response = %+v", response)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old document root survived: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(siteHome, "public_html", "index.html"))
	if err != nil || string(content) != "new" {
		t.Fatalf("published content = %q, err = %v", content, err)
	}
	assertNoCpmoveStage(t, siteHome)
}

func TestExtractCpmoveFilesRejectsSymlinkWithoutChangingCurrentSite(t *testing.T) {
	archiveRoot, siteHome := configureCpmoveTestRoots(t)
	oldPath := filepath.Join(siteHome, "public_html", "index.html")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := writeCpmoveSiteArchive(t, archiveRoot, []cpmoveTestMember{
		{name: "cpmove-user/homedir/public_html/link", typeflag: tar.TypeSymlink},
	})

	response, err := runCpmoveExtraction(t, archivePath)
	if err == nil || response.Complete {
		t.Fatalf("response = %+v, err = %v; want fail-closed extraction", response, err)
	}
	content, readErr := os.ReadFile(oldPath)
	if readErr != nil || string(content) != "old" {
		t.Fatalf("current site changed: content = %q, err = %v", content, readErr)
	}
	assertNoCpmoveStage(t, siteHome)
}

func TestExtractCpmoveFilesRejectsDuplicatePathAtomically(t *testing.T) {
	archiveRoot, siteHome := configureCpmoveTestRoots(t)
	oldPath := filepath.Join(siteHome, "public_html", "index.html")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := writeCpmoveSiteArchive(t, archiveRoot, []cpmoveTestMember{
		{name: "cpmove-user/homedir/public_html/index.html", typeflag: tar.TypeReg, content: "first"},
		{name: "cpmove-user/homedir/public_html/index.html", typeflag: tar.TypeReg, content: "second"},
	})

	response, err := runCpmoveExtraction(t, archivePath)
	if err == nil || response.Complete {
		t.Fatalf("response = %+v, err = %v; want duplicate rejection", response, err)
	}
	content, readErr := os.ReadFile(oldPath)
	if readErr != nil || string(content) != "old" {
		t.Fatalf("current site changed: content = %q, err = %v", content, readErr)
	}
	assertNoCpmoveStage(t, siteHome)
}

func TestExtractCpmoveFilesRejectsSymlinkDocumentRoot(t *testing.T) {
	archiveRoot, siteHome := configureCpmoveTestRoots(t)
	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "marker.txt")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(siteHome, "public_html")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(siteHome, "public_html")); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	archivePath := writeCpmoveSiteArchive(t, archiveRoot, []cpmoveTestMember{
		{name: "cpmove-user/homedir/public_html/index.html", typeflag: tar.TypeReg, content: "new"},
	})

	response, err := runCpmoveExtraction(t, archivePath)
	if err == nil || response.Complete {
		t.Fatalf("response = %+v, err = %v; want symlink destination rejection", response, err)
	}
	content, readErr := os.ReadFile(outsidePath)
	if readErr != nil || string(content) != "outside" {
		t.Fatalf("outside directory changed: content = %q, err = %v", content, readErr)
	}
}

func TestOpenTrustedCpmoveArchiveRejectsOutsideAndReadableFiles(t *testing.T) {
	archiveRoot, _ := configureCpmoveTestRoots(t)
	inside := writeCpmoveSiteArchive(t, archiveRoot, []cpmoveTestMember{
		{name: "cpmove-user/homedir/public_html/index.html", typeflag: tar.TypeReg, content: "new"},
	})
	if err := os.Chmod(inside, 0o644); err != nil {
		t.Fatal(err)
	}
	if file, err := openTrustedCpmoveArchive(inside); err == nil {
		file.Close()
		t.Fatal("group/world-readable archive was accepted")
	}

	outside := filepath.Join(t.TempDir(), "outside.tar.gz")
	if err := os.WriteFile(outside, []byte("not-empty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if file, err := openTrustedCpmoveArchive(outside); err == nil {
		file.Close()
		t.Fatal("archive outside trusted root was accepted")
	}
}
