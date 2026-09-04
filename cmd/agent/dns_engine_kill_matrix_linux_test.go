//go:build linux && dns_kill_matrix

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

const dnsKillMatrixHelperProcessEnv = "CELIKPANEL_DNS_KILL_MATRIX_TEST_HELPER"

func testDNSKillMatrixEnvironment(marker string) map[string]string {
	return map[string]string{
		dnsKillMatrixEnvCellID:    "bind.source-stopped.before.standalone.reachable",
		dnsKillMatrixEnvDriver:    "bind",
		dnsKillMatrixEnvPoint:     dnsEngineSwitchJournalFaultBeforeWrite,
		dnsKillMatrixEnvPhase:     dnsSwitchPhaseSourceStopped,
		dnsKillMatrixEnvRequestID: strings.Repeat("1", 32),
		dnsKillMatrixEnvNonce:     strings.Repeat("a", 64),
		dnsKillMatrixEnvMarker:    marker,
		dnsKillMatrixEnvReadyFD:   "9",
	}
}

func testDNSKillMatrixLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func cloneDNSKillMatrixEnvironment(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for name, value := range values {
		cloned[name] = value
	}
	return cloned
}

func TestDNSKillMatrixConfigFromEnvironment(t *testing.T) {
	config, active, err := dnsKillMatrixConfigFromEnvironment(
		func(string) (string, bool) { return "", false },
	)
	if err != nil || active || config != (dnsKillMatrixConfig{}) {
		t.Fatalf("inactive selector = (%+v, %v, %v)", config, active, err)
	}

	values := testDNSKillMatrixEnvironment(filepath.Join(t.TempDir(), "boundary.json"))
	config, active, err = dnsKillMatrixConfigFromEnvironment(testDNSKillMatrixLookup(values))
	if err != nil || !active {
		t.Fatalf("valid selector active=%v err=%v", active, err)
	}
	if config.CellID != values[dnsKillMatrixEnvCellID] ||
		config.Driver != values[dnsKillMatrixEnvDriver] ||
		config.Point != values[dnsKillMatrixEnvPoint] ||
		config.Phase != values[dnsKillMatrixEnvPhase] ||
		config.RequestID != values[dnsKillMatrixEnvRequestID] ||
		config.Nonce != values[dnsKillMatrixEnvNonce] ||
		config.Marker != values[dnsKillMatrixEnvMarker] || config.ReadyFD != 9 {
		t.Fatalf("parsed selector = %+v", config)
	}

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "cell uppercase", key: dnsKillMatrixEnvCellID, value: "Bind.intent"},
		{name: "unknown driver", key: dnsKillMatrixEnvDriver, value: "unbound"},
		{name: "unknown point", key: dnsKillMatrixEnvPoint, value: "during_write"},
		{name: "phase point mismatch", key: dnsKillMatrixEnvPhase, value: dnsKillMatrixPreIntentPhase},
		{name: "request not identity", key: dnsKillMatrixEnvRequestID, value: strings.Repeat("1", 31)},
		{name: "nonce uppercase", key: dnsKillMatrixEnvNonce, value: strings.Repeat("A", 32)},
		{name: "relative marker", key: dnsKillMatrixEnvMarker, value: "boundary.json"},
		{name: "standard descriptor", key: dnsKillMatrixEnvReadyFD, value: "2"},
		{name: "noncanonical descriptor", key: dnsKillMatrixEnvReadyFD, value: "09"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := cloneDNSKillMatrixEnvironment(values)
			invalid[test.key] = test.value
			_, active, err := dnsKillMatrixConfigFromEnvironment(testDNSKillMatrixLookup(invalid))
			if !active || err == nil {
				t.Fatalf("invalid selector active=%v err=%v", active, err)
			}
		})
	}

	partial := cloneDNSKillMatrixEnvironment(values)
	delete(partial, dnsKillMatrixEnvNonce)
	if _, active, err := dnsKillMatrixConfigFromEnvironment(testDNSKillMatrixLookup(partial)); !active || err == nil || !strings.Contains(err.Error(), dnsKillMatrixEnvNonce) {
		t.Fatalf("partial selector active=%v err=%v", active, err)
	}

	preIntent := cloneDNSKillMatrixEnvironment(values)
	preIntent[dnsKillMatrixEnvPoint] = dnsEngineSwitchJournalFaultPreIntent
	preIntent[dnsKillMatrixEnvPhase] = dnsKillMatrixPreIntentPhase
	if _, active, err := dnsKillMatrixConfigFromEnvironment(testDNSKillMatrixLookup(preIntent)); !active || err != nil {
		t.Fatalf("pre-intent selector active=%v err=%v", active, err)
	}
}

