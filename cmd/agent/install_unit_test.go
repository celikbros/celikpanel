package main

import "testing"

func TestExactInstallUnitForSelectedPostgreSQLMajor(t *testing.T) {
	unit, exact := exactInstallUnit("postgresql", "apt", "postgresql-17")
	if !exact || unit != "postgresql@17-main" {
		t.Fatalf("got (%q, %v), want exact postgresql@17-main", unit, exact)
	}

	unit, exact = exactInstallUnit("postgresql", "apt", "postgresql-17-client")
	if !exact || unit != "" {
		t.Fatalf("invalid selected package got (%q, %v), want exact rejection", unit, exact)
	}

	unit, exact = exactInstallUnit("php-fpm", "apt", "php8.3-fpm")
	if !exact || unit != "php8.3-fpm" {
		t.Fatalf("selected PHP package got (%q, %v), want exact php8.3-fpm", unit, exact)
	}

	unit, exact = exactInstallUnit("php-fpm", "pacman", "php8.3-fpm")
	if exact || unit != "" {
		t.Fatalf("non-apt package got (%q, %v), want normal fallback path", unit, exact)
	}
}
