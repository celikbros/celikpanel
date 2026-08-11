package manifestv2

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func TestUnauditedCatalogFilesystemFailsClosed(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux is the only audited catalog filesystem target")
	}
	path := filepath.Join(t.TempDir(), "catalog.db")
	if _, err := BuildCatalog(context.Background(), path, testCatalogDocument("release-key-1")); !errors.Is(
		err,
		ErrCatalogFilesystemSecurityUnavailable,
	) {
		t.Fatalf("BuildCatalog unaudited-platform error = %v", err)
	}
	if _, err := SignCatalog(
		path,
		strings.Repeat("0", 64),
		"release-key-1",
		make(ed25519.PrivateKey, ed25519.PrivateKeySize),
	); !errors.Is(err, ErrCatalogFilesystemSecurityUnavailable) {
		t.Fatalf("SignCatalog unaudited-platform error = %v", err)
	}
	_, err := OpenVerified(
		context.Background(),
		path,
		[]byte(`{}`),
		nil,
		OpenPolicy{AgentSchema: AgentSchemaVersion},
	)
	if !errors.Is(err, ErrCatalogFilesystemSecurityUnavailable) {
		t.Fatalf("OpenVerified unaudited-platform error = %v", err)
	}
}

func TestSelectorAwareRecipePaths(t *testing.T) {
	for _, test := range []struct {
		name     string
		osFamily string
		path     string
		wantOK   bool
	}{
		{name: "POSIX absolute", osFamily: "linux", path: "/etc/celikpanel/service.conf", wantOK: true},
		{name: "POSIX rejects Windows", osFamily: "linux", path: `C:\ProgramData\CelikPanel\service.conf`},
		{name: "unaudited POSIX family", osFamily: "freebsd", path: "/usr/local/etc/service.conf"},
		{name: "Windows recipe family rejected", osFamily: "windows", path: `C:\ProgramData\CelikPanel\service.conf`},
		{name: "Windows rejects POSIX", osFamily: "windows", path: "/etc/celikpanel/service.conf"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateRecipePath(PlatformSelector{OSFamily: test.osFamily}, test.path)
			if (err == nil) != test.wantOK {
				t.Fatalf("validateRecipePath(%q, %q) error = %v", test.osFamily, test.path, err)
			}
		})
	}
}

func TestSelectorRequiresAuditedOSFamily(t *testing.T) {
	for _, family := range []string{"", "freebsd", "darwin", "windows"} {
		if err := validateSelector("test.recipe", PlatformSelector{OSFamily: family}); err == nil {
			t.Fatalf("validateSelector accepted os_family %q", family)
		}
	}
	if err := validateSelector("test.recipe", PlatformSelector{OSFamily: "linux"}); err != nil {
		t.Fatalf("validateSelector rejected linux os_family: %v", err)
	}
}

func TestTCPProbeRequiresLoopbackIPAddress(t *testing.T) {
	for _, test := range []struct {
		host   string
		wantOK bool
	}{
		{host: "127.0.0.1", wantOK: true},
		{host: "::1", wantOK: true},
		{host: "localhost"},
		{host: "0.0.0.0"},
		{host: "192.0.2.1"},
		{host: ""},
	} {
		err := validateStep(
			"test.recipe",
			PlatformSelector{OSFamily: "linux"},
			RecipeStep{ID: "verify.tcp", Type: "tcp_probe", Host: test.host, Port: 443},
			stepSectionVerify,
		)
		if (err == nil) != test.wantOK {
			t.Fatalf("validateStep TCP host %q error = %v", test.host, err)
		}
	}
}

func TestStrictJSONRejectsDuplicateAndNonCanonicalFields(t *testing.T) {
	for _, data := range []string{
		`{"algorithm":"ed25519-sha256","algorithm":"ed25519-sha256","key_id":"key","digest":"` + strings.Repeat("a", 64) + `","signature":"x"}`,
		`{"Algorithm":"ed25519-sha256","key_id":"key","digest":"` + strings.Repeat("a", 64) + `","signature":"x"}`,
	} {
		var envelope SignatureEnvelope
		if err := strictJSON([]byte(data), &envelope); err == nil {
			t.Fatalf("strictJSON accepted %s", data)
		}
	}
	var envelope SignatureEnvelope
	uppercaseDigest := `{"algorithm":"ed25519-sha256","key_id":"key","digest":"` + strings.Repeat("A", 64) + `","signature":"x"}`
	if err := strictJSON([]byte(uppercaseDigest), &envelope); err != nil {
		t.Fatalf("strict signature shape: %v", err)
	}
	if digestPattern.MatchString(envelope.Digest) {
		t.Fatal("uppercase signature digest was accepted as canonical")
	}
	if err := strictJSONObjectExtension([]byte(`{"name":"one","name":"two"}`)); err == nil {
		t.Fatal("metadata extension accepted a duplicate key")
	}
}

func TestOpenVerifiedPinsDigestAtSameSequence(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	path, signature, publicKey := buildSignedTestCatalog(t, doc)
	digest, _, err := DigestCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	policy := OpenPolicy{
		AgentSchema:            AgentSchemaVersion,
		MinimumCatalogSequence: doc.Metadata.CatalogSequence,
		MinimumCatalogDigest:   digest,
	}
	catalog, err := OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{doc.Metadata.KeyID: publicKey},
		policy,
	)
	if err != nil {
		t.Fatalf("open pinned catalog: %v", err)
	}
	_ = catalog.Close()
	policy.MinimumCatalogDigest = strings.Repeat("0", 64)
	if _, err := OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{doc.Metadata.KeyID: publicKey},
		policy,
	); err == nil || !strings.Contains(err.Error(), "pinned digest") {
		t.Fatalf("same-sequence equivocation error = %v", err)
	}
}