func TestDNSKillMatrixRuntimeSelectsExactBoundaryInOrder(t *testing.T) {
	config := dnsKillMatrixConfig{
		CellID:    "bind.source-stopped.before.standalone.reachable",
		Driver:    "bind",
		Point:     dnsEngineSwitchJournalFaultBeforeWrite,
		Phase:     dnsSwitchPhaseSourceStopped,
		RequestID: strings.Repeat("1", 32),
		Nonce:     strings.Repeat("a", 64),
		Marker:    filepath.Join(t.TempDir(), "boundary.json"),
		ReadyFD:   9,
	}
	var order []string
	var captured dnsKillMatrixMarker
	runtime := &dnsKillMatrixRuntime{
		config: config,
		ops: dnsKillMatrixRuntimeOps{
			pid:        func() int { return 4321 },
			startTicks: func(pid int) (string, error) { return "987654", nil },
			writeMarker: func(path string, marker dnsKillMatrixMarker) error {
				order = append(order, "marker")
				captured = marker
				return nil
			},
			notifyReady: func(fd int, nonce string) error {
				order = append(order, "ready")
				if fd != config.ReadyFD || nonce != config.Nonce {
					t.Fatalf("ready notification = (%d, %q)", fd, nonce)
				}
				return nil
			},
			stopProcess: func(pid int) error {
				order = append(order, "stop")
				if pid != 4321 {
					t.Fatalf("stopped pid = %d", pid)
				}
				return nil
			},
			now: func() time.Time {
				return time.Date(2026, time.August, 31, 12, 34, 56, 0, time.UTC)
			},
		},
	}
	journal := testBINDSwitchJournal(t)
	journal.Phase = dnsSwitchPhaseIntent
	journal.MutationRequestID = config.RequestID
	if err := runtime.hook(config.Driver, config.Point, journal); err != nil {
		t.Fatalf("earlier phase fired hook: %v", err)
	}
	if len(order) != 0 {
		t.Fatalf("earlier phase operations = %v", order)
	}
	journal.Phase = config.Phase
	if err := runtime.hook(config.Driver, config.Point, journal); !errors.Is(err, dnsKillMatrixResumedError) {
		t.Fatalf("selected boundary error = %v", err)
	}
	if want := []string{"marker", "ready", "stop"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("boundary order = %v, want %v", order, want)
	}
	if captured.Schema != dnsKillMatrixMarkerSchema || captured.CellID != config.CellID ||
		captured.Driver != config.Driver || captured.ObservedDriver != config.Driver ||
		captured.Point != config.Point ||
		captured.Phase != config.Phase || captured.RequestID != config.RequestID ||
		captured.Nonce != config.Nonce || captured.Marker != config.Marker ||
		captured.ReadyFD != config.ReadyFD || captured.PID != 4321 ||
		captured.ProcessStartTicks != "987654" ||
		captured.ObservedJournal.MutationRequestID != journal.MutationRequestID ||
		captured.ObservedJournal.Phase != journal.Phase ||
		captured.ObservedJournal.Path != dnsEngineSwitchJournalPath() {
		t.Fatalf("captured marker = %+v", captured)
	}
	if err := runtime.hook(config.Driver, config.Point, journal); err == nil ||
		!strings.Contains(err.Error(), "more than once") {
		t.Fatalf("duplicate boundary error = %v", err)
	}
}

