package core

import "testing"

func TestInstallRequiresManagedRepository(t *testing.T) {
	tests := []struct {
		name            string
		service         *ManagedService
		selectedPackage string
		want            bool
		wantErr         bool
	}{
		{name: "nil service"},
		{name: "no repository", service: &ManagedService{}},
		{name: "required repository", service: &ManagedService{Repo: &ManagedRepo{ID: "vendor", Required: true}}, want: true},
		{name: "optional repository distro default", service: &ManagedService{Repo: &ManagedRepo{ID: "pgdg", PackagePattern: `^postgresql-[0-9]+$`}}, selectedPackage: "postgresql"},
		{name: "optional repository exact vendor version", service: &ManagedService{Repo: &ManagedRepo{ID: "pgdg", PackagePattern: `^postgresql-[0-9]+$`}}, selectedPackage: "postgresql-18", want: true},
		{name: "partial match is rejected", service: &ManagedService{Repo: &ManagedRepo{ID: "vendor", PackagePattern: `postgresql-[0-9]+`}}, selectedPackage: "prefix-postgresql-18-suffix"},
		{name: "invalid trusted pattern fails closed", service: &ManagedService{Repo: &ManagedRepo{ID: "broken", PackagePattern: `(`}}, selectedPackage: "postgresql-18", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InstallRequiresManagedRepository(tt.service, tt.selectedPackage)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("InstallRequiresManagedRepository(...) = %v, want %v", got, tt.want)
			}
		})
	}
}
