package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

func makeCertificatePair(t *testing.T, algorithm string, dnsNames []string) (string, string) {
	t.Helper()

	var signer crypto.Signer
	var keyBlock *pem.Block
	switch algorithm {
	case "rsa":
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		signer = key
		keyBlock = &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	case "ecdsa":
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		signer = key
		keyBlock = &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}
	case "ed25519":
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		signer = key
		keyBlock = &pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}
	default:
		t.Fatalf("unknown key algorithm %q", algorithm)
	}

	commonName := "example.test"
	if len(dnsNames) > 0 {
		commonName = dnsNames[0]
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		DNSNames:     append([]string(nil), dnsNames...),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, signer.Public(), signer)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})), string(pem.EncodeToMemory(keyBlock))
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func hasArgPair(args []string, name, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name && args[i+1] == value {
			return true
		}
	}
	return false
}

func domainArgs(args []string) []string {
	var domains []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-d" {
			domains = append(domains, args[i+1])
		}
	}
	return domains
}

func TestBuildCertbotIssueArgsUsesStableNameAndExplicitForce(t *testing.T) {
	challengeRoot, err := hostingpath.ACMEChallengeRoot(7, 19)
	if err != nil {
		t.Fatal(err)
	}
	req := IssueLetsEncryptRequest{
		Domain:       "example.test",
		Aliases:      []string{"www.example.test", "EXAMPLE.test", ""},
		Email:        "admin@example.test",
		ACMEServer:   "https://acme.example/directory",
		EABKeyID:     "kid",
		EABHMACKey:   "secret",
		ForceRenewal: false,
	}
	args := buildCertbotIssueArgs(req, challengeRoot)

	if !hasArgPair(args, "--cert-name", req.Domain) {
		t.Fatalf("args %v do not pin certbot to the primary domain name", args)
	}
	if !hasArgPair(args, "-w", challengeRoot) ||
		hasArgPair(args, "-w", "/var/www/example.test") {
		t.Fatalf("certbot issue must use only the root-owned challenge root: %v", args)
	}
	for name, value := range map[string]string{
		"--config-dir": siteCertbotConfigDir,
		"--work-dir":   siteCertbotWorkDir,
		"--logs-dir":   siteCertbotLogsDir,
	} {
		if !hasArgPair(args, name, value) {
			t.Fatalf("args %v do not isolate certbot %s at %s", args, name, value)
		}
	}
	if hasArg(args, "--force-renewal") {
		t.Fatalf("ordinary issuance unexpectedly forces renewal: %v", args)
	}
	if !hasArgPair(args, "--server", req.ACMEServer) || !hasArgPair(args, "--eab-kid", req.EABKeyID) {
		t.Fatalf("CA or EAB options were lost: %v", args)
	}
	if got, want := domainArgs(args), []string{"example.test", "www.example.test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("domain args = %v, want %v", got, want)
	}

	req.ForceRenewal = true
	if forced := buildCertbotIssueArgs(req, challengeRoot); !hasArg(forced, "--force-renewal") {
		t.Fatalf("explicit reissue did not force renewal: %v", forced)
	}
}

func TestBuildCertbotIssueArgsOmitsEmptyEmail(t *testing.T) {
	args := buildCertbotIssueArgs(IssueLetsEncryptRequest{
		Domain: "example.test",
	}, "/var/www/example.test")
	if hasArg(args, "--email") {
		t.Fatalf("empty email must not be forwarded to certbot: %v", args)
	}
}

func TestNewStagedSiteLineageUsesDomainIdentityAndRandomSuffix(t *testing.T) {
	originalRead := stagedLineageRandomRead
	t.Cleanup(func() {
		stagedLineageRandomRead = originalRead
	})
	stagedLineageRandomRead = func(destination []byte) (int, error) {
		for index := range destination {
			destination[index] = byte(index)
		}
		return len(destination), nil
	}

	got, err := newStagedSiteLineage(42)
	if err != nil {
		t.Fatal(err)
	}
	if want := "cp-site-42-000102030405060708090a0b"; got != want {
		t.Fatalf("staged lineage = %q, want %q", got, want)
	}
	if !validStagedSiteLineage.MatchString(got) {
		t.Fatalf("generated staged lineage %q does not match the durable format", got)
	}

	if _, err := newStagedSiteLineage(0); err == nil {
		t.Fatal("zero domain identity was accepted")
	}
	stagedLineageRandomRead = func([]byte) (int, error) {
		return 0, fmt.Errorf("entropy unavailable")
	}
	if _, err := newStagedSiteLineage(42); err == nil || !strings.Contains(err.Error(), "generate staged lineage identity") {
		t.Fatalf("random source failure was not reported clearly: %v", err)
	}
}

