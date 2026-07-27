package core

import "testing"

func TestArchDatabaseInstallPathsAreOffered(t *testing.T) {
	for id, wantPackage := range map[string]string{
		"postgresql": "postgresql",
		"mariadb":    "mariadb",
	} {
		svc := GetManagedServiceByID(id)
		if svc == nil {
			t.Fatalf("%s missing from catalogue", id)
		}
		if got := svc.Packages["pacman"]; len(got) != 1 || got[0] != wantPackage {
			t.Errorf("%s pacman packages = %v, want [%s]", id, got, wantPackage)
		}
	}
}

func TestClamAVUpdaterIsAHelperUnit(t *testing.T) {
	svc := GetManagedServiceByID("clamav")
	if svc == nil {
		t.Fatal("clamav missing from catalogue")
	}
	if len(svc.SystemNames) != 1 || svc.SystemNames[0] != "clamav-daemon" {
		t.Fatalf("clamav primary units = %v, want [clamav-daemon]", svc.SystemNames)
	}
	if len(svc.HelperUnits) != 1 || svc.HelperUnits[0] != "clamav-freshclam" {
		t.Fatalf("clamav helper units = %v, want [clamav-freshclam]", svc.HelperUnits)
	}
}
