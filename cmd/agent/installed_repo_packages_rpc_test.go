package main

import (
	"reflect"
	"regexp"
	"testing"
)

func TestInstalledPackagesMatchingPattern(t *testing.T) {
	pattern := regexp.MustCompile(`^postgresql-[0-9]+$`)
	input := "postgresql-17\tii \n" +
		"postgresql-client-17\tii \n" +
		"postgresql-16\trc \n" +
		"postgresql-15\tiF \n" +
		"postgresql-17\tii \n" +
		"postgresql-14\tiU \n" +
		"postgresql-13\tic \n" +
		"foreign-package\tii \n"

	want := []string{"postgresql-17", "postgresql-15", "postgresql-14"}
	if got := installedPackagesMatchingPattern(input, pattern); !reflect.DeepEqual(got, want) {
		t.Fatalf("installed packages = %v, want %v", got, want)
	}
}

func TestInstalledPackagesMatchingPatternRejectsForeignAndUnmanaged(t *testing.T) {
	pattern := regexp.MustCompile(`^(php[0-9]+\.[0-9]+)-fpm$`)
	input := "php8.3-fpm\trc \n" +
		"php8.3-cli\tii \n" +
		"php-fpm\tii \n" +
		"nginx\tii \n"

	if got := installedPackagesMatchingPattern(input, pattern); len(got) != 0 {
		t.Fatalf("installed packages = %v, want none", got)
	}
}

func TestInstalledRPMPackagesMatchingPattern(t *testing.T) {
	pattern := regexp.MustCompile(`^postgresql[0-9]+-server$`)
	input := "CELIKPANEL_PACKAGE:postgresql17-server\n" +
		"CELIKPANEL_PACKAGE:postgresql16-libs\n" +
		"subscription-manager notice\n" +
		"CELIKPANEL_PACKAGE:postgresql15-server\n" +
		"CELIKPANEL_PACKAGE:postgresql17-server\n" +
		"CELIKPANEL_PACKAGE:--invalid\n"

	want := []string{"postgresql17-server", "postgresql15-server"}
	if got := installedRPMPackagesMatchingPattern(input, pattern); !reflect.DeepEqual(got, want) {
		t.Fatalf("installed RPM packages = %v, want %v", got, want)
	}
	if got := installedRPMPackagesMatchingPattern(input, nil); len(got) != 0 {
		t.Fatalf("nil pattern returned RPM packages: %v", got)
	}
}

func TestRPMInventoryArgsUseOnlyFixedQueryFormat(t *testing.T) {
	want := []string{"-qa", "--qf", "CELIKPANEL_PACKAGE:%{NAME}\n"}
	if got := rpmInventoryArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RPM inventory args = %q, want %q", got, want)
	}
}

func TestDpkgRecoverableInstallState(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{status: "ii ", want: true},
		{status: "iU ", want: true},
		{status: "iF ", want: true},
		{status: "iH ", want: true},
		{status: "iW ", want: true},
		{status: "iT ", want: true},
		{status: "it ", want: true},
		{status: "in ", want: false},
		{status: "ic ", want: false},
		{status: "rc ", want: false},
		{status: "pn ", want: false},
		{status: "hi ", want: false},
		{status: "i", want: false},
		{status: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := dpkgRecoverableInstallState(tt.status); got != tt.want {
				t.Fatalf("dpkgRecoverableInstallState(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
