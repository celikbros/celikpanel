//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func cpmoveArchiveReader(t *testing.T, name, content string) *tar.Reader {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{
		Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return tar.NewReader(bytes.NewReader(archive.Bytes()))
}

func TestSecureExtractCpmoveRejectsPreexistingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	var response CpmoveExtractResponse
	err := secureExtractCpmoveFiles(
		cpmoveArchiveReader(t, "cpmove-user/homedir/public_html/escape.txt", "overwrite"),
		root,
		&response,
	)
	if err == nil {
		t.Fatal("expected symlink target to be rejected")
	}
	content, readErr := os.ReadFile(outside)
	if readErr != nil || string(content) != "keep" {
		t.Fatalf("outside file changed: content=%q err=%v", content, readErr)
	}
}

func TestSecureExtractCpmoveRejectsPreexistingHardlink(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	var response CpmoveExtractResponse
	err := secureExtractCpmoveFiles(
		cpmoveArchiveReader(t, "backup-user/homedir/public_html/escape.txt", "overwrite"),
		root,
		&response,
	)
	if err == nil {
		t.Fatal("expected hardlink target to be rejected")
	}
	content, readErr := os.ReadFile(outside)
	if readErr != nil || string(content) != "keep" {
		t.Fatalf("outside file changed: content=%q err=%v", content, readErr)
	}
}

func TestSecureAtomicWriteFailureKeepsPreviousFile(t *testing.T) {
	root := t.TempDir()
	name := "index.html"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	rootFD, err := openFileManagerRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)
	err = secureAtomicWriteAt(rootFD, name, 0o644, func(file *os.File) error {
		if _, err := file.Write([]byte("partial")); err != nil {
			return err
		}
		return errors.New("injected failure")
	})
	if err == nil {
		t.Fatal("expected injected write failure")
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil || string(content) != "old" {
		t.Fatalf("live file changed after failed save: content=%q err=%v", content, readErr)
	}
}