func TestVerifyManagedCertificateSnapshotRequiresImmutableMatchingVersion(t *testing.T) {
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", t.TempDir())
	const domain = "example.test"

	cert, key := makeCertificatePair(t, "ed25519", []string{domain})
	chain, _ := makeCertificatePair(t, "ed25519", []string{"issuer.example.test"})
	certDir, err := ensureManagedCertificateDirectory(domain)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := publishCertificateVersion(
		certDir,
		newCertificateVersionContent(cert, key, chain),
		writeCertificateFile,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyManagedCertificateSnapshot(
		domain, paths.Fullchain, paths.Key, paths.Chain,
	); err != nil {
		t.Fatalf("valid managed snapshot was rejected: %v", err)
	}

	otherCert, otherKey := makeCertificatePair(t, "ed25519", []string{domain})
	otherPaths, err := publishCertificateVersion(
		certDir,
		newCertificateVersionContent(otherCert, otherKey, chain),
		writeCertificateFile,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyManagedCertificateSnapshot(
		domain, paths.Fullchain, otherPaths.Key, paths.Chain,
	); err == nil || !strings.Contains(err.Error(), "different immutable snapshots") {
		t.Fatalf("cross-version key was accepted: %v", err)
	}

	if err := os.WriteFile(paths.Key, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyManagedCertificateSnapshot(
		domain, paths.Fullchain, paths.Key, paths.Chain,
	); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("tampered snapshot was accepted: %v", err)
	}
}

func TestManagedCertificateVersionPathRejectsEscapesAndWrongShape(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	const domain = "example.test"

	candidates := []string{
		filepath.Join(root, "outside", "sha256-"+strings.Repeat("a", 64), "fullchain.pem"),
		filepath.Join(root, domain, "current", "fullchain.pem"),
		filepath.Join(root, domain, "sha256-"+strings.Repeat("a", 64), "cert.pem"),
		filepath.Join(root, domain, "sha256-short", "fullchain.pem"),
	}
	for _, candidate := range candidates {
		if _, err := managedCertificateVersionPath(
			domain, candidate, "fullchain.pem",
		); err == nil {
			t.Fatalf("unsafe managed certificate path %q was accepted", candidate)
		}
	}
}

func TestSelectIssueLineageAllowsFreshCustomToACMEReplacement(t *testing.T) {
	originalRead := stagedLineageRandomRead
	t.Cleanup(func() {
		stagedLineageRandomRead = originalRead
	})
	stagedLineageRandomRead = func(destination []byte) (int, error) {
		for index := range destination {
			destination[index] = byte(index)
		}
		return len(destination), nil
	}

	isolated := certbotStorage{
		ConfigDir: "/isolated/config",
		WorkDir:   "/isolated/work",
		LogsDir:   "/isolated/logs",
	}
	selected, lineage, err := selectIssueLineage(
		IssueLetsEncryptRequest{
			Domain:       "example.test",
			DomainID:     42,
			ForceRenewal: true,
			StageLineage: true,
			FreshLineage: true,
			// A custom certificate has no certbot source path or lineage.
		},
		isolated,
		"/must-not-be-used",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selected, isolated) {
		t.Fatalf("fresh replacement storage = %#v, want %#v", selected, isolated)
	}
	if want := "cp-site-42-000102030405060708090a0b"; lineage != want {
		t.Fatalf("fresh replacement lineage = %q, want %q", lineage, want)
	}

	for _, req := range []IssueLetsEncryptRequest{
		{Domain: "example.test", DomainID: 42, FreshLineage: true},
		{
			Domain: "example.test", DomainID: 42,
			FreshLineage: true, StageLineage: true,
		},
	} {
		if _, _, err := selectIssueLineage(req, isolated, "/unused"); err == nil {
			t.Fatalf("unsafe fresh-lineage combination was accepted: %#v", req)
		}
	}
}

func TestNormalizeCurrentLineageNameAcceptsOnlyCanonicalOrSameDomainStagedIdentity(t *testing.T) {
	const (
		domain   = "example.test"
		domainID = 42
	)
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty falls back to canonical", raw: "", want: domain},
		{name: "canonical is case insensitive", raw: " EXAMPLE.TEST ", want: domain},
		{
			name: "same domain staged",
			raw:  " CP-SITE-42-00112233445566778899AABB ",
			want: "cp-site-42-00112233445566778899aabb",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeCurrentLineageName(test.raw, domain, domainID)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("normalized lineage = %q, want %q", got, test.want)
			}
		})
	}

	for _, raw := range []string{
		"cp-site-41-00112233445566778899aabb",
		"cp-site-420-00112233445566778899aabb",
		"cp-site-42-00112233445566778899aab",
		"cp-site-42-00112233445566778899aabz",
		"../example.test",
		"example.test.",
	} {
		if _, err := normalizeCurrentLineageName(raw, domain, domainID); err == nil {
			t.Fatalf("invalid or foreign lineage %q was accepted", raw)
		}
	}
}

