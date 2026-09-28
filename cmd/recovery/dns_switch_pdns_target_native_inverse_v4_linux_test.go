//go:build linux

package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

type pdnsNativeSequenceV4 struct {
	state dnsenginerecovery.PDNSTargetStageState
	calls []string
	fail  string
}

func (f *pdnsNativeSequenceV4) ops() pdnsTargetNativeInverseOpsV4 {
	call := func(name string) error {
		f.calls = append(f.calls, name)
		if f.fail == name {
			return errors.New("injected cut")
		}
		return nil
	}
	return pdnsTargetNativeInverseOpsV4{
		Assess: func(context.Context) (dnsenginerecovery.PDNSTargetStageState, error) {
			if err := call("assess"); err != nil {
				return dnsenginerecovery.PDNSTargetStageUnknown, err
			}
			return f.state, nil
		},
		DisableTarget: func(context.Context) error {
			if err := call("disable"); err != nil {
				return err
			}
			f.state = dnsenginerecovery.PDNSTargetStageRenamed
			return nil
		},
		RestoreConfigs: func(context.Context) error { return call("configs") },
		ReturnRenamed: func(context.Context) error {
			if err := call("return"); err != nil {
				return err
			}
			f.state = dnsenginerecovery.PDNSTargetStageNeedsRestore
			return nil
		},
		RestoreSourceUnits: func(context.Context) error { return call("units") },
		RemoveCandidate: func(context.Context) error {
			if err := call("remove"); err != nil {
				return err
			}
			f.state = dnsenginerecovery.PDNSTargetStageRestored
			return nil
		},
	}
}

func TestPDNSTargetNativeV4ReturnsRenamedBeforeBINDAndDeletion(t *testing.T) {
	f := &pdnsNativeSequenceV4{state: dnsenginerecovery.PDNSTargetStageRenamed}
	if err := restorePDNSTargetNativeV4(context.Background(), f.ops()); err != nil {
		t.Fatal(err)
	}
	effects := make([]string, 0)
	for _, call := range f.calls {
		if call != "assess" {
			effects = append(effects, call)
		}
	}
	want := []string{"configs", "return", "units", "remove"}
	if !reflect.DeepEqual(effects, want) || f.state != dnsenginerecovery.PDNSTargetStageRestored {
		t.Fatalf("wrong native inverse order: %v, state=%v", effects, f.state)
	}
}

func TestPDNSTargetNativeV4ReplaysCutAfterRenameWithoutSecondRename(t *testing.T) {
	f := &pdnsNativeSequenceV4{state: dnsenginerecovery.PDNSTargetStageRenamed, fail: "units"}
	if err := restorePDNSTargetNativeV4(context.Background(), f.ops()); err == nil || f.state != dnsenginerecovery.PDNSTargetStageNeedsRestore {
		t.Fatalf("cut did not retain returned candidate: %v, state=%v", err, f.state)
	}
	f.calls, f.fail = nil, ""
	if err := restorePDNSTargetNativeV4(context.Background(), f.ops()); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.calls {
		if call == "return" {
			t.Fatal("same request attempted a second rename")
		}
	}
}

func TestPDNSTargetNativeV4RefusesUnknownAndUnprovedEffects(t *testing.T) {
	f := &pdnsNativeSequenceV4{state: dnsenginerecovery.PDNSTargetStageUnknown}
	if err := restorePDNSTargetNativeV4(context.Background(), f.ops()); err == nil || len(f.calls) != 1 {
		t.Fatalf("unknown native state reached mutation: %v %v", err, f.calls)
	}
	f = &pdnsNativeSequenceV4{state: dnsenginerecovery.PDNSTargetStageRenamed}
	ops := f.ops()
	ops.ReturnRenamed = func(context.Context) error { f.calls = append(f.calls, "return-unproved"); return nil }
	if err := restorePDNSTargetNativeV4(context.Background(), ops); err == nil {
		t.Fatal("unproved reverse rename accepted")
	}
	for _, call := range f.calls {
		if call == "units" || call == "remove" {
			t.Fatalf("continued after unproved rename: %v", f.calls)
		}
	}
	f = &pdnsNativeSequenceV4{state: dnsenginerecovery.PDNSTargetStageNeedsRestore, fail: "configs"}
	if err := restorePDNSTargetNativeV4(context.Background(), f.ops()); err == nil {
		t.Fatal("config failure accepted")
	}
	for _, call := range f.calls {
		if call == "units" || call == "remove" {
			t.Fatalf("continued after config failure: %v", f.calls)
		}
	}
}

func TestPDNSTargetNativeV4DisablesEnabledTargetBeforeOtherEffects(t *testing.T) {
	f := &pdnsNativeSequenceV4{state: dnsenginerecovery.PDNSTargetStageRenamedEnabled}
	if err := restorePDNSTargetNativeV4(context.Background(), f.ops()); err != nil {
		t.Fatal(err)
	}
	var effects []string
	for _, call := range f.calls {
		if call != "assess" {
			effects = append(effects, call)
		}
	}
	want := []string{"disable", "configs", "return", "units", "remove"}
	if !reflect.DeepEqual(effects, want) {
		t.Fatalf("target was not disabled before rename: %v", effects)
	}
	f = &pdnsNativeSequenceV4{state: dnsenginerecovery.PDNSTargetStageRenamedEnabled, fail: "disable"}
	if err := restorePDNSTargetNativeV4(context.Background(), f.ops()); err == nil || f.state != dnsenginerecovery.PDNSTargetStageRenamedEnabled {
		t.Fatalf("failed disable reached later inverse: %v %v", err, f.calls)
	}
	f.calls, f.fail = nil, ""
	if err := restorePDNSTargetNativeV4(context.Background(), f.ops()); err != nil {
		t.Fatalf("same-request replay failed: %v", err)
	}
}
