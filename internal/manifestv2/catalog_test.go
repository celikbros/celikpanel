package manifestv2

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildSignOpenAndResolveJSONRecipe(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	path, signature, publicKey := buildSignedTestCatalog(t, doc)

	catalog, err := OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{"release-key-1": publicKey},
		testOpenPolicy(),
	)
	if err != nil {
		t.Fatalf("OpenVerified: %v", err)
	}
	defer catalog.Close()

	resolved, err := catalog.Resolve(context.Background(), "memcached", "install", HostProfile{
		OSFamily:       "linux",
		DistroFamily:   "debian",
		DistroID:       "debian",
		DistroLike:     []string{"debian"},
		Version:        "12",
		Architecture:   "amd64",
		PackageManager: "apt",
		ServiceManager: "systemd",
	})
	if err != nil {
		t.Fatalf("Resolve Debian: %v", err)
	}
	if resolved.Recipe.ID != "memcached.debian12.install" {
		t.Fatalf("recipe = %q, want exact Debian recipe", resolved.Recipe.ID)
	}
	if resolved.Specificity != 730 {
		t.Fatalf("specificity = %d, want 730", resolved.Specificity)
	}
	if resolved.Digest == "" || resolved.Digest != catalog.Digest() {
		t.Fatalf("resolved digest = %q, catalog digest = %q", resolved.Digest, catalog.Digest())
	}
	if got := resolved.Recipe.Spec.Steps[0].Packages; len(got) != 1 || got[0] != "memcached" {
		t.Fatalf("packages = %#v", got)
	}

	unsupported, err := catalog.Resolve(context.Background(), "memcached", "install", HostProfile{
		OSFamily:       "linux",
		DistroFamily:   "debian",
		DistroID:       "ubuntu",
		Version:        "24.04",
		Architecture:   "amd64",
		PackageManager: "apt",
		ServiceManager: "systemd",
	})
	if err != nil {
		t.Fatalf("Resolve Ubuntu: %v", err)
	}
	if unsupported.Recipe.Support != SupportUnsupported {
		t.Fatalf("Ubuntu support = %q, want unsupported", unsupported.Recipe.Support)
	}

	if _, err := catalog.db.Exec(`DELETE FROM catalog_items`); err == nil {
		t.Fatal("verified immutable catalog unexpectedly accepted a write")
	}
}

func TestOpenVerifiedRejectsTamperedDatabase(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	path, signature, publicKey := buildSignedTestCatalog(t, doc)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("tampered")); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{"release-key-1": publicKey},
		testOpenPolicy(),
	)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered catalog error = %v", err)
	}
}

