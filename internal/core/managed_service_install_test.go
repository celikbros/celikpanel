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
		{"bind", "apt", ManagedServiceInstallBlockNone},
		{"bind", "pacman", ManagedServiceInstallBlockNone},
		{"exim", "apt", ManagedServiceInstallBlockIntegration},
		{"exim", "pacman", ManagedServiceInstallBlockIntegration},
		{"vsftpd", "apt", ManagedServiceInstallBlockIntegration},
		{"vsftpd", "pacman", ManagedServiceInstallBlockIntegration},
		{"nginx", "apt", ManagedServiceInstallBlockNone},
		{"nginx", "dnf", ManagedServiceInstallBlockDistribution},
		{"pdns", "apt", ManagedServiceInstallBlockNone},
		{"pdns", "pacman", ManagedServiceInstallBlockDistribution},
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

func TestPowerDNSPacmanPackagesRemainObservableButLifecycleIsClosed(t *testing.T) {
	service := GetManagedServiceByID("pdns")
	if service == nil {
		t.Fatal("PowerDNS is missing from the managed-service catalogue")
	}
	if got := service.Packages["pacman"]; len(got) != 1 || got[0] != "powerdns" {
		t.Fatalf("PowerDNS pacman observation packages = %v, want [powerdns]", got)
	}
	if len(service.LifecycleInstallFamilies) != 1 || service.LifecycleInstallFamilies["pacman"] {
		t.Fatal("PowerDNS pacman lifecycle was accidentally certified")
	}
	if !service.LifecycleInstallFamilies["apt"] {
		t.Fatal("PowerDNS APT lifecycle certification is missing")
	}

	bind := GetManagedServiceByID("bind")
	if bind == nil {
		t.Fatal("BIND is missing from the managed-service catalogue")
	}
	if len(bind.LifecycleInstallFamilies) != 2 ||
		!bind.LifecycleInstallFamilies["apt"] ||
		!bind.LifecycleInstallFamilies["pacman"] {
		t.Fatalf("BIND lifecycle families = %v, want explicit apt and pacman", bind.LifecycleInstallFamilies)
	}
}

func TestRHELPreviewNginxCandidateUsesVerifiedCapabilityNotDistroMetadata(t *testing.T) {
	base := ManagedServiceHostProfile{
		DistroFamily:   "rhel",
		PackageFamily:  "dnf",
		ServiceManager: "systemd",
		DistroID:       "examplelinux",
		VersionID:      "2026.8",
		Architecture:   "amd64",
	}
	tests := []struct {
		name   string
		change func(*ManagedServiceHostProfile)
		want   bool
	}{
		{name: "unknown metadata on amd64", want: true},
		{name: "different metadata on arm64", change: func(h *ManagedServiceHostProfile) {
			h.DistroID, h.VersionID, h.Architecture = "anotherlinux", "rolling", "arm64"
		}, want: true},
		{name: "family only", change: func(h *ManagedServiceHostProfile) {
			h.DistroFamily = ""
		}},
		{name: "wrong family", change: func(h *ManagedServiceHostProfile) { h.DistroFamily = "debian" }},
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
		DistroID:       "customlinux",
		VersionID:      "rolling",
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
		DistroID:       "customlinux",
		VersionID:      "rolling",
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
