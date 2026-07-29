package main

import "testing"

func TestSystemSQLiteRoutesAreAdministratorOnly(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v1/system-databases",
		"/api/v1/system-databases/panel/check",
		"/api/v1/system-databases/powerdns/snapshot",
	} {
		if !isAdminOnlyPath(path) {
			t.Fatalf("isAdminOnlyPath(%q) = false, want true", path)
		}
	}

	if isAdminOnlyPath("/api/v1/system-databases-lookalike") {
		t.Fatal("lookalike route unexpectedly received administrator-only policy")
	}
}
