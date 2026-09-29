//go:build linux && dns_kill_matrix

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
)

// freshPrimaryV3KillMatrixJournals returns the V3 journal of every hooked
// forward write of the fresh paired PowerDNS primary producer, in order.
func freshPrimaryV3KillMatrixJournals(t *testing.T) map[string]dnsEngineSwitchJournal {
	t.Helper()
	policy, intent, staged := freshPrimaryV3AgentFixture(t, freshPDNSGuardMaskV3)
	enable := withFreshPrimaryV3Phase(t, policy, staged, dnsengineartifact.SwitchPhaseTargetEnableIntent)
	started := withFreshPrimaryV3Phase(t, policy, staged, dnsSwitchPhaseTargetStarted)
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "e2e", "dns-kill-matrix", "evidence", "pdns-master-bind-20260928", "debian-primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var measured struct {
		Running struct {
			SQLite pdnsnative.Snapshot `json:"sqlite"`
		} `json:"running"`
	}
	if err := json.Unmarshal(raw, &measured); err != nil {
		t.Fatal(err)
	}
	catalog, err := binddns.CatalogDomain(started.LocalIP)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := policy.AttachPDNSFreshNativeObservationV3(started, catalog, measured.Running.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	verified := withFreshPrimaryV3Phase(t, policy, observed, dnsSwitchPhaseTargetVerified)
	committed := withFreshPrimaryV3Phase(t, policy, observed, dnsSwitchPhaseCommitted)
	return map[string]dnsEngineSwitchJournal{
		"intent": intent, "target-staged": staged, "target-enable-intent": enable,
		"target-started": started, "target-started-observed": observed,
		"target-verified": verified, "committed": committed,
	}
}

func freshPrimaryV3KillMatrixRuntime(t *testing.T, phase, point, requestID string, markers *[]dnsKillMatrixMarker, parks *int) *dnsKillMatrixRuntime {
	t.Helper()
	return &dnsKillMatrixRuntime{
		config: dnsKillMatrixConfig{
			CellID: "pdns-switch.v3." + phase, Driver: dnsEngineSwitchFaultDriverPDNSSwitch,
			Point: point, Phase: phase, RequestID: requestID,
			Nonce: strings.Repeat("a", 64), Marker: filepath.Join(t.TempDir(), "boundary.json"), ReadyFD: 9,
		},
		ops: dnsKillMatrixRuntimeOps{
			pid:        func() int { return 4321 },
			startTicks: func(int) (string, error) { return "987654", nil },
			writeMarker: func(_ string, marker dnsKillMatrixMarker) error {
				*markers = append(*markers, marker)
				return nil
			},
			notifyReady: func(int, string) error { return nil },
			stopProcess: func(int) error { return nil },
			now:         time.Now,
			park:        func(error) { *parks++ },
		},
	}
}

