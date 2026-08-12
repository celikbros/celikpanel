//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderWebmailNginxConfigUsesUnixSocketOnly(t *testing.T) {
	config := renderWebmailNginxConfig(
		"/run/celikpanel-webmail.sock",
		"/var/lib/celikpanel-webmail/public_html",
		"/run/php/php-fpm.sock",
	)
	if !strings.Contains(config, "listen unix:/run/celikpanel-webmail.sock;") {
		t.Fatalf("Unix socket listen is missing:\n%s", config)
	}
	for _, forbidden := range []string{"127.0.0.1:8307", "listen 8307", "proxy_pass http"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("webmail config contains TCP fallback %q:\n%s", forbidden, config)
		}
	}
}

func TestWebmailSocketPathForNginxRejectsConfigInjection(t *testing.T) {
	for _, path := range []string{
		"relative.sock",
		"/run/webmail.sock; listen 0.0.0.0:8307",
		"/run/../tmp/webmail.sock",
		"/run/web mail.sock",
	} {
		t.Run(path, func(t *testing.T) {
			if got, err := validateWebmailSocketPath(path); err == nil {
				t.Fatalf("unsafe socket path accepted: %q", got)
			}
		})
	}
}

func TestWebmailSocketPathForNginxCannotBeRedirectedByEnvironment(t *testing.T) {
	t.Setenv("CELIKPANEL_WEBMAIL_SOCKET", "/tmp/attacker-controlled.sock")
	got, err := webmailSocketPathForNginx()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/run/celikpanel-webmail.sock" {
		t.Fatalf("production webmail socket redirected to %q", got)
	}
}

func TestWebmailProductionBaseDirIsFixed(t *testing.T) {
	if webmailBaseDir != "/var/lib/celikpanel-webmail" {
		t.Fatalf("production webmail base dir = %q, want fixed path", webmailBaseDir)
	}
}

func TestWebmailProductionConfigMetadataIsRootOnly(t *testing.T) {
	uid, gid := webmailConfigMetadataIdentity(webmailConfPath)
	if uid != 0 || gid != 0 {
		t.Fatalf("production metadata identity = %d:%d, want 0:0", uid, gid)
	}
	uid, gid = webmailConfigMetadataIdentity(filepath.Join(t.TempDir(), "webmail.conf"))
	if uid != -1 || gid != -1 {
		t.Fatalf("test metadata identity = %d:%d, want unchanged", uid, gid)
	}
}

func TestPublishRoundcubeStageRefusesExistingDestination(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "final")
	mustWriteTestFile(t, filepath.Join(stage, "stage.txt"), []byte("stage"), 0o600)
	mustWriteTestFile(t, filepath.Join(final, "final.txt"), []byte("final"), 0o600)

	if err := publishRoundcubeStage(stage, final); err == nil {
		t.Fatal("publishRoundcubeStage unexpectedly replaced an existing destination")
	}
	assertTestFileContent(t, filepath.Join(stage, "stage.txt"), []byte("stage"))
	assertTestFileContent(t, filepath.Join(final, "final.txt"), []byte("final"))
}

func TestPublishAndRetireRoundcubeTree(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "final")
	mustWriteTestFile(t, filepath.Join(stage, "marker.txt"), []byte("ready"), 0o600)

	if err := publishRoundcubeStage(stage, final); err != nil {
		t.Fatalf("publishRoundcubeStage: %v", err)
	}
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging tree still exists after publish: %v", err)
	}
	assertTestFileContent(t, filepath.Join(final, "marker.txt"), []byte("ready"))

	result, err := retireRoundcubeTree(final)
	if err != nil {
		t.Fatalf("retireRoundcubeTree: %v", err)
	}
	if !result.Removed || !result.MutationApplied {
		t.Fatalf("retirement result = %+v, want removed applied mutation", result)
	}
	if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published tree still exists after retirement: %v", err)
	}
}

