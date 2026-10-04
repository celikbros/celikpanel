//go:build linux && celikpanel_dns_v3_native

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

// This test is an explicit disposable-guest probe. Ordinary `go test` skips it;
// no build or runtime product entry point can call it. Its authorization is a
// root-owned receipt created outside the test on the exact QEMU fixture boot.
const (
	freshV3NativeCell   = "pdns-switch__intent__after-write__paired-primary__peer-reachable"
	freshV3NativeRoot   = "/var/lib/celikpanel-dns-kill-matrix"
	freshV3NativeAuth   = freshV3NativeRoot + "/v3-native-authorization.json"
	freshV3NativeSchema = "celikpanel/pdns-v3-native-test-authorization/v1"
)

type freshV3NativeAuthorization struct {
	Schema         string `json:"schema"`
	CellID         string `json:"cell_id"`
	Node           string `json:"node"`
	MachineID      string `json:"machine_id"`
	BootID         string `json:"boot_id"`
	RequestID      string `json:"request_id"`
	OwnerID        string `json:"owner_id"`
	Nonce          string `json:"nonce"`
	ScenarioSHA256 string `json:"scenario_sha256"`
}

type freshV3NativeScenario struct {
	Schema         string                                  `json:"schema"`
	Driver         string                                  `json:"driver"`
	SourceFixture  string                                  `json:"source_fixture"`
	Mode           string                                  `json:"mode"`
	SourceEngine   transport.DNSEngine                     `json:"source_engine"`
	TargetEngine   transport.DNSEngine                     `json:"target_engine"`
	SourceEpoch    int64                                   `json:"source_epoch"`
	TargetEpoch    int64                                   `json:"target_epoch"`
	SourceRevision int64                                   `json:"source_revision"`
	Topology       string                                  `json:"topology"`
	PairRole       string                                  `json:"pair_role"`
	LocalIP        string                                  `json:"local_ip"`
	LocalNS        string                                  `json:"local_ns"`
	PeerIP         string                                  `json:"peer_ip"`
	PeerNS         string                                  `json:"peer_ns"`
	Zones          []transport.DNSEngineSwitchZoneSnapshot `json:"zones"`
}

type freshV3NativeSourceProof struct {
	Schema                   string `json:"schema"`
	CellID                   string `json:"cell_id"`
	SourceFixture            string `json:"source_fixture"`
	Engine                   string `json:"engine"`
	EngineEpoch              int64  `json:"engine_epoch"`
	SourceRevision           int64  `json:"source_revision"`
	ServingBeforeTaggedAgent bool   `json:"serving_before_tagged_agent"`
	ScenarioSHA256           string `json:"scenario_sha256"`
}

func freshV3NativeRootFile(path string, mode os.FileMode, limit int64) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap fixture descriptor: %s", path)
	}
	defer f.Close()
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Nlink != 1 ||
		os.FileMode(st.Mode).Perm() != mode || st.Size <= 0 || st.Size > limit {
		return nil, fmt.Errorf("unsafe fixture file metadata: %s", path)
	}
	return io.ReadAll(io.LimitReader(f, limit+1))
}

func freshV3NativeDecodeExact(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("fixture JSON contains trailing data")
	}
	return nil
}