func TestBuildRejectsInvalidStepShapes(t *testing.T) {
	t.Run("generic exec types", func(t *testing.T) {
		for _, stepType := range []string{"exec", "exec_probe"} {
			t.Run(stepType, func(t *testing.T) {
				doc := testCatalogDocument("release-key-1")
				doc.Recipes[0].Spec.Steps = []RecipeStep{{
					ID:   "run.generic",
					Type: stepType,
				}}
				_, err := validateOrBuildTestCatalog(t, doc)
				if err == nil || !strings.Contains(err.Error(), "invalid type") {
					t.Fatalf("BuildCatalog generic type error = %v", err)
				}
			})
		}
	})

	t.Run("irrelevant known field", func(t *testing.T) {
		doc := testCatalogDocument("release-key-1")
		doc.Recipes[0].Spec.Steps[0].Unit = "unrelated.service"
		path, err := validateOrBuildTestCatalog(t, doc)
		if err == nil || !strings.Contains(err.Error(), `field "unit" is not allowed`) {
			t.Fatalf("BuildCatalog step shape error = %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("rejected catalog left output behind: %v", err)
		}
	})

	t.Run("verify rollback reference", func(t *testing.T) {
		doc := testCatalogDocument("release-key-1")
		doc.Recipes[0].Spec.Verify[0].RollbackStepID = "remove.package"
		_, err := validateOrBuildTestCatalog(t, doc)
		if err == nil || !strings.Contains(err.Error(), "rollback only from the main steps list") {
			t.Fatalf("BuildCatalog verify rollback error = %v", err)
		}
	})

	t.Run("rollback rollback reference", func(t *testing.T) {
		doc := testCatalogDocument("release-key-1")
		doc.Recipes[0].Spec.Rollback[0].RollbackStepID = "remove.package"
		_, err := validateOrBuildTestCatalog(t, doc)
		if err == nil || !strings.Contains(err.Error(), "rollback only from the main steps list") {
			t.Fatalf("BuildCatalog rollback reference error = %v", err)
		}
	})
}

func TestBuildRejectsInvalidCreationTimestamp(t *testing.T) {
	doc := testCatalogDocument("release-key-1")
	doc.Metadata.CreatedAt = "not-a-timestamp"

	_, err := validateOrBuildTestCatalog(t, doc)
	if err == nil || !strings.Contains(err.Error(), "created_at must be RFC3339") {
		t.Fatalf("invalid created_at error = %v", err)
	}
}

func TestBuildRejectsNonPositiveCatalogSequence(t *testing.T) {
	doc := testCatalogDocument("release-key-1")
	doc.Metadata.CatalogSequence = 0
	_, err := validateOrBuildTestCatalog(t, doc)
	if err == nil || !strings.Contains(err.Error(), "catalog sequence must be positive") {
		t.Fatalf("invalid catalog sequence error = %v", err)
	}
}

func TestOpenVerifiedRejectsReplayAndInvalidPolicy(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	path, signature, publicKey := buildSignedTestCatalog(t, doc)
	keys := map[string]ed25519.PublicKey{"release-key-1": publicKey}

	_, err := OpenVerified(
		context.Background(),
		path,
		signature,
		keys,
		OpenPolicy{
			AgentSchema:            AgentSchemaVersion,
			MinimumCatalogSequence: doc.Metadata.CatalogSequence + 1,
			MinimumCatalogDigest:   strings.Repeat("0", 64),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "older than required sequence") {
		t.Fatalf("replay error = %v", err)
	}

	_, err = OpenVerified(
		context.Background(),
		path,
		signature,
		keys,
		OpenPolicy{AgentSchema: AgentSchemaVersion, MinimumCatalogSequence: 1},
	)
	if err == nil || !strings.Contains(err.Error(), "must pin the digest") {
		t.Fatalf("invalid policy error = %v", err)
	}

	_, err = OpenVerified(
		context.Background(),
		path,
		signature,
		keys,
		OpenPolicy{},
	)
	if err == nil || !strings.Contains(err.Error(), "agent schema must be positive") {
		t.Fatalf("invalid agent policy error = %v", err)
	}
}

func TestOpenVerifiedUsesPrivateSnapshot(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	names := []string{"components#signed.db"}
	if runtime.GOOS != "windows" {
		names = append(names, "components?signed.db")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			doc := testCatalogDocument("release-key-1")
			path := filepath.Join(t.TempDir(), name)
			signature, publicKey := buildSignedTestCatalogAt(t, path, doc)
			catalog, err := OpenVerified(
				context.Background(),
				path,
				signature,
				map[string]ed25519.PublicKey{"release-key-1": publicKey},
				testOpenPolicy(),
			)
			if err != nil {
				t.Fatalf("OpenVerified: %v", err)
			}
			snapshotDir := catalog.snapshotDir
			if snapshotDir == "" || filepath.Clean(snapshotDir) == filepath.Clean(filepath.Dir(path)) {
				t.Fatalf("snapshot directory = %q", snapshotDir)
			}
			if runtime.GOOS != "windows" {
				directoryInfo, err := os.Stat(snapshotDir)
				if err != nil {
					t.Fatal(err)
				}
				if got := directoryInfo.Mode().Perm(); got != 0o700 {
					t.Fatalf("snapshot directory mode = %o, want 700", got)
				}
				snapshotInfo, err := os.Stat(filepath.Join(snapshotDir, "catalog.db"))
				if err != nil {
					t.Fatal(err)
				}
				if got := snapshotInfo.Mode().Perm(); got != 0o600 {
					t.Fatalf("snapshot file mode = %o, want 600", got)
				}
			}

			if err := os.Remove(path); err != nil {
				t.Fatalf("remove source after open: %v", err)
			}
			if err := os.WriteFile(path, []byte("replaced source"), 0o600); err != nil {
				t.Fatalf("replace source after open: %v", err)
			}
			if _, err := catalog.Resolve(
				context.Background(),
				"memcached",
				"install",
				HostProfile{
					OSFamily:       "linux",
					DistroFamily:   "debian",
					DistroID:       "debian",
					Version:        "12",
					Architecture:   "amd64",
					PackageManager: "apt",
					ServiceManager: "systemd",
				},
			); err != nil {
				t.Fatalf("Resolve after source replacement: %v", err)
			}
			if err := catalog.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
				t.Fatalf("snapshot directory survived Close: %v", err)
			}
		})
	}
}

