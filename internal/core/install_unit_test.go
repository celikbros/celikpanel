package core

import "testing"

func TestPostgreSQLClusterUnitForPackage(t *testing.T) {
	tests := []struct {
		packageName string
		wantUnit    string
		wantOK      bool
	}{
		{packageName: "postgresql-17", wantUnit: "postgresql@17-main", wantOK: true},
		{packageName: "postgresql-18", wantUnit: "postgresql@18-main", wantOK: true},
		{packageName: "postgresql-16", wantUnit: "postgresql@16-main", wantOK: true},
		{packageName: "postgresql", wantOK: false},
		{packageName: "postgresql-17-client", wantOK: false},
		{packageName: "postgresql 17", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.packageName, func(t *testing.T) {
			gotUnit, gotOK := PostgreSQLClusterUnitForPackage(tt.packageName)
			if gotUnit != tt.wantUnit || gotOK != tt.wantOK {
				t.Fatalf("PostgreSQLClusterUnitForPackage(%q) = (%q, %v), want (%q, %v)",
					tt.packageName, gotUnit, gotOK, tt.wantUnit, tt.wantOK)
			}
		})
	}
}