func TestSignCatalogRequiresBuildDigestAndSizeLimit(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	path := filepath.Join(t.TempDir(), "catalog.db")
	digest, err := BuildCatalog(context.Background(), path, doc)
	if err != nil {
		t.Fatal(err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SignCatalog(path, strings.Repeat("0", 64), doc.Metadata.KeyID, privateKey); !errors.Is(
		err,
		ErrCatalogDigestChanged,
	) {
		t.Fatalf("mismatched build digest error = %v", err)
	}
	if _, err := SignCatalog(path, digest, doc.Metadata.KeyID, privateKey); err != nil {
		t.Fatalf("sign bound catalog: %v", err)
	}

	oversized := filepath.Join(t.TempDir(), "oversized.db")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxCatalogBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := SignCatalog(
		oversized,
		strings.Repeat("0", 64),
		doc.Metadata.KeyID,
		privateKey,
	); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized signing error = %v", err)
	}
}

func TestPublishCatalogCleansUpAfterDirectorySyncFailure(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	directoryPath := t.TempDir()
	sourcePath := filepath.Join(directoryPath, "source.db")
	if err := os.WriteFile(sourcePath, []byte("catalog"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	directory, err := os.Open(directoryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	destination := filepath.Join(directoryPath, "published.db")
	calls := 0
	syncFailure := errors.New("injected directory sync failure")
	err = publishCatalog(source, "published.db", destination, directory, func(*os.File) error {
		calls++
		if calls == 1 {
			return syncFailure
		}
		return nil
	})
	var publishErr *CatalogPublishError
	if !errors.As(err, &publishErr) || !errors.Is(err, ErrCatalogPublishDurability) {
		t.Fatalf("publish error = %T %v", err, err)
	}
	if publishErr.DestinationMayRemain || publishErr.CleanupError != nil {
		t.Fatalf("publish cleanup state = %+v", publishErr)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("published destination remains after cleanup: %v", statErr)
	}
}

func TestOpenVerifiedRejectsAmbiguousSignedJSONAndInvalidDomains(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	for _, test := range []struct {
		name   string
		update string
		want   string
	}{
		{
			name:   "duplicate metadata key",
			update: `UPDATE catalog_items SET metadata_json = '{"name":"one","name":"two"}' WHERE id = 'memcached'`,
			want:   "duplicate JSON key",
		},
		{
			name:   "invalid enabled domain",
			update: `UPDATE catalog_items SET enabled = 2 WHERE id = 'memcached'`,
			want:   "item domain",
		},
		{
			name:   "extra metadata singleton",
			update: `INSERT INTO catalog_meta SELECT 2, schema_version, catalog_version, catalog_sequence, minimum_agent_schema, key_id, created_at FROM catalog_meta`,
			want:   "exactly one row",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := testCatalogDocument("release-key-1")
			path := filepath.Join(t.TempDir(), "catalog.db")
			if _, err := BuildCatalog(context.Background(), path, doc); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=rw")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if _, err := db.Exec(test.update); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			signature, publicKey := signTestCatalog(t, path, doc.Metadata.KeyID)
			if _, err := OpenVerified(
				context.Background(),
				path,
				signature,
				map[string]ed25519.PublicKey{doc.Metadata.KeyID: publicKey},
				testOpenPolicy(),
			); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("OpenVerified error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStrictRecipeJSONRejectsDuplicateNestedStepKey(t *testing.T) {
	var spec RecipeSpec
	data := []byte(`{"steps":[{"id":"one","id":"two","type":"service_start","unit":"svc"}]}`)
	if err := strictJSON(data, &spec); err == nil || !strings.Contains(err.Error(), "duplicate JSON key") {
		t.Fatalf("duplicate nested step error = %v", err)
	}
}

func TestMetadataRemainsAnExtensionObject(t *testing.T) {
	item := CatalogItem{
		ID:       "extension",
		Kind:     ItemApplication,
		Revision: 1,
		Enabled:  true,
		Metadata: json.RawMessage(`{"VendorLabel":"kept","nested":{"AnyCase":true}}`),
	}
	if err := validateItem(item); err != nil {
		t.Fatalf("extension metadata: %v", err)
	}
}

func requireSecureCatalogTestFilesystem(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("catalog build/open filesystem activation is audited only on Linux")
	}
}

func TestResolveIsSafeDuringAndAfterClose(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	path, signature, publicKey := buildSignedTestCatalog(t, doc)
	catalog, err := OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{doc.Metadata.KeyID: publicKey},
		testOpenPolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}
	host := HostProfile{
		OSFamily:       "linux",
		DistroFamily:   "debian",
		DistroID:       "debian",
		Version:        "12",
		Architecture:   "amd64",
		PackageManager: "apt",
		ServiceManager: "systemd",
	}

	const workers = 16
	start := make(chan struct{})
	stop := make(chan struct{})
	ready := make(chan struct{}, workers)
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			ready <- struct{}{}
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = catalog.Resolve(context.Background(), "memcached", "install", host)
				}
			}
		}()
	}
	close(start)
	for i := 0; i < workers; i++ {
		<-ready
	}
	runtime.Gosched()
	if err := catalog.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(stop)
	wait.Wait()
	if _, err := catalog.Resolve(context.Background(), "memcached", "install", host); err == nil {
		t.Fatal("Resolve succeeded after Close")
	}
	if err := catalog.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
