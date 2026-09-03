package main

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func ownerPolicyTestSnapshot(
	path string,
	exists bool,
	mode, uid, gid uint32,
	ownerKnown bool,
) dnsFileSnapshot {
	snapshot := dnsFileSnapshot{
		Path: path, Exists: exists, Mode: mode,
		OwnerKnown: ownerKnown, UID: uid, GID: gid,
	}
	if exists {
		snapshot.Data = []byte("managed\n")
		snapshot.SHA256 = digestDNSBytes(snapshot.Data)
	}
	return snapshot
}

func TestBINDConfigOwnerPolicyIsExactForAPTAndPacman(t *testing.T) {
	aptPaths := []string{
		"/etc/bind/named.conf.local", "/etc/bind/named.conf.options",
	}
	apt := bindConfigOwnerPolicy{paths: aptPaths, apt: true, bindGID: 109}
	pair := func(gid uint32) []dnsFileSnapshot {
		return []dnsFileSnapshot{
			ownerPolicyTestSnapshot(aptPaths[0], true, 0o644, 0, gid, true),
			ownerPolicyTestSnapshot(aptPaths[1], true, 0o644, 0, gid, true),
		}
	}
	for _, gid := range []uint32{0, 109} {
		if err := apt.validateSnapshots(pair(gid)); err != nil {
			t.Fatalf("APT gid %d rejected: %v", gid, err)
		}
	}
	tests := []struct {
		name      string
		snapshots func() []dnsFileSnapshot
	}{
		{name: "mixed-gid", snapshots: func() []dnsFileSnapshot {
			value := pair(109)
			value[1].GID = 0
			return value
		}},
		{name: "foreign-gid", snapshots: func() []dnsFileSnapshot { return pair(110) }},
		{name: "nonroot-uid", snapshots: func() []dnsFileSnapshot {
			value := pair(109)
			value[0].UID = 1
			return value
		}},
		{name: "wrong-mode", snapshots: func() []dnsFileSnapshot {
			value := pair(109)
			value[0].Mode = 0o640
			return value
		}},
		{name: "wrong-path", snapshots: func() []dnsFileSnapshot {
			value := pair(109)
			value[0].Path = "/tmp/named.conf.local"
			return value
		}},
		{name: "absent", snapshots: func() []dnsFileSnapshot {
			value := pair(109)
			value[0] = ownerPolicyTestSnapshot(aptPaths[0], false, 0, 0, 0, false)
			return value
		}},
		{name: "owner-known-mismatch", snapshots: func() []dnsFileSnapshot {
			value := pair(109)
			value[1].OwnerKnown = false
			value[1].GID = 0
			return value
		}},
		{name: "gid-over-max-int32", snapshots: func() []dnsFileSnapshot {
			return pair(uint32(1 << 31))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := apt.validateSnapshots(test.snapshots()); err == nil {
				t.Fatal("unsafe APT BIND snapshot set was accepted")
			}
		})
	}

	// The pacman bind package ships /etc/named.conf as root:named 0640
	// (measured on Arch; register R-018). That exact shape is the contract.
	// pacman bind paketi /etc/named.conf'u root:named 0640 gönderir (Arch'ta
	// ölçüldü; defter R-018). Sözleşme tam olarak bu biçimdir.
	pacman := bindConfigOwnerPolicy{paths: []string{"/etc/named.conf"}, pacman: true, bindGID: 40, mode: 0o640}
	vendor := []dnsFileSnapshot{ownerPolicyTestSnapshot("/etc/named.conf", true, 0o640, 0, 40, true)}
	if err := pacman.validateSnapshots(vendor); err != nil {
		t.Fatalf("Pacman root:named 0640 rejected: %v", err)
	}
	for name, mutate := range map[string]func(*dnsFileSnapshot){
		"root:root":     func(s *dnsFileSnapshot) { s.GID = 0 },
		"foreign group": func(s *dnsFileSnapshot) { s.GID = 109 },
		"world-read":    func(s *dnsFileSnapshot) { s.Mode = 0o644 },
		"nonroot uid":   func(s *dnsFileSnapshot) { s.UID = 1 },
	} {
		t.Run("pacman "+name, func(t *testing.T) {
			value := ownerPolicyTestSnapshot("/etc/named.conf", true, 0o640, 0, 40, true)
			mutate(&value)
			if err := pacman.validateSnapshots([]dnsFileSnapshot{value}); err == nil {
				t.Fatal("unsafe pacman BIND config snapshot was accepted")
			}
		})
	}
}