func TestRetireRoundcubeTreeMissingIsConvergedWithoutMutation(t *testing.T) {
	originalSyncParent := roundcubeSyncParent
	syncCalls := 0
	roundcubeSyncParent = func(string) error {
		syncCalls++
		return nil
	}
	t.Cleanup(func() { roundcubeSyncParent = originalSyncParent })

	result, err := retireRoundcubeTree(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Removed || result.MutationApplied {
		t.Fatalf("missing-tree result = %+v, want removed without mutation", result)
	}
	if syncCalls != 1 {
		t.Fatalf("missing-tree parent sync calls = %d, want 1", syncCalls)
	}
}

func TestRetireRoundcubeTreeRetryConvergesAfterFinalParentSyncFailure(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "roundcube")
	retired := filepath.Join(parent, ".roundcube.retired")
	mustWriteTestFile(t, filepath.Join(path, "marker.txt"), []byte("present"), 0o600)

	originalSyncParent := roundcubeSyncParent
	injectedErr := errors.New("injected final parent sync failure")
	syncCalls := 0
	roundcubeSyncParent = func(path string) error {
		syncCalls++
		if syncCalls == 2 {
			return injectedErr
		}
		return syncRoundcubeParent(path)
	}
	t.Cleanup(func() { roundcubeSyncParent = originalSyncParent })

	result, err := retireRoundcubeTree(path)
	if err == nil || !strings.Contains(err.Error(), injectedErr.Error()) {
		t.Fatalf("first retirement error = %v, want final sync failure", err)
	}
	if !result.Removed || !result.MutationApplied {
		t.Fatalf("first retirement result = %+v, want removed applied mutation", result)
	}
	for _, candidate := range []string{path, retired} {
		if _, statErr := os.Lstat(candidate); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("first retirement left %s: %v", candidate, statErr)
		}
	}

	retry, err := retireRoundcubeTree(path)
	if err != nil {
		t.Fatalf("retirement retry: %v", err)
	}
	if !retry.Removed || retry.MutationApplied {
		t.Fatalf("retry result = %+v, want removed without a new mutation", retry)
	}
	if syncCalls != 3 {
		t.Fatalf("parent sync calls = %d, want two first-attempt and one retry sync", syncCalls)
	}
}

func TestRetireRoundcubeTreeRefusesSymlink(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	link := filepath.Join(parent, "roundcube")
	mustWriteTestFile(t, filepath.Join(outside, "marker.txt"), []byte("keep"), 0o600)
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	result, err := retireRoundcubeTree(link)
	if err == nil {
		t.Fatal("retireRoundcubeTree unexpectedly followed a symlink")
	}
	if result.Removed || result.MutationApplied {
		t.Fatalf("unsafe pre-mutation result = %+v, want false flags", result)
	}
	assertTestFileContent(t, filepath.Join(outside, "marker.txt"), []byte("keep"))
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was changed: info=%v err=%v", info, err)
	}
}

func TestRetireRoundcubeTreeRefusesUnsafeRetirementArtifacts(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		parent := t.TempDir()
		path := filepath.Join(parent, "roundcube")
		retired := filepath.Join(parent, ".roundcube.retired")
		outside := filepath.Join(parent, "outside")
		mustWriteTestFile(t, filepath.Join(outside, "marker.txt"), []byte("keep"), 0o600)
		if err := os.Symlink(outside, retired); err != nil {
			t.Fatal(err)
		}

		result, err := retireRoundcubeTree(path)
		if err == nil {
			t.Fatal("unsafe retired symlink unexpectedly accepted")
		}
		if result.Removed || result.MutationApplied {
			t.Fatalf("unsafe retired symlink result = %+v, want false flags", result)
		}
		if info, statErr := os.Lstat(retired); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("retired symlink was changed: info=%v err=%v", info, statErr)
		}
		assertTestFileContent(t, filepath.Join(outside, "marker.txt"), []byte("keep"))
	})

	t.Run("regular file", func(t *testing.T) {
		parent := t.TempDir()
		path := filepath.Join(parent, "roundcube")
		retired := filepath.Join(parent, ".roundcube.retired")
		mustWriteTestFile(t, retired, []byte("keep"), 0o600)

		result, err := retireRoundcubeTree(path)
		if err == nil {
			t.Fatal("unsafe retired regular file unexpectedly accepted")
		}
		if result.Removed || result.MutationApplied {
			t.Fatalf("unsafe retired regular file result = %+v, want false flags", result)
		}
		assertTestFileContent(t, retired, []byte("keep"))
	})
}