func TestStagedIssueArgsUseCurrentLineageStorageAndFreshCertName(t *testing.T) {
	const (
		domain      = "example.test"
		domainID    = 42
		stagedName  = "cp-site-42-000102030405060708090a0b"
		stagedAlias = "www.example.test"
	)
	for _, test := range []struct {
		name            string
		currentLineage  string
		selectedStorage string
	}{
		{
			name:            "isolated staged lineage account",
			currentLineage:  "cp-site-42-111111111111111111111111",
			selectedStorage: "isolated",
		},
		{
			name:            "legacy canonical lineage account",
			currentLineage:  domain,
			selectedStorage: "legacy",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			isolated := certbotStorage{
				ConfigDir: filepath.Join(root, "isolated-config"),
				WorkDir:   filepath.Join(root, "isolated-work"),
				LogsDir:   filepath.Join(root, "isolated-logs"),
			}
			legacyConfig := filepath.Join(root, "legacy-config")
			legacy := isolated
			legacy.ConfigDir = legacyConfig

			activeCert, activeKey := makeCertificatePair(t, "ed25519", []string{domain})
			decoyCert, decoyKey := makeCertificatePair(t, "ed25519", []string{domain})
			chain, _ := makeCertificatePair(t, "ed25519", []string{"issuer.example.test"})
			activePath := filepath.Join(root, "active", "fullchain.pem")
			if err := os.MkdirAll(filepath.Dir(activePath), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(activePath, []byte(activeCert), 0600); err != nil {
				t.Fatal(err)
			}

			writeCertbotRenewalMarker(t, isolated.ConfigDir, test.currentLineage)
			writeCertbotRenewalMarker(t, legacy.ConfigDir, test.currentLineage)
			if test.selectedStorage == "isolated" {
				writeCertbotCertificateFiles(t, isolated, test.currentLineage, activeCert, activeKey, chain)
				writeCertbotCertificateFiles(t, legacy, test.currentLineage, decoyCert, decoyKey, chain)
			} else {
				writeCertbotCertificateFiles(t, isolated, test.currentLineage, decoyCert, decoyKey, chain)
				writeCertbotCertificateFiles(t, legacy, test.currentLineage, activeCert, activeKey, chain)
			}

			currentLineage, err := normalizeCurrentLineageName(
				test.currentLineage, domain, domainID,
			)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := certbotStorageForExistingLineage(
				currentLineage, activePath, isolated, legacyConfig,
			)
			if err != nil {
				t.Fatal(err)
			}
			wantConfig := isolated.ConfigDir
			if test.selectedStorage == "legacy" {
				wantConfig = legacy.ConfigDir
			}
			if selected.ConfigDir != wantConfig {
				t.Fatalf("selected config/account = %q, want %q", selected.ConfigDir, wantConfig)
			}

			challengeRoot, err := hostingpath.ACMEChallengeRoot(7, domainID)
			if err != nil {
				t.Fatal(err)
			}
			args := buildCertbotIssueArgsForStorage(
				IssueLetsEncryptRequest{
					Domain:       domain,
					Aliases:      []string{stagedAlias},
					ForceRenewal: true,
					StageLineage: true,
				},
				challengeRoot,
				selected,
				stagedName,
			)
			for name, value := range map[string]string{
				"--config-dir": selected.ConfigDir,
				"--work-dir":   selected.WorkDir,
				"--logs-dir":   selected.LogsDir,
				"--cert-name":  stagedName,
			} {
				if !hasArgPair(args, name, value) {
					t.Fatalf("staged args %v do not use selected storage/account %s %s", args, name, value)
				}
			}
			if hasArgPair(args, "--cert-name", currentLineage) {
				t.Fatalf("staged issue would overwrite current lineage %q: %v", currentLineage, args)
			}
			if got, want := domainArgs(args), []string{domain, stagedAlias}; !reflect.DeepEqual(got, want) {
				t.Fatalf("staged issue domains = %v, want %v", got, want)
			}
		})
	}
}

func TestNormalizeCertificateAliasesCanonicalizesDeduplicatesAndRejectsUnsafeNames(t *testing.T) {
	got, err := normalizeCertificateAliases("example.test", []string{
		"WWW.EXAMPLE.TEST.", "example.test", "www.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"www.example.test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}

	for _, aliases := range [][]string{
		{""},
		{"--config-dir"},
		{"../escape.test"},
		{"*.example.test"},
		{"bad_name.example.test"},
	} {
		if _, err := normalizeCertificateAliases("example.test", aliases); err == nil {
			t.Fatalf("unsafe aliases %q were accepted", aliases)
		}
	}

	tooMany := make([]string, 100)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("alias-%d.example.test", i)
	}
	if _, err := normalizeCertificateAliases("example.test", tooMany); err == nil {
		t.Fatal("certificate name limit was not enforced")
	}
}

