//go:build linux

package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type vpnRPCTestHost struct {
	configPath     string
	sysctlPath     string
	forwardingPath string
	interfacePath  string
	enabledPath    string
	liveConfigPath string
}

const vpnRPCTestAsyncTimeout = 30 * time.Second

func newVPNRPCTestHost(t *testing.T) vpnRPCTestHost {
	t.Helper()
	root := t.TempDir()
	configDirectory := filepath.Join(root, "wireguard")
	sysctlDirectory := filepath.Join(root, "sysctl.d")
	for _, directory := range []string{configDirectory, sysctlDirectory} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	previousConfigDirectory := wgConfDir
	previousSysctlPath := wgSysctlPath
	previousOwner := repoFileOwnerUID
	wgConfDir = configDirectory
	wgSysctlPath = filepath.Join(sysctlDirectory, "99-celikpanel-vpn.conf")
	repoFileOwnerUID = func(os.FileInfo) (uint32, bool) { return 0, true }
	t.Cleanup(func() {
		wgConfDir = previousConfigDirectory
		wgSysctlPath = previousSysctlPath
		repoFileOwnerUID = previousOwner
	})

	stateDirectory := filepath.Join(root, "state")
	if err := os.Mkdir(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	host := vpnRPCTestHost{
		configPath:     filepath.Join(configDirectory, "wg0.conf"),
		sysctlPath:     wgSysctlPath,
		forwardingPath: filepath.Join(stateDirectory, "forwarding"),
		interfacePath:  filepath.Join(stateDirectory, "interface-up"),
		enabledPath:    filepath.Join(stateDirectory, "unit-enabled"),
		liveConfigPath: filepath.Join(stateDirectory, "live.conf"),
	}
	for path, value := range map[string]string{
		host.forwardingPath: "0\n",
		host.interfacePath:  "0\n",
		host.enabledPath:    "0\n",
		host.liveConfigPath: "",
	} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	binDirectory := filepath.Join(root, "bin")
	if err := os.Mkdir(binDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
command_name="${0##*/}"
case "$command_name" in
  ip)
    printf '%s\n' 'default via 192.0.2.1 dev eth0'
    ;;
  nft)
    if [ "$1" = "-j" ]; then
      printf '%s\n' '{"nftables":[]}'
    fi
    ;;
  sysctl)
    if [ "$1" = "-n" ]; then
      /bin/cat "$VPN_TEST_FORWARDING"
    elif [ "$1" = "-w" ]; then
      value="${2#*=}"
      printf '%s\n' "$value" > "$VPN_TEST_FORWARDING"
      printf 'net.ipv4.ip_forward = %s\n' "$value"
    fi
    ;;
  systemctl)
    case "$1" in
      is-enabled)
        if [ "$(/bin/cat "$VPN_TEST_ENABLED")" = "1" ]; then
          printf '%s\n' enabled
        else
          printf '%s\n' disabled
          exit 1
        fi
        ;;
      enable)
        printf '%s\n' 1 > "$VPN_TEST_ENABLED"
        if [ "$2" = "--now" ]; then
          printf '%s\n' 1 > "$VPN_TEST_INTERFACE"
        fi
        ;;
      show)
        if [ "$(/bin/cat "$VPN_TEST_INTERFACE")" = "1" ]; then
          printf '%s\n' 'ActiveState=active' 'SubState=running' 'Result=success' 'ConditionResult=yes'
        else
          printf '%s\n' 'ActiveState=inactive' 'SubState=dead' 'Result=success' 'ConditionResult=yes'
        fi
        ;;
      stop)
        printf '%s\n' 0 > "$VPN_TEST_INTERFACE"
        ;;
      disable)
        printf '%s\n' 0 > "$VPN_TEST_ENABLED"
        ;;
    esac
    ;;
  wg)
    if [ "$1" = "show" ] && [ "$2" = "interfaces" ]; then
      if [ -n "${VPN_TEST_INTERFACE_PROBE_STARTED:-}" ]; then
        printf '%s\n' started > "$VPN_TEST_INTERFACE_PROBE_STARTED"
        while [ ! -e "$VPN_TEST_INTERFACE_PROBE_RELEASE" ]; do
          /bin/sleep 0.01
        done
      fi
      if [ "${VPN_TEST_FAIL_INTERFACE_PROBE:-0}" = "1" ]; then
        exit 1
      fi
      if [ -n "${VPN_TEST_INTERFACE_PROBE_OUTPUT:-}" ]; then
        printf '%s\n' "$VPN_TEST_INTERFACE_PROBE_OUTPUT"
      elif [ "$(/bin/cat "$VPN_TEST_INTERFACE")" = "1" ]; then
        printf '%s\n' wg0
      fi
    elif [ "$1" = "show" ]; then
      if [ "${VPN_TEST_FAIL_READY_SHOW:-0}" = "1" ] &&
         [ "$(/bin/cat "$VPN_TEST_ENABLED")" = "1" ]; then
        exit 1
      fi
      [ "$(/bin/cat "$VPN_TEST_INTERFACE")" = "1" ] || exit 1
    elif [ "$1" = "syncconf" ]; then
      /bin/cp "$3" "$VPN_TEST_LIVE_CONFIG"
	  if [ "${VPN_TEST_SYNCCONF_FAIL_ALWAYS:-0}" = "1" ]; then
	    exit 1
	  fi
	  if [ -n "${VPN_TEST_SYNCCONF_FAIL_AFTER_FIRST_MARKER:-}" ]; then
	    if [ -e "$VPN_TEST_SYNCCONF_FAIL_AFTER_FIRST_MARKER" ]; then
	      exit 1
	    fi
	    printf '%s\n' first > "$VPN_TEST_SYNCCONF_FAIL_AFTER_FIRST_MARKER"
	  fi
      if [ -n "${VPN_TEST_SYNCCONF_FAIL_ONCE_MARKER:-}" ] &&
         [ ! -e "$VPN_TEST_SYNCCONF_FAIL_ONCE_MARKER" ]; then
        printf '%s\n' failed > "$VPN_TEST_SYNCCONF_FAIL_ONCE_MARKER"
        exit 1
      fi
      if [ -n "${VPN_TEST_SYNCCONF_CANCEL_STARTED:-}" ] &&
         [ ! -e "$VPN_TEST_SYNCCONF_CANCEL_STARTED" ]; then
        printf '%s\n' started > "$VPN_TEST_SYNCCONF_CANCEL_STARTED"
        while [ ! -e "$VPN_TEST_SYNCCONF_CANCEL_RELEASE" ]; do
          /bin/sleep 0.01
        done
      fi
      if [ -n "${VPN_TEST_SYNCCONF_RECOVERY_STARTED:-}" ]; then
        printf '%s\n' started > "$VPN_TEST_SYNCCONF_RECOVERY_STARTED"
        while [ ! -e "$VPN_TEST_SYNCCONF_RECOVERY_RELEASE" ]; do
          /bin/sleep 0.01
        done
      fi
	  if [ "${VPN_TEST_SYNCCONF_FAIL_RECOVERY:-0}" = "1" ] &&
	     [ -e "${VPN_TEST_SYNCCONF_RECOVERY_STARTED:-/nonexistent}" ]; then
	    exit 1
	  fi
    fi
    ;;
  wg-quick)
    if [ "$1" = "strip" ]; then
      case "${2##*/}" in
        wg0.conf) ;;
        *)
          echo 'wg-quick: The config file must be a valid interface name, followed by .conf' >&2
          exit 1
          ;;
      esac
      /bin/cat "$2"
	  if [ -n "${VPN_TEST_REMOVE_STAGE_ONCE_MARKER:-}" ] &&
	     [ ! -e "$VPN_TEST_REMOVE_STAGE_ONCE_MARKER" ]; then
	    printf '%s\n' removed > "$VPN_TEST_REMOVE_STAGE_ONCE_MARKER"
	    /bin/rm -f "$VPN_TEST_CONFIG_DIR"/.wg0.conf.tmp-*.conf
	  fi
    fi
    ;;