// Item 5: the hook accepts the fresh paired PowerDNS primary's V3 journal at
// every hooked forward write, maps the harness coordinate source-stopped to
// target-enable-intent for this shape only, and always selects the FIRST
// target-started write (before the native observation is attached).
func TestDNSKillMatrixV3FreshPrimaryEveryBoundary(t *testing.T) {
	journals := freshPrimaryV3KillMatrixJournals(t)
	id := journals["intent"].MutationRequestID
	for _, tc := range []struct {
		configPhase string
		journal     string
	}{
		{dnsSwitchPhaseIntent, "intent"},
		{dnsSwitchPhaseTargetStaged, "target-staged"},
		{dnsengineartifact.SwitchPhaseTargetEnableIntent, "target-enable-intent"},
		{dnsSwitchPhaseSourceStopped, "target-enable-intent"},
		{dnsSwitchPhaseTargetStarted, "target-started"},
		{dnsSwitchPhaseTargetVerified, "target-verified"},
		{dnsSwitchPhaseCommitted, "committed"},
	} {
		for _, point := range []string{dnsEngineSwitchJournalFaultBeforeWrite, dnsEngineSwitchJournalFaultAfterWrite} {
			t.Run(tc.configPhase+"/"+point, func(t *testing.T) {
				var markers []dnsKillMatrixMarker
				parks := 0
				runtime := freshPrimaryV3KillMatrixRuntime(t, tc.configPhase, point, id, &markers, &parks)
				// Every write of the producer in order: only the selected one
				// fires; the others pass through untouched.
				for _, name := range []string{"intent", "target-staged", "target-enable-intent", "target-started", "target-started-observed", "target-verified", "committed"} {
					err := runtime.hook(dnsEngineSwitchFaultDriverPDNSSwitch, point, journals[name])
					if name == tc.journal {
						if !errors.Is(err, dnsKillMatrixResumedError) {
							t.Fatalf("selected V3 %s write: %v", name, err)
						}
						break
					}
					if err != nil {
						t.Fatalf("unselected V3 %s write refused: %v", name, err)
					}
				}
				if len(markers) != 1 || parks != 1 {
					t.Fatalf("markers=%d parks=%d", len(markers), parks)
				}
				marker := markers[0]
				want := journals[tc.journal]
				if marker.ObservedJournal.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
					marker.ObservedJournal.Phase != want.Phase || marker.Phase != tc.configPhase ||
					marker.Driver != dnsEngineSwitchFaultDriverPDNSSwitch || marker.Point != point {
					t.Fatalf("marker = %+v", marker)
				}
			})
		}
	}
	// The second target-started write (native observation attached) is never
	// the selected one.
	var markers []dnsKillMatrixMarker
	parks := 0
	runtime := freshPrimaryV3KillMatrixRuntime(t, dnsSwitchPhaseTargetStarted, dnsEngineSwitchJournalFaultBeforeWrite, id, &markers, &parks)
	if err := runtime.hook(dnsEngineSwitchFaultDriverPDNSSwitch, dnsEngineSwitchJournalFaultBeforeWrite, journals["target-started-observed"]); err != nil || len(markers) != 0 {
		t.Fatalf("the observed target-started write was selected: err=%v markers=%d", err, len(markers))
	}
}

// Any other driver, or any other V3 manifest shape, keeps the refusal; the
// source-stopped mapping applies to the fresh paired primary only.
func TestDNSKillMatrixV3RefusedOutsideTheFreshPrimaryShape(t *testing.T) {
	journals := freshPrimaryV3KillMatrixJournals(t)
	staged := journals["target-staged"]
	for _, driver := range []string{
		dnsEngineSwitchFaultDriverBIND, dnsEngineSwitchFaultDriverPDNSAdopt,
		dnsEngineSwitchFaultDriverPDNSSecondaryReconfigure, dnsEngineSwitchFaultDriverSignedUpdateFinalize,
	} {
		var markers []dnsKillMatrixMarker
		parks := 0
		runtime := freshPrimaryV3KillMatrixRuntime(t, dnsSwitchPhaseTargetStaged, dnsEngineSwitchJournalFaultAfterWrite, staged.MutationRequestID, &markers, &parks)
		runtime.config.Driver = driver
		err := runtime.hook(driver, dnsEngineSwitchJournalFaultAfterWrite, staged)
		if err == nil || !strings.Contains(err.Error(), "schema mismatch") || len(markers) != 0 || runtime.fired.Load() {
			t.Fatalf("driver %s accepted a V3 journal: err=%v markers=%d", driver, err, len(markers))
		}
	}
	for name, edit := range map[string]func(*dnsEngineSwitchJournal){
		"secondary":    func(j *dnsEngineSwitchJournal) { j.PairRole = "secondary" },
		"standalone":   func(j *dnsEngineSwitchJournal) { j.Topology = "standalone"; j.PairRole = "" },
		"with-source":  func(j *dnsEngineSwitchJournal) { j.SourceEngine, j.SourceEpoch = "bind", 1 },
		"without-plan": func(j *dnsEngineSwitchJournal) { j.PDNSFreshPlan = nil },
	} {
		journal := staged
		edit(&journal)
		var markers []dnsKillMatrixMarker
		parks := 0
		runtime := freshPrimaryV3KillMatrixRuntime(t, dnsSwitchPhaseTargetStaged, dnsEngineSwitchJournalFaultAfterWrite, staged.MutationRequestID, &markers, &parks)
		err := runtime.hook(dnsEngineSwitchFaultDriverPDNSSwitch, dnsEngineSwitchJournalFaultAfterWrite, journal)
		if err == nil || !strings.Contains(err.Error(), "schema mismatch") || len(markers) != 0 {
			t.Fatalf("%s V3 journal was accepted: err=%v markers=%d", name, err, len(markers))
		}
		// source-stopped never maps for another shape.
		enable := journals["target-enable-intent"]
		edit(&enable)
		runtime = freshPrimaryV3KillMatrixRuntime(t, dnsSwitchPhaseSourceStopped, dnsEngineSwitchJournalFaultAfterWrite, staged.MutationRequestID, &markers, &parks)
		if err := runtime.hook(dnsEngineSwitchFaultDriverPDNSSwitch, dnsEngineSwitchJournalFaultAfterWrite, enable); err != nil || len(markers) != 0 {
			t.Fatalf("%s: source-stopped mapped for a non-fresh-primary V3 journal: err=%v markers=%d", name, err, len(markers))
		}
	}
}

