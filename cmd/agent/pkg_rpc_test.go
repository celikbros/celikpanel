package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

// aptListsAge decides whether an install is preceded by `apt-get update`; a
// wrong answer either slows every install or lets a stale list 404 through.
func TestAptListsAge(t *testing.T) {
	orig := aptListsDir
	defer func() { aptListsDir = orig }()

	// Missing directory → unknown, callers must treat as stale.
	aptListsDir = filepath.Join(t.TempDir(), "does-not-exist")
	if _, ok := aptListsAge(); ok {
		t.Error("missing lists dir must report ok=false")
	}

	// Empty directory (only subdirs, like partial/) → still unknown.
	dir := t.TempDir()
	aptListsDir = dir
	if err := os.Mkdir(filepath.Join(dir, "partial"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := aptListsAge(); ok {
		t.Error("dir with no list files must report ok=false")
	}

	// A fresh list file → small age, ok=true.
	f := filepath.Join(dir, "deb.debian.org_debian_dists_trixie_InRelease")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	age, ok := aptListsAge()
	if !ok || age > time.Minute {
		t.Errorf("fresh file: age=%v ok=%v, want recent + true", age, ok)
	}

	// Back-date it → age must reflect the newest file, and an even older
	// second file must not win.
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
	age, ok = aptListsAge()
	if !ok || age < 2*time.Hour {
		t.Errorf("back-dated file: age=%v ok=%v, want ≥2h + true", age, ok)
	}
}

func TestPacmanInstallArgsUseOneFullUpgradeTransaction(t *testing.T) {
	packages := []string{"nginx", "php-fpm"}
	got := pacmanInstallArgs(packages)
	want := []string{"-Syu", "--noconfirm", "--needed", "nginx", "php-fpm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pacman install args = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(packages, []string{"nginx", "php-fpm"}) {
		t.Fatalf("pacmanInstallArgs mutated caller packages: %q", packages)
	}
}

func TestDNFTransactionArgsAreNonInteractiveAndConservative(t *testing.T) {
	packages := []string{"nginx", "policycoreutils-python-utils"}
	wantInstall := []string{
		"-y",
		"--setopt=install_weak_deps=False",
		"install",
		"nginx",
		"policycoreutils-python-utils",
	}
	wantRemove := []string{
		"-y",
		"--setopt=clean_requirements_on_remove=False",
		"remove",
		"nginx",
		"policycoreutils-python-utils",
	}
	if got := dnfInstallArgs(packages); !reflect.DeepEqual(got, wantInstall) {
		t.Fatalf("DNF install args = %q, want %q", got, wantInstall)
	}
	if got := dnfRemoveArgs(packages); !reflect.DeepEqual(got, wantRemove) {
		t.Fatalf("DNF remove args = %q, want %q", got, wantRemove)
	}
	if !reflect.DeepEqual(packages, []string{"nginx", "policycoreutils-python-utils"}) {
		t.Fatalf("DNF argument helpers mutated caller packages: %q", packages)
	}
}

func TestDNFCandidateQueryIsCacheOnly(t *testing.T) {
	want := []string{
		"-C",
		"-q",
		"repoquery",
		"--available",
		"--latest-limit=1",
		`--queryformat=CELIKPANEL_EVR:%{evr}\n`,
		"nginx",
	}
	if got := dnfCandidateQueryArgs("nginx"); !reflect.DeepEqual(got, want) {
		t.Fatalf("DNF candidate args = %q, want %q", got, want)
	}
	if got, want := dnfMetadataRefreshArgs(), []string{"-q", "--refresh", "makecache"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DNF metadata refresh args = %q, want %q", got, want)
	}
}

func TestDNFInstallCandidateRefreshesBeforeCacheOnlyQuery(t *testing.T) {
	source, err := os.ReadFile("pkg_rpc.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	start := strings.Index(body, "func dnfInstallCandidateWithExecutable")
	end := strings.Index(body[start:], "func parseDNFInstallCandidate")
	if start < 0 || end < 0 {
		t.Fatal("DNF install candidate function was not found")
	}
	body = body[start : start+end]
	refresh := strings.Index(body, "refreshDNFMetadataWithExecutable(ctx, dnf)")
	query := strings.Index(body, "dnfCandidateQueryArgs(packageName)")
	if refresh < 0 || query < 0 || refresh >= query {
		t.Fatalf("DNF candidate refresh must precede the cache-only query: refresh=%d query=%d", refresh, query)
	}
}

func TestParseDNFInstallCandidateFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{name: "single", output: "CELIKPANEL_EVR:1:1.24.0-6.el9_5\n", want: "1:1.24.0-6.el9_5"},
		{name: "duplicate arches", output: "CELIKPANEL_EVR:1.24.0-1.fc42\nCELIKPANEL_EVR:1.24.0-1.fc42\n", want: "1.24.0-1.fc42"},
		{name: "unrelated noise", output: "Updating repositories\n"},
		{name: "ambiguous", output: "CELIKPANEL_EVR:1.24.0-1.el9\nCELIKPANEL_EVR:1.26.0-1.el9\n", wantErr: true},
		{name: "invalid whitespace", output: "CELIKPANEL_EVR:1.24.0 bad-1.el9\n", wantErr: true},
		{name: "empty marker", output: "CELIKPANEL_EVR:\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDNFInstallCandidate([]byte(tt.output))
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("candidate = %q, err = %v; want %q, wantErr=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestDNFExecutableRequiresTrustedProfilePin(t *testing.T) {
	for _, tt := range []struct {
		name    string
		profile hostplatform.Profile
		want    string
		wantErr bool
	}{
		{
			name:    "current detector dnf",
			profile: hostplatform.Profile{PackageManager: hostplatform.PackageManagerDNF, Executables: map[string]string{"dnf": "/usr/bin/dnf"}},
			want:    "/usr/bin/dnf",
		},
		{
			name:    "future detector dnf5",
			profile: hostplatform.Profile{PackageManager: hostplatform.PackageManagerDNF, Executables: map[string]string{"dnf5": "/usr/bin/dnf5"}},
			want:    "/usr/bin/dnf5",
		},
		{
			name:    "missing pin",
			profile: hostplatform.Profile{PackageManager: hostplatform.PackageManagerDNF, Executables: map[string]string{}},
			wantErr: true,
		},
		{
			name:    "foreign family",
			profile: hostplatform.Profile{PackageManager: hostplatform.PackageManagerAPT, Executables: map[string]string{"dnf": "/usr/bin/dnf"}},
			wantErr: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dnfExecutableForProfile(tt.profile)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("executable = %q, err = %v; want %q, wantErr=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestGenericDNFMutationsRemainCertificationBlockedWithoutRunner(t *testing.T) {
	originalDetect := detectHostPlatform
	originalRunner := runDNFPreviewCommand
	defer func() {
		detectHostPlatform = originalDetect
		runDNFPreviewCommand = originalRunner
	}()
	detectHostPlatform = func() (hostplatform.Profile, error) {
		return hostplatform.Profile{
			PackageManager: hostplatform.PackageManagerDNF,
			Executables: map[string]string{
				"dnf": "/usr/bin/dnf",
				"rpm": "/usr/bin/rpm",
			},
		}, nil
	}
	runnerCalls := 0
	runDNFPreviewCommand = func(context.Context, []string, string, ...string) ([]byte, error) {
		runnerCalls++
		return []byte("unexpected mutation"), nil
	}

	installOutput, installErr := installPackagesWithCandidateContext(
		context.Background(),
		"dnf",
		[]string{"nginx"},
		"nginx",
	)
	if !errors.Is(installErr, errDNFMutationCertificationPending) || installOutput != "" {
		t.Fatalf("generic DNF install = (%q, %v), want certification-pending refusal", installOutput, installErr)
	}
	removeOutput, removeErr := removePackagesContext(context.Background(), "dnf", []string{"nginx"})
	if !errors.Is(removeErr, errDNFMutationCertificationPending) || removeOutput != "" {
		t.Fatalf("generic DNF remove = (%q, %v), want certification-pending refusal", removeOutput, removeErr)
	}
	if runnerCalls != 0 {
		t.Fatalf("generic DNF mutation invoked preview runner %d times", runnerCalls)
	}
}

func TestDNFPreviewPrimitivesRemainAuditedButUnreachable(t *testing.T) {
	originalDetect := detectHostPlatform
	originalRunner := runDNFPreviewCommand
	defer func() {
		detectHostPlatform = originalDetect
		runDNFPreviewCommand = originalRunner
	}()
	detectHostPlatform = func() (hostplatform.Profile, error) {
		return hostplatform.Profile{
			PackageManager: hostplatform.PackageManagerDNF,
			Executables:    map[string]string{"dnf": "/usr/bin/dnf"},
		}, nil
	}
	var calls [][]string
	runDNFPreviewCommand = func(_ context.Context, _ []string, executable string, args ...string) ([]byte, error) {
		call := append([]string{executable}, args...)
		calls = append(calls, call)
		if reflect.DeepEqual(args, dnfCandidateQueryArgs("nginx")) {
			return []byte("CELIKPANEL_EVR:1.24.0-1.el9\n"), nil
		}
		return []byte("ok"), nil
	}

	if _, err := installDNFPreviewPackagesContext(context.Background(), []string{"nginx"}, "nginx"); err != nil {
		t.Fatal(err)
	}
	if _, err := removeDNFPreviewPackagesContext(context.Background(), []string{"nginx"}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		append([]string{"/usr/bin/dnf"}, dnfMetadataRefreshArgs()...),
		append([]string{"/usr/bin/dnf"}, dnfCandidateQueryArgs("nginx")...),
		append([]string{"/usr/bin/dnf"}, dnfInstallArgs([]string{"nginx"})...),
		append([]string{"/usr/bin/dnf"}, dnfRemoveArgs([]string{"nginx"})...),
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("DNF preview calls = %q, want %q", calls, want)
	}
}

func TestDNFPreviewMutationGraphIsClosed(t *testing.T) {
	// Every reference must be a direct call from one of these exact internal
	// owners. Empty sets make both preview entry points definition-only. This
	// catches direct calls, function-value aliases, selectors and package-level
	// references instead of relying on source-text spelling.
	allowedCallers := map[string]map[string]bool{
		"installDNFPreviewPackagesContext": {},
		"removeDNFPreviewPackagesContext":  {},
		"refreshDNFMetadataWithExecutable": {
			"installDNFPreviewPackagesContext":  true,
			"dnfInstallCandidateWithExecutable": true,
		},
		"dnfInstallCandidateWithExecutable": {
			"installDNFPreviewPackagesContext": true,
		},
		"runDNFPreviewCommand": {
			"refreshDNFMetadataWithExecutable":  true,
			"dnfInstallCandidateWithExecutable": true,
			"installDNFPreviewPackagesContext":  true,
			"removeDNFPreviewPackagesContext":   true,
		},
	}
	expectedDeclarationKind := map[string]string{
		"installDNFPreviewPackagesContext":  "func",
		"removeDNFPreviewPackagesContext":   "func",
		"refreshDNFMetadataWithExecutable":  "func",
		"dnfInstallCandidateWithExecutable": "func",
		"runDNFPreviewCommand":              "var",
	}
	declarationCounts := make(map[string]int, len(allowedCallers))
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			switch decl := declaration.(type) {
			case *ast.FuncDecl:
				if kind, guarded := expectedDeclarationKind[decl.Name.Name]; guarded {
					if kind != "func" {
						t.Errorf("%s: %s must remain a %s declaration", name, decl.Name.Name, kind)
					}
					declarationCounts[decl.Name.Name]++
				}
				owner := decl.Name.Name
				auditDNFPreviewReferences(t, fset, name, owner+" signature", decl.Type, allowedCallers)
				auditDNFPreviewReferences(t, fset, name, owner, decl.Body, allowedCallers)
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch typed := spec.(type) {
					case *ast.ValueSpec:
						for _, ident := range typed.Names {
							if kind, guarded := expectedDeclarationKind[ident.Name]; guarded {
								if kind != decl.Tok.String() {
									t.Errorf("%s: %s must remain a %s declaration", name, ident.Name, kind)
								}
								declarationCounts[ident.Name]++
							}
						}
						auditDNFPreviewReferences(t, fset, name, "package scope", typed.Type, allowedCallers)
						for _, value := range typed.Values {
							auditDNFPreviewReferences(t, fset, name, "package scope", value, allowedCallers)
						}
					case *ast.TypeSpec:
						auditDNFPreviewReferences(t, fset, name, "package scope", typed.Type, allowedCallers)
					}
				}
			}
		}
	}
	for target := range allowedCallers {
		if declarationCounts[target] != 1 {
			t.Errorf("%s has %d production declarations; want exactly one", target, declarationCounts[target])
		}
	}
}

func auditDNFPreviewReferences(
	t *testing.T,
	fset *token.FileSet,
	fileName string,
	owner string,
	root ast.Node,
	allowedCallers map[string]map[string]bool,
) {
	t.Helper()
	if root == nil {
		return
	}
	var stack []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		var parent ast.Node
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		stack = append(stack, node)

		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		callers, guarded := allowedCallers[ident.Name]
		if !guarded {
			return true
		}
		position := fset.Position(ident.Pos())
		if !callers[owner] {
			t.Errorf("%s: forbidden production reference to %s from %s at %s", fileName, ident.Name, owner, position)
			return true
		}
		call, direct := parent.(*ast.CallExpr)
		if !direct || call.Fun != ident {
			t.Errorf("%s: %s reference from %s at %s must remain a direct call (no aliases or selectors)", fileName, ident.Name, owner, position)
		}
		return true
	})
}

func TestPackageInstalledForUnknownFamilyFailsClosed(t *testing.T) {
	if packageInstalledForFamily("", "bash") {
		t.Fatal("unknown package family must not be treated as apt")
	}
	if packageInstalledForFamily("windows", "bash") {
		t.Fatal("unsupported package family must fail closed")
	}
}

func TestPackageInstalledForFamilyRejectsInvalidPackageBeforeExec(t *testing.T) {
	for _, family := range []string{"apt", "pacman", "dnf"} {
		if packageInstalledForFamily(family, "--help") {
			t.Fatalf("family %q accepted an invalid package name", family)
		}
	}
}

func TestDetectPkgFamilyUsesVerifiedHostProfile(t *testing.T) {
	original := detectHostPlatform
	defer func() { detectHostPlatform = original }()
	detectHostPlatform = func() (hostplatform.Profile, error) {
		return hostplatform.Profile{PackageManager: hostplatform.PackageManagerAPT}, nil
	}
	if got := detectPkgFamily(); got != "apt" {
		t.Fatalf("detectPkgFamily() = %q, want apt", got)
	}
}

func TestPkgFamilySurfacesDetectionError(t *testing.T) {
	original := detectHostPlatform
	defer func() { detectHostPlatform = original }()
	detectHostPlatform = func() (hostplatform.Profile, error) {
		return hostplatform.Profile{}, errors.New("systemd offline")
	}
	var reply string
	err := (&Agent{}).PkgFamily(&transport.Empty{}, &reply)
	if err == nil || !strings.Contains(err.Error(), "systemd offline") {
		t.Fatalf("PkgFamily error = %v, want platform detection detail", err)
	}
	if reply != "" {
		t.Fatalf("PkgFamily reply = %q, want fail-closed empty value", reply)
	}
	if got := detectPkgFamily(); got != "" {
		t.Fatalf("compatibility family = %q, want fail-closed empty value", got)
	}
}