func TestResolvedBINDConfigOwnerPolicyUsesTheLayoutsDaemonGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		// bindConfigPaths cleans with the host separator; the layouts are
		// Linux paths and the product only runs there.
		// bindConfigPaths konağın ayırıcısıyla temizler; yerleşimler Linux
		// yoludur ve ürün yalnız orada çalışır.
		t.Skip("Linux path layout")
	}
	pacmanLayout := bindHostLayout{OptionsConfig: "/etc/named.conf", AnchorConfig: "/etc/named.conf"}
	policy, err := resolveBINDConfigOwnerPolicyWithResolver(
		context.Background(), pacmanLayout,
		func(context.Context) (uint32, error) { return 40, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.pacman || policy.apt || policy.bindGID != 40 || policy.fileMode() != 0o640 {
		t.Fatalf("pacman policy=%+v", policy)
	}
	aptLayout := bindHostLayout{OptionsConfig: "/etc/bind/named.conf.options", AnchorConfig: "/etc/bind/named.conf.local"}
	policy, err = resolveBINDConfigOwnerPolicyWithResolver(
		context.Background(), aptLayout,
		func(context.Context) (uint32, error) { return 109, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.apt || policy.pacman || policy.bindGID != 109 || policy.fileMode() != 0o644 {
		t.Fatalf("apt policy=%+v", policy)
	}
	if bindVendorConfigMode(pacmanLayout) != 0o640 || bindVendorConfigMode(aptLayout) != 0o644 {
		t.Fatal("vendor config mode does not follow the layout")
	}
}

func TestResolvedBINDConfigOwnerPolicyRejectsUnsafeGIDBounds(t *testing.T) {
	layout := bindHostLayout{
		OptionsConfig: "/etc/bind/named.conf.options",
		AnchorConfig:  "/etc/bind/named.conf.local",
	}
	for _, gid := range []uint32{0, uint32(1 << 31), ^uint32(0)} {
		if _, err := resolveBINDConfigOwnerPolicyWithResolver(
			context.Background(), layout,
			func(context.Context) (uint32, error) { return gid, nil },
		); err == nil {
			t.Fatalf("unsafe resolved BIND gid %d was accepted", gid)
		}
	}
}

func TestGenericDNSFileSnapshotValidationRemainsRootRoot(t *testing.T) {
	snapshot := ownerPolicyTestSnapshot("/etc/example.conf", true, 0o644, 0, 109, true)
	if err := validateDNSFileSnapshot(snapshot); err == nil {
		t.Fatal("generic DNS snapshot accepted root:bind ownership")
	}
}

func TestBINDRecoveryProvesConfigBeforeRestorePointer(t *testing.T) {
	called := false
	err := restoreBINDPointerAfterConfigProof(
		func() error { return errors.New("foreign BIND config owner") },
		func() error {
			called = true
			return nil
		},
	)
	if err == nil || called {
		t.Fatalf("RestorePointer ran before owner proof: called=%v err=%v", called, err)
	}
	var order []string
	if err := restoreBINDPointerAfterConfigProof(
		func() error {
			order = append(order, "proof")
			return nil
		},
		func() error {
			order = append(order, "pointer")
			return nil
		},
	); err != nil || !reflect.DeepEqual(order, []string{"proof", "pointer"}) {
		t.Fatalf("recovery order=%v err=%v", order, err)
	}
}

func TestBINDRecoveryJournalReconstructsOriginalAndDesiredConfigs(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	paths := []string{
		"/etc/bind/named.conf.local", "/etc/bind/named.conf.options",
	}
	journal.ConfigBefore = []dnsFileSnapshot{
		ownerPolicyTestSnapshot(paths[0], true, 0o644, 0, 109, true),
		ownerPolicyTestSnapshot(paths[1], true, 0o644, 0, 109, true),
	}
	journal.ConfigBefore[0].Data = []byte("// local\n")
	journal.ConfigBefore[0].SHA256 = digestDNSBytes(journal.ConfigBefore[0].Data)
	journal.ConfigBefore[1].Data = []byte("options { };\n")
	journal.ConfigBefore[1].SHA256 = digestDNSBytes(journal.ConfigBefore[1].Data)
	layout := bindHostLayout{
		GenerationRoot: "/var/cache/bind/celikpanel",
		OptionsConfig:  paths[1], AnchorConfig: paths[0],
	}
	mutation, err := bindConfigMutationFromJournal(layout, "", journal)
	if err != nil {
		t.Fatal(err)
	}
	if !mutation.ownerAware || len(mutation.desired) != 2 {
		t.Fatalf("journal mutation was not owner-aware and complete: %#v", mutation)
	}
	for _, path := range mutation.paths {
		if len(mutation.original[path]) == 0 || len(mutation.desired[path]) == 0 {
			t.Fatalf("journal path %s lacks original or desired bytes", path)
		}
	}
}

func TestBINDJournalRejectsMixedOwnershipEvidence(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	journal.ConfigBefore = []dnsFileSnapshot{
		ownerPolicyTestSnapshot("/etc/bind/named.conf.local", true, 0o644, 0, 109, true),
		ownerPolicyTestSnapshot("/etc/bind/named.conf.options", true, 0o644, 0, 0, true),
	}
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("BIND journal accepted mixed root:bind and root:root snapshots")
	}
	journal.ConfigBefore[1].GID = 109
	journal.ConfigBefore[1].OwnerKnown = false
	if _, err := encodeDNSEngineSwitchJournal(journal); err == nil {
		t.Fatal("BIND journal accepted mixed ownership knowledge")
	}
}

func TestBINDRollbackFailureRetainsRollingJournal(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	writes := 0
	removed := false
	verified := false
	err := runBINDRollbackWithJournal(&journal, bindSwitchRollbackJournalOps{
		write: func(current dnsEngineSwitchJournal) error {
			writes++
			if current.Phase != dnsSwitchPhaseRollingBack {
				t.Fatalf("persisted unexpected journal phase %q", current.Phase)
			}
			return nil
		},
		rollback: func() error {
			return errors.New("post-rename config outcome remains ambiguous")
		},
		verify: func() error {
			verified = true
			return nil
		},
		remove: func() error {
			removed = true
			return nil
		},
	})
	if err == nil || journal.Phase != dnsSwitchPhaseRollingBack ||
		writes != 1 || verified || removed {
		t.Fatalf(
			"ambiguous rollback did not retain rolling journal: phase=%q writes=%d verified=%v removed=%v err=%v",
			journal.Phase, writes, verified, removed, err,
		)
	}
}

func TestBINDRollbackFinalPhaseWriteFailureRetainsJournal(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	writes := 0
	removed := false
	err := runBINDRollbackWithJournal(&journal, bindSwitchRollbackJournalOps{
		write: func(current dnsEngineSwitchJournal) error {
			writes++
			switch writes {
			case 1:
				if current.Phase != dnsSwitchPhaseRollingBack {
					t.Fatalf("first journal phase = %q", current.Phase)
				}
				return nil
			case 2:
				if current.Phase != dnsSwitchPhaseRolledBack {
					t.Fatalf("final journal phase = %q", current.Phase)
				}
				return errors.New("forced final rolled-back journal write failure")
			default:
				t.Fatalf("unexpected journal write %d", writes)
				return nil
			}
		},
		rollback: func() error { return nil },
		verify:   func() error { return nil },
		remove: func() error {
			removed = true
			return nil
		},
	})
	if err == nil || writes != 2 || removed ||
		!strings.Contains(err.Error(), "final rolled-back journal write failure") {
		t.Fatalf(
			"failed final phase removed journal: writes=%d removed=%v err=%v",
			writes, removed, err,
		)
	}
}