func freshV3NativePreflight(allowJournal bool) (freshV3NativeAuthorization, mutationpayload.DNSEngineSwitchManifestCommitment, error) {
	var zero freshV3NativeAuthorization
	var empty mutationpayload.DNSEngineSwitchManifestCommitment
	for _, name := range []string{"CELIKPANEL_AGENT_STATE_DIR", "CELIKPANEL_PDNS_DB", "CELIKPANEL_MUTATION_LOCK", "CELIKPANEL_MUTATION_LOCK_FD", "CELIKPANEL_DISPOSABLE_MAIL_VM"} {
		if _, present := os.LookupEnv(name); present {
			return zero, empty, fmt.Errorf("native V3 fixture refuses environment override %s", name)
		}
	}
	if serviceMutationStateDirectory() != "/var/lib/celikpanel-agent-private" || pdnsDBPath() != "/var/lib/powerdns/pdns.sqlite3" || serviceMutationLockFile() != "/run/celikpanel/service-mutation.lock" {
		return zero, empty, errors.New("native V3 fixture paths differ from production")
	}
	if os.Geteuid() != 0 {
		return zero, empty, errors.New("native V3 probe requires root in the disposable guest")
	}
	// Production celikpanel-agent runs as root:celikpanel; the group owns
	// durable mutation evidence. A root:root test produces invalid receipts.
	group, err := user.LookupGroup("celikpanel")
	if err != nil {
		return zero, empty, fmt.Errorf("native V3 fixture group: %w", err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil || os.Getegid() != gid {
		return zero, empty, fmt.Errorf("native V3 probe requires root:celikpanel (effective gid=%d, expected=%s)", os.Getegid(), group.Gid)
	}
	marker, err := freshV3NativeRootFile("/etc/celikpanel-dns-kill-matrix", 0o444, 4096)
	if err != nil {
		return zero, empty, fmt.Errorf("fixture marker: %w", err)
	}
	for _, line := range []string{"schema=celikpanel/dns-kill-fixture-plan/v1", "cell_id=" + freshV3NativeCell, "node=debian13"} {
		if !strings.Contains("\n"+string(marker), "\n"+line+"\n") {
			return zero, empty, fmt.Errorf("fixture marker lacks %s", line)
		}
	}
	authBytes, err := freshV3NativeRootFile(freshV3NativeAuth, 0o600, 4096)
	if err != nil {
		return zero, empty, fmt.Errorf("native authorization: %w", err)
	}
	var auth freshV3NativeAuthorization
	if err := freshV3NativeDecodeExact(authBytes, &auth); err != nil {
		return zero, empty, err
	}
	machine, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return zero, empty, err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return zero, empty, err
	}
	if auth.Schema != freshV3NativeSchema || auth.CellID != freshV3NativeCell || auth.Node != "debian13" ||
		auth.MachineID == "" || auth.MachineID != strings.TrimSpace(string(machine)) ||
		auth.BootID == "" || auth.BootID != strings.TrimSpace(string(boot)) ||
		!validMutationIdentity(auth.RequestID) || !validMutationIdentity(auth.OwnerID) ||
		len(auth.Nonce) < 32 || len(auth.Nonce) > 128 || len(auth.Nonce)%2 != 0 ||
		strings.ToLower(auth.Nonce) != auth.Nonce {
		return zero, empty, errors.New("native authorization differs from this boot or request")
	}
	if _, err := hex.DecodeString(auth.Nonce); err != nil {
		return zero, empty, err
	}
	if auth.ScenarioSHA256 == "" || len(auth.ScenarioSHA256) != 64 {
		return zero, empty, errors.New("native authorization has no scenario digest")
	}
	scenarioBytes, err := freshV3NativeRootFile(freshV3NativeRoot+"/scenario.json", 0o600, 1<<20)
	if err != nil {
		return zero, empty, err
	}
	digest := sha256.Sum256(scenarioBytes)
	if auth.ScenarioSHA256 != hex.EncodeToString(digest[:]) {
		return zero, empty, errors.New("authorized scenario changed")
	}
	var scenario freshV3NativeScenario
	if err := freshV3NativeDecodeExact(scenarioBytes, &scenario); err != nil {
		return zero, empty, err
	}
	if scenario.Schema != "celikpanel-dns-kill-matrix-trigger/v1" || scenario.Driver != "pdns-switch" ||
		scenario.SourceFixture != "uninitialized" || scenario.Mode != transport.DNSEngineSwitchModeSwitch ||
		scenario.SourceEngine != "" || scenario.TargetEngine != transport.DNSEnginePowerDNS ||
		scenario.SourceEpoch != 0 || scenario.TargetEpoch != 1 || scenario.SourceRevision != 0 ||
		scenario.Topology != transport.DNSTopologyPaired || scenario.PairRole != transport.DNSPairRolePrimary ||
		scenario.LocalIP != "192.0.2.10" || scenario.PeerIP != "192.0.2.11" ||
		scenario.LocalNS != "ns1.s1-kill.test" || scenario.PeerNS != "ns2.s1-kill.test" || len(scenario.Zones) != 1 {
		return zero, empty, errors.New("scenario is not the exact uninitialized Debian paired-primary fixture")
	}
	proofBytes, err := freshV3NativeRootFile(freshV3NativeRoot+"/source-proof.json", 0o600, 1<<20)
	if err != nil {
		return zero, empty, err
	}
	var proof freshV3NativeSourceProof
	// The existing source proof deliberately has additional independently checked fields.
	if err := json.Unmarshal(proofBytes, &proof); err != nil {
		return zero, empty, err
	}
	if proof.Schema != "celikpanel/dns-kill-source-proof/v1" || proof.CellID != freshV3NativeCell ||
		proof.SourceFixture != "uninitialized" || proof.Engine != "" || proof.EngineEpoch != 0 ||
		proof.SourceRevision != 0 || proof.ServingBeforeTaggedAgent || proof.ScenarioSHA256 != auth.ScenarioSHA256 {
		return zero, empty, errors.New("uninitialized source proof differs from authorized scenario")
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		scenario.Mode, scenario.SourceEngine, scenario.TargetEngine, scenario.SourceEpoch,
		scenario.TargetEpoch, scenario.SourceRevision, scenario.Topology, scenario.PairRole,
		scenario.LocalIP, scenario.LocalNS, scenario.PeerIP, scenario.PeerNS, scenario.Zones,
	)
	if err != nil {
		return zero, empty, err
	}
	if !allowJournal {
		if _, exists, err := readDNSEngineState(); err != nil || exists {
			return zero, empty, errors.Join(errors.New("fixture DNS state must be absent"), err)
		}
		if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
			return zero, empty, errors.Join(errors.New("fixture switch journal must be absent"), err)
		}
		if _, err := os.Lstat(freshV3NativeRoot + "/v3-native-boundary.json"); !errors.Is(err, os.ErrNotExist) {
			return zero, empty, errors.New("native boundary marker already exists before production")
		}
	}
	if _, err := os.Lstat("/run/celikpanel/agent.sock"); !errors.Is(err, os.ErrNotExist) {
		return zero, empty, errors.New("production Agent socket must be absent")
	}
	for _, unit := range []string{"celikpanel-agent.service", "celikpanel-panel.service"} {
		output, err := exec.Command("/usr/bin/systemctl", "show", unit, "--property=ActiveState", "--value").CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "inactive" {
			return zero, empty, fmt.Errorf("disposable fixture management service %s is not inactive: %s: %w", unit, strings.TrimSpace(string(output)), err)
		}
	}
	if _, err := os.Lstat("/opt/celikpanel/bin/agent.kill"); err != nil {
		return zero, empty, errors.New("disposable fixture tagged Agent is absent")
	}
	return auth, manifest, nil
}

