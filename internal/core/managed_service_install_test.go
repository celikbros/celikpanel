package core

import (
	"strings"
	"testing"
)

func TestManagedServiceInstallDisabledReason(t *testing.T) {
	tests := []struct {
		id, family string
		want       ManagedServiceInstallBlockKind
	}{
		{"apache", "apt", ManagedServiceInstallBlockIntegration},
		{"apache", "pacman", ManagedServiceInstallBlockIntegration},
		{"bind", "apt", ManagedServiceInstallBlockIntegration},
		{"bind", "pacman", ManagedServiceInstallBlockIntegration},
		{"exim", "apt", ManagedServiceInstallBlockIntegration},
		{"exim", "pacman", ManagedServiceInstallBlockIntegration},
		{"vsftpd", "apt", ManagedServiceInstallBlockIntegration},
		{"vsftpd", "pacman", ManagedServiceInstallBlockIntegration},
		{"nginx", "apt", ManagedServiceInstallBlockNone},
		{"nginx", "dnf", ManagedServiceInstallBlockDistribution},
		{"pdns", "pacman", ManagedServiceInstallBlockNone},
		{"postfix", "apt", ManagedServiceInstallBlockNone},
		{"spamassassin", "pacman", ManagedServiceInstallBlockDistribution},
		{"roundcube", "pacman", ManagedServiceInstallBlockNone},
		{"node", "apt", ManagedServiceInstallBlockNone},
	}
	for _, tt := range tests {
		t.Run(tt.id+"/"+tt.family, func(t *testing.T) {
			kind, reason := ManagedServiceInstallBlock(GetManagedServiceByID(tt.id), tt.family)
			if kind != tt.want {
				t.Fatalf("block kind = %q, want %q (reason %q)", kind, tt.want, reason)
			}
			if (reason == "") != (kind == ManagedServiceInstallBlockNone) {
				t.Fatalf("block kind = %q with inconsistent reason %q", kind, reason)
			}
		})
	}
}

func TestRHELPreviewNginxCandidateRequiresExactCertifiedIdentity(t *testing.T) {
	base := ManagedServiceHostProfile{
		DistroFamily:   "rhel",
		PackageFamily:  "dnf",
		ServiceManager: "systemd",
		DistroID:       "almalinux",
		VersionID:      "9.6",
		Architecture:   "amd64",
	}
	tests := []struct {
		name   string
		change func(*ManagedServiceHostProfile)
		want   bool
	}{
		{name: "AlmaLinux 9 amd64", want: true},
		{name: "Rocky Linux 9 arm64", change: func(h *ManagedServiceHostProfile) {
			h.DistroID, h.Architecture = "rocky", "arm64"
		}, want: true},
		{name: "family only", change: func(h *ManagedServiceHostProfile) {
			h.DistroFamily, h.DistroID, h.VersionID = "", "", ""
		}},
		{name: "RHEL", change: func(h *ManagedServiceHostProfile) { h.DistroID = "rhel" }},
		{name: "Fedora", change: func(h *ManagedServiceHostProfile) { h.DistroID = "fedora" }},
		{name: "CentOS Stream", change: func(h *ManagedServiceHostProfile) { h.DistroID = "centos" }},
		{name: "CloudLinux", change: func(h *ManagedServiceHostProfile) { h.DistroID = "cloudlinux" }},
		{name: "AlmaLinux 8", change: func(h *ManagedServiceHostProfile) { h.VersionID = "8.10" }},
		{name: "Rocky Linux 10", change: func(h *ManagedServiceHostProfile) {
			h.DistroID, h.VersionID = "rocky", "10.0"
		}},
		{name: "stream-like version", change: func(h *ManagedServiceHostProfile) { h.VersionID = "9-stream" }},
		{name: "missing version", change: func(h *ManagedServiceHostProfile) { h.VersionID = "" }},
		{name: "unsupported 386", change: func(h *ManagedServiceHostProfile) { h.Architecture = "386" }},
		{name: "unsupported custom arch", change: func(h *ManagedServiceHostProfile) { h.Architecture = "s390x" }},
		{name: "wrong package manager", change: func(h *ManagedServiceHostProfile) { h.PackageFamily = "apt" }},
		{name: "wrong service manager", change: func(h *ManagedServiceHostProfile) { h.ServiceManager = "openrc" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := base
			if test.change != nil {
				test.change(&host)
			}
			if got := IsRHELPreviewNginxCandidate(host); got != test.want {
				t.Fatalf("candidate(%+v) = %v, want %v", host, got, test.want)
			}
		})
	}
}

func TestRHELPreviewRemainsClosedEvenForQualifiedNginxCandidate(t *testing.T) {
	host := ManagedServiceHostProfile{
		DistroFamily:   "rhel",
		PackageFamily:  "dnf",
		ServiceManager: "systemd",
		DistroID:       "rocky",
		VersionID:      "9.6",
		Architecture:   "amd64",
	}
	kind, reason := ManagedServiceInstallBlockForHost(GetManagedServiceByID("nginx"), host)
	if kind != ManagedServiceInstallBlockDistribution {
		t.Fatalf("qualified candidate block kind = %q, want distribution", kind)
	}
	for _, required := range []string{"platform-capability firewall", "SELinux"} {
		if !strings.Contains(reason, required) {
			t.Fatalf("qualified candidate reason %q does not name %q", reason, required)
		}
	}
	if packages := GetManagedServiceByID("nginx").Packages["dnf"]; len(packages) != 0 {
		t.Fatalf("RHEL preview was accidentally enabled through dnf packages: %v", packages)
	}
}

func TestRHELPreviewBlocksEveryNonNginxComponentIncludingPortableInstallers(t *testing.T) {
	host := ManagedServiceHostProfile{
		DistroFamily:   "rhel",
		PackageFamily:  "dnf",
		ServiceManager: "systemd",
		DistroID:       "almalinux",
		VersionID:      "9.6",
		Architecture:   "arm64",
	}
	for i := range ManagedServices {
		service := &ManagedServices[i]
		if service.ID == "nginx" {
			continue
		}
		kind, reason := ManagedServiceInstallBlockForHost(service, host)
		if kind == ManagedServiceInstallBlockNone || reason == "" {
			t.Errorf("%s became installable on RHEL preview: kind=%q reason=%q", service.ID, kind, reason)
		}
	}
}