func TestOpenVerifiedRejectsOversizedCatalog(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	path := filepath.Join(t.TempDir(), "oversized.db")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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
	_, err = OpenVerified(
		context.Background(),
		path,
		nil,
		nil,
		testOpenPolicy(),
	)
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized catalog error = %v", err)
	}
}

func TestOpenVerifiedRejectsOversizedSignature(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	_, err := OpenVerified(
		context.Background(),
		filepath.Join(t.TempDir(), "missing.db"),
		make([]byte, maxSignatureBytes+1),
		nil,
		testOpenPolicy(),
	)
	if err == nil || !strings.Contains(err.Error(), "signature exceeds") {
		t.Fatalf("oversized signature error = %v", err)
	}
}

func TestBuildCatalogNeverOverwritesExistingTarget(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	path := filepath.Join(t.TempDir(), "catalog.db")
	sentinel := []byte("existing target must survive")
	if err := os.WriteFile(path, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := BuildCatalog(context.Background(), path, testCatalogDocument("release-key-1"))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("BuildCatalog existing target error = %v", err)
	}
	actual, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(actual) != string(sentinel) {
		t.Fatalf("existing target changed to %q", actual)
	}
}

func TestOpenVerifiedRejectsExtraSignedSchemaObject(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	path := filepath.Join(t.TempDir(), "catalog.db")
	if _, err := BuildCatalog(context.Background(), path, doc); err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	dsn, err := sqliteCatalogURI(path, url.Values{"mode": {"rw"}})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE signed_but_unexpected (value TEXT)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	signature, publicKey := signTestCatalog(t, path, doc.Metadata.KeyID)
	_, err = OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{"release-key-1": publicKey},
		testOpenPolicy(),
	)
	if err == nil || !strings.Contains(err.Error(), "schema objects, expected") {
		t.Fatalf("extra schema object error = %v", err)
	}
}

