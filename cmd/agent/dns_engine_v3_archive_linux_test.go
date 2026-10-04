//go:build linux

package main

import (
	"context"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshPrimaryArchiveArtifactAbsenceRequiresPinnedDirectory(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "private")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, ".celikpanel-switch-request.sqlite3")
	if err := verifyFreshPrimaryArtifactAbsentAtV3(path); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal", ".unknown"} {
		name := path + suffix
		if err := os.WriteFile(name, []byte("owner data"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := verifyFreshPrimaryArtifactAbsentAtV3(path); err == nil {
			t.Fatalf("accepted %s", name)
		}
		if err := os.Remove(name); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(parent, link); err != nil {
		t.Fatal(err)
	}
	if err := verifyFreshPrimaryArtifactAbsentAtV3(filepath.Join(link, filepath.Base(path))); err == nil {
		t.Fatal("accepted symlinked parent")
	}
}

func TestFreshPrimaryArchiveJournalFreeProofRejectsWrongIdentityAndMalformedArchive(t *testing.T) {
	_, root := newMutationTestManager(t)
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(root, "state"))
	binding := mutationTestBinding()
	state := dnsEngineStateReceipt{
		Schema:      dnsengineartifact.StateSchemaV1,
		Mode:        transport.DNSEngineSwitchModeSwitch,
		Engine:      transport.DNSEnginePowerDNS,
		EngineEpoch: 1,
		PairRole:    transport.DNSPairRolePrimary,
		PairLocalIP: "192.0.2.10", PairPeerIP: "192.0.2.11",
		PrimaryCatalogSerial: 1790542951,
		ManifestQualifier:    canonicalSwitchRequest(t).ManifestQualifier,
		MutationRequestID:    binding.MutationRequestID,
		MutationOwnerID:      binding.MutationOwnerID,
		NativeCatalogV3:      dnsengineartifact.NativeCatalogDebian49V3,
	}
	if err := writeDNSEngineState(state); err != nil {
		t.Fatal(err)
	}
	wrongRequest := binding
	wrongRequest.MutationRequestID = strings.Repeat("f", 32)
	if proven, v3, err := exactArchivedFreshPrimaryProvenanceV3(context.Background(), state.Engine, state.ManifestQualifier, wrongRequest); err != nil || proven || v3 {
		t.Fatalf("wrong request: proven=%v v3=%v err=%v", proven, v3, err)
	}
	wrongOwner := binding
	wrongOwner.MutationOwnerID = strings.Repeat("e", 32)
	if proven, v3, err := exactArchivedFreshPrimaryProvenanceV3(context.Background(), state.Engine, state.ManifestQualifier, wrongOwner); err == nil || proven || !v3 {
		t.Fatalf("wrong owner: proven=%v v3=%v err=%v", proven, v3, err)
	}
	if proven, v3, err := exactArchivedFreshPrimaryProvenanceV3(context.Background(), state.Engine, state.ManifestQualifier, binding); err == nil || proven || !v3 {
		t.Fatalf("missing archive: proven=%v v3=%v err=%v", proven, v3, err)
	}
	stateRaw, err := dnsengineartifact.CanonicalStateDocumentV3(state)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(serviceMutationStateDirectory(), freshPrimaryArchivePrefixV3+binding.MutationRequestID+"-"+dnsengineartifact.DigestBytes(stateRaw)+".json")
	if err := secureWriteConfigOwnedBy(archive, []byte("not a journal"), 0600, serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID); err != nil {
		t.Fatal(err)
	}
	if proven, v3, err := exactArchivedFreshPrimaryProvenanceV3(context.Background(), state.Engine, state.ManifestQualifier, binding); err == nil || proven || !v3 {
		t.Fatalf("malformed archive: proven=%v v3=%v err=%v", proven, v3, err)
	}
	pending := dnsEngineSwitchJournal{Schema: dnsengineartifact.SwitchJournalSchemaV3, Phase: dnsengineartifact.SwitchPhaseTargetStarted}
	if _, err := freshPrimaryArchivePathV3(pending, state); err == nil {
		t.Fatal("pending journal accepted for archive")
	}
}
