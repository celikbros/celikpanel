//go:build linux

package main

import (
	"encoding/base64"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

func TestFileManagerScopeDerivesRootAndRejectsUntrustedPaths(t *testing.T) {
	root, relativePath, err := fileManagerScope(4, 13, "assets/logo.svg")
	if err != nil {
		t.Fatal(err)
	}
	if root != "/var/www/celikpanel/subscriptions/4/sites/13/public_html" {
		t.Fatalf("root = %q", root)
	}
	if relativePath != "assets/logo.svg" {
		t.Fatalf("relative path = %q", relativePath)
	}

	for _, tc := range []struct {
		subscriptionID int
		domainID       int
		path           string
	}{
		{0, 13, "."},
		{4, 0, "."},
		{4, 13, ""},
		{4, 13, "/etc/passwd"},
		{4, 13, "../public_html-sibling/secret"},
		{4, 13, "a/../../secret"},
		{4, 13, "a/./b"},
		{4, 13, `..\outside`},
	} {
		if _, _, err := fileManagerScope(tc.subscriptionID, tc.domainID, tc.path); err == nil {
			t.Errorf("fileManagerScope(%d, %d, %q) succeeded", tc.subscriptionID, tc.domainID, tc.path)
		}
	}
}

func TestSecureFileManagerNormalNestedOperations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "public_html")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := secureCreateFileOrDir(root, "nested/deeper", true); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}
	if err := secureCreateFileOrDir(root, "nested/deeper/index.txt", false); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := secureWriteFile(root, "nested/deeper/index.txt", []byte("hello")); err != nil {
		t.Fatalf("write file: %v", err)
	}

	content, info, err := secureReadFile(root, "nested/deeper/index.txt", maxFileManagerFileBytes)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != "hello" || info.Path != "nested/deeper/index.txt" || info.Size != 5 {
		t.Fatalf("unexpected read result: content=%q info=%+v", content, info)
	}

	entries, err := secureListFiles(root, "nested/deeper", 100)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "index.txt" ||
		entries[0].Path != "nested/deeper/index.txt" {
		t.Fatalf("unexpected entries: %+v", entries)
	}

	if err := secureChmodFile(root, "nested/deeper/index.txt", 0o600); err != nil {
		t.Fatalf("chmod file: %v", err)
	}
	stat, err := os.Stat(filepath.Join(root, "nested", "deeper", "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", stat.Mode().Perm())
	}

	if err := secureRenameFile(root, "nested/deeper/index.txt", "nested/deeper/renamed.txt"); err != nil {
		t.Fatalf("rename file: %v", err)
	}
	if err := secureUploadFile(root, "nested/deeper/upload.bin", []byte{0, 1, 2}); err != nil {
		t.Fatalf("upload file: %v", err)
	}
	uploaded, _, err := secureReadFile(root, "nested/deeper/upload.bin", maxFileManagerFileBytes)
	if err != nil || len(uploaded) != 3 || uploaded[2] != 2 {
		t.Fatalf("unexpected uploaded file: %v %v", uploaded, err)
	}

	if err := secureDeleteFileOrDir(root, "nested"); err != nil {
		t.Fatalf("recursive delete: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "nested")); !os.IsNotExist(err) {
		t.Fatalf("nested still exists: %v", err)
	}
}

func TestSecureFileManagerSymlinkOutsideCannotBeTouched(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "public_html")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("outside-secret"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	if _, _, err := secureReadFile(root, "escape/secret.txt", maxFileManagerFileBytes); err == nil {
		t.Fatal("read through outside symlink succeeded")
	}
	if err := secureWriteFile(root, "escape/secret.txt", []byte("overwritten")); err == nil {
		t.Fatal("write through outside symlink succeeded")
	}
	if err := secureUploadFile(root, "escape/upload.txt", []byte("uploaded")); err == nil {
		t.Fatal("upload through outside symlink succeeded")
	}
	if err := secureChmodFile(root, "escape", 0o777); err == nil {
		t.Fatal("chmod of outside symlink succeeded")
	}
	if err := secureRenameFile(root, "escape", "renamed"); err == nil {
		t.Fatal("rename of outside symlink succeeded")
	}
	if err := secureDeleteFileOrDir(root, "escape"); err == nil {
		t.Fatal("delete of outside symlink succeeded")
	}

	content, err := os.ReadFile(outsideFile)
	if err != nil || string(content) != "outside-secret" {
		t.Fatalf("outside content changed: %q, %v", content, err)
	}
	stat, err := os.Stat(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm() != 0o640 {
		t.Fatalf("outside mode changed to %o", stat.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(outside, "upload.txt")); !os.IsNotExist(err) {
		t.Fatalf("upload escaped document root: %v", err)
	}
}

func TestSecureFileManagerRejectsSymlinkedDocumentRoot(t *testing.T) {
	base := t.TempDir()
	actualRoot := filepath.Join(base, "actual")
	rootLink := filepath.Join(base, "public_html")
	if err := os.Mkdir(actualRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actualRoot, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actualRoot, rootLink); err != nil {
		t.Fatal(err)
	}
	if _, _, err := secureReadFile(rootLink, "secret", maxFileManagerFileBytes); err == nil {
		t.Fatal("symlinked document root was accepted")
	}
}

func TestSecureFileManagerRecursiveDeleteDoesNotFollowNestedSymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "public_html")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(root, "tree"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(outsideFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "tree", "outside-link")); err != nil {
		t.Fatal(err)
	}

	if err := secureDeleteFileOrDir(root, "tree"); err != nil {
		t.Fatalf("recursive delete: %v", err)
	}
	if content, err := os.ReadFile(outsideFile); err != nil || string(content) != "keep" {
		t.Fatalf("recursive delete followed symlink: %q, %v", content, err)
	}
}