func TestOpenVerifiedRejectsSignedStructuralSchemaChanges(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	tests := []struct {
		name      string
		statement string
		wantError string
	}{
		{
			name: "column default",
			statement: `UPDATE sqlite_master
			               SET sql = replace(
			                   sql,
			                   'created_at TEXT NOT NULL',
			                   'created_at TEXT NOT NULL DEFAULT ''unexpected'''
			               )
			             WHERE type = 'table' AND name = 'catalog_meta'`,
			wantError: "does not exactly match",
		},
		{
			name: "foreign key action",
			statement: `UPDATE sqlite_master
			               SET sql = replace(sql, 'ON DELETE CASCADE', 'ON DELETE RESTRICT')
			             WHERE type = 'table' AND name = 'catalog_recipes'`,
			wantError: "does not exactly match",
		},
		{
			name: "partial index",
			statement: `UPDATE sqlite_master
			               SET sql = sql || ' WHERE 1'
			             WHERE type = 'index' AND name = 'idx_catalog_recipe_lookup'`,
			wantError: "does not exactly match",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := testCatalogDocument("release-key-1")
			path := filepath.Join(t.TempDir(), "catalog.db")
			if _, err := BuildCatalog(context.Background(), path, doc); err != nil {
				t.Fatalf("BuildCatalog: %v", err)
			}
			dsn, err := sqliteCatalogURI(path, url.Values{"mode": {"rw"}})
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", dsn)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`PRAGMA writable_schema = ON`); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if _, err := db.Exec(test.statement); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if _, err := db.Exec(`PRAGMA schema_version = 999`); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			signature, publicKey := signTestCatalog(t, path, doc.Metadata.KeyID)
			_, err = OpenVerified(
				context.Background(),
				path,
				signature,
				map[string]ed25519.PublicKey{"release-key-1": publicKey},
				testOpenPolicy(),
			)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("structural schema error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestOpenVerifiedRejectsRemovedGenericExecFields(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	path := filepath.Join(t.TempDir(), "catalog.db")
	if _, err := BuildCatalog(context.Background(), path, doc); err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	dsn, err := sqliteCatalogURI(path, url.Values{"mode": {"rw"}})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		`UPDATE catalog_recipes
		    SET recipe_json =
		        '{"steps":[{"id":"run.program","type":"exec","program":"/usr/bin/python3","args":["-c","print(1)"]}]}'
		  WHERE id = 'memcached.debian12.install'`,
	)
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	signature, publicKey := signTestCatalog(t, path, doc.Metadata.KeyID)
	_, err = OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{"release-key-1": publicKey},
		testOpenPolicy(),
	)
	if err == nil || !strings.Contains(err.Error(), "unknown or non-canonical JSON field") {
		t.Fatalf("removed generic exec field error = %v", err)
	}
}

func TestOpenVerifiedRejectsExplicitEmptyIrrelevantStepField(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	path := filepath.Join(t.TempDir(), "catalog.db")
	if _, err := BuildCatalog(context.Background(), path, doc); err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	dsn, err := sqliteCatalogURI(path, url.Values{"mode": {"rw"}})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		`UPDATE catalog_recipes
		    SET recipe_json =
		        '{"steps":[{"id":"install.package","type":"package_install","packages":["memcached"],"unit":""}]}'
		  WHERE id = 'memcached.debian12.install'`,
	)
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	signature, publicKey := signTestCatalog(t, path, doc.Metadata.KeyID)
	_, err = OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{"release-key-1": publicKey},
		testOpenPolicy(),
	)
	if err == nil || !strings.Contains(err.Error(), `field "unit" is not allowed`) {
		t.Fatalf("explicit empty irrelevant field error = %v", err)
	}
}

func TestResolveRejectsEqualSpecificity(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	duplicate := doc.Recipes[0]
	duplicate.ID = "memcached.debian12.alternate"
	duplicate.PlatformKey = "debian12-alternate"
	doc.Recipes = append(doc.Recipes, duplicate)
	path, signature, publicKey := buildSignedTestCatalog(t, doc)
	catalog, err := OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{"release-key-1": publicKey},
		testOpenPolicy(),
	)
	if err != nil {
		t.Fatalf("OpenVerified: %v", err)
	}
	defer catalog.Close()

	_, err = catalog.Resolve(context.Background(), "memcached", "install", HostProfile{
		OSFamily:       "linux",
		DistroFamily:   "debian",
		DistroID:       "debian",
		Version:        "12",
		Architecture:   "amd64",
		PackageManager: "apt",
		ServiceManager: "systemd",
	})
	if !errors.Is(err, ErrRecipeAmbiguous) {
		t.Fatalf("Resolve ambiguity error = %v", err)
	}
}

