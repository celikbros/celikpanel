package core

import "testing"

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