func writeCertbotRenewalMarker(t *testing.T, configDir, domain string) {
	t.Helper()
	renewalDir := filepath.Join(configDir, "renewal")
	if err := os.MkdirAll(renewalDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(renewalDir, domain+".conf"), []byte("renewal"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCertbotStorageForRenewalPrefersIsolatedAndFallsBackToLegacy(t *testing.T) {
	root := t.TempDir()
	isolated := certbotStorage{
		ConfigDir: filepath.Join(root, "isolated-config"),
		WorkDir:   filepath.Join(root, "isolated-work"),
		LogsDir:   filepath.Join(root, "isolated-logs"),
	}
	legacyConfig := filepath.Join(root, "legacy-config")
	const domain = "example.test"

	got, err := certbotStorageForRenewal(domain, "", isolated, legacyConfig)
	if err != nil {
		t.Fatal(err)
	}
	if got != isolated {
		t.Fatalf("missing lineage selected storage %+v, want isolated %+v", got, isolated)
	}

	writeCertbotRenewalMarker(t, legacyConfig, domain)
	got, err = certbotStorageForRenewal(domain, "", isolated, legacyConfig)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigDir != legacyConfig || got.WorkDir != isolated.WorkDir || got.LogsDir != isolated.LogsDir {
		t.Fatalf("legacy fallback storage = %+v, want legacy config with isolated work/logs", got)
	}

	writeCertbotRenewalMarker(t, isolated.ConfigDir, domain)
	got, err = certbotStorageForRenewal(domain, "", isolated, legacyConfig)
	if err != nil {
		t.Fatal(err)
	}
	if got != isolated {
		t.Fatalf("isolated lineage did not take precedence: got %+v, want %+v", got, isolated)
	}
}

func TestCertbotStorageForRenewalMatchesActiveFingerprintInLegacyArchive(t *testing.T) {
	root := t.TempDir()
	isolated := certbotStorage{
		ConfigDir: filepath.Join(root, "isolated-config"),
		WorkDir:   filepath.Join(root, "isolated-work"),
		LogsDir:   filepath.Join(root, "isolated-logs"),
	}
	legacyConfig := filepath.Join(root, "legacy-config")
	const domain = "example.test"
	writeCertbotRenewalMarker(t, isolated.ConfigDir, domain)
	writeCertbotRenewalMarker(t, legacyConfig, domain)

	activeCert, _ := makeCertificatePair(t, "rsa", []string{domain})
	isolatedLiveCert, _ := makeCertificatePair(t, "ecdsa", []string{domain})
	legacyLiveCert, _ := makeCertificatePair(t, "ed25519", []string{domain})
	activePath := filepath.Join(root, "active", "fullchain.pem")
	paths := map[string]string{
		activePath: activeCert,
		filepath.Join(isolated.ConfigDir, "live", domain, "cert.pem"): isolatedLiveCert,
		filepath.Join(legacyConfig, "live", domain, "cert.pem"):       legacyLiveCert,
		filepath.Join(legacyConfig, "archive", domain, "cert1.pem"):   activeCert,
		filepath.Join(legacyConfig, "archive", domain, "cert2.pem"):   legacyLiveCert,
	}
	for path, content := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := certbotStorageForRenewal(domain, activePath, isolated, legacyConfig)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigDir != legacyConfig {
		t.Fatalf("active archived fingerprint selected config %q, want legacy %q", got.ConfigDir, legacyConfig)
	}
	if got.WorkDir != isolated.WorkDir || got.LogsDir != isolated.LogsDir {
		t.Fatalf("legacy fingerprint match lost isolated work/log dirs: %+v", got)
	}
}

func TestBuildCertbotRenewArgsUsesSelectedStorage(t *testing.T) {
	challengeRoot, err := hostingpath.ACMEChallengeRoot(7, 19)
	if err != nil {
		t.Fatal(err)
	}
	storage := certbotStorage{
		ConfigDir: "/config",
		WorkDir:   "/work",
		LogsDir:   "/logs",
	}
	args := buildCertbotRenewArgs("example.test", storage, challengeRoot)
	for name, value := range map[string]string{
		"--config-dir": storage.ConfigDir,
		"--work-dir":   storage.WorkDir,
		"--logs-dir":   storage.LogsDir,
		"--cert-name":  "example.test",
	} {
		if !hasArgPair(args, name, value) {
			t.Fatalf("renew args %v do not contain %s %s", args, name, value)
		}
	}
	if hasArg(args, "--force-renewal") {
		t.Fatalf("scheduled renewal unexpectedly forces renewal: %v", args)
	}
	if !hasArg(args, "--webroot") || !hasArgPair(args, "-w", challengeRoot) {
		t.Fatalf("renewal does not override legacy tenant webroot with %s: %v", challengeRoot, args)
	}
	for _, flag := range []string{"--no-random-sleep-on-renew", "--non-interactive"} {
		if !hasArg(args, flag) {
			t.Fatalf("targeted renewal is missing %s: %v", flag, args)
		}
	}
}

func TestBuildCertbotRenewArgsTargetsDurableStagedLineage(t *testing.T) {
	const (
		primaryDomain = "example.test"
		lineageName   = "cp-site-42-00112233445566778899aabb"
	)
	storage := certbotStorage{
		ConfigDir: "/config",
		WorkDir:   "/work",
		LogsDir:   "/logs",
	}
	args := buildCertbotRenewArgs(lineageName, storage, "/challenge")
	if !hasArgPair(args, "--cert-name", lineageName) {
		t.Fatalf("renew args %v do not target durable lineage %q", args, lineageName)
	}
	if hasArgPair(args, "--cert-name", primaryDomain) {
		t.Fatalf("renew args unexpectedly target the primary-domain lineage: %v", args)
	}
}

func TestCertbotStorageForExistingLineageFailsClosedWhenMissing(t *testing.T) {
	root := t.TempDir()
	isolated := certbotStorage{
		ConfigDir: filepath.Join(root, "isolated-config"),
		WorkDir:   filepath.Join(root, "isolated-work"),
		LogsDir:   filepath.Join(root, "isolated-logs"),
	}
	const lineageName = "cp-site-42-00112233445566778899aabb"
	if _, err := certbotStorageForExistingLineage(
		lineageName, "", isolated, filepath.Join(root, "legacy-config"),
	); err == nil || !strings.Contains(err.Error(), "is not available") {
		t.Fatalf("missing active lineage did not fail closed: %v", err)
	}
}

func TestCertbotRenewContextHasBoundedDeadline(t *testing.T) {
	ctx, cancel := certbotRenewContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("certbot renewal context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= certbotRenewTimeout-time.Second || remaining > certbotRenewTimeout {
		t.Fatalf("certbot renewal deadline is %s away, want approximately %s", remaining, certbotRenewTimeout)
	}
}

func TestEnsureIsolatedSiteCertbotStorageCreatesPrivateSafeDirectories(t *testing.T) {
	root := t.TempDir()
	storage := certbotStorage{
		ConfigDir: filepath.Join(root, "config"),
		WorkDir:   filepath.Join(root, "work"),
		LogsDir:   filepath.Join(root, "logs"),
	}
	if err := ensureIsolatedSiteCertbotStorage(storage); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{storage.ConfigDir, storage.WorkDir, storage.LogsDir} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Type() == os.ModeSymlink {
			t.Fatalf("certbot storage path is not a safe directory: %s", path)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
			t.Fatalf("mode for %s = %o, want 700", path, info.Mode().Perm())
		}
	}
}

func TestValidateCertificateMatchesSupportedPrivateKeysAndReportsSANs(t *testing.T) {
	dnsNames := []string{"example.test", "www.example.test"}
	for _, algorithm := range []string{"rsa", "ecdsa", "ed25519"} {
		t.Run(algorithm, func(t *testing.T) {
			certPEM, keyPEM := makeCertificatePair(t, algorithm, dnsNames)
			var resp ValidateCertResponse
			if err := (&Agent{}).ValidateCertificate(ValidateCertRequest{
				CertContent: certPEM,
				KeyContent:  keyPEM,
				Domain:      "example.test",
			}, &resp); err != nil {
				t.Fatal(err)
			}
			if !resp.Valid {
				t.Fatalf("valid %s pair was rejected: %s", algorithm, resp.Error)
			}
			if !reflect.DeepEqual(resp.DNSNames, dnsNames) {
				t.Fatalf("DNSNames = %v, want %v", resp.DNSNames, dnsNames)
			}

			_, unrelatedKey := makeCertificatePair(t, algorithm, dnsNames)
			var mismatch ValidateCertResponse
			if err := (&Agent{}).ValidateCertificate(ValidateCertRequest{
				CertContent: certPEM,
				KeyContent:  unrelatedKey,
				Domain:      "example.test",
			}, &mismatch); err != nil {
				t.Fatal(err)
			}
			if mismatch.Valid || !strings.Contains(mismatch.Error, "does not match") {
				t.Fatalf("unrelated %s key was not rejected clearly: %+v", algorithm, mismatch)
			}
		})
	}
}

func TestValidateCertificateReportsSelfSignedAsValidButUntrusted(t *testing.T) {
	certPEM, keyPEM := makeCertificatePair(t, "ecdsa", []string{"example.test"})
	var resp ValidateCertResponse
	if err := (&Agent{}).ValidateCertificate(ValidateCertRequest{
		CertContent: certPEM,
		KeyContent:  keyPEM,
		Domain:      "example.test",
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Valid {
		t.Fatalf("self-signed certificate failed structural validation: %+v", resp)
	}
	if !resp.TrustChecked {
		t.Fatalf("self-signed certificate trust was not checked: %+v", resp)
	}
	if resp.Trusted || strings.TrimSpace(resp.TrustError) == "" {
		t.Fatalf("self-signed certificate was not reported as untrusted: %+v", resp)
	}
}

func TestGetCertificateInfoReportsDNSNames(t *testing.T) {
	dnsNames := []string{"example.test", "www.example.test"}
	certPEM, _ := makeCertificatePair(t, "ecdsa", dnsNames)
	path := filepath.Join(t.TempDir(), "fullchain.pem")
	if err := os.WriteFile(path, []byte(certPEM), 0600); err != nil {
		t.Fatal(err)
	}

	var resp ValidateCertResponse
	if err := (&Agent{}).GetCertificateInfo(path, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Valid || !reflect.DeepEqual(resp.DNSNames, dnsNames) {
		t.Fatalf("unexpected certificate info: %+v", resp)
	}
}

func TestInspectInstalledCertificateRejectsMissingAndMismatchedKey(t *testing.T) {
	root := t.TempDir()
	certPEM, _ := makeCertificatePair(t, "rsa", []string{"example.test"})
	_, wrongKeyPEM := makeCertificatePair(t, "rsa", []string{"example.test"})
	certPath := filepath.Join(root, "fullchain.pem")
	if err := os.WriteFile(certPath, []byte(certPEM), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("missing", func(t *testing.T) {
		var resp ValidateCertResponse
		if err := (&Agent{}).InspectInstalledCertificate(InspectCertificateRequest{
			CertPath: certPath,
			KeyPath:  filepath.Join(root, "missing-privkey.pem"),
		}, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Valid || !strings.Contains(resp.Error, "failed to read private key") {
			t.Fatalf("missing private key was not rejected clearly: %+v", resp)
		}
	})

	t.Run("mismatched", func(t *testing.T) {
		keyPath := filepath.Join(root, "wrong-privkey.pem")
		if err := os.WriteFile(keyPath, []byte(wrongKeyPEM), 0600); err != nil {
			t.Fatal(err)
		}
		var resp ValidateCertResponse
		if err := (&Agent{}).InspectInstalledCertificate(InspectCertificateRequest{
			CertPath: certPath,
			KeyPath:  keyPath,
		}, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Valid || !strings.Contains(resp.Error, "does not match") {
			t.Fatalf("mismatched private key was not rejected clearly: %+v", resp)
		}
	})
}

func TestInstallCustomCertificateReturnsLeafFirstFullchain(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	leafPEM, keyPEM := makeCertificatePair(t, "rsa", []string{"example.test"})
	chainPEM, _ := makeCertificatePair(t, "ecdsa", []string{"intermediate.test"})

	var resp InstallCertResponse
	if err := (&Agent{}).InstallCustomCertificate(InstallCertRequest{
		Domain:       "example.test",
		CertContent:  leafPEM,
		KeyContent:   keyPEM,
		ChainContent: chainPEM,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("install failed: %s", resp.Error)
	}
	content := newCertificateVersionContent(leafPEM, keyPEM, chainPEM)
	wantPath := filepath.Join(root, "example.test", certificateVersionID(content), "fullchain.pem")
	if resp.CertPath != wantPath {
		t.Fatalf("CertPath = %q, want served fullchain %q", resp.CertPath, wantPath)
	}
	got, err := os.ReadFile(resp.CertPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := pemSequence(leafPEM, chainPEM); !bytes.Equal(got, want) {
		t.Fatal("fullchain is not the leaf followed by the supplied chain")
	}

	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{
			resp.CertPath:  0644,
			resp.ChainPath: 0644,
			filepath.Join(filepath.Dir(resp.CertPath), "cert.pem"): 0644,
			resp.KeyPath:                0600,
			filepath.Dir(resp.CertPath): 0750,
		} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if gotMode := info.Mode().Perm(); gotMode != want {
				t.Fatalf("mode for %s = %o, want %o", path, gotMode, want)
			}
		}
	}
}

func TestInstallCustomCertificatePreservesPreviousVersion(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	certOne, keyOne := makeCertificatePair(t, "rsa", []string{"example.test"})
	certTwo, keyTwo := makeCertificatePair(t, "ecdsa", []string{"example.test"})

	var first InstallCertResponse
	if err := (&Agent{}).InstallCustomCertificate(InstallCertRequest{
		Domain: "example.test", CertContent: certOne, KeyContent: keyOne,
	}, &first); err != nil {
		t.Fatal(err)
	}
	if !first.Success {
		t.Fatalf("first install failed: %s", first.Error)
	}
	oldCert, err := os.ReadFile(first.CertPath)
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := os.ReadFile(first.KeyPath)
	if err != nil {
		t.Fatal(err)
	}

	var second InstallCertResponse
	if err := (&Agent{}).InstallCustomCertificate(InstallCertRequest{
		Domain: "example.test", CertContent: certTwo, KeyContent: keyTwo,
	}, &second); err != nil {
		t.Fatal(err)
	}
	if !second.Success {
		t.Fatalf("second install failed: %s", second.Error)
	}
	if filepath.Dir(first.CertPath) == filepath.Dir(second.CertPath) {
		t.Fatal("replacement certificate reused the previous version directory")
	}
	if got, err := os.ReadFile(first.CertPath); err != nil || !bytes.Equal(got, oldCert) {
		t.Fatalf("previous certificate changed after replacement: %v", err)
	}
	if got, err := os.ReadFile(first.KeyPath); err != nil || !bytes.Equal(got, oldKey) {
		t.Fatalf("previous private key changed after replacement: %v", err)
	}
}

func TestPublishCertificateVersionCleansPartialStage(t *testing.T) {
	certDir := t.TempDir()
	oldContent := newCertificateVersionContent("old certificate", "old key", "")
	oldPaths, err := publishCertificateVersion(certDir, oldContent, writeCertificateFile)
	if err != nil {
		t.Fatal(err)
	}
	oldBytes, err := os.ReadFile(oldPaths.Cert)
	if err != nil {
		t.Fatal(err)
	}

	newContent := newCertificateVersionContent("new certificate", "new key", "new chain")
	writes := 0
	failingWriter := func(path string, content []byte, mode os.FileMode) error {
		writes++
		if writes == 2 {
			return os.ErrPermission
		}
		return writeCertificateFile(path, content, mode)
	}
	if _, err := publishCertificateVersion(certDir, newContent, failingWriter); err == nil {
		t.Fatal("partial staging failure unexpectedly succeeded")
	}

	entries, err := os.ReadDir(certDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != certificateVersionID(oldContent) {
		t.Fatalf("partial staging left unexpected entries: %v", entries)
	}
	if got, err := os.ReadFile(oldPaths.Cert); err != nil || !bytes.Equal(got, oldBytes) {
		t.Fatalf("partial staging changed the active version: %v", err)
	}
	newDir := filepath.Join(certDir, certificateVersionID(newContent))
	if _, err := os.Stat(newDir); !os.IsNotExist(err) {
		t.Fatalf("failed version was published: %v", err)
	}
}

func TestPublishCertificateVersionReusesVerifiedVersionWithoutWriting(t *testing.T) {
	certDir := t.TempDir()
	content := newCertificateVersionContent("certificate", "key", "chain")
	first, err := publishCertificateVersion(certDir, content, writeCertificateFile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := publishCertificateVersion(certDir, content, func(string, []byte, os.FileMode) error {
		t.Fatal("verified version was rewritten")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("reused paths = %+v, want %+v", second, first)
	}
}

func writeCertbotCertificateFiles(t *testing.T, storage certbotStorage, domain, certPEM, keyPEM, chainPEM string) {
	t.Helper()
	liveDir := storage.certificateDir(domain)
	if err := os.MkdirAll(liveDir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"cert.pem":    certPEM,
		"privkey.pem": keyPEM,
		"chain.pem":   chainPEM,
	} {
		if err := os.WriteFile(filepath.Join(liveDir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSnapshotCertbotCertificatePublishesImmutableVersion(t *testing.T) {
	managedRoot := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", managedRoot)
	storage := certbotStorage{ConfigDir: filepath.Join(t.TempDir(), "certbot")}
	const domain = "example.test"

	leafOne, keyOne := makeCertificatePair(t, "rsa", []string{domain, "www." + domain})
	chainOne, _ := makeCertificatePair(t, "ecdsa", []string{"issuer-one.test"})
	writeCertbotCertificateFiles(t, storage, domain, leafOne, keyOne, chainOne)
	first, cert, err := snapshotCertbotCertificate(domain, storage)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cert.DNSNames, []string{domain, "www." + domain}) {
		t.Fatalf("snapshot DNS names = %v", cert.DNSNames)
	}
	if !strings.HasPrefix(first.Fullchain, filepath.Join(managedRoot, domain)+string(os.PathSeparator)) {
		t.Fatalf("snapshot path %q is outside managed root", first.Fullchain)
	}
	if strings.HasPrefix(first.Fullchain, storage.certificateDir(domain)+string(os.PathSeparator)) {
		t.Fatalf("snapshot returned mutable certbot live path %q", first.Fullchain)
	}
	oldFullchain, err := os.ReadFile(first.Fullchain)
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := os.ReadFile(first.Key)
	if err != nil {
		t.Fatal(err)
	}

	leafTwo, keyTwo := makeCertificatePair(t, "ecdsa", []string{domain})
	chainTwo, _ := makeCertificatePair(t, "rsa", []string{"issuer-two.test"})
	writeCertbotCertificateFiles(t, storage, domain, leafTwo, keyTwo, chainTwo)
	second, _, err := snapshotCertbotCertificate(domain, storage)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(first.Fullchain) == filepath.Dir(second.Fullchain) {
		t.Fatal("replacement certbot certificate reused the previous immutable version")
	}
	if got, err := os.ReadFile(first.Fullchain); err != nil || !bytes.Equal(got, oldFullchain) {
		t.Fatalf("previous fullchain changed after certbot source replacement: %v", err)
	}
	if got, err := os.ReadFile(first.Key); err != nil || !bytes.Equal(got, oldKey) {
		t.Fatalf("previous key changed after certbot source replacement: %v", err)
	}
}

func TestSnapshotCertbotCertificateFromLineageReadsSourceAndPublishesForDomain(t *testing.T) {
	managedRoot := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", managedRoot)
	storage := certbotStorage{ConfigDir: filepath.Join(t.TempDir(), "certbot")}
	const (
		domain      = "example.test"
		lineageName = "cp-site-42-00112233445566778899aabb"
	)

	leaf, key := makeCertificatePair(t, "ed25519", []string{domain, "www." + domain})
	chain, _ := makeCertificatePair(t, "ed25519", []string{"issuer.example.test"})
	writeCertbotCertificateFiles(t, storage, lineageName, leaf, key, chain)

	paths, cert, err := snapshotCertbotCertificateFromLineage(lineageName, domain, storage)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cert.DNSNames, []string{domain, "www." + domain}; !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot DNS names = %v, want %v", got, want)
	}
	domainRoot := filepath.Join(managedRoot, domain) + string(os.PathSeparator)
	if !strings.HasPrefix(paths.Fullchain, domainRoot) ||
		!strings.HasPrefix(paths.Key, domainRoot) ||
		!strings.HasPrefix(paths.Chain, domainRoot) {
		t.Fatalf("lineage snapshot paths %+v are not published under domain root %q", paths, domainRoot)
	}
	if strings.HasPrefix(paths.Fullchain, storage.certificateDir(lineageName)+string(os.PathSeparator)) {
		t.Fatalf("snapshot exposed mutable staged lineage path %q", paths.Fullchain)
	}
	if _, err := os.Stat(filepath.Join(managedRoot, lineageName)); !os.IsNotExist(err) {
		t.Fatalf("snapshot was published under the lineage identity instead of the domain: %v", err)
	}
	fullchain, err := os.ReadFile(paths.Fullchain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fullchain, newCertificateVersionContent(leaf, key, chain).Fullchain) {
		t.Fatal("published fullchain was not read from the selected source lineage")
	}
}

func TestSnapshotCertbotCertificateRejectsMismatchedKeyBeforePublishing(t *testing.T) {
	managedRoot := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", managedRoot)
	storage := certbotStorage{ConfigDir: filepath.Join(t.TempDir(), "certbot")}
	const domain = "example.test"
	leaf, _ := makeCertificatePair(t, "ed25519", []string{domain})
	_, wrongKey := makeCertificatePair(t, "ed25519", []string{domain})
	chain, _ := makeCertificatePair(t, "rsa", []string{"issuer.test"})
	writeCertbotCertificateFiles(t, storage, domain, leaf, wrongKey, chain)

	if _, _, err := snapshotCertbotCertificate(domain, storage); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched certbot key was not rejected clearly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(managedRoot, domain)); !os.IsNotExist(err) {
		t.Fatalf("invalid certbot material was published: %v", err)
	}
}

func TestInstallCustomCertificateRejectsMismatchedKeyBeforeWriting(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	certPEM, _ := makeCertificatePair(t, "ed25519", []string{"example.test"})
	_, wrongKey := makeCertificatePair(t, "ed25519", []string{"example.test"})

	var resp InstallCertResponse
	if err := (&Agent{}).InstallCustomCertificate(InstallCertRequest{
		Domain:      "example.test",
		CertContent: certPEM,
		KeyContent:  wrongKey,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Success || !strings.Contains(resp.Error, "does not match") {
		t.Fatalf("mismatched install was not rejected clearly: %+v", resp)
	}
	if _, err := os.Stat(filepath.Join(root, "example.test")); !os.IsNotExist(err) {
		t.Fatalf("invalid certificate wrote files before validation: %v", err)
	}
}

func TestCustomCertificateDirectoryRejectsUnsafeDomain(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	for _, domain := range []string{"", ".", "..", "../escape", "bad/name.test", "bad_name.test", "-bad.test", "bad-.test"} {
		if _, err := customCertificateDirectory(domain); err == nil {
			t.Fatalf("customCertificateDirectory(%q) accepted an unsafe domain", domain)
		}
	}
	got, err := customCertificateDirectory("EXAMPLE.TEST.")
	if err != nil {
		t.Fatalf("canonical domain rejected: %v", err)
	}
	if want := filepath.Join(root, "example.test"); got != want {
		t.Fatalf("customCertificateDirectory() = %q, want %q", got, want)
	}
}

func TestInstallCustomCertificateRejectsSymlinkDomainDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available without developer mode")
	}
	root := t.TempDir()
	outside := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	if err := os.Symlink(outside, filepath.Join(root, "example.test")); err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM := makeCertificatePair(t, "rsa", []string{"example.test"})
	var resp InstallCertResponse
	if err := (&Agent{}).InstallCustomCertificate(InstallCertRequest{
		Domain: "example.test", CertContent: certPEM, KeyContent: keyPEM,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Success || !strings.Contains(resp.Error, "safe directory") {
		t.Fatalf("symlink certificate directory was not rejected: %+v", resp)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("certificate files escaped through symlink: %v", entries)
	}
}
