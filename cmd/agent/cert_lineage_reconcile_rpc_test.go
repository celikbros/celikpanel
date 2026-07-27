package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type certificateCleanupTestHarness struct {
	isolated certbotStorage
	legacy   string
	calls    [][]string
	lookups  []string
}

func newCertificateCleanupTestHarness(t *testing.T) *certificateCleanupTestHarness {
	t.Helper()

	root := t.TempDir()
	harness := &certificateCleanupTestHarness{
		isolated: certbotStorage{
			ConfigDir: filepath.Join(root, "isolated", "config"),
			WorkDir:   filepath.Join(root, "isolated", "work"),
			LogsDir:   filepath.Join(root, "isolated", "logs"),
		},
		legacy: filepath.Join(root, "legacy", "config"),
	}
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", filepath.Join(root, "snapshots"))

	oldIsolatedStorage := certificateCleanupIsolatedStorage
	oldLegacyConfigDir := certificateCleanupLegacyConfigDir
	oldLookPath := certificateCleanupLookPath
	oldRunCertbot := certificateCleanupRunCertbot
	certificateCleanupIsolatedStorage = func() certbotStorage {
		return harness.isolated
	}
	certificateCleanupLegacyConfigDir = func() string {
		return harness.legacy
	}
	certificateCleanupLookPath = func(name string) (string, error) {
		harness.lookups = append(harness.lookups, name)
		return filepath.Join(root, "bin", name), nil
	}
	certificateCleanupRunCertbot = func(args ...string) ([]byte, error) {
		harness.calls = append(harness.calls, append([]string(nil), args...))
		return nil, nil
	}
	t.Cleanup(func() {
		certificateCleanupIsolatedStorage = oldIsolatedStorage
		certificateCleanupLegacyConfigDir = oldLegacyConfigDir
		certificateCleanupLookPath = oldLookPath
		certificateCleanupRunCertbot = oldRunCertbot
	})

	return harness
}

func writeCertificateCleanupRenewal(t *testing.T, configDir, lineage string) {
	t.Helper()

	renewalDir := filepath.Join(configDir, "renewal")
	if err := os.MkdirAll(renewalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(renewalDir, lineage+".conf"),
		[]byte("managed test renewal"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}

func expectedCertificateDeleteArgs(storage certbotStorage, lineage string) []string {
	args := []string{"delete"}
	args = append(args, storage.commandArgs()...)
	return append(args, "--cert-name", lineage, "--non-interactive")
}

func TestStagedSiteLineageNameContract(t *testing.T) {
	for _, lineage := range []string{
		"cp-site-1-0123456789abcdef01234567",
		"cp-site-987654321-ffffffffffffffffffffffff",
	} {
		if !validStagedSiteLineage.MatchString(lineage) {
			t.Errorf("valid staged lineage %q was rejected", lineage)
		}
	}

	for _, lineage := range []string{
		"example.test",
		"cp-site-0-0123456789abcdef01234567",
		"cp-site-01-0123456789abcdef01234567",
		"cp-site-1-0123456789abcdef0123456",
		"cp-site-1-0123456789abcdef012345678",
		"cp-site-1-0123456789abcdef0123456g",
		"cp-site-1-0123456789ABCDEF01234567",
		"cp-site-1-../../operator",
	} {
		if validStagedSiteLineage.MatchString(lineage) {
			t.Errorf("unsafe staged lineage %q was accepted", lineage)
		}
	}
}

func TestReconcileSiteCertLineagesKeepsEveryReferencedLineageIncludingRevokedAndDeletesOnlyOrphans(t *testing.T) {
	harness := newCertificateCleanupTestHarness(t)
	const (
		referencedRevoked = "cp-site-7-aaaaaaaaaaaaaaaaaaaaaaaa"
		isolatedOrphan    = "cp-site-8-bbbbbbbbbbbbbbbbbbbbbbbb"
		legacyOrphan      = "cp-site-9-cccccccccccccccccccccccc"
		globalCanonical   = "frankfurt.celikhost.com"
	)

	for _, lineage := range []string{
		referencedRevoked,
		isolatedOrphan,
		globalCanonical,
		"cp-site-0-dddddddddddddddddddddddd",
		"cp-site-10-EEEEEEEEEEEEEEEEEEEEEEEE",
	} {
		writeCertificateCleanupRenewal(t, harness.isolated.ConfigDir, lineage)
	}
	for _, lineage := range []string{
		referencedRevoked,
		legacyOrphan,
		globalCanonical,
	} {
		writeCertificateCleanupRenewal(t, harness.legacy, lineage)
	}

	var resp ReconcileSiteCertLineagesResponse
	err := (&Agent{}).ReconcileSiteCertLineages(
		&ReconcileSiteCertLineagesRequest{
			ReferencedLineages: []string{
				"  " + strings.ToUpper(referencedRevoked) + "  ",
			},
		},
		&resp,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" {
		t.Fatalf("reconcile failed: %s", resp.Error)
	}
	if resp.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2", resp.Deleted)
	}

	legacyStorage := harness.isolated
	legacyStorage.ConfigDir = harness.legacy
	wantCalls := [][]string{
		expectedCertificateDeleteArgs(harness.isolated, isolatedOrphan),
		expectedCertificateDeleteArgs(legacyStorage, legacyOrphan),
	}
	if !reflect.DeepEqual(harness.calls, wantCalls) {
		t.Fatalf("certbot calls = %#v, want %#v", harness.calls, wantCalls)
	}
	if !reflect.DeepEqual(harness.lookups, []string{"certbot", "certbot"}) {
		t.Fatalf("lookups = %v, want two certbot lookups", harness.lookups)
	}
	for _, call := range harness.calls {
		joined := strings.Join(call, " ")
		for _, forbidden := range []string{
			referencedRevoked,
			globalCanonical,
			"cp-site-0-",
			"cp-site-10-EEEE",
		} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("cleanup touched protected or invalid lineage %q in %v", forbidden, call)
			}
		}
	}
}

