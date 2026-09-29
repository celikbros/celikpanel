package dnsenginerecovery

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// firstInstallPDNSJournal is the V1 journal switchToPDNS writes for a
// first PowerDNS install: no source, no prior state receipt, pdns.service not
// active before. paired selects the paired-secondary form.
func firstInstallPDNSJournal(t *testing.T, phase string, paired bool) dnsengineartifact.SwitchJournalV1 {
	t.Helper()
	j := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV1, Phase: phase,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: "0123456789abcdef0123456789abcdef", TargetEngine: transport.DNSEnginePowerDNS,
		TargetEpoch: 1, SourceRevision: 3, Topology: transport.DNSTopologyStandalone,
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "pdns.service", LoadState: "not-found", ActiveState: "inactive"},
		},
	}
	if paired {
		j.Topology, j.PairRole = transport.DNSTopologyPaired, transport.DNSPairRoleSecondary
		j.LocalIP, j.LocalNS = "192.0.2.20", "ns2.example.test"
		j.PeerIP, j.PeerNS = "192.0.2.10", "ns1.example.test"
	}
	return reconstructedPDNSJournal(t, j)
}

func reconstructedPDNSJournal(t *testing.T, j dnsengineartifact.SwitchJournalV1) dnsengineartifact.SwitchJournalV1 {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		j.Mode, j.SourceEngine, j.TargetEngine, j.SourceEpoch, j.TargetEpoch, j.SourceRevision,
		j.Topology, j.PairRole, j.LocalIP, j.LocalNS, j.PeerIP, j.PeerNS, j.Zones,
	)
	if err != nil {
		t.Fatalf("test journal does not form a manifest: %v", err)
	}
	j.ManifestQualifier, j.SnapshotBytes = manifest.Qualifier, manifest.SnapshotBytes
	if _, err := dnsengineartifact.SwitchJournalManifest(j); err != nil {
		t.Fatal(err)
	}
	return j
}

// The class follows RecoverSwitch/Reconcile: forward phases of a V1 first
// install are resolved by the restarted Agent; decided phases are not.
func TestPDNSV1FirstInstallAgentRecoveredJournalIsPhaseBounded(t *testing.T) {
	for _, paired := range []bool{false, true} {
		for _, tc := range []struct {
			phase string
			class bool
		}{
			{dnsengineartifact.SwitchPhaseIntent, true},
			{dnsengineartifact.SwitchPhaseTargetStaged, true},
			{dnsengineartifact.SwitchPhaseSourceStopped, true},
			{dnsengineartifact.SwitchPhaseTargetStarted, true},
			{dnsengineartifact.SwitchPhaseTargetVerified, true},
			{dnsengineartifact.SwitchPhaseCommitted, true},
			{dnsengineartifact.SwitchPhaseRollingBack, false},
			{dnsengineartifact.SwitchPhaseRolledBack, false},
			{dnsengineartifact.SwitchPhaseTargetEnableIntent, false},
		} {
			j := firstInstallPDNSJournal(t, tc.phase, paired)
			if got := PDNSV1FirstInstallAgentRecoveredJournal(j); got != tc.class {
				t.Fatalf("paired=%v %s: class = %v", paired, tc.phase, got)
			}
		}
	}
}

// Journals outside the first-install class keep their own rules.
func TestPDNSV1FirstInstallAgentRecoveredJournalExcludesOtherShapes(t *testing.T) {
	for name, change := range map[string]func(*dnsengineartifact.SwitchJournalV1){
		// The paired-secondary reconfiguration shares the manifest; its
		// active pdns.service preimage is what separates it.
		"reconfiguration": func(j *dnsengineartifact.SwitchJournalV1) {
			j.Topology, j.PairRole = transport.DNSTopologyPaired, transport.DNSPairRoleSecondary
			j.LocalIP, j.LocalNS = "192.0.2.20", "ns2.example.test"
			j.PeerIP, j.PeerNS = "192.0.2.10", "ns1.example.test"
			j.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}}
		},
		"active-standalone-target": func(j *dnsengineartifact.SwitchJournalV1) {
			j.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}}
		},
		"paired-primary": func(j *dnsengineartifact.SwitchJournalV1) {
			j.Topology, j.PairRole = transport.DNSTopologyPaired, transport.DNSPairRolePrimary
			j.LocalIP, j.LocalNS = "192.0.2.10", "ns1.example.test"
			j.PeerIP, j.PeerNS = "192.0.2.20", "ns2.example.test"
		},
		"bind-source": func(j *dnsengineartifact.SwitchJournalV1) {
			j.SourceEngine, j.SourceEpoch = transport.DNSEngineBIND, 1
			j.TargetEpoch = 2
		},
		"prior-state-receipt": func(j *dnsengineartifact.SwitchJournalV1) {
			j.StateBefore = dnsengineartifact.FileSnapshot{Exists: true}
		},
		"adoption": func(j *dnsengineartifact.SwitchJournalV1) {
			j.Mode = transport.DNSEngineSwitchModeAdopt
		},
		"v2": func(j *dnsengineartifact.SwitchJournalV1) { j.Schema = dnsengineartifact.SwitchJournalSchemaV2 },
		"v3": func(j *dnsengineartifact.SwitchJournalV1) { j.Schema = dnsengineartifact.SwitchJournalSchemaV3 },
		"v4": func(j *dnsengineartifact.SwitchJournalV1) { j.Schema = dnsengineartifact.SwitchJournalSchemaV4 },
		"bind-target": func(j *dnsengineartifact.SwitchJournalV1) {
			j.TargetEngine = transport.DNSEngineBIND
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, phase := range []string{dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseCommitted} {
				j := firstInstallPDNSJournal(t, phase, false)
				change(&j)
				if manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
					j.Mode, j.SourceEngine, j.TargetEngine, j.SourceEpoch, j.TargetEpoch, j.SourceRevision,
					j.Topology, j.PairRole, j.LocalIP, j.LocalNS, j.PeerIP, j.PeerNS, j.Zones,
				); err == nil {
					j.ManifestQualifier, j.SnapshotBytes = manifest.Qualifier, manifest.SnapshotBytes
				}
				if PDNSV1FirstInstallAgentRecoveredJournal(j) {
					t.Fatalf("%s at %s entered the Agent-recovered first-install class", name, phase)
				}
			}
		})
	}
	// A journal that does not reconstruct its manifest is not classified.
	j := firstInstallPDNSJournal(t, dnsengineartifact.SwitchPhaseIntent, false)
	j.ManifestQualifier = "0000000000000000000000000000000000000000000000000000000000000000"
	if PDNSV1FirstInstallAgentRecoveredJournal(j) {
		t.Fatal("a journal with a foreign manifest qualifier was classified")
	}
}
