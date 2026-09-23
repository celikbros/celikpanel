package dnsengineartifact

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func separationArtifacts(t *testing.T, plan LegacySeparationV1) (a, before, current []byte) {
	t.Helper()
	var err error
	if a, err = CanonicalAcquisitionV1(plan.Acquisition); err != nil {
		t.Fatal(err)
	}
	if before, err = CanonicalPublicationV1(plan.Acquisition, plan.OwnershipPublication); err != nil {
		t.Fatal(err)
	}
	if current, err = CanonicalPublicationV1(plan.Acquisition, plan.CurrentPublication); err != nil {
		t.Fatal(err)
	}
	return
}

func TestLegacySeparationActualProducerBeforeImages(t *testing.T) {
	paths, err := filepath.Glob("testdata/alpha81-*.json")
	if err != nil || len(paths) != 7 {
		t.Fatalf("historical paths: %v %v", paths, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			current, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ownership := current
			if strings.Contains(path, "bind-zone-") {
				ownership = legacyBytes(t, "bind-acquisition")
			}
			plan, err := PlanLegacySeparationV1(ownership, current)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := CanonicalLegacySeparationV1(plan)
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := DecodeLegacySeparationV1(raw)
			if err != nil {
				t.Fatal(err)
			}
			a, b, c := separationArtifacts(t, frozen)
			oldOwner, oldState, err := LegacyBeforeImagesV1(frozen, a, b, c)
			if err != nil || !bytes.Equal(oldOwner, ownership) || !bytes.Equal(oldState, current) {
				t.Fatalf("historical before-image was lost: %v", err)
			}
			if err := VerifyLegacySeparationSourceV1(frozen, ownership, current); err != nil {
				t.Fatal(err)
			}
			// Returned buffers and caller input cannot rewrite retained evidence.
			oldOwner[0] = 'x'
			oldState[0] = 'x'
			ownership[0] = 'x'
			current[0] = 'x'
			if _, err := CanonicalLegacySeparationV1(plan); err != nil {
				t.Fatalf("source aliases plan: %v", err)
			}
			if _, err := CanonicalLegacySeparationV1(frozen); err != nil {
				t.Fatalf("result aliases plan: %v", err)
			}
		})
	}
}

func TestLegacySeparationRefusesMissingConflictingOrRegressedSources(t *testing.T) {
	owner := legacyBytes(t, "bind-acquisition")
	current := legacyBytes(t, "bind-zone-add")
	changedOwner := fixture(t, "bind-acquisition")
	changedOwner.MutationOwnerID = strings.Repeat("a", 32)
	changed, err := CanonicalV1(changedOwner)
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2][]byte{
		"missing-owner":     {nil, current},
		"missing-current":   {owner, nil},
		"owner-drift":       {changed, current},
		"noncanonical":      {owner, append([]byte(" "), current...)},
		"serial-regression": {current, owner},
		"engine-change":     {owner, legacyBytes(t, "pdns-standalone")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := PlanLegacySeparationV1(pair[0], pair[1]); err == nil {
				t.Fatal("unsupported migration source accepted")
			}
		})
	}
}

func TestLegacySeparationPreservesFrozenEvidenceOnLaterWork(t *testing.T) {
	owner, current := legacyBytes(t, "bind-acquisition"), legacyBytes(t, "bind-zone-add")
	plan, err := PlanLegacySeparationV1(owner, current)
	if err != nil {
		t.Fatal(err)
	}
	frozen, _ := CanonicalLegacySeparationV1(plan)
	a, b, c := separationArtifacts(t, plan)
	later := legacyBytes(t, "bind-zone-edit")
	if err := VerifyLegacySeparationSourceV1(plan, owner, later); err == nil {
		t.Fatal("stale source proposal admitted")
	}
	// A supported later publication is NOT permission to restore over it.
	laterState, err := DecodeV1(later)
	if err != nil {
		t.Fatal(err)
	}
	_, pub, err := SeparateV1(laterState)
	if err != nil {
		t.Fatal(err)
	}
	laterPublication, err := CanonicalPublicationV1(plan.Acquisition, pub)
	if err != nil {
		t.Fatal(err)
	}
	for name, observed := range map[string][3][]byte{
		"later-publication":              {a, b, laterPublication},
		"rewritten-ownership-checkpoint": {a, c, c},
		"missing-acquisition":            {nil, b, c},
		"missing-owner-publication":      {a, nil, c},
		"missing-current":                {a, b, nil},
		"malformed-current":              {a, b, []byte("{}\n")},
	} {
		t.Run(name, func(t *testing.T) {
			ownerBytes, stateBytes, err := LegacyBeforeImagesV1(plan, observed[0], observed[1], observed[2])
			if err == nil || ownerBytes != nil || stateBytes != nil {
				t.Fatal("changed evidence yielded restore bytes")
			}
			after, err := CanonicalLegacySeparationV1(plan)
			if err != nil || !bytes.Equal(frozen, after) {
				t.Fatal("refusal rewrote frozen evidence")
			}
		})
	}
}

func TestLegacySeparationRefusesForgedOrMixedPlan(t *testing.T) {
	owner, current := legacyBytes(t, "bind-acquisition"), legacyBytes(t, "bind-zone-add")
	plan, err := PlanLegacySeparationV1(owner, current)
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := separationArtifacts(t, plan)
	for name, mutate := range map[string]func(*LegacySeparationV1){
		"schema":              func(p *LegacySeparationV1) { p.Schema = "unknown" },
		"new-owner":           func(p *LegacySeparationV1) { p.Acquisition.MutationOwnerID = strings.Repeat("a", 32) },
		"new-publication":     func(p *LegacySeparationV1) { p.CurrentPublication.Generation = strings.Repeat("f", 64) },
		"replaced-checkpoint": func(p *LegacySeparationV1) { p.OwnershipPublication = p.CurrentPublication },
		"missing-before":      func(p *LegacySeparationV1) { p.LegacyOwnership = nil },
		"copied-before":       func(p *LegacySeparationV1) { p.LegacyOwnership = bytes.Clone(p.LegacyState) },
		"replaced-source":     func(p *LegacySeparationV1) { p.LegacyState = legacyBytes(t, "bind-zone-edit") },
	} {
		t.Run(name, func(t *testing.T) {
			changed := plan
			mutate(&changed)
			if _, err := CanonicalLegacySeparationV1(changed); err == nil {
				t.Fatal("forged plan encoded")
			}
			if err := VerifyLegacySeparationSourceV1(changed, owner, current); err == nil {
				t.Fatal("forged plan passed source proof")
			}
			if _, _, err := LegacyBeforeImagesV1(changed, a, b, c); err == nil {
				t.Fatal("forged plan yielded recovery bytes")
			}
		})
	}
}