func TestDNSKillMatrixRuntimeRequestMismatchFailsBeforeActions(t *testing.T) {
	config := dnsKillMatrixConfig{
		CellID:    "bind.intent.after.standalone.reachable",
		Driver:    "bind",
		Point:     dnsEngineSwitchJournalFaultAfterWrite,
		Phase:     dnsSwitchPhaseIntent,
		RequestID: strings.Repeat("1", 32),
		Nonce:     strings.Repeat("a", 32),
		Marker:    filepath.Join(t.TempDir(), "boundary.json"),
		ReadyFD:   9,
	}
	runtime := &dnsKillMatrixRuntime{config: config}
	err := runtime.hook(config.Driver, config.Point, dnsEngineSwitchJournal{
		Schema:            dnsEngineSwitchJournalSchema,
		Phase:             config.Phase,
		MutationRequestID: strings.Repeat("2", 32),
	})
	if err == nil || !strings.Contains(err.Error(), "request mismatch") {
		t.Fatalf("request mismatch error = %v", err)
	}
	if runtime.fired.Load() {
		t.Fatal("request mismatch consumed the selected boundary")
	}
}

func TestDNSKillMatrixRuntimeDriverMismatchFailsBeforeActions(t *testing.T) {
	config := dnsKillMatrixConfig{
		CellID:    "bind.intent.after.standalone.reachable",
		Driver:    dnsEngineSwitchFaultDriverBIND,
		Point:     dnsEngineSwitchJournalFaultAfterWrite,
		Phase:     dnsSwitchPhaseIntent,
		RequestID: strings.Repeat("1", 32),
		Nonce:     strings.Repeat("a", 32),
		Marker:    filepath.Join(t.TempDir(), "boundary.json"),
		ReadyFD:   9,
	}
	journal := testBINDSwitchJournal(t)
	journal.Phase = config.Phase
	journal.MutationRequestID = config.RequestID
	runtime := &dnsKillMatrixRuntime{config: config}
	err := runtime.hook(
		dnsEngineSwitchFaultDriverSignedUpdateFinalize,
		config.Point,
		journal,
	)
	if err == nil || !strings.Contains(err.Error(), "driver mismatch") {
		t.Fatalf("driver mismatch error = %v", err)
	}
	if runtime.fired.Load() {
		t.Fatal("driver mismatch consumed the selected boundary")
	}
}

func TestDNSKillMatrixRuntimePreIntentRequiresCanonicalIdentity(t *testing.T) {
	config := dnsKillMatrixConfig{
		CellID:    `bind.pre-intent.standalone.reachable`,
		Driver:    `bind`,
		Point:     dnsEngineSwitchJournalFaultPreIntent,
		Phase:     dnsKillMatrixPreIntentPhase,
		RequestID: strings.Repeat(`1`, 32),
		Nonce:     strings.Repeat(`a`, 32),
		Marker:    filepath.Join(t.TempDir(), `boundary.json`),
		ReadyFD:   9,
	}
	validJournal := testBINDSwitchJournal(t)
	manifest, err := switchJournalManifest(validJournal)
	if err != nil {
		t.Fatal(err)
	}
	journal := dnsEngineSwitchJournal{
		Schema:            dnsEngineSwitchJournalSchema,
		Phase:             dnsKillMatrixPreIntentPhase,
		Mode:              manifest.Mode,
		MutationRequestID: config.RequestID,
		MutationOwnerID:   strings.Repeat(`2`, 32),
		ManifestQualifier: `not-a-canonical-qualifier`,
		SourceEngine:      manifest.SourceEngine,
		TargetEngine:      manifest.TargetEngine,
		SourceEpoch:       manifest.SourceEpoch,
		TargetEpoch:       manifest.TargetEpoch,
		SourceRevision:    manifest.SourceRevision,
		Topology:          manifest.Topology,
		PairRole:          manifest.PairRole,
		LocalIP:           manifest.LocalIP,
		LocalNS:           manifest.LocalNS,
		PeerIP:            manifest.PeerIP,
		PeerNS:            manifest.PeerNS,
		SnapshotBytes:     manifest.SnapshotBytes,
		Zones:             manifest.Zones,
	}
	runtime := &dnsKillMatrixRuntime{config: config}
	err = runtime.hook(config.Driver, config.Point, journal)
	if err == nil || !strings.Contains(err.Error(), `pre-intent manifest`) {
		t.Fatalf(`non-canonical pre-intent error = %v`, err)
	}
	if runtime.fired.Load() {
		t.Fatal(`non-canonical pre-intent identity consumed the selected boundary`)
	}
	journal.ManifestQualifier = manifest.Qualifier
	journal.MutationOwnerID = `not-an-owner-id`
	err = runtime.hook(config.Driver, config.Point, journal)
	if err == nil || !strings.Contains(err.Error(), `owner identity`) {
		t.Fatalf(`invalid pre-intent owner error = %v`, err)
	}
	if runtime.fired.Load() {
		t.Fatal(`invalid pre-intent owner consumed the selected boundary`)
	}
}

