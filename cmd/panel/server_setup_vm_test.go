//go:build linux

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

// This acceptance fixture is deliberately inert in ordinary test runs. The
// driver first verifies a fresh disposable VM and creates its opt-in marker.
// It never runs an installer or updates an existing panel.
func TestServerSetupDisposableVMFirewall(t *testing.T) {
	stage := os.Getenv("CELIKPANEL_SETUP_VM_STAGE")
	if stage == "" {
		t.Skip("requires the explicit disposable-VM acceptance driver")
	}
	host, err := os.Hostname()
	if err != nil || !slices.Contains([]string{"setup65-debian13", "setup65-arch", "setup65-ubuntu24"}, host) || os.Geteuid() != 0 {
		t.Fatal("not an authorized disposable setup VM")
	}
	marker, err := os.ReadFile("/var/lib/celikpanel-setup-vm/fixture")
	if err != nil || string(marker) != "fresh-setup-firewall-acceptance-v1\n" {
		t.Fatal("missing disposable-VM fixture marker")
	}
	if _, err := os.Stat("/opt/celikpanel/bin/panel"); !os.IsNotExist(err) {
		t.Fatal("fixture cannot run on an installed panel")
	}
	database, err := paneldb.NewSQLiteDB("/var/lib/celikpanel-setup-vm/panel.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	client, err := transport.ConnectAgent()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	p := &Panel{db: database, agentClient: transport.NewReconnectingClient(client)}
	p.license = testPanelLicense(t, "active")
	if _, err := database.GetDB().Exec(`INSERT OR IGNORE INTO users(id,username,password_hash,email,role) VALUES(1,'vm-fixture','test-only','fixture@example.test','admin')`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	status := func() FirewallStatusResp {
		t.Helper()
		var out FirewallStatusResp
		if err := p.callAgentContext(ctx, "Agent.FirewallStatus", &transport.Empty{}, &out); err != nil {
			t.Fatal(err)
		}
		if out.Error != "" {
			t.Fatal(out.Error)
		}
		return out
	}
	check := func(enabled, persisted bool) {
		t.Helper()
		got := status()
		if got.Enabled != enabled || (got.PersistenceState == "ready") != persisted || !got.EngineAvailable || !slices.Contains(got.SSHPorts, 22) {
			t.Fatalf("firewall=%+v", got)
		}
		encoded, _ := json.Marshal(got)
		t.Logf("evidence %s: %s", stage, encoded)
	}
	unit := func(want string) {
		t.Helper()
		out, err := exec.Command("systemctl", "is-enabled", "celikpanel-firewall-restore.service").CombinedOutput()
		if strings.TrimSpace(string(out)) != want {
			t.Fatalf("restore unit=%q %v", out, err)
		}
	}
	if stage == "baseline" {
		check(false, false)
		unit("disabled")
		if _, err := os.Stat("/etc/celikpanel/firewall.nft"); !os.IsNotExist(err) {
			t.Fatal("fresh fixture acquired a saved policy")
		}
		return
	}
	if stage == "verify_saved" {
		check(true, true)
		unit("enabled")
		return
	}
	if stage == "verify_off" {
		check(false, false)
		unit("disabled")
		if _, err := os.Stat("/etc/celikpanel/firewall.nft"); !os.IsNotExist(err) {
			t.Fatal("turn off retained saved policy")
		}
		return
	}
	if stage == "off" {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/firewall", strings.NewReader(`{"enabled":false}`))
		r = r.WithContext(context.WithValue(ctx, callerKey, &Caller{ID: 1, Role: roleAdmin}))
		w := httptest.NewRecorder()
		p.handleFirewall(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("panel turn off=%d %s", w.Code, w.Body.String())
		}
		check(false, false)
		unit("disabled")
		return
	}
	if stage != "setup" {
		t.Fatal("unknown VM acceptance stage")
	}
	tcp, udp, err := p.desiredFirewallPorts(80)
	if err != nil {
		t.Fatal(err)
	}
	// The proven-empty fixture has no operator ports. Keep the same reviewed
	// service policy on retries; agent-discovered SSH remains a separate promise.
	slices.Sort(tcp)
	tcp = slices.Compact(tcp)
	slices.Sort(udp)
	udp = slices.Compact(udp)
	plan := serverSetupPlan{Version: serverSetupPlanVersion, TCPPorts: tcp, UDPPorts: udp, PreserveSSH: true, PersistFirewall: true}
	step := serverSetupExecutionStep{RequestID: strings.Repeat("6", 32), OwnerID: strings.Repeat("7", 32)}
	for retry := 0; retry < 2; retry++ {
		applied, err := p.runServerSetupFirewall(ctx, plan, step)
		if err != nil || !applied {
			t.Fatalf("reviewed firewall attempt %d applied=%v err=%v", retry, applied, err)
		}
	}
	job, err := p.statusAgentMutation(ctx, step.RequestID)
	if err != nil || job == nil || job.Status != agentMutationSucceeded {
		t.Fatalf("durable firewall receipt=%+v %v", job, err)
	}
	check(true, true)
	unit("enabled")
	if _, err := os.Stat("/etc/celikpanel/firewall.nft"); err != nil {
		t.Fatal("explicit reviewed persistence did not save snapshot", err)
	}
}