func TestReconcileSiteCertLineagesAcceptsLegacyRetainFieldDuringRollingUpgrade(t *testing.T) {
	harness := newCertificateCleanupTestHarness(t)
	const (
		referenced = "cp-site-51-aaaaaaaaaaaaaaaaaaaaaaaa"
		orphan     = "cp-site-52-bbbbbbbbbbbbbbbbbbbbbbbb"
	)
	writeCertificateCleanupRenewal(t, harness.isolated.ConfigDir, referenced)
	writeCertificateCleanupRenewal(t, harness.isolated.ConfigDir, orphan)

	var resp ReconcileSiteCertLineagesResponse
	if err := (&Agent{}).ReconcileSiteCertLineages(
		&ReconcileSiteCertLineagesRequest{
			ActiveLineages: []string{referenced},
		},
		&resp,
	); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" {
		t.Fatalf("reconcile failed: %s", resp.Error)
	}
	if resp.Deleted != 1 {
		t.Fatalf("deleted = %d, want one true orphan", resp.Deleted)
	}
	want := [][]string{
		expectedCertificateDeleteArgs(harness.isolated, orphan),
	}
	if !reflect.DeepEqual(harness.calls, want) {
		t.Fatalf("certbot calls = %#v, want %#v", harness.calls, want)
	}
}

func TestDeleteCertLineageUsesExactStorageAndNeverDeletesGlobalCanonicalLineage(t *testing.T) {
	harness := newCertificateCleanupTestHarness(t)
	const (
		domain       = "example.test"
		stagedBoth   = "cp-site-21-0123456789abcdef01234567"
		stagedLegacy = "cp-site-22-fedcba9876543210fedcba98"
		operator     = "boston.celikhost.com"
	)

	writeCertificateCleanupRenewal(t, harness.isolated.ConfigDir, domain)
	writeCertificateCleanupRenewal(t, harness.legacy, domain)
	writeCertificateCleanupRenewal(t, harness.legacy, operator)
	writeCertificateCleanupRenewal(t, harness.isolated.ConfigDir, stagedBoth)
	writeCertificateCleanupRenewal(t, harness.legacy, stagedBoth)
	writeCertificateCleanupRenewal(t, harness.legacy, stagedLegacy)

	var resp DeleteCertLineageResponse
	err := (&Agent{}).DeleteCertLineage(
		&DeleteCertLineageRequest{
			Domain:          domain,
			DeleteCanonical: true,
			LineageNames: []string{
				stagedBoth,
				"  " + strings.ToUpper(stagedBoth) + "  ",
				stagedLegacy,
			},
		},
		&resp,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" {
		t.Fatalf("delete failed: %s", resp.Error)
	}
	if !resp.Deleted {
		t.Fatal("delete did not report any deleted material")
	}

	legacyStorage := harness.isolated
	legacyStorage.ConfigDir = harness.legacy
	wantCalls := [][]string{
		expectedCertificateDeleteArgs(harness.isolated, domain),
		expectedCertificateDeleteArgs(harness.isolated, stagedBoth),
		expectedCertificateDeleteArgs(legacyStorage, stagedBoth),
		expectedCertificateDeleteArgs(legacyStorage, stagedLegacy),
	}
	if !reflect.DeepEqual(harness.calls, wantCalls) {
		t.Fatalf("certbot calls = %#v, want %#v", harness.calls, wantCalls)
	}
	for _, call := range harness.calls {
		if hasArgPair(call, "--config-dir", harness.legacy) &&
			(hasArgPair(call, "--cert-name", domain) || hasArgPair(call, "--cert-name", operator)) {
			t.Fatalf("global canonical/operator lineage was passed to certbot delete: %v", call)
		}
	}
}