esac
`
	scriptPath := filepath.Join(binDirectory, "vpn-command")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scriptPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ip", "nft", "sysctl", "systemctl", "wg", "wg-quick"} {
		if err := os.Link(scriptPath, filepath.Join(binDirectory, name)); err != nil {
			t.Fatal(err)
		}
	}
	previousSystemctlResolver := serviceMutationSystemctlResolver
	serviceMutationSystemctlResolver = func() (string, error) {
		return filepath.Join(binDirectory, "systemctl"), nil
	}
	t.Cleanup(func() { serviceMutationSystemctlResolver = previousSystemctlResolver })
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VPN_TEST_FORWARDING", host.forwardingPath)
	t.Setenv("VPN_TEST_INTERFACE", host.interfacePath)
	t.Setenv("VPN_TEST_ENABLED", host.enabledPath)
	t.Setenv("VPN_TEST_LIVE_CONFIG", host.liveConfigPath)
	t.Setenv("VPN_TEST_CONFIG_DIR", configDirectory)
	return host
}

func beginVPNRPCTestMutation(t *testing.T, kind, packageName string) ServiceMutationBinding {
	t.Helper()
	binding, _ := beginVPNRPCTestMutationWithManager(t, kind, packageName)
	return binding
}

func beginVPNRPCTestMutationWithManager(
	t *testing.T,
	kind, packageName string,
) (ServiceMutationBinding, *serviceMutationManager) {
	t.Helper()
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(t, manager, kind, "wireguard", packageName)
	return ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	}, manager
}

func managedVPNTestConfig(peerLabel string) []byte {
	config := `[Interface]