// The boundary-stop guarantee holds for V3 writes: the writer's caller never
// resumes after the selected V3 write, whatever the stop reported.
func TestDNSKillMatrixV3BoundaryNeverReturnsIntoTheWriter(t *testing.T) {
	journals := freshPrimaryV3KillMatrixJournals(t)
	intent, staged := journals["intent"], journals["target-staged"]
	config := dnsKillMatrixConfig{
		CellID: "pdns-switch.v3.target-staged.after", Driver: dnsEngineSwitchFaultDriverPDNSSwitch,
		Point: dnsEngineSwitchJournalFaultAfterWrite, Phase: dnsSwitchPhaseTargetStaged,
		RequestID: staged.MutationRequestID, Nonce: strings.Repeat("a", 64),
		Marker: filepath.Join(t.TempDir(), "boundary.json"), ReadyFD: 9,
	}
	parked := make(chan error, 1)
	release := make(chan struct{})
	var marker dnsKillMatrixMarker
	runtime := &dnsKillMatrixRuntime{config: config, ops: dnsKillMatrixRuntimeOps{
		pid:         func() int { return 4321 },
		startTicks:  func(int) (string, error) { return "987654", nil },
		writeMarker: func(_ string, m dnsKillMatrixMarker) error { marker = m; return nil },
		notifyReady: func(int, string) error { return nil },
		stopProcess: func(int) error { return nil },
		now:         time.Now,
		park: func(reason error) {
			parked <- reason
			<-release
			goruntime.Goexit()
		},
	}}
	var continued atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		stored, exists := intent, true
		_ = writeDNSEngineSwitchJournalWithOps(
			staged,
			func([]byte) error { stored = staged; return nil },
			func() (dnsEngineSwitchJournal, bool, error) { return stored, exists, nil },
			func(point string, observed dnsEngineSwitchJournal) error {
				return runtime.hook(config.Driver, point, observed)
			},
		)
		continued.Store(true)
	}()
	select {
	case <-parked:
	case <-time.After(10 * time.Second):
		t.Fatal("the V3 boundary did not park the journal writer")
	}
	time.Sleep(50 * time.Millisecond)
	if continued.Load() {
		t.Fatal("the V3 journal writer's caller continued past the boundary")
	}
	if marker.ObservedJournal.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
		marker.ObservedJournal.Phase != dnsSwitchPhaseTargetStaged {
		t.Fatalf("marker = %+v", marker)
	}
	close(release)
	<-done
	if continued.Load() {
		t.Fatal("the V3 journal writer's caller continued after the park ended")
	}
}
