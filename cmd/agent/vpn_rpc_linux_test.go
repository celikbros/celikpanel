//go:build linux

package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type vpnRPCTestHost struct {
	configPath     string
	sysctlPath     string
	forwardingPath string
	interfacePath  string
	enabledPath    string
	liveConfigPath string
}

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
    if [ "$1" = "show" ]; then
      if [ "${VPN_TEST_FAIL_READY_SHOW:-0}" = "1" ] &&
         [ "$(/bin/cat "$VPN_TEST_ENABLED")" = "1" ]; then
        exit 1
      fi
      [ "$(/bin/cat "$VPN_TEST_INTERFACE")" = "1" ] || exit 1
    elif [ "$1" = "syncconf" ]; then
      /bin/cp "$3" "$VPN_TEST_LIVE_CONFIG"
    fi
    ;;
  wg-quick)
    if [ "$1" = "strip" ]; then
      /bin/cat "$2"
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
	return host
}

func beginVPNRPCTestMutation(t *testing.T, kind string) ServiceMutationBinding {
	t.Helper()
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(t, manager, kind, "wireguard", "")
	return ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	}
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

func TestSyncVPNPeersCommitFailureRestoresLiveAndDurableConfig(t *testing.T) {
	host := newVPNRPCTestHost(t)
	binding := beginVPNRPCTestMutation(t, "vpn_peer_sync")
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
	var response SyncVPNPeersResponse
	if err := (&Agent{}).SyncVPNPeers(&SyncVPNPeersRequest{
		ServiceMutationBinding: binding,
		Peers: []VPNPeerSpec{{
			PublicKey: key, PresharedKey: psk, IP: "10.8.0.9",
		}},
	}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Applied || !strings.Contains(response.Error, "previous state restored") {
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
	if !bytes.Equal(durable, original) {
		t.Fatalf("durable config was not restored:\n%s", durable)
	}
	if !bytes.Equal(live, original) {
		t.Fatalf("live config was not restored:\n%s", live)
	}
	if syncCalls < 2 {
		t.Fatalf("parent sync calls=%d, rollback did not durably republish", syncCalls)
	}
}

func TestSetupVPNReadyFailureRestoresConfigSysctlUnitAndForwarding(t *testing.T) {
	host := newVPNRPCTestHost(t)
	binding := beginVPNRPCTestMutation(t, "vpn_setup")
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
