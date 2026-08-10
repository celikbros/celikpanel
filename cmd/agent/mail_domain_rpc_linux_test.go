//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func configureMailDomainDeletionTest(t *testing.T) transport.DeleteMailDomainRequest {
	t.Helper()
	configureMailMutationTest(t)
	previousCommit := buildCommit
	buildCommit = "mail-domain-test"
	t.Cleanup(func() { buildCommit = previousCommit })
	return transport.DeleteMailDomainRequest{
		ExpectedBuildCommit: "mail-domain-test",
		DomainID:            41,
		Domain:              "example.com",
	}
}

func TestDeleteMailDomainRemovesOnlyTargetSourcesAndQuarantines(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	writeMailTestFile(t, dovecotUsersPath,
		"target@example.com:{CRYPT}hash\nkeep@sub.example.com:{CRYPT}hash\nkeep@other.test:{CRYPT}hash\n", 0o640)
	writeMailTestFile(t, postfixVBoxPath,
		"target@example.com example.com/target/\nkeep@sub.example.com sub.example.com/keep/\nkeep@other.test other.test/keep/\n", 0o644)
	writeMailTestFile(t, postfixDomainsPath,
		"example.com OK\nsub.example.com OK\nother.test OK\n", 0o644)
	writeMailTestFile(t, postfixVirtualPath,
		"alias@example.com dest@else.test\n@example.com catch@else.test\nkeep@other.test target@example.com\nkeep@sub.example.com dest@else.test\n", 0o644)
	messagePath := filepath.Join(mailRootDir, "example.com", "user", "new", "message")
	if err := os.MkdirAll(filepath.Dir(messagePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(messagePath, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}

	var postmaps []string
	mailMutationPostmap = func(_ context.Context, path string) error {
		postmaps = append(postmaps, path)
		return nil
	}
	var response transport.DeleteMailDomainResponse
	if err := (&Agent{}).DeleteMailDomain(&request, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Applied || !response.Quarantined {
		t.Fatalf("response = %+v", response)
	}
	if got := readMailTestFile(t, dovecotUsersPath); strings.Contains(got, "@example.com") ||
		!strings.Contains(got, "@sub.example.com") || !strings.Contains(got, "@other.test") {
		t.Fatalf("dovecot users = %q", got)
	}
	if got := readMailTestFile(t, postfixVirtualPath); strings.Contains(got, "alias@example.com") ||
		strings.Contains(got, "@example.com catch") ||
		!strings.Contains(got, "keep@other.test target@example.com") {
		t.Fatalf("virtual map = %q", got)
	}
	wantPostmaps := []string{postfixVBoxPath, postfixDomainsPath, postfixVirtualPath}
	if !reflect.DeepEqual(postmaps, wantPostmaps) {
		t.Fatalf("postmaps = %v, want %v", postmaps, wantPostmaps)
	}
	if _, err := os.Stat(filepath.Join(mailRootDir, "example.com")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source directory still exists: %v", err)
	}
	quarantinedMessage := filepath.Join(
		mailRootDir, mailDomainQuarantineDirectory,
		mailDomainQuarantineName(request.Domain, request.DomainID), "user", "new", "message",
	)
	if content, err := os.ReadFile(quarantinedMessage); err != nil || string(content) != "retained" {
		t.Fatalf("quarantined message = %q, %v", content, err)
	}

	postmaps = nil
	response = transport.DeleteMailDomainResponse{}
	if err := (&Agent{}).DeleteMailDomain(&request, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Applied || !response.Quarantined ||
		!reflect.DeepEqual(postmaps, wantPostmaps) {
		t.Fatalf("idempotent response=%+v postmaps=%v", response, postmaps)
	}
}

func TestDeleteMailDomainDoesNotCreateMissingMaps(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	if err := os.Remove(dovecotUsersPath); err != nil {
		t.Fatal(err)
	}
	postmapCalls := 0
	mailMutationPostmap = func(context.Context, string) error {
		postmapCalls++
		return errors.New("unexpected postmap")
	}
	var response transport.DeleteMailDomainResponse
	if err := (&Agent{}).DeleteMailDomain(&request, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Applied || response.Quarantined || postmapCalls != 0 {
		t.Fatalf("response=%+v postmap calls=%d", response, postmapCalls)
	}
	for _, path := range []string{
		dovecotUsersPath, postfixVBoxPath, postfixDomainsPath, postfixVirtualPath, mailRootDir,
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing path %s was created: %v", path, err)
		}
	}
}

func TestDeleteMailDomainRejectsOrphanCompiledMap(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	if err := os.WriteFile(postfixVBoxPath+".db", []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	postmapCalls := 0
	mailMutationPostmap = func(context.Context, string) error {
		postmapCalls++
		return nil
	}
	var response transport.DeleteMailDomainResponse
	err := (&Agent{}).DeleteMailDomain(&request, &response)
	if err == nil || !strings.Contains(err.Error(), "orphan compiled mail map") {
		t.Fatalf("error = %v", err)
	}
	if response.Applied || response.Quarantined || postmapCalls != 0 {
		t.Fatalf("response=%+v postmap calls=%d", response, postmapCalls)
	}
	if content, readErr := os.ReadFile(postfixVBoxPath + ".db"); readErr != nil ||
		string(content) != "stale" {
		t.Fatalf("orphan index = %q, %v", content, readErr)
	}
}

func TestDeleteMailDomainPostmapFailureRollsBackBeforeQuarantine(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	fixtures := map[string]string{
		dovecotUsersPath:   "target@example.com:{CRYPT}hash\nkeep@other.test:{CRYPT}hash\n",
		postfixVBoxPath:    "target@example.com example.com/target/\nkeep@other.test other.test/keep/\n",
		postfixDomainsPath: "example.com OK\nother.test OK\n",
		postfixVirtualPath: "alias@example.com dest@other.test\nkeep@other.test target@example.com\n",
	}
	before := make(map[string]string, len(fixtures))
	for path, content := range fixtures {
		mode := os.FileMode(0o644)
		if path == dovecotUsersPath {
			mode = 0o640
		}
		writeMailTestFile(t, path, content, mode)
		before[path] = readMailTestFile(t, path)
	}
	messagePath := filepath.Join(mailRootDir, "example.com", "target", "new", "message")
	if err := os.MkdirAll(filepath.Dir(messagePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(messagePath, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	mailMutationPostmap = func(_ context.Context, path string) error {
		if err := os.WriteFile(path+".db", []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
		return errors.New("simulated postmap failure")
	}
	var response transport.DeleteMailDomainResponse
	err := (&Agent{}).DeleteMailDomain(&request, &response)
	if err == nil || response.Applied || response.Quarantined {
		t.Fatalf("error=%v response=%+v", err, response)
	}
	for path, want := range before {
		if got := readMailTestFile(t, path); got != want {
			t.Fatalf("rolled back %s = %q, want %q", path, got, want)
		}
		if _, statErr := os.Stat(path + ".db"); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("partial index for %s remains: %v", path, statErr)
		}
	}
	if content, readErr := os.ReadFile(messagePath); readErr != nil || string(content) != "retained" {
		t.Fatalf("source message = %q, %v", content, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(mailRootDir, mailDomainQuarantineDirectory)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("quarantine ran before postmap completed: %v", statErr)
	}
}

func TestDeleteMailDomainQuarantineConflictRollsBackMaps(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	writeMailTestFile(t, dovecotUsersPath, "target@example.com:{CRYPT}hash\n", 0o640)
	writeMailTestFile(t, postfixVBoxPath, "target@example.com example.com/target/\n", 0o644)
	writeMailTestFile(t, postfixDomainsPath, "example.com OK\n", 0o644)
	writeMailTestFile(t, postfixVirtualPath, "alias@example.com dest@other.test\n", 0o644)
	before := map[string]string{
		dovecotUsersPath:   readMailTestFile(t, dovecotUsersPath),
		postfixVBoxPath:    readMailTestFile(t, postfixVBoxPath),
		postfixDomainsPath: readMailTestFile(t, postfixDomainsPath),
		postfixVirtualPath: readMailTestFile(t, postfixVirtualPath),
	}
	source := filepath.Join(mailRootDir, request.Domain)
	target := filepath.Join(
		mailRootDir, mailDomainQuarantineDirectory,
		mailDomainQuarantineName(request.Domain, request.DomainID),
	)
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	var response transport.DeleteMailDomainResponse
	err := (&Agent{}).DeleteMailDomain(&request, &response)
	if err == nil || !strings.Contains(err.Error(), "both exist") || response.Applied {
		t.Fatalf("error=%v response=%+v", err, response)
	}
	for path, want := range before {
		if got := readMailTestFile(t, path); got != want {
			t.Fatalf("rolled back %s = %q, want %q", path, got, want)
		}
	}
	for _, path := range []string{source, target} {
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			t.Fatalf("directory %s was lost: %v", path, statErr)
		}
	}
}

func TestDeleteMailDomainRejectsSymlinkedSourceAndRollsBackMaps(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	writeMailTestFile(t, postfixDomainsPath, "example.com OK\n", 0o644)
	before := readMailTestFile(t, postfixDomainsPath)
	if err := os.MkdirAll(mailRootDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(mailRootDir, request.Domain)); err != nil {
		t.Fatal(err)
	}
	var response transport.DeleteMailDomainResponse
	err := (&Agent{}).DeleteMailDomain(&request, &response)
	if err == nil || response.Applied {
		t.Fatalf("error=%v response=%+v", err, response)
	}
	if got := readMailTestFile(t, postfixDomainsPath); got != before {
		t.Fatalf("map was not rolled back: %q", got)
	}
	if content, readErr := os.ReadFile(sentinel); readErr != nil || string(content) != "safe" {
		t.Fatalf("outside sentinel = %q, %v", content, readErr)
	}
}

func TestDeleteMailDomainRejectsBuildAndIdentityBeforeMutation(t *testing.T) {
	valid := configureMailDomainDeletionTest(t)
	writeMailTestFile(t, postfixDomainsPath, "example.com OK\n", 0o644)
	before := readMailTestFile(t, postfixDomainsPath)
	cases := []struct {
		name   string
		mutate func(*transport.DeleteMailDomainRequest)
	}{
		{name: "build", mutate: func(req *transport.DeleteMailDomainRequest) {
			req.ExpectedBuildCommit = "wrong-build"
		}},
		{name: "identity", mutate: func(req *transport.DeleteMailDomainRequest) {
			req.DomainID = 0
		}},
		{name: "canonical domain", mutate: func(req *transport.DeleteMailDomainRequest) {
			req.Domain = "Example.COM"
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			var response transport.DeleteMailDomainResponse
			if err := (&Agent{}).DeleteMailDomain(&request, &response); err == nil {
				t.Fatal("request unexpectedly succeeded")
			}
			if response.Applied || response.Quarantined {
				t.Fatalf("response = %+v", response)
			}
			if got := readMailTestFile(t, postfixDomainsPath); got != before {
				t.Fatalf("map changed to %q", got)
			}
		})
	}
}

func TestDeleteMailDomainHardensMailRootAndQuarantine(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	if err := os.MkdirAll(filepath.Join(mailRootDir, request.Domain), 0o777); err != nil {
		t.Fatal(err)
	}
	var response transport.DeleteMailDomainResponse
	if err := (&Agent{}).DeleteMailDomain(&request, &response); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{
		mailRootDir: managedMailRootMode,
		filepath.Join(mailRootDir, mailDomainQuarantineDirectory): 0o700,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() {
			t.Fatalf("%s ownership = %#v", path, info.Sys())
		}
	}
}