func TestDNSKillMatrixRollbackPrecursorMapping(t *testing.T) {
	tests := []struct {
		driver string
		phase  string
		want   bool
	}{
		{dnsEngineSwitchFaultDriverBIND, dnsSwitchPhaseTargetStaged, true},
		{dnsEngineSwitchFaultDriverPDNSSwitch, dnsSwitchPhaseTargetStaged, true},
		{dnsEngineSwitchFaultDriverPDNSSecondaryReconfigure, dnsSwitchPhaseTargetStaged, true},
		{dnsEngineSwitchFaultDriverPDNSAdopt, dnsSwitchPhaseIntent, true},
		{dnsEngineSwitchFaultDriverSignedUpdateFinalize, "", false},
	}
	for _, test := range tests {
		for _, selectedPhase := range []string{
			dnsSwitchPhaseRollingBack,
			dnsSwitchPhaseRolledBack,
		} {
			for _, selectedPoint := range []string{
				dnsEngineSwitchJournalFaultBeforeWrite,
				dnsEngineSwitchJournalFaultAfterWrite,
			} {
				config := dnsKillMatrixConfig{
					Driver: test.driver,
					Point:  selectedPoint,
					Phase:  selectedPhase,
				}
				spec, required := dnsKillMatrixRollbackPrecursorFor(config)
				if required != test.want {
					t.Fatalf(
						"driver=%s selected=%s/%s required=%v, want %v",
						test.driver, selectedPhase, selectedPoint, required, test.want,
					)
				}
				if test.want && (spec.Point != dnsEngineSwitchJournalFaultAfterWrite ||
					spec.Phase != test.phase) {
					t.Fatalf(
						"driver=%s selected=%s/%s precursor=%+v, want %s/%s",
						test.driver, selectedPhase, selectedPoint, spec,
						test.phase, dnsEngineSwitchJournalFaultAfterWrite,
					)
				}
			}
		}
	}
	if spec, required := dnsKillMatrixRollbackPrecursorFor(dnsKillMatrixConfig{
		Driver: dnsEngineSwitchFaultDriverBIND,
		Point:  dnsEngineSwitchJournalFaultAfterWrite,
		Phase:  dnsSwitchPhaseTargetStaged,
	}); required || spec != (dnsKillMatrixRollbackPrecursorSpec{}) {
		t.Fatalf("forward selector unexpectedly requires precursor: %+v/%v", spec, required)
	}
}

