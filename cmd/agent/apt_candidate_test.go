package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseAptInstallCandidate(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{name: "version", out: "postgresql-18:\n  Installed: (none)\n  Candidate: 18.0-1.pgdg13+3\n", want: "18.0-1.pgdg13+3"},
		{name: "none", out: "postgresql-18:\n  Installed: (none)\n  Candidate: (none)\n"},
		{name: "empty candidate", out: "Candidate:   \n"},
		{name: "missing candidate", out: "N: Unable to locate package postgresql-18\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseAptInstallCandidate([]byte(tt.out)); got != tt.want {
				t.Fatalf("parseAptInstallCandidate(...) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAptCandidateGuardPrecedesInstallMutation(t *testing.T) {
	source, err := os.ReadFile("pkg_rpc.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	start := strings.Index(body, "func installPackagesWithCandidateContext")
	end := strings.Index(body[start:], "func aptInstallCandidateContext")
	if start < 0 || end < 0 {
		t.Fatal("candidate-aware APT install function was not found")
	}
	body = body[start : start+end]
	lookup := strings.Index(body, "aptInstallCandidateContext(ctx, requiredCandidate)")
	rejection := strings.Index(body, "selected package %s has no APT installation candidate")
	mutation := strings.Index(body, `runServiceMutationCombinedOutputEnv(ctx, env, "apt-get", args...)`)
	if lookup < 0 || rejection < 0 || mutation < 0 {
		t.Fatalf("candidate lookup, rejection, or apt install mutation is missing")
	}
	if !(lookup < rejection && rejection < mutation) {
		t.Fatalf("APT candidate rejection must happen before apt-get install: lookup=%d rejection=%d mutation=%d", lookup, rejection, mutation)
	}
}

func TestSelectedPackageRepositoryGuardPrecedesInstallMutation(t *testing.T) {
	source, err := os.ReadFile("install_rpc.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	start := strings.Index(body, "func (a *Agent) InstallService")
	end := strings.Index(body[start:], "func (a *Agent) UninstallService")
	if start < 0 || end < 0 {
		t.Fatal("InstallService function was not found")
	}
	body = body[start : start+end]
	packageValidation := strings.Index(body, "validateRepoPackageSelection(svc, req.Package)")
	repositoryGuard := strings.Index(body, "core.InstallRequiresManagedRepository(svc, req.Package)")
	installMutation := strings.Index(body, "installPackagesWithCandidateContext(ctx, family, missingPackages")
	if packageValidation < 0 || repositoryGuard < 0 || installMutation < 0 {
		t.Fatalf("package validation, repository guard, or install mutation is missing")
	}
	if !(packageValidation < repositoryGuard && repositoryGuard < installMutation) {
		t.Fatalf("selected-package validation and repository guard must precede package mutation: validation=%d repository=%d mutation=%d", packageValidation, repositoryGuard, installMutation)
	}
}
