//go:build linux

package main

import "testing"

func TestFreshPDNSTargetPackageMaskProvenanceV3(t *testing.T) {
	absent := dnsUnitSnapshot{Name: "pdns.service", LoadState: "not-found", ActiveState: "inactive"}
	disabled := dnsUnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}
	masked := dnsUnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}
	cases := []struct {
		name               string
		before, after      dnsUnitSnapshot
		installedNow, want bool
	}{
		{"new-install-owned-mask", absent, masked, true, true},
		{"partial-install-owned-mask", disabled, masked, true, true},
		{"existing-package-unchanged", disabled, disabled, false, true},
		{"owner-mask-before-install", masked, masked, true, false},
		{"owner-mask-without-install", masked, masked, false, false},
		{"unsealed-install", absent, disabled, true, false},
		{"unexpected-mask-without-install", disabled, masked, false, false},
		{"active-owned-mask", absent, dnsUnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "active", UnitFileState: "masked"}, true, false},
		{"runtime-mask", absent, dnsUnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked-runtime"}, true, false},
		{"enabled-preimage", dnsUnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "enabled"}, masked, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFreshPDNSTargetAfterPackagesV3(tc.before, tc.after, tc.installedNow)
			if (err == nil) != tc.want {
				t.Fatalf("accepted=%t, want %t: %v", err == nil, tc.want, err)
			}
		})
	}
}