type freshV3NativeBoundary struct {
	Schema    string `json:"schema"`
	CellID    string `json:"cell_id"`
	MachineID string `json:"machine_id"`
	BootID    string `json:"boot_id"`
	RequestID string `json:"request_id"`
	OwnerID   string `json:"owner_id"`
	Nonce     string `json:"nonce"`
	Qualifier string `json:"qualifier"`
	Phase     string `json:"phase"`
	PID       int    `json:"pid"`
}

func freshV3NativeBoundaryFor(auth freshV3NativeAuthorization, qualifier string, pid int) freshV3NativeBoundary {
	return freshV3NativeBoundary{
		Schema: "celikpanel/pdns-v3-native-test-boundary/v1", CellID: auth.CellID,
		MachineID: auth.MachineID, BootID: auth.BootID,
		RequestID: auth.RequestID, OwnerID: auth.OwnerID, Nonce: auth.Nonce,
		Qualifier: qualifier, Phase: dnsengineartifact.SwitchPhaseTargetEnableIntent, PID: pid,
	}
}

func freshV3NativePublishBoundary(value freshV3NativeBoundary) error {
	path := freshV3NativeRoot + "/v3-native-boundary.json"
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return syncAtomicParentDirectory(freshV3NativeRoot)
}

func freshV3NativeVerifyBoundary(auth freshV3NativeAuthorization, qualifier string) error {
	raw, err := freshV3NativeRootFile(freshV3NativeRoot+"/v3-native-boundary.json", 0o600, 4096)
	if err != nil {
		return err
	}
	var actual freshV3NativeBoundary
	if err := freshV3NativeDecodeExact(raw, &actual); err != nil {
		return err
	}
	if actual.PID <= 1 || actual != freshV3NativeBoundaryFor(auth, qualifier, actual.PID) {
		return errors.New("native cut marker differs from this boot and exact request")
	}
	return nil
}