func TestDNSKillMatrixRuntimeRollbackPrecursorThenSelectedBoundary(t *testing.T) {
	drivers := []struct {
		driver         string
		precursorPhase string
	}{
		{dnsEngineSwitchFaultDriverBIND, dnsSwitchPhaseTargetStaged},
		{dnsEngineSwitchFaultDriverPDNSSwitch, dnsSwitchPhaseTargetStaged},
		{dnsEngineSwitchFaultDriverPDNSSecondaryReconfigure, dnsSwitchPhaseTargetStaged},
		{dnsEngineSwitchFaultDriverPDNSAdopt, dnsSwitchPhaseIntent},
	}
	for _, test := range drivers {
		for _, selectedPhase := range []string{
			dnsSwitchPhaseRollingBack,
			dnsSwitchPhaseRolledBack,
		} {
			for _, selectedPoint := range []string{
				dnsEngineSwitchJournalFaultBeforeWrite,
				dnsEngineSwitchJournalFaultAfterWrite,
			} {
				name := strings.Join([]string{
					test.driver, selectedPhase, selectedPoint,
				}, "/")
				t.Run(name, func(t *testing.T) {
					config := dnsKillMatrixConfig{
						CellID:    "rollback-precursor",
						Driver:    test.driver,
						Point:     selectedPoint,
						Phase:     selectedPhase,
						RequestID: strings.Repeat("1", 32),
						Nonce:     strings.Repeat("a", 32),
						Marker:    filepath.Join(t.TempDir(), "boundary.json"),
						ReadyFD:   9,
					}
					var order []string
					var captured dnsKillMatrixMarker
					runtime := &dnsKillMatrixRuntime{
						config: config,
						ops: dnsKillMatrixRuntimeOps{
							pid:        func() int { return 4321 },
							startTicks: func(int) (string, error) { return "987654", nil },
							writeMarker: func(_ string, marker dnsKillMatrixMarker) error {
								order = append(order, "marker")
								captured = marker
								return nil
							},
							notifyReady: func(int, string) error {
								order = append(order, "ready")
								return nil
							},
							stopProcess: func(int) error {
								order = append(order, "stop")
								return nil
							},
							now: func() time.Time {
								return time.Date(2026, time.August, 31, 12, 34, 56, 0, time.UTC)
							},
						},
					}
					journal := testBINDSwitchJournal(t)
					journal.MutationRequestID = config.RequestID
					journal.Phase = test.precursorPhase
					err := runtime.hook(
						config.Driver,
						dnsEngineSwitchJournalFaultAfterWrite,
						journal,
					)
					if !errors.Is(err, dnsEngineSwitchRollbackPrecursorError) {
						t.Fatalf("precursor error = %v", err)
					}
					if runtime.fired.Load() || len(order) != 0 {
						t.Fatalf("precursor fired=%v operations=%v", runtime.fired.Load(), order)
					}

					journal.Phase = config.Phase
					err = runtime.hook(config.Driver, config.Point, journal)
					if !errors.Is(err, dnsKillMatrixResumedError) {
						t.Fatalf("selected rollback boundary error = %v", err)
					}
					if want := []string{"marker", "ready", "stop"}; !reflect.DeepEqual(order, want) {
						t.Fatalf("rollback boundary order = %v, want %v", order, want)
					}
					precursor := captured.RollbackPrecursor
					if precursor == nil ||
						precursor.Schema != dnsKillMatrixRollbackPrecursorSchema ||
						precursor.Driver != config.Driver ||
						precursor.ObservedDriver != config.Driver ||
						precursor.Point != dnsEngineSwitchJournalFaultAfterWrite ||
						precursor.Phase != test.precursorPhase ||
						precursor.RequestID != config.RequestID ||
						precursor.Action != dnsKillMatrixRollbackPrecursorAction ||
						precursor.ObservedJournal.Phase != test.precursorPhase ||
						precursor.ObservedJournal.MutationRequestID != config.RequestID ||
						precursor.ObservedJournal.Path != dnsEngineSwitchJournalPath() {
						t.Fatalf("rollback precursor evidence = %+v", precursor)
					}
				})
			}
		}
	}
}