func TestCertificateLineageCleanupRejectsInvalidAndOversizedRequestsBeforeCertbot(t *testing.T) {
	tests := []struct {
		name      string
		run       func(*Agent) string
		wantError string
	}{
		{
			name: "delete canonical lineage passed as staged",
			run: func(agent *Agent) string {
				var resp DeleteCertLineageResponse
				if err := agent.DeleteCertLineage(&DeleteCertLineageRequest{
					LineageNames: []string{"operator.example.test"},
				}, &resp); err != nil {
					t.Fatal(err)
				}
				return resp.Error
			},
			wantError: "invalid staged lineage name",
		},
		{
			name: "delete traversal lineage",
			run: func(agent *Agent) string {
				var resp DeleteCertLineageResponse
				if err := agent.DeleteCertLineage(&DeleteCertLineageRequest{
					LineageNames: []string{"cp-site-1-../../etc"},
				}, &resp); err != nil {
					t.Fatal(err)
				}
				return resp.Error
			},
			wantError: "invalid staged lineage name",
		},
		{
			name: "delete too many lineages",
			run: func(agent *Agent) string {
				var resp DeleteCertLineageResponse
				if err := agent.DeleteCertLineage(&DeleteCertLineageRequest{
					LineageNames: make([]string, 101),
				}, &resp); err != nil {
					t.Fatal(err)
				}
				return resp.Error
			},
			wantError: "too many staged lineages",
		},
		{
			name: "reconcile canonical referenced lineage",
			run: func(agent *Agent) string {
				var resp ReconcileSiteCertLineagesResponse
				if err := agent.ReconcileSiteCertLineages(&ReconcileSiteCertLineagesRequest{
					ReferencedLineages: []string{"operator.example.test"},
				}, &resp); err != nil {
					t.Fatal(err)
				}
				return resp.Error
			},
			wantError: "invalid referenced staged lineage name",
		},
		{
			name: "reconcile too many referenced lineages",
			run: func(agent *Agent) string {
				var resp ReconcileSiteCertLineagesResponse
				if err := agent.ReconcileSiteCertLineages(&ReconcileSiteCertLineagesRequest{
					ReferencedLineages: make([]string, 10001),
				}, &resp); err != nil {
					t.Fatal(err)
				}
				return resp.Error
			},
			wantError: "too many referenced lineages",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newCertificateCleanupTestHarness(t)
			if got := test.run(&Agent{}); got != test.wantError {
				t.Fatalf("error = %q, want %q", got, test.wantError)
			}
			if len(harness.lookups) != 0 || len(harness.calls) != 0 {
				t.Fatalf(
					"rejected request reached certbot: lookups=%v calls=%v",
					harness.lookups,
					harness.calls,
				)
			}
		})
	}
}

func TestDeleteCertLineageValidatesAllStagedNamesBeforeDeletingCanonicalLineage(t *testing.T) {
	harness := newCertificateCleanupTestHarness(t)
	const domain = "example.test"
	writeCertificateCleanupRenewal(t, harness.isolated.ConfigDir, domain)

	var resp DeleteCertLineageResponse
	if err := (&Agent{}).DeleteCertLineage(
		&DeleteCertLineageRequest{
			Domain:          domain,
			DeleteCanonical: true,
			LineageNames:    []string{"operator.example.test"},
		},
		&resp,
	); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "invalid staged lineage name" {
		t.Fatalf("error = %q, want invalid staged lineage rejection", resp.Error)
	}
	if resp.Deleted {
		t.Fatal("rejected request reported canonical lineage deletion")
	}
	if len(harness.lookups) != 0 || len(harness.calls) != 0 {
		t.Fatalf(
			"invalid staged lineage caused canonical certbot deletion: lookups=%v calls=%v",
			harness.lookups,
			harness.calls,
		)
	}
}