func TestSecureFileManagerOpenat2RejectsSiblingAndTraversal(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "site")
	sibling := filepath.Join(base, "site-sibling")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, candidate := range []string{"../site-sibling/secret", "../../etc/passwd", "/etc/passwd"} {
		if _, _, err := secureReadFile(root, candidate, maxFileManagerFileBytes); err == nil {
			t.Errorf("secureReadFile(%q) escaped root", candidate)
		}
	}
}

func TestFileManagerRPCRejectsUploadTraversalRootMutationAndLimits(t *testing.T) {
	agent := &Agent{}
	for _, name := range []string{"../escape", "/absolute", `..\escape`, "dir/name"} {
		var ok bool
		err := agent.UploadFile(&UploadFileRequest{
			SubscriptionID: 4,
			DomainID:       13,
			Path:           ".",
			Name:           name,
			Content:        base64.StdEncoding.EncodeToString([]byte("x")),
		}, &ok)
		if err == nil {
			t.Errorf("UploadFile accepted %q", name)
		}
	}

	var ok bool
	if err := agent.DeleteFileOrDir(&DeleteRequest{
		SubscriptionID: 4, DomainID: 13, Path: ".",
	}, &ok); err == nil {
		t.Fatal("root delete succeeded")
	}
	if err := agent.ChmodFile(&ChmodRequest{
		SubscriptionID: 4, DomainID: 13, Path: ".", Permissions: "755",
	}, &ok); err == nil {
		t.Fatal("root chmod succeeded")
	}
	if err := agent.RenameFile(&RenameRequest{
		SubscriptionID: 4, DomainID: 13, OldPath: ".", NewPath: "renamed",
	}, &ok); err == nil {
		t.Fatal("root rename succeeded")
	}
	if err := agent.WriteFile(&WriteFileRequest{
		SubscriptionID: 4,
		DomainID:       13,
		Path:           "large.txt",
		Content:        strings.Repeat("x", maxFileManagerFileBytes+1),
	}, &ok); err == nil {
		t.Fatal("oversized write succeeded")
	}
	if err := agent.UploadFile(&UploadFileRequest{
		SubscriptionID: 4,
		DomainID:       13,
		Path:           ".",
		Name:           "large.bin",
		Content:        strings.Repeat("A", base64.StdEncoding.EncodedLen(maxFileManagerFileBytes)+1),
	}, &ok); err == nil {
		t.Fatal("oversized upload succeeded")
	}
}

func TestSecureFileManagerRejectsOversizedRead(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "large")
	if err := os.WriteFile(name, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := secureReadFile(root, "large", 4); err == nil {
		t.Fatal("oversized read succeeded")
	}
}

func TestValidateUploadNameMatchesSharedContract(t *testing.T) {
	if err := hostingpath.ValidateFileName("normal.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestFileManagerMethodsAreExposedByNetRPC(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", &Agent{}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	go server.ServeConn(serverConn)
	client := rpc.NewClient(clientConn)
	defer client.Close()
	defer serverConn.Close()

	var response ListFilesResponse
	err := client.Call("Agent.ListFiles", &ListFilesRequest{
		SubscriptionID: 0,
		DomainID:       13,
		Path:           ".",
	}, &response)
	if err == nil {
		t.Fatal("invalid identity unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "can't find method") {
		t.Fatalf("Agent.ListFiles was not registered: %v", err)
	}
}