func TestRetireRoundcubeTreeFailsClosedOnActiveRetiredConflict(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "roundcube")
	retired := filepath.Join(parent, ".roundcube.retired")
	mustWriteTestFile(t, filepath.Join(path, "active.txt"), []byte("active"), 0o600)
	mustWriteTestFile(t, filepath.Join(retired, "retired.txt"), []byte("retired"), 0o600)

	result, err := retireRoundcubeTree(path)
	if err == nil {
		t.Fatal("active/retired conflict unexpectedly accepted")
	}
	if result.Removed || result.MutationApplied {
		t.Fatalf("conflict result = %+v, want false flags", result)
	}
	assertTestFileContent(t, filepath.Join(path, "active.txt"), []byte("active"))
	assertTestFileContent(t, filepath.Join(retired, "retired.txt"), []byte("retired"))
}

func TestReconcileRoundcubeArtifactsRemovesOnlyBaseSpecificCrashLeftovers(t *testing.T) {
	parent := t.TempDir()
	final := filepath.Join(parent, "roundcube")
	hexSuffix := "0123456789abcdef01234567"
	ownedDirectories := []string{
		filepath.Join(parent, ".roundcube.retired-"+hexSuffix),
		filepath.Join(parent, ".roundcube.stage"),
		filepath.Join(parent, "..roundcube.stage.retired"),
		filepath.Join(parent, "..roundcube.stage.retired-"+hexSuffix),
	}
	outside := filepath.Join(parent, "outside")
	mustWriteTestFile(t, filepath.Join(outside, "keep.txt"), []byte("keep"), 0o600)
	for _, path := range ownedDirectories {
		mustWriteTestFile(t, filepath.Join(path, "marker.txt"), []byte("stale"), 0o600)
	}
	if err := os.Symlink(outside, filepath.Join(ownedDirectories[1], "outside-link")); err != nil {
		t.Fatal(err)
	}

	ambiguousDirectories := []string{
		filepath.Join(parent, "rc-stage-123456789"),
		filepath.Join(parent, ".rc-stage-987.retired"),
		filepath.Join(parent, ".rc-stage-42.retired-"+hexSuffix),
	}
	for _, path := range ambiguousDirectories {
		mustWriteTestFile(t, filepath.Join(path, "keep.txt"), []byte("ambiguous"), 0o600)
	}
	ambiguousDownload := filepath.Join(parent, "rc-dl-314159265")
	mustWriteTestFile(t, ambiguousDownload, []byte("ambiguous archive"), 0o600)
	unrelatedDirectories := []string{
		filepath.Join(parent, "rc-stage-not-digits"),
		filepath.Join(parent, "rc-dl-123.extra"),
		filepath.Join(parent, ".roundcube.retired-0123456789ABCDEF01234567"),
		filepath.Join(parent, "customer-data"),
	}
	for _, path := range unrelatedDirectories {
		mustWriteTestFile(t, filepath.Join(path, "keep.txt"), []byte("unrelated"), 0o600)
	}

	if err := reconcileRoundcubeArtifacts(final, ""); err != nil {
		t.Fatalf("reconcileRoundcubeArtifacts: %v", err)
	}
	for _, path := range ownedDirectories {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned crash artifact still exists at %s: %v", path, err)
		}
	}
	assertTestFileContent(t, filepath.Join(outside, "keep.txt"), []byte("keep"))
	for _, path := range ambiguousDirectories {
		assertTestFileContent(t, filepath.Join(path, "keep.txt"), []byte("ambiguous"))
	}
	assertTestFileContent(t, ambiguousDownload, []byte("ambiguous archive"))
	for _, path := range unrelatedDirectories {
		assertTestFileContent(t, filepath.Join(path, "keep.txt"), []byte("unrelated"))
	}
}

func TestDeterministicRoundcubeStageConvergesAfterCrash(t *testing.T) {
	parent := t.TempDir()
	final := filepath.Join(parent, "roundcube")
	stage, err := createRoundcubeInstallStage(final)
	if err != nil {
		t.Fatal(err)
	}
	wantStage := filepath.Join(parent, ".roundcube.stage")
	if stage != wantStage {
		t.Fatalf("stage path = %q, want %q", stage, wantStage)
	}
	mustWriteTestFile(t, filepath.Join(stage, "partial.txt"), []byte("partial"), 0o600)

	if err := reconcileRoundcubeArtifacts(final, ""); err != nil {
		t.Fatalf("retry reconciliation: %v", err)
	}
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("crashed deterministic stage remains: %v", err)
	}
	stage, err = createRoundcubeInstallStage(final)
	if err != nil {
		t.Fatalf("create stage after retry cleanup: %v", err)
	}
	if stage != wantStage {
		t.Fatalf("retry stage path = %q, want %q", stage, wantStage)
	}
}