func TestOpenVerifiedRejectsNewerAgentSchemaAndUnknownJSON(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	t.Run("agent schema", func(t *testing.T) {
		doc := testCatalogDocument("release-key-1")
		doc.Metadata.MinimumAgentSchema = AgentSchemaVersion + 1
		path, signature, publicKey := buildSignedTestCatalog(t, doc)
		_, err := OpenVerified(
			context.Background(),
			path,
			signature,
			map[string]ed25519.PublicKey{"release-key-1": publicKey},
			testOpenPolicy(),
		)
		if err == nil || !strings.Contains(err.Error(), "requires agent schema") {
			t.Fatalf("agent schema error = %v", err)
		}
	})

	t.Run("unknown selector field", func(t *testing.T) {
		doc := testCatalogDocument("release-key-1")
		path := filepath.Join(t.TempDir(), "catalog.db")
		if _, err := BuildCatalog(context.Background(), path, doc); err != nil {
			t.Fatalf("BuildCatalog: %v", err)
		}
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=rw")
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(
			`UPDATE catalog_recipes SET selector_json =
             '{"distro_id":"debian","unknown_selector_field":true}'
             WHERE id = 'memcached.debian12.install'`,
		)
		if closeErr := db.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatalf("mutate test catalog: %v", err)
		}
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		digest, _, err := DigestCatalog(path)
		if err != nil {
			t.Fatal(err)
		}
		signature, err := SignCatalog(path, digest, "release-key-1", privateKey)
		if err != nil {
			t.Fatal(err)
		}
		_, err = OpenVerified(
			context.Background(),
			path,
			signature,
			map[string]ed25519.PublicKey{"release-key-1": publicKey},
			testOpenPolicy(),
		)
		if err == nil || !strings.Contains(err.Error(), "unknown or non-canonical JSON field") {
			t.Fatalf("unknown JSON field error = %v", err)
		}
	})
}

func TestVersionConstraintMatches(t *testing.T) {
	for _, test := range []struct {
		version    string
		constraint string
		want       bool
	}{
		{version: "12", constraint: ">=12,<13", want: true},
		{version: "12.4", constraint: ">=12,<13", want: true},
		{version: "13", constraint: ">=12,<13", want: false},
		{version: "24.04", constraint: "24.04", want: true},
		{version: "24.10", constraint: "24.04", want: false},
	} {
		got, err := versionConstraintMatches(test.version, test.constraint)
		if err != nil {
			t.Fatalf("%s against %s: %v", test.version, test.constraint, err)
		}
		if got != test.want {
			t.Fatalf("%s against %s = %v, want %v", test.version, test.constraint, got, test.want)
		}
	}
}

func buildSignedTestCatalog(
	t *testing.T,
	doc CatalogDocument,
) (path string, signature []byte, publicKey ed25519.PublicKey) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "components-v2.db")
	signature, publicKey = buildSignedTestCatalogAt(t, path, doc)
	return path, signature, publicKey
}

func buildSignedTestCatalogAt(
	t *testing.T,
	path string,
	doc CatalogDocument,
) (signature []byte, publicKey ed25519.PublicKey) {
	t.Helper()
	digest, err := BuildCatalog(context.Background(), path, doc)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	return signTestCatalogWithDigest(t, path, digest, doc.Metadata.KeyID)
}

func signTestCatalog(
	t *testing.T,
	path string,
	keyID string,
) (signature []byte, publicKey ed25519.PublicKey) {
	t.Helper()
	digest, _, err := DigestCatalog(path)
	if err != nil {
		t.Fatalf("DigestCatalog: %v", err)
	}
	return signTestCatalogWithDigest(t, path, digest, keyID)
}