func TestDeleteCertLineageDeletesOnlyVerifiedExactSnapshot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("exact managed snapshot deletion requires Linux openat2")
	}
	harness := newCertificateCleanupTestHarness(t)
	const domain = "example.test"
	certDir, err := ensureManagedCertificateDirectory(domain)
	if err != nil {
		t.Fatal(err)
	}
	retainedContent := newCertificateVersionContent(
		"retained certificate", "retained private key", "retained chain",
	)
	retained, err := publishCertificateVersion(
		certDir, retainedContent, writeCertificateFile,
	)
	if err != nil {
		t.Fatal(err)
	}
	uncommittedContent := newCertificateVersionContent(
		"uncommitted certificate", "uncommitted private key", "uncommitted chain",
	)
	uncommitted, err := publishCertificateVersion(
		certDir, uncommittedContent, writeCertificateFile,
	)
	if err != nil {
		t.Fatal(err)
	}

	var resp DeleteCertLineageResponse
	if err := (&Agent{}).DeleteCertLineage(
		&DeleteCertLineageRequest{
			Domain:       domain,
			SnapshotPath: uncommitted.Fullchain,
		},
		&resp,
	); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" || !resp.Deleted {
		t.Fatalf("exact snapshot cleanup failed: %+v", resp)
	}
	if _, err := os.Stat(filepath.Dir(uncommitted.Fullchain)); !os.IsNotExist(err) {
		t.Fatalf("uncommitted exact version still exists: %v", err)
	}
	if _, err := os.Stat(retained.Fullchain); err != nil {
		t.Fatalf("retained immutable version was removed: %v", err)
	}
	if _, err := os.Stat(certDir); err != nil {
		t.Fatalf("domain snapshot root was removed: %v", err)
	}
	if len(harness.calls) != 0 {
		t.Fatalf("snapshot-only cleanup reached certbot: %v", harness.calls)
	}
}

func TestDeleteCertLineageStillDeletesExactSnapshotWhenCertbotCleanupFails(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("exact managed snapshot deletion requires Linux openat2")
	}
	harness := newCertificateCleanupTestHarness(t)
	const (
		domain  = "example.test"
		lineage = "cp-site-41-0123456789abcdef01234567"
	)
	writeCertificateCleanupRenewal(t, harness.isolated.ConfigDir, lineage)
	certificateCleanupRunCertbot = func(args ...string) ([]byte, error) {
		harness.calls = append(harness.calls, append([]string(nil), args...))
		return []byte("Error: simulated certbot delete failure"), fmt.Errorf("exit status 1")
	}

	certDir, err := ensureManagedCertificateDirectory(domain)
	if err != nil {
		t.Fatal(err)
	}
	uncommitted, err := publishCertificateVersion(
		certDir,
		newCertificateVersionContent("uncommitted cert", "uncommitted key", ""),
		writeCertificateFile,
	)
	if err != nil {
		t.Fatal(err)
	}

	var resp DeleteCertLineageResponse
	if err := (&Agent{}).DeleteCertLineage(
		&DeleteCertLineageRequest{
			Domain:       domain,
			LineageNames: []string{lineage},
			SnapshotPath: uncommitted.Fullchain,
		},
		&resp,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "simulated certbot delete failure") {
		t.Fatalf("cleanup error = %q, want certbot detail", resp.Error)
	}
	if !resp.Deleted {
		t.Fatal("exact snapshot deletion was not reported after lineage failure")
	}
	if _, err := os.Stat(filepath.Dir(uncommitted.Fullchain)); !os.IsNotExist(err) {
		t.Fatalf("exact snapshot survived certbot failure: %v", err)
	}
	if len(harness.calls) != 1 {
		t.Fatalf("certbot calls = %d, want 1", len(harness.calls))
	}
}

