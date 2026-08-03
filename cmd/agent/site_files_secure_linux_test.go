//go:build linux

package main

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testSiteFileSubscriptionID = 7
	testSiteFileDomainID       = 19
)

func useTemporarySiteFileRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	previous := siteFileDocumentRoot
	siteFileDocumentRoot = func(subscriptionID, domainID int) (string, error) {
		if subscriptionID != testSiteFileSubscriptionID || domainID != testSiteFileDomainID {
			return "", os.ErrPermission
		}
		return root, nil
	}
	t.Cleanup(func() { siteFileDocumentRoot = previous })
	return root
}

func TestSiteFilesRejectAbsoluteTraversalAndMissingIdentity(t *testing.T) {
	useTemporarySiteFileRoot(t)
	agent := new(Agent)
	for _, requestPath := range []string{"/etc/passwd", "../outside", `..\\outside`} {
		var response ReadFileResponse
		err := agent.ReadFile(&ReadFileRequest{
			SubscriptionID: testSiteFileSubscriptionID,
			DomainID:       testSiteFileDomainID,
			Path:           requestPath,
		}, &response)
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("ReadFile(%q) error = %v, want permission error", requestPath, err)
		}
	}
	var response ReadFileResponse
	if err := agent.ReadFile(&ReadFileRequest{Path: "index.html"}, &response); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("missing identity error = %v, want permission error", err)
	}
}

func TestSiteFilesOpenat2RejectsSymlinkEscape(t *testing.T) {
	root := useTemporarySiteFileRoot(t)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	agent := new(Agent)
	var readResponse ReadFileResponse
	if err := agent.ReadFile(&ReadFileRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "escape/secret.txt",
	}, &readResponse); err == nil {
		t.Fatal("symlink escape was readable")
	}
	var writeResponse bool
	if err := agent.WriteFile(&WriteFileRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "escape/secret.txt",
		Content:        "changed",
	}, &writeResponse); err == nil || writeResponse {
		t.Fatalf("symlink escape write = (%v, %v), want refusal", writeResponse, err)
	}
	content, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "secret" {
		t.Fatalf("outside file changed to %q", content)
	}
}

func TestSiteFilesLifecycleStaysRelativeAndAtomic(t *testing.T) {
	root := useTemporarySiteFileRoot(t)
	agent := new(Agent)
	identity := func(path string) CreateRequest {
		return CreateRequest{SubscriptionID: testSiteFileSubscriptionID, DomainID: testSiteFileDomainID, Path: path}
	}
	var success bool
	directoryRequest := identity("assets")
	directoryRequest.IsDir = true
	if err := agent.CreateFileOrDir(&directoryRequest, &success); err != nil || !success {
		t.Fatalf("create directory = (%v, %v)", success, err)
	}
	success = false
	fileRequest := identity("assets/index.txt")
	if err := agent.CreateFileOrDir(&fileRequest, &success); err != nil || !success {
		t.Fatalf("create file = (%v, %v)", success, err)
	}
	success = false
	if err := agent.WriteFile(&WriteFileRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "assets/index.txt",
		Content:        "hello",
	}, &success); err != nil || !success {
		t.Fatalf("write file = (%v, %v)", success, err)
	}
	var readResponse ReadFileResponse
	if err := agent.ReadFile(&ReadFileRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "assets/index.txt",
	}, &readResponse); err != nil {
		t.Fatal(err)
	}
	if readResponse.Path != "assets/index.txt" || readResponse.Content != "hello" || readResponse.IsBinary {
		t.Fatalf("unexpected read response: %+v", readResponse)
	}
	var listResponse ListFilesResponse
	if err := agent.ListFiles(&ListFilesRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "assets",
	}, &listResponse); err != nil {
		t.Fatal(err)
	}
	if len(listResponse.Files) != 1 || listResponse.Files[0].Path != "assets/index.txt" {
		t.Fatalf("unexpected listing: %+v", listResponse)
	}
	success = false
	if err := agent.RenameFile(&RenameRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		OldPath:        "assets/index.txt",
		NewPath:        "assets/renamed.txt",
	}, &success); err != nil || !success {
		t.Fatalf("rename = (%v, %v)", success, err)
	}
	success = false
	if err := agent.ChmodFile(&ChmodRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "assets/renamed.txt",
		Permissions:    "600",
	}, &success); err != nil || !success {
		t.Fatalf("chmod = (%v, %v)", success, err)
	}
	mode, err := os.Stat(filepath.Join(root, "assets", "renamed.txt"))
	if err != nil || mode.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, error = %v", mode, err)
	}
	success = false
	if err := agent.UploadFile(&UploadFileRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "assets",
		Name:           "binary.dat",
		Content:        base64.StdEncoding.EncodeToString([]byte{0, 1, 2}),
	}, &success); err != nil || !success {
		t.Fatalf("upload = (%v, %v)", success, err)
	}
	success = false
	if err := agent.DeleteFileOrDir(&DeleteRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "assets",
	}, &success); err != nil || !success {
		t.Fatalf("delete = (%v, %v)", success, err)
	}
	if _, err := os.Stat(filepath.Join(root, "assets")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted tree still exists: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".celikpanel-write-") {
			t.Fatalf("staging artifact leaked: %s", entry.Name())
		}
	}
}

func TestSiteFileDeleteUnlinksSymlinkWithoutFollowingIt(t *testing.T) {
	root := useTemporarySiteFileRoot(t)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(outsideFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	var success bool
	if err := new(Agent).DeleteFileOrDir(&DeleteRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "link",
	}, &success); err != nil || !success {
		t.Fatalf("delete symlink = (%v, %v)", success, err)
	}
	if content, err := os.ReadFile(outsideFile); err != nil || string(content) != "keep" {
		t.Fatalf("outside target changed: %q, %v", content, err)
	}
}

func TestSiteFilesRefuseRootDeletionAndSpecialPermissionBits(t *testing.T) {
	root := useTemporarySiteFileRoot(t)
	if err := os.WriteFile(filepath.Join(root, "index.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := new(Agent)
	var success bool
	if err := agent.DeleteFileOrDir(&DeleteRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "",
	}, &success); !errors.Is(err, os.ErrPermission) || success {
		t.Fatalf("root delete = (%v, %v), want refusal", success, err)
	}
	if err := agent.ChmodFile(&ChmodRequest{
		SubscriptionID: testSiteFileSubscriptionID,
		DomainID:       testSiteFileDomainID,
		Path:           "index.txt",
		Permissions:    "4755",
	}, &success); !errors.Is(err, os.ErrInvalid) || success {
		t.Fatalf("special chmod = (%v, %v), want refusal", success, err)
	}
}