func signTestCatalogWithDigest(
	t *testing.T,
	path string,
	digest string,
	keyID string,
) (signature []byte, publicKey ed25519.PublicKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature, err = SignCatalog(path, digest, keyID, privateKey)
	if err != nil {
		t.Fatalf("SignCatalog: %v", err)
	}
	return signature, publicKey
}

func validateOrBuildTestCatalog(t *testing.T, doc CatalogDocument) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.db")
	if runtime.GOOS != "linux" {
		return path, validateDocument(doc)
	}
	_, err := BuildCatalog(context.Background(), path, doc)
	return path, err
}

func testCatalogDocument(keyID string) CatalogDocument {
	return CatalogDocument{
		Metadata: CatalogMetadata{
			SchemaVersion:      SchemaVersion,
			CatalogVersion:     "2026.07.28-test",
			CatalogSequence:    42,
			MinimumAgentSchema: AgentSchemaVersion,
			KeyID:              keyID,
			CreatedAt:          "2026-07-28T00:00:00Z",
		},
		Items: []CatalogItem{
			{
				ID:       "memcached",
				Kind:     ItemComponent,
				Revision: 1,
				Enabled:  true,
				Metadata: json.RawMessage(`{"name":"Memcached","category":"cache"}`),
			},
			{
				ID:       "vpn",
				Kind:     ItemAddon,
				Revision: 1,
				Enabled:  true,
				Metadata: json.RawMessage(`{"name":"VPN access"}`),
			},
		},
		Recipes: []CatalogRecipe{
			{
				ID:          "memcached.debian12.install",
				ItemID:      "memcached",
				PlatformKey: "debian12",
				Operation:   "install",
				Revision:    1,
				Support:     SupportSupported,
				Selector: PlatformSelector{
					OSFamily:       "linux",
					DistroFamily:   "debian",
					DistroID:       "debian",
					Version:        ">=12,<13",
					Architectures:  []string{"amd64", "arm64"},
					PackageManager: "apt",
					ServiceManager: "systemd",
				},
				Spec: RecipeSpec{
					Steps: []RecipeStep{
						{
							ID:             "install.package",
							Type:           "package_install",
							Packages:       []string{"memcached"},
							TimeoutSeconds: 600,
							RollbackStepID: "remove.package",
						},
						{ID: "enable.service", Type: "service_enable", Unit: "memcached.service"},
					},
					Verify: []RecipeStep{
						{ID: "verify.service", Type: "service_active", Unit: "memcached.service"},
						{ID: "verify.tcp", Type: "tcp_probe", Host: "127.0.0.1", Port: 11211},
					},
					Rollback: []RecipeStep{
						{ID: "remove.package", Type: "package_remove", Packages: []string{"memcached"}},
					},
				},
			},
			{
				ID:                "memcached.ubuntu.install",
				ItemID:            "memcached",
				PlatformKey:       "ubuntu",
				Operation:         "install",
				Revision:          1,
				Support:           SupportUnsupported,
				UnsupportedReason: "recipe has not passed the Ubuntu integration suite",
				Selector: PlatformSelector{
					OSFamily:       "linux",
					DistroFamily:   "debian",
					DistroID:       "ubuntu",
					PackageManager: "apt",
					ServiceManager: "systemd",
				},
			},
			{
				ID:          "memcached.apt-systemd.install",
				ItemID:      "memcached",
				PlatformKey: "apt-systemd",
				Operation:   "install",
				Revision:    1,
				Support:     SupportSupported,
				Selector: PlatformSelector{
					OSFamily:       "linux",
					PackageManager: "apt",
					ServiceManager: "systemd",
				},
				Spec: RecipeSpec{
					Steps: []RecipeStep{
						{ID: "install.package", Type: "package_install", Packages: []string{"memcached"}},
					},
				},
			},
		},
	}
}

func testOpenPolicy() OpenPolicy {
	return OpenPolicy{
		AgentSchema: AgentSchemaVersion,
	}
}