func TestDeleteCertLineageRejectsTamperedEscapingAndSymlinkedSnapshots(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("managed snapshot deletion hardening requires Linux")
	}
	t.Run("tampered fingerprint", func(t *testing.T) {
		harness := newCertificateCleanupTestHarness(t)
		const domain = "example.test"
		certDir, err := ensureManagedCertificateDirectory(domain)
		if err != nil {
			t.Fatal(err)
		}
		paths, err := publishCertificateVersion(
			certDir,
			newCertificateVersionContent("certificate", "private key", "chain"),
			writeCertificateFile,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(filepath.Dir(paths.Fullchain), "cert.pem"),
			[]byte("tampered certificate\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		var resp DeleteCertLineageResponse
		if err := (&Agent{}).DeleteCertLineage(
			&DeleteCertLineageRequest{
				Domain:       domain,
				SnapshotPath: paths.Fullchain,
			},
			&resp,
		); err != nil {
			t.Fatal(err)
		}
		if resp.Error == "" || resp.Deleted {
			t.Fatalf("tampered snapshot was not refused: %+v", resp)
		}
		if _, err := os.Stat(filepath.Dir(paths.Fullchain)); err != nil {
			t.Fatalf("refused snapshot was modified: %v", err)
		}
		if len(harness.calls) != 0 {
			t.Fatalf("refused snapshot reached certbot: %v", harness.calls)
		}
	})

	t.Run("path escape", func(t *testing.T) {
		harness := newCertificateCleanupTestHarness(t)
		outside := filepath.Join(t.TempDir(), "fullchain.pem")
		if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
			t.Fatal(err)
		}
		var resp DeleteCertLineageResponse
		if err := (&Agent{}).DeleteCertLineage(
			&DeleteCertLineageRequest{
				Domain:       "example.test",
				SnapshotPath: outside,
			},
			&resp,
		); err != nil {
			t.Fatal(err)
		}
		if resp.Error == "" || resp.Deleted {
			t.Fatalf("escaping snapshot path was not refused: %+v", resp)
		}
		if _, err := os.Stat(outside); err != nil {
			t.Fatalf("outside path was modified: %v", err)
		}
		if len(harness.calls) != 0 {
			t.Fatalf("escaping snapshot reached certbot: %v", harness.calls)
		}
	})

	t.Run("symlinked managed root", func(t *testing.T) {
		harness := newCertificateCleanupTestHarness(t)
		const domain = "example.test"
		realRoot := t.TempDir()
		t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", realRoot)
		certDir, err := ensureManagedCertificateDirectory(domain)
		if err != nil {
			t.Fatal(err)
		}
		paths, err := publishCertificateVersion(
			certDir,
			newCertificateVersionContent("certificate", "private key", "chain"),
			writeCertificateFile,
		)
		if err != nil {
			t.Fatal(err)
		}
		linkRoot := filepath.Join(t.TempDir(), "managed-link")
		if err := os.Symlink(realRoot, linkRoot); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", linkRoot)
		linkPath := filepath.Join(
			linkRoot, domain, filepath.Base(filepath.Dir(paths.Fullchain)),
			"fullchain.pem",
		)
		var resp DeleteCertLineageResponse
		if err := (&Agent{}).DeleteCertLineage(
			&DeleteCertLineageRequest{
				Domain:       domain,
				SnapshotPath: linkPath,
			},
			&resp,
		); err != nil {
			t.Fatal(err)
		}
		if resp.Error == "" || resp.Deleted {
			t.Fatalf("symlinked managed root was not refused: %+v", resp)
		}
		if _, err := os.Stat(paths.Fullchain); err != nil {
			t.Fatalf("snapshot behind refused symlink root was modified: %v", err)
		}
		if len(harness.calls) != 0 {
			t.Fatalf("symlinked snapshot reached certbot: %v", harness.calls)
		}
	})
}

func TestCertificateLineageCleanupCertbotFailureStopsAndReportsError(t *testing.T) {
	harness := newCertificateCleanupTestHarness(t)
	const orphan = "cp-site-31-aaaaaaaaaaaaaaaaaaaaaaaa"
	writeCertificateCleanupRenewal(t, harness.isolated.ConfigDir, orphan)
	certificateCleanupRunCertbot = func(args ...string) ([]byte, error) {
		harness.calls = append(harness.calls, append([]string(nil), args...))
		return []byte("Error: simulated certbot delete failure"), fmt.Errorf("exit status 1")
	}

	var resp ReconcileSiteCertLineagesResponse
	if err := (&Agent{}).ReconcileSiteCertLineages(
		&ReconcileSiteCertLineagesRequest{},
		&resp,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "simulated certbot delete failure") {
		t.Fatalf("error = %q, want certbot detail", resp.Error)
	}
	if resp.Deleted != 0 || len(harness.calls) != 1 {
		t.Fatalf("failed deletion state = deleted:%d calls:%d", resp.Deleted, len(harness.calls))
	}
}
