package core

import "testing"

func TestManagedServiceInstallDisabledReason(t *testing.T) {
	tests := []struct {
		id, family string
		blocked    bool
	}{
		{"apache", "apt", true},
		{"apache", "pacman", true},
		{"bind", "apt", true},
		{"bind", "pacman", true},
		{"exim", "apt", true},
		{"exim", "pacman", true},
		{"vsftpd", "apt", true},
		{"vsftpd", "pacman", true},
		{"nginx", "apt", false},
		{"pdns", "pacman", false},
		{"postfix", "apt", false},
		{"spamassassin", "pacman", true},
		{"roundcube", "pacman", false},
		{"node", "apt", false},
	}
	for _, tt := range tests {
		t.Run(tt.id+"/"+tt.family, func(t *testing.T) {
			got := ManagedServiceInstallDisabledReason(GetManagedServiceByID(tt.id), tt.family)
			if (got != "") != tt.blocked {
				t.Fatalf("disabled reason = %q, blocked = %v", got, tt.blocked)
			}
		})
	}
}