// Admit through the production coordinator so privileged package and rollback
// workers receive a durable execution tracker under the shared host lock.
func freshV3NativeBeginStep(auth freshV3NativeAuthorization, manifest mutationpayload.DNSEngineSwitchManifestCommitment) (*serviceMutationManager, context.Context, func(), transport.ServiceMutationBinding, error) {
	manager, err := newServiceMutationManager("", "")
	if err != nil {
		return nil, nil, nil, transport.ServiceMutationBinding{}, err
	}
	job, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: auth.RequestID, OwnerID: auth.OwnerID,
		Kind: "dns_engine_switch", Target: string(transport.DNSEnginePowerDNS),
		PackageName: manifest.Qualifier,
	})
	if err != nil {
		return nil, nil, nil, transport.ServiceMutationBinding{}, err
	}
	if job == nil || job.RequestID != auth.RequestID || job.OwnerID != auth.OwnerID ||
		job.Status != serviceMutationStatusRunning || manager.ledger.ActiveRequestID != auth.RequestID {
		return nil, nil, nil, transport.ServiceMutationBinding{}, errors.New("native fixture did not receive the exact durable DNS switch lease")
	}
	binding := transport.ServiceMutationBinding{MutationRequestID: auth.RequestID, MutationOwnerID: auth.OwnerID}
	ctx, done, err := manager.acquireStep(binding, newServiceMutationStepClaim(
		serviceMutationStepSwitchDNSEngine, string(transport.DNSEnginePowerDNS),
		manifest.Qualifier, manifest.Mode,
	))
	if err != nil {
		return nil, nil, nil, transport.ServiceMutationBinding{}, err
	}
	if err := markDNSEngineSwitchFinalizing(ctx, transport.DNSEnginePowerDNS, manifest.Qualifier, binding); err != nil {
		done()
		return nil, nil, nil, transport.ServiceMutationBinding{}, err
	}
	return manager, ctx, done, binding, nil
}

func TestNativeFreshPDNSPrimaryV3StartBeforeCheckpoint(t *testing.T) {
	freshV3NativeProduceAtBoundary(t, false)
}

// This cut occurs after enable-intent is fsynced but before the candidate is
// renamed or PowerDNS is started. Owner recovery runs in a separate process.
func TestNativeFreshPDNSPrimaryV3PrestartInverse(t *testing.T) {
	freshV3NativeProduceAtBoundary(t, true)
}