func TestReconcileRoundcubeArtifactsPreservesIncompleteCanonicalTree(t *testing.T) {
	parent := t.TempDir()
	final := filepath.Join(parent, "roundcube")
	mustWriteTestFile(t, filepath.Join(final, "db", "roundcube.sqlite3"), []byte("customer data"), 0o600)
	deterministicStage := filepath.Join(parent, ".roundcube.stage")
	mustWriteTestFile(t, filepath.Join(deterministicStage, "partial.txt"), []byte("partial"), 0o600)
	ambiguousStage := filepath.Join(parent, "rc-stage-12345")
	mustWriteTestFile(t, filepath.Join(ambiguousStage, "keep.txt"), []byte("ambiguous"), 0o600)
	ambiguousDownload := filepath.Join(parent, "rc-dl-67890")
	mustWriteTestFile(t, ambiguousDownload, []byte("ambiguous"), 0o600)

	if err := reconcileRoundcubeArtifacts(final, ""); err != nil {
		t.Fatal(err)
	}
	assertTestFileContent(t, filepath.Join(final, "db", "roundcube.sqlite3"), []byte("customer data"))
	if _, err := os.Lstat(deterministicStage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deterministic stage still exists: %v", err)
	}
	assertTestFileContent(t, filepath.Join(ambiguousStage, "keep.txt"), []byte("ambiguous"))
	assertTestFileContent(t, ambiguousDownload, []byte("ambiguous"))
}