# CelikPanel VPN server - managed by the panel.
Address = 10.8.0.1/24
ListenPort = 51820
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
PostUp = nft add table inet celikpanel_vpn '{ comment "celikpanel-vpn-managed"; }'
PostDown = nft delete table inet celikpanel_vpn
`
	if peerLabel != "" {
		config += "\n[Peer]\n# " + peerLabel + "\n" +
			"PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
			"PresharedKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n" +
			"AllowedIPs = 10.8.0.2/32\n"
	}
	return []byte(config)
}

func readVPNTestState(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(value))
}

func waitForVPNTestFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(vpnRPCTestAsyncTimeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", filepath.Base(path))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForVPNRecoveryWorker(
	t *testing.T,
	manager *serviceMutationManager,
	wantStatus string,
) *ServiceMutationJob {
	t.Helper()
	deadline := time.Now().Add(vpnRPCTestAsyncTimeout)
	for {
		job := manager.status(testMutationRequestID)
		if job != nil && job.WorkerPID > 0 {
			if job.Status != wantStatus || job.WorkerCommand != "wg" {
				t.Fatalf("recovery worker job=%+v", job)
			}
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovery worker was not durably registered: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSyncVPNPeersLegacyEndpointFailsClosedWithoutLeaseOrHost(t *testing.T) {
	response := SyncVPNPeersResponse{Applied: true, AppliedGeneration: 99, Error: "stale"}
	err := (&Agent{}).SyncVPNPeers(&SyncVPNPeersRequest{
		DesiredGeneration: -1,
		Peers: []VPNPeerSpec{{
			PublicKey: "not-a-key", PresharedKey: "not-a-key", IP: "not-an-address",
		}},
	}, &response)
	if err != nil {
		t.Fatal(err)
	}
	if response.Applied || response.AppliedGeneration != 0 ||
		response.Error != syncVPNPeersLegacyUnsupportedError {
		t.Fatalf("legacy response=%+v", response)
	}
}

func TestSyncVPNPeersV2DirectorySyncFailureVerifiesAndCompletesPublication(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(host.configPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.liveConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	previousSync := syncAtomicParentDirectory
	syncCalls := 0
	syncAtomicParentDirectory = func(string) error {
		syncCalls++
		if syncCalls == 1 {
			return os.ErrInvalid
		}
		return nil
	}
	t.Cleanup(func() { syncAtomicParentDirectory = previousSync })

	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	psk := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	const generation = int64(19)
	peers := []transport.VPNPeerSpec{{
		PublicKey: key, PresharedKey: psk, IP: "10.8.0.9",
	}}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(generation, peers)
	if err != nil {
		t.Fatal(err)
	}
	binding := beginVPNRPCTestMutation(t, "vpn_peer_sync", commitment.Qualifier)
	var response SyncVPNPeersResponse
	if err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
		ServiceMutationBinding: binding,
		DesiredGeneration:      generation,
		Peers:                  peers,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Applied || response.AppliedGeneration != generation || response.Error != "" {
		t.Fatalf("sync response=%+v", response)
	}
	durable, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(host.liveConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(durable, original) {
		t.Fatal("durable config did not retain the verified publication")
	}
	if !bytes.Equal(live, durable) {
		t.Fatalf("live and durable publication differ:\nlive=%s\ndurable=%s", live, durable)
	}
	markerRequestID, markerQualifier, found, err := parseVPNPeerSyncReceiptMarker(durable)
	if err != nil || !found || markerRequestID != testMutationRequestID || markerQualifier != commitment.Qualifier {
		t.Fatalf("durable receipt=(%q,%q,%v) err=%v", markerRequestID, markerQualifier, found, err)
	}
	if syncCalls < 2 {
		t.Fatalf("parent sync calls=%d, publication was not stabilized", syncCalls)
	}
}

func TestSyncVPNPeersV2PartialLiveApplyErrorRestoresTrackedLiveState(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.liveConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const generation = int64(21)
	peers := []transport.VPNPeerSpec{{
		PublicKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{14}, 32)),
		PresharedKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{15}, 32)),
		IP:           "10.8.0.14",
	}}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(generation, peers)
	if err != nil {
		t.Fatal(err)
	}
	binding, manager := beginVPNRPCTestMutationWithManager(
		t, "vpn_peer_sync", commitment.Qualifier,
	)
	state := t.TempDir()
	failMarker := filepath.Join(state, "sync.failed")
	recoveryStarted := filepath.Join(state, "recovery.started")
	recoveryRelease := filepath.Join(state, "recovery.release")
	t.Setenv("VPN_TEST_SYNCCONF_FAIL_ONCE_MARKER", failMarker)
	t.Setenv("VPN_TEST_SYNCCONF_RECOVERY_STARTED", recoveryStarted)
	t.Setenv("VPN_TEST_SYNCCONF_RECOVERY_RELEASE", recoveryRelease)

	previousSync := syncAtomicParentDirectory
	syncCalls := 0
	syncAtomicParentDirectory = func(string) error {
		syncCalls++
		return nil
	}
	t.Cleanup(func() { syncAtomicParentDirectory = previousSync })

	type syncResult struct {
		response SyncVPNPeersResponse
		err      error
	}
	result := make(chan syncResult, 1)
	go func() {
		var response SyncVPNPeersResponse
		err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
			ServiceMutationBinding: binding,
			DesiredGeneration:      generation,
			Peers:                  peers,
		}, &response)
		result <- syncResult{response: response, err: err}
	}()
	waitForVPNTestFile(t, recoveryStarted)
	waitForVPNRecoveryWorker(t, manager, serviceMutationStatusRunning)
	if err := os.WriteFile(recoveryRelease, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case call := <-result:
		if call.err != nil || call.response.Applied || call.response.AppliedGeneration != 0 ||
			call.response.Error == "" {
			t.Fatalf("partial apply response=%+v err=%v", call.response, call.err)
		}
	case <-time.After(vpnRPCTestAsyncTimeout):
		t.Fatal("partial live apply recovery did not finish")
	}
	if syncCalls != 0 {
		t.Fatalf("partial live apply reached durable commit: sync calls=%d", syncCalls)
	}
	durable, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(host.liveConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(durable, original) || !bytes.Equal(live, original) {
		t.Fatal("partial live apply did not restore exact live and durable state")
	}
	if job := manager.status(testMutationRequestID); job == nil || job.WorkerPID != 0 || job.WorkerStarted != "" || job.WorkerCommand != "" {
		t.Fatalf("recovery worker identity was not cleared: %+v", job)
	}
}

func TestSyncVPNPeersV2CancellationAfterLiveApplyRestoresTrackedLiveState(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.liveConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const generation = int64(22)
	peers := []transport.VPNPeerSpec{{
		PublicKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{16}, 32)),
		PresharedKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32)),
		IP:           "10.8.0.16",
	}}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(generation, peers)
	if err != nil {
		t.Fatal(err)
	}
	binding, manager := beginVPNRPCTestMutationWithManager(
		t, "vpn_peer_sync", commitment.Qualifier,
	)
	state := t.TempDir()
	cancelStarted := filepath.Join(state, "sync.started")
	cancelRelease := filepath.Join(state, "sync.release")
	recoveryStarted := filepath.Join(state, "recovery.started")
	recoveryRelease := filepath.Join(state, "recovery.release")
	t.Setenv("VPN_TEST_SYNCCONF_CANCEL_STARTED", cancelStarted)
	t.Setenv("VPN_TEST_SYNCCONF_CANCEL_RELEASE", cancelRelease)
	t.Setenv("VPN_TEST_SYNCCONF_RECOVERY_STARTED", recoveryStarted)
	t.Setenv("VPN_TEST_SYNCCONF_RECOVERY_RELEASE", recoveryRelease)

	previousSync := syncAtomicParentDirectory
	syncCalls := 0
	syncAtomicParentDirectory = func(string) error {
		syncCalls++
		return nil
	}
	t.Cleanup(func() { syncAtomicParentDirectory = previousSync })

	type syncResult struct {
		response SyncVPNPeersResponse
		err      error
	}
	result := make(chan syncResult, 1)
	go func() {
		var response SyncVPNPeersResponse
		err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
			ServiceMutationBinding: binding,
			DesiredGeneration:      generation,
			Peers:                  peers,
		}, &response)
		result <- syncResult{response: response, err: err}
	}()
	waitForVPNTestFile(t, cancelStarted)
	mutated, err := os.ReadFile(host.liveConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(mutated, original) {
		t.Fatal("fake syncconf did not mutate live state before cancellation")
	}
	if _, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:     binding.MutationRequestID,
		ExpectedOwner: binding.MutationOwnerID,
		Reason:        "test_cancel_after_vpn_live_apply",
	}); err != nil {
		t.Fatal(err)
	}
	waitForVPNTestFile(t, recoveryStarted)
	waitForVPNRecoveryWorker(t, manager, serviceMutationStatusCancelling)
	if err := os.WriteFile(recoveryRelease, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case call := <-result:
		if call.err != nil || call.response.Applied || call.response.AppliedGeneration != 0 ||
			call.response.Error == "" {
			t.Fatalf("canceled live apply response=%+v err=%v", call.response, call.err)
		}
	case <-time.After(vpnRPCTestAsyncTimeout):
		_ = os.WriteFile(cancelRelease, []byte("release\n"), 0o600)
		t.Fatal("canceled live apply recovery did not finish")
	}
	if syncCalls != 0 {
		t.Fatalf("canceled live apply reached durable commit: sync calls=%d", syncCalls)
	}
	durable, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(host.liveConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(durable, original) || !bytes.Equal(live, original) {
		t.Fatal("canceled live apply did not restore exact live and durable state")
	}
	if job := manager.status(testMutationRequestID); job == nil || job.WorkerPID != 0 || job.WorkerStarted != "" || job.WorkerCommand != "" {
		t.Fatalf("cancelling recovery worker identity was not cleared: %+v", job)
	}
}

func TestSyncVPNPeersV2AppliesExactCommittedGenerationWhenInterfaceAbsent(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(host.configPath, 0o600); err != nil {
		t.Fatal(err)
	}
	const generation = int64(23)
	peers := []transport.VPNPeerSpec{
		{
			PublicKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32)),
			PresharedKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32)),
			IP:           "10.8.0.9",
		},
		{
			PublicKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
			PresharedKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)),
			IP:           "10.8.0.2",
		},
	}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(generation, peers)
	if err != nil {
		t.Fatal(err)
	}
	binding := beginVPNRPCTestMutation(t, "vpn_peer_sync", commitment.Qualifier)
	response := SyncVPNPeersResponse{Applied: true, AppliedGeneration: 99, Error: "stale"}
	if err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
		ServiceMutationBinding: binding,
		DesiredGeneration:      generation,
		Peers:                  peers,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Applied || response.AppliedGeneration != generation || response.Error != "" {
		t.Fatalf("sync response=%+v", response)
	}
	durable, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	first := strings.Index(string(durable), "AllowedIPs = 10.8.0.2/32")
	second := strings.Index(string(durable), "AllowedIPs = 10.8.0.9/32")
	if first < 0 || second < 0 || first >= second {
		t.Fatalf("durable peer order is not canonical:\n%s", durable)
	}
}

func TestSyncVPNPeersV2ProbeFailureDoesNotStageOrCommit(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
	}{
		{name: "command error", env: "VPN_TEST_FAIL_INTERFACE_PROBE", value: "1"},
		{name: "malformed output", env: "VPN_TEST_INTERFACE_PROBE_OUTPUT", value: strings.Repeat("x", 16)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := newVPNRPCTestHost(t)
			original := managedVPNTestConfig("original")
			if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
				t.Fatal(err)
			}
			const generation = int64(25)
			peers := []transport.VPNPeerSpec{{
				PublicKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{10}, 32)),
				PresharedKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{11}, 32)),
				IP:           "10.8.0.10",
			}}
			commitment, err := mutationpayload.CanonicalVPNPeerSync(generation, peers)
			if err != nil {
				t.Fatal(err)
			}
			binding := beginVPNRPCTestMutation(t, "vpn_peer_sync", commitment.Qualifier)
			t.Setenv(test.env, test.value)

			previousSync := syncAtomicParentDirectory
			syncCalls := 0
			syncAtomicParentDirectory = func(string) error {
				syncCalls++
				return nil
			}
			t.Cleanup(func() { syncAtomicParentDirectory = previousSync })

			response := SyncVPNPeersResponse{Applied: true, AppliedGeneration: 99, Error: "stale"}
			if err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
				ServiceMutationBinding: binding,
				DesiredGeneration:      generation,
				Peers:                  peers,
			}, &response); err != nil {
				t.Fatal(err)
			}
			if response.Applied || response.AppliedGeneration != 0 ||
				response.Error != "could not determine VPN interface state" {
				t.Fatalf("probe failure response=%+v", response)
			}
			if syncCalls != 0 {
				t.Fatalf("probe failure reached durable commit: sync calls=%d", syncCalls)
			}
			durable, err := os.ReadFile(host.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(durable, original) || readVPNTestState(t, host.liveConfigPath) != "" {
				t.Fatal("probe failure changed live or durable VPN state")
			}
			entries, err := os.ReadDir(filepath.Dir(host.configPath))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".tmp-") {
					t.Fatalf("probe failure left a staged file: %s", entry.Name())
				}
			}
		})
	}
}

func TestSyncVPNPeersV2CanceledProbeDoesNotStageOrCommit(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	const generation = int64(27)
	peers := []transport.VPNPeerSpec{{
		PublicKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{12}, 32)),
		PresharedKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{13}, 32)),
		IP:           "10.8.0.12",
	}}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(generation, peers)
	if err != nil {
		t.Fatal(err)
	}
	binding, manager := beginVPNRPCTestMutationWithManager(
		t, "vpn_peer_sync", commitment.Qualifier,
	)
	probeState := t.TempDir()
	started := filepath.Join(probeState, "probe.started")
	release := filepath.Join(probeState, "probe.release")
	t.Setenv("VPN_TEST_INTERFACE_PROBE_STARTED", started)
	t.Setenv("VPN_TEST_INTERFACE_PROBE_RELEASE", release)

	previousSync := syncAtomicParentDirectory
	syncCalls := 0
	syncAtomicParentDirectory = func(string) error {
		syncCalls++
		return nil
	}
	t.Cleanup(func() { syncAtomicParentDirectory = previousSync })

	type syncResult struct {
		response SyncVPNPeersResponse
		err      error
	}
	result := make(chan syncResult, 1)
	go func() {
		var response SyncVPNPeersResponse
		err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
			ServiceMutationBinding: binding,
			DesiredGeneration:      generation,
			Peers:                  peers,
		}, &response)
		result <- syncResult{response: response, err: err}
	}()
	deadline := time.Now().Add(vpnRPCTestAsyncTimeout)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("WireGuard interface probe did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:     binding.MutationRequestID,
		ExpectedOwner: binding.MutationOwnerID,
		Reason:        "test_cancel_vpn_interface_probe",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case call := <-result:
		if call.err != nil || call.response.Applied || call.response.AppliedGeneration != 0 ||
			call.response.Error != "could not determine VPN interface state" {
			t.Fatalf("canceled probe response=%+v err=%v", call.response, call.err)
		}
	case <-time.After(vpnRPCTestAsyncTimeout):
		_ = os.WriteFile(release, []byte("release\n"), 0o600)
		t.Fatal("canceled WireGuard interface probe did not stop")
	}
	if syncCalls != 0 {
		t.Fatalf("canceled probe reached durable commit: sync calls=%d", syncCalls)
	}
	durable, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(durable, original) || readVPNTestState(t, host.liveConfigPath) != "" {
		t.Fatal("canceled probe changed live or durable VPN state")
	}
}

func TestSyncVPNPeersV2RejectsPayloadDifferentFromDirectLease(t *testing.T) {
	host := newVPNRPCTestHost(t)
	const generation = int64(29)
	committedPeers := []transport.VPNPeerSpec{{
		PublicKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{6}, 32)),
		PresharedKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)),
		IP:           "10.8.0.6",
	}}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(generation, committedPeers)
	if err != nil {
		t.Fatal(err)
	}
	binding := beginVPNRPCTestMutation(t, "vpn_peer_sync", commitment.Qualifier)
	changedPeers := append([]transport.VPNPeerSpec(nil), committedPeers...)
	changedPeers[0].IP = "10.8.0.7"
	response := SyncVPNPeersResponse{Applied: true, AppliedGeneration: 99, Error: "stale"}
	if err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
		ServiceMutationBinding: binding,
		DesiredGeneration:      generation,
		Peers:                  changedPeers,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Applied || response.AppliedGeneration != 0 ||
		response.Error != errServiceMutationStepUnauthorized.Error() {
		t.Fatalf("mismatched payload response=%+v", response)
	}
	if _, err := os.Lstat(host.configPath); !os.IsNotExist(err) {
		t.Fatalf("mismatched payload reached the host config: %v", err)
	}
}

func TestSyncVPNPeersV2RejectsNestedWireGuardInstallBeforeHostAccess(t *testing.T) {
	host := newVPNRPCTestHost(t)
	const generation = int64(31)
	peers := []transport.VPNPeerSpec{{
		PublicKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)),
		PresharedKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		IP:           "10.8.0.8",
	}}
	binding := beginVPNRPCTestMutation(t, "service_install", "")
	var response SyncVPNPeersResponse
	if err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
		ServiceMutationBinding: binding,
		DesiredGeneration:      generation,
		Peers:                  peers,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Error, "does not authorize") {
		t.Fatalf("nested install unexpectedly reached VPN peer sync: %+v", response)
	}
	if _, err := os.Lstat(host.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nested install reached VPN host config: %v", err)
	}
}

func vpnPeerSyncRecoveryPayload(
	t *testing.T,
	generation int64,
) ([]transport.VPNPeerSpec, mutationpayload.VPNPeerSyncCommitment) {
	t.Helper()
	peers := []transport.VPNPeerSpec{{
		PublicKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{41}, 32)),
		PresharedKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32)),
		IP:           "10.8.0.41",
	}}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(generation, peers)
	if err != nil {
		t.Fatal(err)
	}
	return peers, commitment
}

func markedVPNPeerSyncTestConfig(
	t *testing.T,
	requestID, qualifier string,
) []byte {
	t.Helper()
	config, err := replaceVPNPeerSyncReceiptMarker(
		string(managedVPNTestConfig("desired")),
		requestID,
		qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(config)
}

func persistVPNPeerSyncTestPhase(
	t *testing.T,
	manager *serviceMutationManager,
	phase string,
) {
	t.Helper()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.Phase = phase
	manager.active.job.UpdatedAt = manager.now()
	if err := manager.persistLedgerMutationLocked(before); err != nil {
		t.Fatal(err)
	}
}

func abandonVPNPeerSyncTestRuntime(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	manager.mu.Lock()
	runtime := manager.active
	if runtime == nil {
		manager.mu.Unlock()
		t.Fatal("test mutation has no active runtime")
	}
	runtime.cancel()
	manager.active = nil
	manager.mu.Unlock()
	if err := runtime.lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func releasePoisonedVPNPeerSyncTestManager(manager *serviceMutationManager) {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	var locks []*serviceMutationFileLock
	if manager.active != nil {
		manager.active.cancel()
		locks = append(locks, manager.active.lock)
		manager.active = nil
	}
	if manager.poisonLock != nil {
		locks = append(locks, manager.poisonLock)
		manager.poisonLock = nil
	}
	manager.mu.Unlock()
	seen := make(map[*serviceMutationFileLock]bool)
	for _, lock := range locks {
		if lock != nil && !seen[lock] {
			_ = lock.Close()
			seen[lock] = true
		}
	}
}

func TestVPNPeerSyncStartupRecoveryRollsBackExactIntentStage(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.liveConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, commitment := vpnPeerSyncRecoveryPayload(t, 41)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(t, manager, "vpn_peer_sync", "wireguard", commitment.Qualifier)
	desired := markedVPNPeerSyncTestConfig(t, testMutationRequestID, commitment.Qualifier)
	stage, err := stageAtomicFile(host.configPath, desired, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := formatVPNPeerSyncCommitPhase(
		vpnPeerSyncCommitIntent,
		testMutationRequestID,
		commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	persistVPNPeerSyncTestPhase(t, manager, intent)
	abandonVPNPeerSyncTestRuntime(t, manager)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusFailed || job.Phase != "interrupted" {
		t.Fatalf("recovered job=%+v", job)
	}
	durable, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(host.liveConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(durable, original) || !bytes.Equal(live, original) {
		t.Fatal("intent-stage crash did not restore exact durable config to the live interface")
	}
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uncommitted intent stage still exists: %v", err)
	}
}

func TestVPNPeerSyncStartupRecoveryAcceptsOnlyExactPublishedTarget(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.liveConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, commitment := vpnPeerSyncRecoveryPayload(t, 46)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(t, manager, "vpn_peer_sync", "wireguard", commitment.Qualifier)
	published := markedVPNPeerSyncTestConfig(t, testMutationRequestID, commitment.Qualifier)
	if err := os.WriteFile(host.configPath, published, 0o600); err != nil {
		t.Fatal(err)
	}
	abandonVPNPeerSyncTestRuntime(t, manager)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusSucceeded {
		t.Fatalf("published recovery job=%+v", job)
	}
	state, requestID, qualifier, err := parseVPNPeerSyncCommitPhase(job.Phase)
	if err != nil || state != vpnPeerSyncCommitPublished ||
		requestID != testMutationRequestID || qualifier != commitment.Qualifier {
		t.Fatalf("terminal receipt=(%q,%q,%q) err=%v", state, requestID, qualifier, err)
	}
	live, err := os.ReadFile(host.liveConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(live, published) {
		t.Fatal("published durable target was not restored to the live interface")
	}
}

func TestVPNPeerSyncStartupRecoveryRollsBackPreIntentLiveStateAndRemovesStage(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, commitment := vpnPeerSyncRecoveryPayload(t, 42)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(t, manager, "vpn_peer_sync", "wireguard", commitment.Qualifier)
	desired := markedVPNPeerSyncTestConfig(t, testMutationRequestID, commitment.Qualifier)
	if err := os.WriteFile(host.liveConfigPath, desired, 0o600); err != nil {
		t.Fatal(err)
	}
	stage, err := stageAtomicFile(host.configPath, desired, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	abandonVPNPeerSyncTestRuntime(t, manager)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusFailed || job.Phase != "interrupted" {
		t.Fatalf("pre-intent recovery job=%+v", job)
	}
	durable, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(host.liveConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(durable, original) || !bytes.Equal(live, original) {
		t.Fatal("pre-intent crash did not restore exact durable config to the live interface")
	}
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-intent stage was not removed: %v", err)
	}
}

func TestVPNPeerSyncStartupRecoveryRollsBackLegacyUnboundJob(t *testing.T) {
	host := newVPNRPCTestHost(t)
	previousQualifier := "vpn-peer-sync/v1:sha256:" + strings.Repeat("f", 64)
	durable := markedVPNPeerSyncTestConfig(t, strings.Repeat("e", 32), previousQualifier)
	if err := os.WriteFile(host.configPath, durable, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyDesired := managedVPNTestConfig("legacy-desired")
	if err := os.WriteFile(host.liveConfigPath, legacyDesired, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, commitment := vpnPeerSyncRecoveryPayload(t, 47)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(t, manager, "vpn_peer_sync", "wireguard", commitment.Qualifier)
	stage, err := stageAtomicFile(host.configPath, legacyDesired, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.PackageName = ""
	manager.active.job.UpdatedAt = manager.now()
	if err := manager.persistLedgerMutationLocked(before); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()
	abandonVPNPeerSyncTestRuntime(t, manager)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusFailed || job.PackageName != "" {
		t.Fatalf("legacy recovery job=%+v", job)
	}
	live, err := os.ReadFile(host.liveConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(live, durable) {
		t.Fatal("legacy recovery did not restore the durable config to the live interface")
	}
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy unbound stage was not removed: %v", err)
	}
}

func TestVPNPeerSyncStartupRecoveryMalformedReceiptPoisonsAndRetainsLock(t *testing.T) {
	host := newVPNRPCTestHost(t)
	malformed := append(managedVPNTestConfig(""), []byte(vpnPeerSyncReceiptMarkerPrefix+"malformed\n")...)
	if err := os.WriteFile(host.configPath, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	_, commitment := vpnPeerSyncRecoveryPayload(t, 43)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(t, manager, "vpn_peer_sync", "wireguard", commitment.Qualifier)
	abandonVPNPeerSyncTestRuntime(t, manager)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err == nil || reloaded == nil || reloaded.poisoned == nil || reloaded.active == nil {
		t.Fatalf("ambiguous recovery manager=%v err=%v", reloaded, err)
	}
	t.Cleanup(func() { releasePoisonedVPNPeerSyncTestManager(reloaded) })
	second, secondErr := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if second != nil || !errors.Is(secondErr, errServiceMutationHostBusy) {
		t.Fatalf("retained recovery lock manager=%v err=%v", second, secondErr)
	}
}

func TestReadSecureVPNConfigRejectsOversizedFile(t *testing.T) {
	host := newVPNRPCTestHost(t)
	oversized := bytes.Repeat([]byte{'x'}, vpnPeerSyncConfigMaxSize+1)
	if err := os.WriteFile(host.configPath, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecureVPNConfig(); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized config error=%v", err)
	}
}

func TestSyncVPNPeersV2CancelOrderedAgainstCommitGateStillSucceeds(t *testing.T) {
	tests := []struct {
		name    string
		install func(*testing.T, *serviceMutationManager, chan struct{}, chan struct{})
	}{
		{
			name: "after durable intent before host rename",
			install: func(t *testing.T, manager *serviceMutationManager, reached, release chan struct{}) {
				blocked := false
				manager.writeFault = func(point string) error {
					intentPhase := manager.active != nil && manager.active.job != nil &&
						strings.HasPrefix(
							manager.active.job.Phase,
							vpnPeerSyncCommitPhasePrefix+vpnPeerSyncCommitIntent+"/",
						)
					if point == serviceMutationWriteFaultAfterSync && intentPhase && !blocked {
						blocked = true
						close(reached)
						<-release
					}
					return nil
				}
			},
		},
		{
			name: "after host rename before directory sync",
			install: func(t *testing.T, _ *serviceMutationManager, reached, release chan struct{}) {
				previous := syncAtomicParentDirectory
				var once sync.Once
				syncAtomicParentDirectory = func(path string) error {
					once.Do(func() {
						close(reached)
						<-release
					})
					return previous(path)
				}
				t.Cleanup(func() { syncAtomicParentDirectory = previous })
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := newVPNRPCTestHost(t)
			if err := os.WriteFile(host.configPath, managedVPNTestConfig("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			peers, commitment := vpnPeerSyncRecoveryPayload(t, 44)
			binding, manager := beginVPNRPCTestMutationWithManager(
				t, "vpn_peer_sync", commitment.Qualifier,
			)
			reached := make(chan struct{})
			release := make(chan struct{})
			test.install(t, manager, reached, release)
			type callResult struct {
				response SyncVPNPeersResponse
				err      error
			}
			callDone := make(chan callResult, 1)
			go func() {
				var response SyncVPNPeersResponse
				err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
					ServiceMutationBinding: binding,
					DesiredGeneration:      44,
					Peers:                  peers,
				}, &response)
				callDone <- callResult{response: response, err: err}
			}()
			select {
			case <-reached:
			case <-time.After(vpnRPCTestAsyncTimeout):
				t.Fatal("commit gate seam was not reached")
			}
			cancelDone := make(chan *ServiceMutationJob, 1)
			cancelErr := make(chan error, 1)
			go func() {
				job, err := manager.cancelJob(&ServiceMutationCancelRequest{
					RequestID:     binding.MutationRequestID,
					ExpectedOwner: binding.MutationOwnerID,
					Reason:        "test_cancel_at_commit_gate",
				})
				cancelDone <- job
				cancelErr <- err
			}()
			select {
			case err := <-cancelErr:
				t.Fatalf("cancel escaped commit gate early: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			close(release)
			call := <-callDone
			if call.err != nil || !call.response.Applied ||
				call.response.AppliedGeneration != 44 || call.response.Error != "" {
				t.Fatalf("committed response=%+v err=%v", call.response, call.err)
			}
			if err := <-cancelErr; err != nil {
				t.Fatal(err)
			}
			cancelledJob := <-cancelDone
			if cancelledJob == nil || cancelledJob.Status != serviceMutationStatusSucceeded {
				t.Fatalf("cancel observed job=%+v", cancelledJob)
			}
			job := manager.status(testMutationRequestID)
			if job == nil || job.Status != serviceMutationStatusSucceeded {
				t.Fatalf("terminal job=%+v", job)
			}
			markerRequestID, markerQualifier, found, err := parseVPNPeerSyncReceiptMarker(
				func() []byte {
					config, readErr := os.ReadFile(host.configPath)
					if readErr != nil {
						t.Fatal(readErr)
					}
					return config
				}(),
			)
			if err != nil || !found || markerRequestID != testMutationRequestID || markerQualifier != commitment.Qualifier {
				t.Fatalf("published marker=(%q,%q,%v) err=%v", markerRequestID, markerQualifier, found, err)
			}
		})
	}
}

func TestSyncVPNPeersV2TerminalReceiptWriteFailurePoisonsAfterPublication(t *testing.T) {
	host := newVPNRPCTestHost(t)
	if err := os.WriteFile(host.configPath, managedVPNTestConfig("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	peers, commitment := vpnPeerSyncRecoveryPayload(t, 45)
	binding, manager := beginVPNRPCTestMutationWithManager(
		t, "vpn_peer_sync", commitment.Qualifier,
	)
	writes := 0
	manager.writeFault = func(point string) error {
		vpnCommitPhase := manager.active != nil && manager.active.job != nil &&
			manager.active.job.WorkerPID == 0 &&
			strings.HasPrefix(manager.active.job.Phase, vpnPeerSyncCommitPhasePrefix)
		if point == serviceMutationWriteFaultBeforeRename && vpnCommitPhase {
			writes++
			if writes == 2 {
				return os.ErrInvalid
			}
		}
		return nil
	}
	var response SyncVPNPeersResponse
	if err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
		ServiceMutationBinding: binding,
		DesiredGeneration:      45,
		Peers:                  peers,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Applied || response.AppliedGeneration != 45 || response.Error != "" {
		t.Fatalf("published response=%+v", response)
	}
	manager.mu.Lock()
	poisoned, active := manager.poisoned != nil, manager.active != nil
	manager.mu.Unlock()
	if !poisoned || !active {
		t.Fatalf("terminal receipt failure poisoned=%v active=%v", poisoned, active)
	}
	t.Cleanup(func() { releasePoisonedVPNPeerSyncTestManager(manager) })
	job := manager.status(testMutationRequestID)
	if job == nil || job.Status == serviceMutationStatusFailed ||
		!strings.HasPrefix(job.Phase, vpnPeerSyncCommitPhasePrefix+vpnPeerSyncCommitIntent+"/") {
		t.Fatalf("ambiguous terminal receipt job=%+v", job)
	}
	lock, err := acquireServiceMutationFileLock(manager.lockPath)
	if err == nil {
		_ = lock.Close()
		t.Fatal("poisoned publication released the host lock")
	}
	config, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if published, verifyErr := verifyPublishedVPNPeerSyncReceipt(testMutationRequestID, commitment.Qualifier); verifyErr != nil || !published {
		t.Fatalf("host receipt published=%v err=%v config=%s", published, verifyErr, config)
	}
}

func assertVPNPeerSyncRollbackPoisoned(
	t *testing.T,
	manager *serviceMutationManager,
	qualifier string,
) {
	t.Helper()
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	active := manager.active != nil
	job := cloneServiceMutationJob(manager.ledger.Jobs[testMutationRequestID])
	manager.mu.Unlock()
	if !poisoned || !active || job == nil || !serviceMutationStatusActive(job.Status) {
		t.Fatalf("rollback ambiguity poisoned=%v active=%v job=%+v", poisoned, active, job)
	}
	t.Cleanup(func() { releasePoisonedVPNPeerSyncTestManager(manager) })
	lock, err := acquireServiceMutationFileLock(manager.lockPath)
	if err == nil {
		_ = lock.Close()
		t.Fatal("rollback ambiguity released the host lock")
	}
	next, beginErr := manager.begin(&ServiceMutationBeginRequest{
		RequestID:   strings.Repeat("b", 32),
		OwnerID:     strings.Repeat("c", 32),
		Kind:        "vpn_peer_sync",
		Target:      "wireguard",
		PackageName: qualifier,
	})
	if beginErr == nil || next != nil {
		t.Fatalf("poisoned manager accepted next mutation: job=%+v err=%v", next, beginErr)
	}
}

func TestSyncVPNPeersV2PartialApplyRollbackFailurePoisonsAndRetainsLock(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.liveConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VPN_TEST_SYNCCONF_FAIL_ALWAYS", "1")
	peers, commitment := vpnPeerSyncRecoveryPayload(t, 48)
	binding, manager := beginVPNRPCTestMutationWithManager(
		t, "vpn_peer_sync", commitment.Qualifier,
	)
	var response SyncVPNPeersResponse
	if err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
		ServiceMutationBinding: binding,
		DesiredGeneration:      48,
		Peers:                  peers,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Applied || response.AppliedGeneration != 0 ||
		!strings.Contains(response.Error, "automatic recovery is required") {
		t.Fatalf("partial rollback response=%+v", response)
	}
	assertVPNPeerSyncRollbackPoisoned(t, manager, commitment.Qualifier)
}

func TestSyncVPNPeersV2CancellationRollbackFailurePoisonsAndRetainsLock(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.liveConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	cancelStarted := filepath.Join(state, "sync.started")
	cancelRelease := filepath.Join(state, "sync.release")
	recoveryStarted := filepath.Join(state, "recovery.started")
	recoveryRelease := filepath.Join(state, "recovery.release")
	t.Setenv("VPN_TEST_SYNCCONF_CANCEL_STARTED", cancelStarted)
	t.Setenv("VPN_TEST_SYNCCONF_CANCEL_RELEASE", cancelRelease)
	t.Setenv("VPN_TEST_SYNCCONF_RECOVERY_STARTED", recoveryStarted)
	t.Setenv("VPN_TEST_SYNCCONF_RECOVERY_RELEASE", recoveryRelease)
	t.Setenv("VPN_TEST_SYNCCONF_FAIL_RECOVERY", "1")
	peers, commitment := vpnPeerSyncRecoveryPayload(t, 49)
	binding, manager := beginVPNRPCTestMutationWithManager(
		t, "vpn_peer_sync", commitment.Qualifier,
	)
	type syncResult struct {
		response SyncVPNPeersResponse
		err      error
	}
	result := make(chan syncResult, 1)
	go func() {
		var response SyncVPNPeersResponse
		err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
			ServiceMutationBinding: binding,
			DesiredGeneration:      49,
			Peers:                  peers,
		}, &response)
		result <- syncResult{response: response, err: err}
	}()
	waitForVPNTestFile(t, cancelStarted)
	if _, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:     binding.MutationRequestID,
		ExpectedOwner: binding.MutationOwnerID,
		Reason:        "test_cancel_with_failed_vpn_rollback",
	}); err != nil {
		t.Fatal(err)
	}
	waitForVPNTestFile(t, recoveryStarted)
	if err := os.WriteFile(recoveryRelease, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case call := <-result:
		if call.err != nil || call.response.Applied || call.response.AppliedGeneration != 0 ||
			!strings.Contains(call.response.Error, "automatic recovery is required") {
			t.Fatalf("cancellation rollback response=%+v err=%v", call.response, call.err)
		}
	case <-time.After(vpnRPCTestAsyncTimeout):
		_ = os.WriteFile(cancelRelease, []byte("release\n"), 0o600)
		t.Fatal("cancellation rollback failure did not return")
	}
	assertVPNPeerSyncRollbackPoisoned(t, manager, commitment.Qualifier)
}

func TestSyncVPNPeersV2CommitRollbackFailurePoisonsAndRetainsLock(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.liveConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	t.Setenv("VPN_TEST_REMOVE_STAGE_ONCE_MARKER", filepath.Join(state, "stage.removed"))
	t.Setenv("VPN_TEST_SYNCCONF_FAIL_AFTER_FIRST_MARKER", filepath.Join(state, "first.applied"))
	peers, commitment := vpnPeerSyncRecoveryPayload(t, 50)
	binding, manager := beginVPNRPCTestMutationWithManager(
		t, "vpn_peer_sync", commitment.Qualifier,
	)
	var response SyncVPNPeersResponse
	if err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
		ServiceMutationBinding: binding,
		DesiredGeneration:      50,
		Peers:                  peers,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Applied || response.AppliedGeneration != 0 ||
		!strings.Contains(response.Error, "automatic recovery is required") {
		t.Fatalf("commit rollback response=%+v", response)
	}
	assertVPNPeerSyncRollbackPoisoned(t, manager, commitment.Qualifier)
}

func TestSetupVPNReadyFailureRestoresConfigSysctlUnitAndForwarding(t *testing.T) {
	host := newVPNRPCTestHost(t)
	binding := beginVPNRPCTestMutation(t, "vpn_setup", "")
	originalSysctl := []byte("# previous policy\nnet.ipv4.ip_forward = 0\n")
	if err := os.WriteFile(host.sysctlPath, originalSysctl, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(host.sysctlPath, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VPN_TEST_FAIL_READY_SHOW", "1")

	var response SetupVPNResponse
	if err := (&Agent{}).SetupVPN(&SetupVPNRequest{
		ServiceMutationBinding: binding,
		Port:                   51820,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Created || !strings.Contains(response.Error, "did not become ready") {
		t.Fatalf("setup response=%+v", response)
	}
	if _, err := os.Lstat(host.configPath); !os.IsNotExist(err) {
		t.Fatalf("new VPN config survived rollback: %v", err)
	}
	sysctl, err := os.ReadFile(host.sysctlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sysctl, originalSysctl) {
		t.Fatalf("sysctl policy was not restored: %q", sysctl)
	}
	if got := readVPNTestState(t, host.forwardingPath); got != "0" {
		t.Fatalf("runtime forwarding=%q, want 0", got)
	}
	if got := readVPNTestState(t, host.interfacePath); got != "0" {
		t.Fatalf("interface state=%q, want down", got)
	}
	if got := readVPNTestState(t, host.enabledPath); got != "0" {
		t.Fatalf("unit enabled state=%q, want disabled", got)
	}
}