func freshV3NativeProduceAtBoundary(t *testing.T, prestart bool) {
	if os.Getenv("CELIKPANEL_V3_NATIVE_TRIAL") != freshV3NativeCell {
		t.Skip("explicit disposable V3 native trial only")
	}
	mode := os.Getenv("CELIKPANEL_V3_NATIVE_MODE")
	if mode != "error" && mode != "kill" {
		t.Fatal("native V3 producer mode must be error or kill")
	}
	if prestart && mode != "kill" {
		t.Fatal("prestart inverse trial requires a real process kill")
	}
	auth, manifest, err := freshV3NativePreflight(false)
	if err != nil {
		t.Fatal(err)
	}
	manager, ctx, done, binding, err := freshV3NativeBeginStep(auth, manifest)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	// Mirror the panel's renewal of the exact accepted lease during a long
	// native package install. No status poll starts another mutation.
	heartbeatStop := make(chan struct{})
	heartbeatDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatStop:
				heartbeatDone <- nil
				return
			case <-ticker.C:
				_, err := manager.heartbeat(&ServiceMutationHeartbeatRequest{
					RequestID: auth.RequestID, OwnerID: auth.OwnerID,
				})
				if err != nil {
					heartbeatDone <- err
					return
				}
			}
		}
	}()
	var heartbeatOnce sync.Once
	stopHeartbeat := func() {
		heartbeatOnce.Do(func() {
			close(heartbeatStop)
			if err := <-heartbeatDone; err != nil {
				t.Errorf("native fixture lease renewal failed: %v", err)
			}
		})
	}
	defer stopHeartbeat()
	cut := errors.New("native V3 start-before-checkpoint interruption")
	fired := false
	previous := dnsEngineSwitchJournalFaultHook
	dnsEngineSwitchJournalFaultHook = func(driver, point string, j dnsEngineSwitchJournal) error {
		cutPoint := point == dnsEngineSwitchJournalFaultBeforeWrite && j.Phase == dnsSwitchPhaseTargetStarted
		if prestart {
			cutPoint = point == dnsEngineSwitchJournalFaultAfterWrite && j.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent
		}
		if driver == dnsEngineSwitchFaultDriverPDNSSwitch && cutPoint && j.Schema == "celikpanel-dns-engine-switch-journal/v3" &&
			j.MutationRequestID == auth.RequestID && j.MutationOwnerID == auth.OwnerID && j.ManifestQualifier == manifest.Qualifier {
			fired = true
			if mode == "kill" {
				if err := freshV3NativePublishBoundary(freshV3NativeBoundaryFor(auth, manifest.Qualifier, os.Getpid())); err != nil {
					return err
				}
				if err := unix.Kill(os.Getpid(), unix.SIGKILL); err != nil {
					return err
				}
				return errors.New("SIGKILL unexpectedly returned")
			}
			return cut
		}
		return nil
	}
	_, producerErr := switchToPDNS(ctx, manifest, binding)
	dnsEngineSwitchJournalFaultHook = previous
	if mode == "kill" {
		t.Fatalf("hard-kill boundary returned: fired=%v err=%v", fired, producerErr)
	}
	if !fired || !errors.Is(producerErr, cut) {
		t.Fatalf("real producer missed start-before-checkpoint cut: fired=%v err=%v", fired, producerErr)
	}
	j, exists, err := readDNSEngineSwitchJournal()
	if err != nil || !exists || j.Phase != "target-enable-intent" || j.MutationRequestID != auth.RequestID {
		t.Fatalf("interrupted journal is not exact enable intent: exists=%v phase=%s err=%v", exists, j.Phase, err)
	}
	backend := hostDNSEngineBackend{}
	outcome, err := backend.RecoverSwitch(ctx, transport.DNSEnginePowerDNS, manifest.Qualifier, binding)
	if err != nil {
		t.Fatalf("same-request backend native recovery remains pending: %v", err)
	}
	if outcome != dnsEngineSwitchRecoveryCommitted {
		t.Fatalf("unexpected native recovery outcome: %s", outcome)
	}
	if err := backend.FinalizeSwitch(ctx, transport.DNSEnginePowerDNS, manifest.Qualifier, binding); err != nil {
		t.Fatalf("same-request native finalization remains pending: %v", err)
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		t.Fatalf("native journal retirement unproved: exists=%v err=%v", exists, err)
	}
	stopHeartbeat()
	if err := publishFinalizedDNSEngineSwitchTerminal(ctx, transport.DNSEnginePowerDNS, manifest.Qualifier, binding); err != nil {
		t.Fatalf("durable terminal DNS switch receipt was not published: %v", err)
	}
	if job := manager.status(auth.RequestID); job == nil || job.Status != serviceMutationStatusSucceeded {
		t.Fatalf("coordinator did not publish an exact terminal job: %+v", job)
	}
	t.Logf("backend-native V3 producer and same-request recovery passed on disposable fixture; request=%s boot=%s (ledger/RPC acceptance not measured)", auth.RequestID, auth.BootID)
}

