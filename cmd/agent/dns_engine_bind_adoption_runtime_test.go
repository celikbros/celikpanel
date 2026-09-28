package main

import "testing"

func TestAdoptedBINDRuntimeTopologyExactVendorOrAbsentAlias(t *testing.T) {
	named := bindInstallUnitState{loadState: "loaded", activeState: "active", unitFileState: "enabled"}
	absent := bindInstallUnitState{loadState: "not-found", activeState: "inactive"}
	identity := canonicalAPTNamedIdentity()
	identity.Names = []string{"named.service"}
	files := bindVendorFilesIdentity{
		Unit:        bindSecureFileIdentity{Device: 1, Inode: 2, Size: 376},
		Environment: bindSecureFileIdentity{Device: 1, Inode: 3, Size: 86},
	}
	processes := dnsUnitProcesses{MainPID: 123, SubState: "running"}
	ops := bindUnitIdentityProofOps{
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) { return named, absent, nil },
		inspectIdentity: func(unit string) (dnsUnitIdentity, error) {
			if unit != "named.service" {
				t.Fatalf("unexpected unit: %s", unit)
			}
			return identity, nil
		},
		inspectVendorFiles: func() (bindVendorFilesIdentity, error) { return files, nil },
		inspectProcesses: func(unit string) (dnsUnitProcesses, error) {
			if unit != "named.service" {
				t.Fatalf("unexpected process unit: %s", unit)
			}
			return processes, nil
		},
	}
	if _, err := inspectVerifiedAdoptedBINDRuntimeTopologyWithOps(ops); err != nil {
		t.Fatalf("exact native owner BIND rejected: %v", err)
	}
	activeAlias := bindInstallUnitState{loadState: "loaded", activeState: "active", unitFileState: "enabled"}
	vendorIdentity := canonicalAPTNamedIdentity()
	vendorOps := ops
	vendorOps.inspectStates = func() (bindInstallUnitState, bindInstallUnitState, error) { return named, activeAlias, nil }
	vendorOps.inspectIdentity = func(unit string) (dnsUnitIdentity, error) {
		if unit != "named.service" && unit != "bind9.service" {
			t.Fatalf("unexpected unit: %s", unit)
		}
		return vendorIdentity, nil
	}
	vendorOps.inspectProcesses = func(unit string) (dnsUnitProcesses, error) {
		if unit != "named.service" && unit != "bind9.service" {
			t.Fatalf("unexpected process unit: %s", unit)
		}
		return processes, nil
	}
	if _, err := inspectVerifiedAdoptedBINDRuntimeTopologyWithOps(vendorOps); err != nil {
		t.Fatalf("exact active vendor alias rejected: %v", err)
	}
	t.Run("alias-process-differs", func(t *testing.T) {
		unsafe := vendorOps
		unsafe.inspectProcesses = func(unit string) (dnsUnitProcesses, error) {
			if unit == "bind9.service" {
				return dnsUnitProcesses{MainPID: 999, SubState: "running"}, nil
			}
			return processes, nil
		}
		if _, err := inspectVerifiedAdoptedBINDRuntimeTopologyWithOps(unsafe); err == nil {
			t.Fatal("different vendor alias process accepted")
		}
	})
	t.Run("alias-identity-differs", func(t *testing.T) {
		unsafe := vendorOps
		unsafe.inspectIdentity = func(unit string) (dnsUnitIdentity, error) {
			if unit == "bind9.service" {
				return identity, nil
			}
			return vendorIdentity, nil
		}
		if _, err := inspectVerifiedAdoptedBINDRuntimeTopologyWithOps(unsafe); err == nil {
			t.Fatal("different vendor alias identity accepted")
		}
	})
	for _, tc := range []struct {
		name  string
		alias bindInstallUnitState
	}{
		{"loaded-inactive-alias", bindInstallUnitState{loadState: "loaded", activeState: "inactive", unitFileState: "disabled"}},
		{"masked-alias", bindInstallUnitState{loadState: "masked", activeState: "inactive", unitFileState: "masked"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unsafe := ops
			unsafe.inspectStates = func() (bindInstallUnitState, bindInstallUnitState, error) { return named, tc.alias, nil }
			if _, err := inspectVerifiedAdoptedBINDRuntimeTopologyWithOps(unsafe); err == nil {
				t.Fatal("non-absent alias accepted")
			}
		})
	}
	t.Run("unit-state-moves", func(t *testing.T) {
		unsafe := ops
		calls := 0
		unsafe.inspectStates = func() (bindInstallUnitState, bindInstallUnitState, error) {
			calls++
			if calls == 2 {
				named.activeState = "inactive"
			}
			return named, absent, nil
		}
		if _, err := inspectVerifiedAdoptedBINDRuntimeTopologyWithOps(unsafe); err == nil {
			t.Fatal("moving named state accepted")
		}
	})
}
