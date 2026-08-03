package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

func candidateTestService(t *testing.T) *core.ManagedService {
	t.Helper()
	svc := core.GetManagedServiceByID("nginx")
	if svc == nil {
		t.Fatal("nginx is missing from the managed-service catalog")
	}
	return svc
}

func TestCandidateVersionForServiceReturnsVerifiedAptCandidate(t *testing.T) {
	version, err := candidateVersionForService(candidateTestService(t), "apt", func(name string, args ...string) ([]byte, error) {
		if name != "apt-cache" || strings.Join(args, " ") != "policy nginx" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return []byte("Installed: (none)\nCandidate: 2:1.24.0-2ubuntu7.3\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.24.0" {
		t.Fatalf("version = %q, want 1.24.0", version)
	}
}

func TestCandidateVersionForServiceFailsClosedForUnsupportedFamily(t *testing.T) {
	for _, family := range []string{"", "dnf", "pacman", "windows"} {
		t.Run(family, func(t *testing.T) {
			called := false
			_, err := candidateVersionForService(candidateTestService(t), family, func(string, ...string) ([]byte, error) {
				called = true
				return nil, nil
			})
			if err == nil || called {
				t.Fatalf("family %q: err=%v commandCalled=%v", family, err, called)
			}
		})
	}
}

func TestCandidateVersionForServicePropagatesCommandFailure(t *testing.T) {
	want := errors.New("apt cache unavailable")
	_, err := candidateVersionForService(candidateTestService(t), "apt", func(string, ...string) ([]byte, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped %v", err, want)
	}
}

func TestCandidateVersionForServiceRejectsMissingCandidate(t *testing.T) {
	for name, output := range map[string]string{
		"missing": "Installed: (none)\n",
		"none":    "Candidate: (none)\n",
		"empty":   "Candidate:   \n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := candidateVersionForService(candidateTestService(t), "apt", func(string, ...string) ([]byte, error) {
				return []byte(output), nil
			})
			if err == nil {
				t.Fatal("missing candidate returned nil error")
			}
		})
	}
}

func TestServiceCandidateVersionRejectsInvalidRPCInputs(t *testing.T) {
	agent := &Agent{}
	var reply string
	if err := agent.ServiceCandidateVersion(nil, &reply); err == nil {
		t.Fatal("nil request returned nil error")
	}
	if err := agent.ServiceCandidateVersion(&InstallServiceRequest{ID: "nginx"}, nil); err == nil {
		t.Fatal("nil reply returned nil error")
	}
	if err := agent.ServiceCandidateVersion(&InstallServiceRequest{ID: "not-in-catalog"}, &reply); err == nil {
		t.Fatal("unknown service returned nil error")
	}
}