func TestReconcileRoundcubeArtifactsFailsClosedBeforeAnyDeletion(t *testing.T) {
	tests := []struct {
		name     string
		impostor func(*testing.T, string, string)
	}{
		{
			name: "retired symlink",
			impostor: func(t *testing.T, parent, outside string) {
				if err := os.Symlink(outside, filepath.Join(parent, ".roundcube.retired-0123456789abcdef01234567")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "deterministic stage regular file",
			impostor: func(t *testing.T, parent, _ string) {
				mustWriteTestFile(t, filepath.Join(parent, ".roundcube.stage"), []byte("not a directory"), 0o600)
			},
		},
		{
			name: "deterministic retirement regular file",
			impostor: func(t *testing.T, parent, _ string) {
				mustWriteTestFile(t, filepath.Join(parent, ".roundcube.retired"), []byte("not a directory"), 0o600)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			final := filepath.Join(parent, "roundcube")
			validArtifact := filepath.Join(parent, "..roundcube.stage.retired")
			mustWriteTestFile(t, filepath.Join(validArtifact, "keep.txt"), []byte("keep"), 0o600)
			outside := filepath.Join(parent, "outside")
			mustWriteTestFile(t, filepath.Join(outside, "keep.txt"), []byte("outside"), 0o600)
			test.impostor(t, parent, outside)

			if err := reconcileRoundcubeArtifacts(final, ""); err == nil {
				t.Fatal("matching impostor unexpectedly accepted")
			}
			assertTestFileContent(t, filepath.Join(validArtifact, "keep.txt"), []byte("keep"))
			assertTestFileContent(t, filepath.Join(outside, "keep.txt"), []byte("outside"))
		})
	}
}

func TestRetireRoundcubeTreeReconcilesBaseSpecificArtifactsWhenCanonicalIsMissing(t *testing.T) {
	parent := t.TempDir()
	final := filepath.Join(parent, "roundcube")
	hexSuffix := "0123456789abcdef01234567"
	artifacts := []string{
		filepath.Join(parent, ".roundcube.retired-"+hexSuffix),
		filepath.Join(parent, ".roundcube.stage"),
		filepath.Join(parent, "..roundcube.stage.retired"),
	}
	for _, path := range artifacts {
		mustWriteTestFile(t, filepath.Join(path, "marker"), []byte("stale"), 0o600)
	}
	ambiguousStage := filepath.Join(parent, "rc-stage-12345")
	mustWriteTestFile(t, filepath.Join(ambiguousStage, "keep"), []byte("ambiguous"), 0o600)
	ambiguousRetired := filepath.Join(parent, ".rc-stage-678.retired")
	mustWriteTestFile(t, filepath.Join(ambiguousRetired, "keep"), []byte("ambiguous"), 0o600)
	ambiguousDownload := filepath.Join(parent, "rc-dl-24680")
	mustWriteTestFile(t, ambiguousDownload, []byte("ambiguous"), 0o600)

	result, err := retireRoundcubeTree(final)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Removed || !result.MutationApplied {
		t.Fatalf("retirement result = %+v, want converged artifact mutation", result)
	}
	for _, path := range artifacts {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uninstall left base-specific artifact %s: %v", path, err)
		}
	}
	assertTestFileContent(t, filepath.Join(ambiguousStage, "keep"), []byte("ambiguous"))
	assertTestFileContent(t, filepath.Join(ambiguousRetired, "keep"), []byte("ambiguous"))
	assertTestFileContent(t, ambiguousDownload, []byte("ambiguous"))
}

func TestRetireRoundcubeTreeFailsClosedOnLegacyActiveRetiredConflict(t *testing.T) {
	parent := t.TempDir()
	final := filepath.Join(parent, "roundcube")
	legacyRetired := filepath.Join(parent, ".roundcube.retired-0123456789abcdef01234567")
	staleStage := filepath.Join(parent, "rc-stage-12345")
	mustWriteTestFile(t, filepath.Join(final, "db", "roundcube.sqlite3"), []byte("customer data"), 0o600)
	mustWriteTestFile(t, filepath.Join(legacyRetired, "old.txt"), []byte("old data"), 0o600)
	mustWriteTestFile(t, filepath.Join(staleStage, "partial.txt"), []byte("partial"), 0o600)

	result, err := retireRoundcubeTree(final)
	if err == nil {
		t.Fatal("legacy active/retired conflict unexpectedly accepted")
	}
	if result.Removed || result.MutationApplied {
		t.Fatalf("conflict result = %+v, want no mutation", result)
	}
	assertTestFileContent(t, filepath.Join(final, "db", "roundcube.sqlite3"), []byte("customer data"))
	assertTestFileContent(t, filepath.Join(legacyRetired, "old.txt"), []byte("old data"))
	assertTestFileContent(t, filepath.Join(staleStage, "partial.txt"), []byte("partial"))
}

func TestPublishRoundcubeStageReconcilesLegacyArtifacts(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, ".roundcube.stage")
	final := filepath.Join(parent, "roundcube")
	legacyRetired := filepath.Join(parent, ".roundcube.retired-0123456789abcdef01234567")
	deterministicStageRetired := filepath.Join(parent, "..roundcube.stage.retired")
	ambiguousStage := filepath.Join(parent, ".rc-stage-123.retired-0123456789abcdef01234567")
	ambiguousDownload := filepath.Join(parent, "rc-dl-13579")
	unrelated := filepath.Join(parent, "rc-stage-operator-notes")
	mustWriteTestFile(t, filepath.Join(stage, "ready.txt"), []byte("ready"), 0o600)
	mustWriteTestFile(t, filepath.Join(legacyRetired, "old.txt"), []byte("old"), 0o600)
	mustWriteTestFile(t, filepath.Join(deterministicStageRetired, "partial.txt"), []byte("partial"), 0o600)
	mustWriteTestFile(t, filepath.Join(ambiguousStage, "keep.txt"), []byte("ambiguous"), 0o600)
	mustWriteTestFile(t, ambiguousDownload, []byte("ambiguous"), 0o600)
	mustWriteTestFile(t, filepath.Join(unrelated, "keep.txt"), []byte("keep"), 0o600)

	if err := publishRoundcubeStage(stage, final); err != nil {
		t.Fatal(err)
	}
	assertTestFileContent(t, filepath.Join(final, "ready.txt"), []byte("ready"))
	for _, path := range []string{legacyRetired, deterministicStageRetired} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("publish left base-specific artifact %s: %v", path, err)
		}
	}
	assertTestFileContent(t, filepath.Join(ambiguousStage, "keep.txt"), []byte("ambiguous"))
	assertTestFileContent(t, ambiguousDownload, []byte("ambiguous"))
	assertTestFileContent(t, filepath.Join(unrelated, "keep.txt"), []byte("keep"))
}