func TestDNSKillMatrixRuntimeRollbackBoundaryFailsClosedWithoutExactPrecursor(
	t *testing.T,
) {
	config := dnsKillMatrixConfig{
		CellID:    "rollback-precursor-required",
		Driver:    dnsEngineSwitchFaultDriverBIND,
		Point:     dnsEngineSwitchJournalFaultBeforeWrite,
		Phase:     dnsSwitchPhaseRollingBack,
		RequestID: strings.Repeat("1", 32),
		Nonce:     strings.Repeat("a", 32),
		Marker:    filepath.Join(t.TempDir(), "boundary.json"),
		ReadyFD:   9,
	}
	journal := testBINDSwitchJournal(t)
	journal.MutationRequestID = config.RequestID
	journal.Phase = config.Phase
	runtime := &dnsKillMatrixRuntime{config: config}
	err := runtime.hook(config.Driver, config.Point, journal)
	if err == nil || !strings.Contains(err.Error(), "without one exact precursor") ||
		runtime.fired.Load() {
		t.Fatalf("missing precursor fired=%v error=%v", runtime.fired.Load(), err)
	}

	runtime = &dnsKillMatrixRuntime{config: config}
	journal.Phase = dnsSwitchPhaseTargetStaged
	for call := 0; call < 2; call++ {
		err = runtime.hook(
			config.Driver,
			dnsEngineSwitchJournalFaultAfterWrite,
			journal,
		)
		if call == 0 && !errors.Is(err, dnsEngineSwitchRollbackPrecursorError) {
			t.Fatalf("first precursor error = %v", err)
		}
		if call == 1 && (err == nil || !strings.Contains(err.Error(), "more than once")) {
			t.Fatalf("duplicate precursor error = %v", err)
		}
	}
	journal.Phase = config.Phase
	err = runtime.hook(config.Driver, config.Point, journal)
	if err == nil || !strings.Contains(err.Error(), "without one exact precursor") ||
		runtime.fired.Load() {
		t.Fatalf("invalid precursor fired=%v error=%v", runtime.fired.Load(), err)
	}
}

func TestDNSKillMatrixMarkerIsAtomicAndNoReplace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "boundary.json")
	marker := dnsKillMatrixMarker{
		Schema:            dnsKillMatrixMarkerSchema,
		CellID:            "bind.intent.before.standalone.reachable",
		Driver:            "bind",
		Point:             dnsEngineSwitchJournalFaultBeforeWrite,
		Phase:             dnsSwitchPhaseIntent,
		RequestID:         strings.Repeat("1", 32),
		Nonce:             strings.Repeat("a", 32),
		Marker:            path,
		ReadyFD:           9,
		PID:               123,
		ProcessStartTicks: "456",
		RecordedAt:        "2026-08-31T12:34:56Z",
		ObservedJournal: dnsKillMatrixObservedJournal{
			Path:              "/state/dns-engine-switch-journal.json",
			Schema:            dnsEngineSwitchJournalSchema,
			Phase:             dnsSwitchPhaseIntent,
			MutationRequestID: strings.Repeat("1", 32),
		},
	}
	if err := dnsKillMatrixWriteMarker(path, marker); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded dnsKillMatrixMarker
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	if !reflect.DeepEqual(decoded, marker) {
		t.Fatalf("decoded marker = %+v, want %+v", decoded, marker)
	}
	status, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if status.Mode().Perm() != 0o600 {
		t.Fatalf("marker mode = %o", status.Mode().Perm())
	}
	if err := dnsKillMatrixWriteMarker(path, dnsKillMatrixMarker{Schema: "replacement"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("replace existing marker error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, raw) {
		t.Fatal("existing marker was overwritten")
	}
}

func TestDNSKillMatrixMarkerRejectsSymlinkParent(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(root, "link")
	if err := os.Symlink(realParent, linkParent); err != nil {
		t.Fatal(err)
	}
	err := dnsKillMatrixWriteMarker(
		filepath.Join(linkParent, "boundary.json"),
		dnsKillMatrixMarker{Schema: dnsKillMatrixMarkerSchema},
	)
	if err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("symlink parent error = %v", err)
	}
}

