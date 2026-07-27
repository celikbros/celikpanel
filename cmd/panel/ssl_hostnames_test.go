package main

import (
	"reflect"
	"testing"
)

func TestNormalizeManagedHostnamesKeepsPrimaryAndDeduplicates(t *testing.T) {
	got, err := normalizeManagedHostnames([]string{
		"Example.COM.",
		"www.example.com",
		"shop.example.com",
		"WWW.EXAMPLE.COM.",
	})
	if err != nil {
		t.Fatalf("normalizeManagedHostnames() error = %v", err)
	}
	want := []string{"example.com", "shop.example.com", "www.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeManagedHostnames() = %#v, want %#v", got, want)
	}
}

func TestNormalizeManagedHostnamesRejectsNginxInjection(t *testing.T) {
	if _, err := normalizeManagedHostnames([]string{"example.com", "bad.example.com; return 200"}); err == nil {
		t.Fatal("normalizeManagedHostnames() accepted an unsafe hostname")
	}
}

func TestCertificateCoversHostname(t *testing.T) {
	tests := []struct {
		name     string
		dnsNames []string
		host     string
		want     bool
	}{
		{name: "exact", dnsNames: []string{"example.com"}, host: "EXAMPLE.COM.", want: true},
		{name: "wildcard one label", dnsNames: []string{"*.example.com"}, host: "mail.example.com", want: true},
		{name: "wildcard not apex", dnsNames: []string{"*.example.com"}, host: "example.com", want: false},
		{name: "wildcard not two labels", dnsNames: []string{"*.example.com"}, host: "a.b.example.com", want: false},
		{name: "uncovered", dnsNames: []string{"example.com"}, host: "www.example.com", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := certificateCoversHostname(tt.dnsNames, tt.host); got != tt.want {
				t.Fatalf("certificateCoversHostname(%v, %q) = %v, want %v", tt.dnsNames, tt.host, got, tt.want)
			}
		})
	}
}

func TestValidateIssuedCertificateLineage(t *testing.T) {
	if err := validateIssuedCertificateLineage(
		"example.test", 42, false, "example.test",
	); err != nil {
		t.Fatalf("canonical initial lineage was rejected: %v", err)
	}
	if err := validateIssuedCertificateLineage(
		"example.test", 42, true,
		"cp-site-42-00112233445566778899aabb",
	); err != nil {
		t.Fatalf("valid staged lineage was rejected: %v", err)
	}

	for _, test := range []struct {
		replacement bool
		lineage     string
	}{
		{replacement: false, lineage: ""},
		{replacement: false, lineage: "cp-site-42-00112233445566778899aabb"},
		{replacement: false, lineage: "example.test "},
		{replacement: true, lineage: "example.test"},
		{replacement: true, lineage: "cp-site-41-00112233445566778899aabb"},
		{replacement: true, lineage: "cp-site-420-00112233445566778899aabb"},
		{replacement: true, lineage: "cp-site-42-00112233445566778899aab"},
		{replacement: true, lineage: "cp-site-42-00112233445566778899AABB"},
	} {
		if err := validateIssuedCertificateLineage(
			"example.test", 42, test.replacement, test.lineage,
		); err == nil {
			t.Fatalf(
				"unsafe lineage %q (replacement=%t) was accepted",
				test.lineage, test.replacement,
			)
		}
	}
}