func TestPublishRoundcubeStageRollsBackAfterParentSyncFailure(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "roundcube")
	retired := filepath.Join(parent, ".roundcube.retired")
	mustWriteTestFile(t, filepath.Join(stage, "marker.txt"), []byte("ready"), 0o600)

	originalSyncParent := roundcubeSyncParent
	injectedErr := errors.New("injected publish parent sync failure")
	syncCalls := 0
	roundcubeSyncParent = func(path string) error {
		syncCalls++
		if syncCalls == 1 {
			return injectedErr
		}
		return syncRoundcubeParent(path)
	}
	t.Cleanup(func() { roundcubeSyncParent = originalSyncParent })

	err := publishRoundcubeStage(stage, final)
	if err == nil || !strings.Contains(err.Error(), injectedErr.Error()) {
		t.Fatalf("publish error = %v, want injected sync failure", err)
	}
	if syncCalls != 3 {
		t.Fatalf("parent sync calls = %d, want publish plus two rollback syncs", syncCalls)
	}
	for _, path := range []string{stage, final, retired} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("publish rollback left %s: %v", path, statErr)
		}
	}
}

func TestPublishRoundcubeStageRetryReconcilesFailedRollbackArtifact(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "roundcube")
	retired := filepath.Join(parent, ".roundcube.retired")
	mustWriteTestFile(t, filepath.Join(stage, "first.txt"), []byte("first"), 0o600)

	originalSyncParent := roundcubeSyncParent
	injectedErr := errors.New("injected persistent parent sync failure")
	syncCalls := 0
	roundcubeSyncParent = func(path string) error {
		syncCalls++
		if syncCalls <= 2 {
			return injectedErr
		}
		return syncRoundcubeParent(path)
	}
	t.Cleanup(func() { roundcubeSyncParent = originalSyncParent })

	err := publishRoundcubeStage(stage, final)
	if err == nil || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("first publish error = %v, want failed rollback", err)
	}
	if info, statErr := os.Lstat(retired); statErr != nil || !info.IsDir() {
		t.Fatalf("failed rollback artifact missing: info=%v err=%v", info, statErr)
	}
	if _, statErr := os.Lstat(final); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed rollback left canonical tree: %v", statErr)
	}

	retryStage := filepath.Join(parent, "retry-stage")
	mustWriteTestFile(t, filepath.Join(retryStage, "retry.txt"), []byte("retry"), 0o600)
	if err := publishRoundcubeStage(retryStage, final); err != nil {
		t.Fatalf("publish retry: %v", err)
	}
	if syncCalls != 4 {
		t.Fatalf("parent sync calls = %d, want two failed rollback and two retry syncs", syncCalls)
	}
	if _, statErr := os.Lstat(retired); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("publish retry left retirement artifact: %v", statErr)
	}
	assertTestFileContent(t, filepath.Join(final, "retry.txt"), []byte("retry"))
}

func TestRemoveRoundcubeReportsAppliedMutationAfterRenameSyncFailure(t *testing.T) {
	originalBaseDir := webmailBaseDir
	originalSyncParent := roundcubeSyncParent
	webmailBaseDir = filepath.Join(t.TempDir(), "roundcube")
	t.Cleanup(func() {
		webmailBaseDir = originalBaseDir
		roundcubeSyncParent = originalSyncParent
	})
	mustWriteTestFile(t, filepath.Join(webmailBaseDir, "marker.txt"), []byte("present"), 0o600)

	injectedErr := errors.New("injected Roundcube parent sync failure")
	syncCalls := 0
	roundcubeSyncParent = func(path string) error {
		syncCalls++
		if syncCalls == 1 {
			return injectedErr
		}
		return syncRoundcubeParent(path)
	}

	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(t, manager, "service_uninstall", "roundcube", "")
	request := &WebmailMutationRequest{ServiceMutationBinding: ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	}}
	var response RemoveRoundcubeResponse
	if err := (&Agent{}).RemoveRoundcube(request, &response); err != nil {
		t.Fatalf("RemoveRoundcube RPC error: %v", err)
	}

	if !response.Removed || !response.MutationApplied {
		t.Fatalf("response = %+v, want removed applied mutation", response)
	}
	if !strings.Contains(response.Error, injectedErr.Error()) {
		t.Fatalf("response error = %q, want %q", response.Error, injectedErr)
	}
	if syncCalls != 1 {
		t.Fatalf("parent sync calls = %d, want 1", syncCalls)
	}
	if _, err := os.Lstat(webmailBaseDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final Roundcube path still exists after successful rename: %v", err)
	}
	retired := filepath.Join(filepath.Dir(webmailBaseDir), "."+filepath.Base(webmailBaseDir)+".retired")
	if info, err := os.Lstat(retired); err != nil || !info.IsDir() {
		t.Fatalf("retirement artifact missing after injected sync failure: info=%v err=%v", info, err)
	}

	var retry RemoveRoundcubeResponse
	if err := (&Agent{}).RemoveRoundcube(request, &retry); err != nil {
		t.Fatalf("RemoveRoundcube retry RPC error: %v", err)
	}
	if retry.Error != "" || !retry.Removed || !retry.MutationApplied {
		t.Fatalf("retry response = %+v, want converged applied removal", retry)
	}
	if syncCalls != 2 {
		t.Fatalf("parent sync calls after retry = %d, want 2", syncCalls)
	}
	if _, err := os.Lstat(retired); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retirement artifact still exists after retry: %v", err)
	}
}

