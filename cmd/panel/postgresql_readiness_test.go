package main

import "testing"

func TestManagedServiceUnitReadyPostgreSQLLayouts(t *testing.T) {
	tests := []struct {
		name      string
		serviceID string
		family    string
		unit      string
		status    string
		want      bool
	}{
		{
			name:      "apt aggregate wrapper is not a healthy cluster",
			serviceID: "postgresql",
			family:    "apt",
			unit:      "postgresql",
			status:    "active (exited)",
			want:      false,
		},
		{
			name:      "apt active cluster proves readiness",
			serviceID: "postgresql",
			family:    "apt",
			unit:      "postgresql@17-main",
			status:    "active (running)",
			want:      true,
		},
		{
			name:      "apt inactive cluster is not ready",
			serviceID: "postgresql",
			family:    "apt",
			unit:      "postgresql@16-main",
			status:    "inactive (dead)",
			want:      false,
		},
		{
			name:      "arch unversioned unit is the real daemon",
			serviceID: "postgresql",
			family:    "pacman",
			unit:      "postgresql",
			status:    "active (running)",
			want:      true,
		},
		{
			name:      "other service keeps normal active rule",
			serviceID: "nginx",
			family:    "apt",
			unit:      "nginx",
			status:    "active (running)",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := managedServiceUnitReady(tt.serviceID, tt.family, tt.unit, tt.status); got != tt.want {
				t.Fatalf("managedServiceUnitReady(%q, %q, %q, %q) = %v, want %v",
					tt.serviceID, tt.family, tt.unit, tt.status, got, tt.want)
			}
		})
	}
}