func TestDNSKillMatrixReadyNotificationCarriesNonceAndCloses(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	nonce := strings.Repeat("b", 32)
	writerFD := int(writer.Fd())
	if err := dnsKillMatrixPrepareReadyFD(writerFD); err != nil {
		t.Fatalf("prepare ready descriptor: %v", err)
	}
	if err := dnsKillMatrixNotifyReady(writerFD, nonce); err != nil {
		t.Fatalf("notify ready: %v", err)
	}
	_ = writer.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != nonce+"\n" {
		t.Fatalf("ready payload = %q", raw)
	}
}

func TestDNSKillMatrixRealSIGSTOPSIGKILLExit137(t *testing.T) {
	if os.Getenv(dnsKillMatrixHelperProcessEnv) == "1" {
		journal := testBINDSwitchJournal(t)
		journal.Phase = dnsSwitchPhaseSourceStopped
		journal.MutationRequestID = os.Getenv(dnsKillMatrixEnvRequestID)
		if dnsEngineSwitchJournalFaultHook == nil {
			t.Fatal("tagged helper process has no DNS kill-matrix hook")
		}
		if err := dnsEngineSwitchJournalFaultHook(
			dnsEngineSwitchFaultDriverBIND,
			dnsEngineSwitchJournalFaultBeforeWrite, journal,
		); err != nil {
			t.Fatalf("real DNS kill-matrix hook returned: %v", err)
		}
		t.Fatal("real DNS kill-matrix hook returned without stopping")
	}

	root := t.TempDir()
	markerPath := filepath.Join(root, "boundary.json")
	logPath := filepath.Join(root, "child.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyReader.Close()
	defer readyWriter.Close()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	selector := testDNSKillMatrixEnvironment(markerPath)
	selector[dnsKillMatrixEnvReadyFD] = "3"
	command := exec.Command(
		executable, "-test.run=^TestDNSKillMatrixRealSIGSTOPSIGKILLExit137$",
	)
	command.Env = dnsKillMatrixHelperEnvironment(selector, root)
	command.ExtraFiles = []*os.File{readyWriter}
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	_ = readyWriter.Close()

	waited := false
	var waitErr error
	waitChild := func(kill bool) error {
		if waited {
			return waitErr
		}
		if kill && command.Process != nil {
			_ = syscall.Kill(command.Process.Pid, syscall.SIGKILL)
		}
		waitErr = command.Wait()
		waited = true
		return waitErr
	}
	t.Cleanup(func() {
		_ = waitChild(true)
	})
	fail := func(format string, arguments ...any) {
		_ = waitChild(true)
		_ = logFile.Close()
		childLog, _ := os.ReadFile(logPath)
		t.Fatalf(format+"\nchild output:\n%s", append(arguments, childLog)...)
	}

	if err := readyReader.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		fail("set helper ready deadline: %v", err)
	}
	readyLine, err := bufio.NewReader(readyReader).ReadString('\n')
	if err != nil {
		fail("read helper ready nonce: %v", err)
	}
	if readyLine != selector[dnsKillMatrixEnvNonce]+"\n" {
		fail("helper ready nonce = %q", readyLine)
	}

	rawMarker, err := os.ReadFile(markerPath)
	if err != nil {
		fail("read durable helper marker: %v", err)
	}
	var marker dnsKillMatrixMarker
	if err := json.Unmarshal(rawMarker, &marker); err != nil {
		fail("decode durable helper marker: %v", err)
	}
	if marker.Schema != dnsKillMatrixMarkerSchema ||
		marker.CellID != selector[dnsKillMatrixEnvCellID] ||
		marker.Driver != selector[dnsKillMatrixEnvDriver] ||
		marker.ObservedDriver != selector[dnsKillMatrixEnvDriver] ||
		marker.Point != selector[dnsKillMatrixEnvPoint] ||
		marker.Phase != selector[dnsKillMatrixEnvPhase] ||
		marker.RequestID != selector[dnsKillMatrixEnvRequestID] ||
		marker.Nonce != selector[dnsKillMatrixEnvNonce] ||
		marker.Marker != markerPath || marker.ReadyFD != 3 ||
		marker.PID != command.Process.Pid ||
		marker.ObservedJournal.Phase != selector[dnsKillMatrixEnvPhase] ||
		marker.ObservedJournal.MutationRequestID != selector[dnsKillMatrixEnvRequestID] ||
		filepath.Base(marker.ObservedJournal.Path) != dnsEngineSwitchJournalFile {
		fail("durable helper marker identity = %+v", marker)
	}

	deadline := time.Now().Add(5 * time.Second)
	lastState := ""
	for {
		state, stateErr := dnsKillMatrixTestProcessState(command.Process.Pid)
		if stateErr == nil {
			lastState = state
			if state == "T" {
				break
			}
		}
		if time.Now().After(deadline) {
			fail("helper did not enter SIGSTOP state; last state=%q err=%v", lastState, stateErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
	startTicks, err := serviceMutationProcessStartIdentity(command.Process.Pid)
	if err != nil {
		fail("read stopped helper start ticks: %v", err)
	}
	if startTicks != marker.ProcessStartTicks {
		fail(
			"stopped helper start ticks = %q, marker = %q",
			startTicks, marker.ProcessStartTicks,
		)
	}

	if err := syscall.Kill(command.Process.Pid, syscall.SIGKILL); err != nil {
		fail("send SIGKILL to stopped helper: %v", err)
	}
	waitErr = waitChild(false)
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		fail("SIGKILL helper wait error = %v", waitErr)
	}
	waitStatus, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !waitStatus.Signaled() || waitStatus.Signal() != syscall.SIGKILL {
		fail("helper wait status = %v, want signal 9", exitErr.Sys())
	}
	status, err := dnsKillMatrixNormalizedShellStatus(waitErr)
	if err != nil {
		fail("normalize helper exit status: %v", err)
	}
	if status != 137 {
		fail("helper exit status = %d, want exactly 137", status)
	}
}

func dnsKillMatrixHelperEnvironment(
	selector map[string]string,
	temporaryRoot string,
) []string {
	blocked := map[string]bool{
		dnsKillMatrixHelperProcessEnv: true,
		"TMPDIR":                      true,
	}
	for _, name := range dnsKillMatrixEnvironment {
		blocked[name] = true
	}
	environment := make([]string, 0, len(os.Environ())+len(selector)+2)
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && !blocked[name] {
			environment = append(environment, entry)
		}
	}
	for _, name := range dnsKillMatrixEnvironment {
		environment = append(environment, name+"="+selector[name])
	}
	return append(
		environment,
		dnsKillMatrixHelperProcessEnv+"=1",
		"TMPDIR="+temporaryRoot,
	)
}

func dnsKillMatrixTestProcessState(pid int) (string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	text := string(raw)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+2 >= len(text) {
		return "", fmt.Errorf("invalid /proc stat for pid %d", pid)
	}
	fields := strings.Fields(text[end+2:])
	if len(fields) == 0 || len(fields[0]) != 1 {
		return "", fmt.Errorf("short /proc stat for pid %d", pid)
	}
	return fields[0], nil
}

func dnsKillMatrixNormalizedShellStatus(waitErr error) (int, error) {
	if waitErr == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		return 0, waitErr
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return 0, errors.New("DNS kill-matrix helper has no wait status")
	}
	if status.Signaled() {
		return 128 + int(status.Signal()), nil
	}
	return status.ExitStatus(), nil
}