func TestRoundcubeInstallStateRequiresCompleteTree(t *testing.T) {
	original := webmailBaseDir
	webmailBaseDir = filepath.Join(t.TempDir(), "roundcube")
	t.Cleanup(func() { webmailBaseDir = original })

	mustWriteTestFile(t, filepath.Join(webmailBaseDir, "public_html", "index.php"), []byte("index"), 0o600)
	if installed, err := roundcubeInstallState(); err != nil || installed {
		t.Fatalf("entry point alone reported complete: installed=%v err=%v", installed, err)
	}
	mustWriteTestFile(t, filepath.Join(webmailBaseDir, "config", "config.inc.php"), []byte("config"), 0o600)
	if installed, err := roundcubeInstallState(); err != nil || installed {
		t.Fatalf("tree without database reported complete: installed=%v err=%v", installed, err)
	}
	mustWriteTestFile(t, filepath.Join(webmailBaseDir, "db", "roundcube.sqlite3"), []byte("db"), 0o600)
	if installed, err := roundcubeInstallState(); err != nil || !installed {
		t.Fatalf("complete tree not reported installed: installed=%v err=%v", installed, err)
	}
}

func TestApplyWebmailNginxMutationRestoresOnValidationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webmail.conf")
	old := []byte("old configuration\n")
	mustWriteTestFile(t, path, old, 0o640)

	var calls []string
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return []byte("invalid configuration"), errors.New("exit status 1")
	}
	err := applyWebmailNginxMutation(context.Background(), path, []byte("new\n"), true, runner)
	if err == nil {
		t.Fatal("validation failure unexpectedly succeeded")
	}
	assertTestFileContent(t, path, old)
	assertTestFileMode(t, path, 0o640)
	if len(calls) != 1 || calls[0] != "nginx -t" {
		t.Fatalf("unexpected commands: %#v", calls)
	}
}

func TestApplyWebmailNginxMutationSecuresSuccessfulConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webmail.conf")
	mustWriteTestFile(t, path, []byte("old\n"), 0o644)
	if err := applyWebmailNginxMutation(
		context.Background(), path, []byte("new\n"), true,
		func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
	); err != nil {
		t.Fatal(err)
	}
	assertTestFileContent(t, path, []byte("new\n"))
	assertTestFileMode(t, path, 0o600)
}

func TestApplyWebmailNginxMutationMetadataFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webmail.conf")
	old := []byte("old\n")
	mustWriteTestFile(t, path, old, 0o640)
	previous := webmailSetConfigMetadata
	webmailSetConfigMetadata = func(string, os.FileMode, int, int) error {
		return errors.New("metadata refused")
	}
	t.Cleanup(func() { webmailSetConfigMetadata = previous })

	runnerCalls := 0
	err := applyWebmailNginxMutation(
		context.Background(), path, []byte("new\n"), true,
		func(context.Context, string, ...string) ([]byte, error) {
			runnerCalls++
			return nil, nil
		},
	)
	if err == nil {
		t.Fatal("metadata failure unexpectedly succeeded")
	}
	if runnerCalls != 0 {
		t.Fatalf("nginx commands ran before metadata was secured: %d", runnerCalls)
	}
	assertTestFileContent(t, path, old)
	assertTestFileMode(t, path, 0o640)
}