// Recovery is a separate process after the producer's verified exit 137. This
// test does not claim that its marker alone proves a kill: the QEMU controller
// must record the actual process exit, native serving state, and boot identity.
func TestNativeFreshPDNSPrimaryV3RecoverAfterKill(t *testing.T) {
	if os.Getenv("CELIKPANEL_V3_NATIVE_TRIAL") != freshV3NativeCell ||
		os.Getenv("CELIKPANEL_V3_NATIVE_MODE") != "recover" {
		t.Skip("explicit disposable V3 native recovery only")
	}
	auth, manifest, err := freshV3NativePreflight(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := freshV3NativeVerifyBoundary(auth, manifest.Qualifier); err != nil {
		t.Fatal(err)
	}
	journal, exists, err := readDNSEngineSwitchJournal()
	phaseCanResume := journal.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent ||
		journal.Phase == dnsengineartifact.SwitchPhaseTargetStarted ||
		journal.Phase == dnsengineartifact.SwitchPhaseTargetVerified ||
		journal.Phase == dnsengineartifact.SwitchPhaseCommitted
	if err != nil || !exists || journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
		!phaseCanResume || journal.MutationRequestID != auth.RequestID || journal.MutationOwnerID != auth.OwnerID ||
		journal.ManifestQualifier != manifest.Qualifier {
		t.Fatalf("native kill left no exact resumable V3 journal: exists=%v phase=%s err=%v", exists, journal.Phase, err)
	}
	ledgerRaw, ledgerExists, err := readSecureServiceMutationLedger(
		serviceMutationStateDirectory()+"/service-mutations.json", serviceMutationLedgerMaxSize,
	)
	if err != nil || !ledgerExists {
		t.Fatalf("durable coordinator ledger unavailable: exists=%v err=%v", ledgerExists, err)
	}
	priorLedger, err := decodeServiceMutationLedger(ledgerRaw)
	if err != nil {
		t.Fatal(err)
	}
	identity := dnsengineartifact.SwitchIdentity{
		RequestID: auth.RequestID, OwnerID: auth.OwnerID,
		Target: transport.DNSEnginePowerDNS, Qualifier: manifest.Qualifier,
	}
	wasReleasedUnknown := identity.ReleasedUndecidedJob(priorLedger)
	if !wasReleasedUnknown && priorLedger.ActiveRequestID != auth.RequestID {
		t.Fatal("neither the exact active job nor its released-unknown receipt survives")
	}
	manager, err := newServiceMutationManager("", "")
	if err != nil {
		t.Fatalf("durable coordinator restart could not reconcile the exact switch: %v", err)
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		t.Fatalf("native journal retirement unproved: exists=%v err=%v", exists, err)
	}
	job := manager.status(auth.RequestID)
	if job == nil || job.Kind != "dns_engine_switch" || job.Target != "pdns" ||
		job.PackageName != manifest.Qualifier {
		t.Fatalf("coordinator lost the exact durable job: %+v", job)
	}
	if wasReleasedUnknown {
		if job.Status != serviceMutationStatusFailed || job.ErrorCode != dnsengineartifact.ReleasedNativeUnknownCode {
			t.Fatalf("later host reconciliation rewrote a known historical failure: %+v", job)
		}
	} else if job.Status != serviceMutationStatusSucceeded {
		t.Fatalf("first recovery did not publish success: %+v", job)
	}
	t.Logf("native V3 process-kill producer and coordinator recovery passed on disposable fixture; request=%s boot=%s (public paired-primary RPC acceptance remains paused)", auth.RequestID, auth.BootID)
}