func TestRemoveInactiveWebmailSocketOnlyRemovesSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "webmail.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := removeInactiveWebmailSocketAt(socketPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inactive socket still exists: %v", err)
	}

	regular := filepath.Join(t.TempDir(), "not-a-socket.sock")
	mustWriteTestFile(t, regular, []byte("keep"), 0o600)
	if err := removeInactiveWebmailSocketAt(regular); err == nil {
		t.Fatal("regular file at socket path was removed")
	}
	assertTestFileContent(t, regular, []byte("keep"))
}

func TestApplyWebmailNginxMutationRestoresAndReloadsOnReloadFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webmail.conf")
	old := []byte("old configuration\n")
	mustWriteTestFile(t, path, old, 0o640)

	var calls []string
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, call)
		if len(calls) == 2 {
			return []byte("reload failed"), errors.New("exit status 1")
		}
		return nil, nil
	}
	err := applyWebmailNginxMutation(context.Background(), path, []byte("new\n"), true, runner)
	if err == nil {
		t.Fatal("reload failure unexpectedly succeeded")
	}
	assertTestFileContent(t, path, old)
	assertTestFileMode(t, path, 0o640)
	want := []string{
		"nginx -t",
		"systemctl reload nginx",
		"nginx -t",
		"systemctl reload nginx",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected commands:\n got: %#v\nwant: %#v", calls, want)
	}
}

func TestApplyWebmailNginxMutationRemoveReloadFailureRestoresConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webmail.conf")
	old := []byte("old configuration\n")
	mustWriteTestFile(t, path, old, 0o600)

	callCount := 0
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		callCount++
		if callCount == 2 {
			return []byte("reload failed"), errors.New("exit status 1")
		}
		return nil, nil
	}
	err := applyWebmailNginxMutation(context.Background(), path, nil, false, runner)
	if err == nil {
		t.Fatal("remove reload failure unexpectedly succeeded")
	}
	assertTestFileContent(t, path, old)
	assertTestFileMode(t, path, 0o600)
	if callCount != 4 {
		t.Fatalf("unexpected command count: got %d want 4", callCount)
	}
}

func TestApplyRoundcubePermissionsReturnsChownFailure(t *testing.T) {
	base := filepath.Join(t.TempDir(), "roundcube")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("permission denied"), errors.New("exit status 1")
	}
	if err := applyRoundcubePermissions(context.Background(), base, filepath.Join(base, "db.sqlite3"), "root", runner); err == nil {
		t.Fatal("chown failure unexpectedly succeeded")
	}
}

func TestApplyRoundcubePermissionsRefusesSymlinkBeforeChown(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	link := filepath.Join(parent, "roundcube")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	called := false
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	if err := applyRoundcubePermissions(context.Background(), link, filepath.Join(link, "db.sqlite3"), "root", runner); err == nil {
		t.Fatal("symlinked Roundcube tree unexpectedly accepted")
	}
	if called {
		t.Fatal("chown runner was called before symlink rejection")
	}
}

func TestApplyRoundcubePermissionsAppliesExactModes(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing Roundcube ownership requires root")
	}
	base := filepath.Join(t.TempDir(), "roundcube")
	for _, dir := range []string{"db", "temp", "logs", "config"} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	config := filepath.Join(base, "config", "config.inc.php")
	database := filepath.Join(base, "db", "roundcube.sqlite3")
	mustWriteTestFile(t, config, []byte("config"), 0o600)
	mustWriteTestFile(t, database, []byte("db"), 0o600)
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	if err := applyRoundcubePermissions(context.Background(), base, database, "root", runner); err != nil {
		t.Fatalf("applyRoundcubePermissions: %v", err)
	}
	assertTestFileMode(t, base, 0o750)
	for _, dir := range []string{"db", "temp", "logs"} {
		assertTestFileMode(t, filepath.Join(base, dir), 0o770)
	}
	assertTestFileMode(t, config, 0o640)
	assertTestFileMode(t, database, 0o660)
}

func mustWriteTestFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func assertTestFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch for %s: got %q want %q", path, got, want)
	}
}

func assertTestFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want.Perm() {
		t.Fatalf("mode mismatch for %s: got %04o want %04o", path, got, want.Perm())
	}
}
