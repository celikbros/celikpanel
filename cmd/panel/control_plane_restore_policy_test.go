package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The precedence table decided in slice 2 (docs/DISASTER-RECOVERY.md section 8)
// has exactly three rows, and every row has two branches: the installer already
// wrote the file, or it did not. These tests walk all six, plus the one thing
// the key-by-key report must never do.
//
// Dilim 2'de kararlaştırılan öncelik tablosunun üç satırı ve her satırın iki
// dalı vardır; bu testler altısını da yürür.

type controlPlanePolicyRun struct {
	Source  controlPlaneTestTree
	Target  controlPlaneRoots
	Report  string
	Result  controlPlaneRestoreResult
	Archive string
}

// runControlPlanePolicyRestore archives a complete miniature control plane
// whose configuration members carry the given content, then restores it onto a
// host on which the installer has already written the given files.
func runControlPlanePolicyRestore(
	t *testing.T,
	archived map[string]string,
	installed map[string]string,
) controlPlanePolicyRun {
	t.Helper()
	source := newControlPlaneTestTree(t)
	for name, content := range archived {
		path := filepath.Join(source.Root.ConfDir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write the archived %s: %v", name, err)
		}
		source.Files[path] = []byte(content)
	}
	archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
	if _, err := createControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		source.Root,
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("create the archive: %v", err)
	}

	target := newControlPlaneTargetRoots(t)
	if err := os.MkdirAll(target.ConfDir, 0o700); err != nil {
		t.Fatalf("create the target configuration directory: %v", err)
	}
	for name, content := range installed {
		path := filepath.Join(target.ConfDir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write the installer's %s: %v", name, err)
		}
	}
	var report bytes.Buffer
	result, err := restoreControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		target,
		&report,
	)
	if err != nil {
		t.Fatalf("restore the archive: %v\n%s", err, report.String())
	}
	return controlPlanePolicyRun{
		Source:  source,
		Target:  target,
		Report:  report.String(),
		Result:  result,
		Archive: archivePath,
	}
}

func controlPlaneTargetFile(t *testing.T, roots controlPlaneRoots, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(roots.ConfDir, name))
	if err != nil {
		t.Fatalf("read the restored %s: %v", name, err)
	}
	return string(content)
}

func TestControlPlaneRestoreKeepsTheInstallerPanelEnvAndNamesEveryDifferingKey(t *testing.T) {
	// The archived host listened elsewhere, ran a different TLS layout and
	// carried a key whose NAME says it is a secret. The installer's file is the
	// one that describes this host.
	archived := "CELIKPANEL_LISTEN=:2083\n" +
		"CELIKPANEL_TLS=1\n" +
		"CELIKPANEL_TLS_KEY=/var/lib/celikpanel/tls/dead-host.pem\n" +
		"CELIKPANEL_ONLY_ON_THE_DEAD_HOST=yes\n" +
		"CELIKPANEL_SESSION_SECRET=archived-secret-value\n"
	installed := "# written by install.sh\n" +
		"CELIKPANEL_LISTEN=:8443\n" +
		"CELIKPANEL_TLS=1\n" +
		"CELIKPANEL_TLS_KEY=/var/lib/celikpanel/tls/this-host.pem\n" +
		"CELIKPANEL_SESSION_SECRET=installer-secret-value\n"

	run := runControlPlanePolicyRestore(
		t,
		map[string]string{"panel.env": archived},
		map[string]string{"panel.env": installed},
	)

	if got := controlPlaneTargetFile(t, run.Target, "panel.env"); got != installed {
		t.Fatalf("panel.env was replaced:\n%s", got)
	}
	if run.Result.Kept != 1 {
		t.Fatalf("kept=%d, want 1\n%s", run.Result.Kept, run.Report)
	}

	// One line per differing key, and only for the differing keys.
	for _, name := range []string{
		"CELIKPANEL_LISTEN",
		"CELIKPANEL_TLS_KEY",
		"CELIKPANEL_ONLY_ON_THE_DEAD_HOST",
		"CELIKPANEL_SESSION_SECRET",
	} {
		wanted := "panel.env: " + name + " differs from the archive; the installer's value is kept"
		if strings.Count(run.Report, wanted) != 1 {
			t.Fatalf("the report does not name %s exactly once:\n%s", name, run.Report)
		}
	}
	if strings.Contains(run.Report, "CELIKPANEL_TLS ") ||
		strings.Contains(run.Report, "panel.env: CELIKPANEL_TLS differs") {
		t.Fatalf("an identical key was reported as differing:\n%s", run.Report)
	}

	// Not one value of any key reaches the report, whatever its name says.
	for _, value := range []string{
		"archived-secret-value",
		"installer-secret-value",
		":8443",
		":2083",
		"/var/lib/celikpanel/tls/dead-host.pem",
		"/var/lib/celikpanel/tls/this-host.pem",
	} {
		if strings.Contains(run.Report, value) {
			t.Fatalf("the report printed the value %q:\n%s", value, run.Report)
		}
	}
}

func TestControlPlaneRestorePlacesPanelEnvWhenTheInstallerWroteNone(t *testing.T) {
	archived := "CELIKPANEL_LISTEN=:2083\nCELIKPANEL_TLS=1\n"
	run := runControlPlanePolicyRestore(
		t,
		map[string]string{"panel.env": archived},
		nil,
	)
	if got := controlPlaneTargetFile(t, run.Target, "panel.env"); got != archived {
		t.Fatalf("panel.env was not placed:\n%s", got)
	}
	if run.Result.Kept != 0 {
		t.Fatalf("kept=%d, want 0\n%s", run.Result.Kept, run.Report)
	}
	if strings.Contains(run.Report, "differs from the archive") {
		t.Fatalf("an absent file produced a difference warning:\n%s", run.Report)
	}
}

func TestControlPlaneRestoreKeepsTheInstallerAgentToken(t *testing.T) {
	run := runControlPlanePolicyRestore(
		t,
		map[string]string{"agent.token": "archived-token\n"},
		map[string]string{"agent.token": "installer-token\n"},
	)
	if got := controlPlaneTargetFile(t, run.Target, "agent.token"); got != "installer-token\n" {
		t.Fatalf("agent.token was replaced: %q", got)
	}
	if strings.Count(run.Report, "agent.token: the installer's token is kept") != 1 {
		t.Fatalf("the kept-token line is missing or repeated:\n%s", run.Report)
	}
	if strings.Contains(run.Report, "archived-token") ||
		strings.Contains(run.Report, "installer-token") {
		t.Fatalf("the report printed a token:\n%s", run.Report)
	}
	if run.Result.Kept != 1 {
		t.Fatalf("kept=%d, want 1\n%s", run.Result.Kept, run.Report)
	}
}

func TestControlPlaneRestorePlacesTheAgentTokenWhenTheAgentNeverRan(t *testing.T) {
	// This is the ordinary install-hook case: the restore runs before the agent
	// has ever started, so no token exists yet.
	run := runControlPlanePolicyRestore(
		t,
		map[string]string{"agent.token": "archived-token\n"},
		nil,
	)
	if got := controlPlaneTargetFile(t, run.Target, "agent.token"); got != "archived-token\n" {
		t.Fatalf("agent.token was not placed: %q", got)
	}
	if strings.Contains(run.Report, "the installer's token is kept") {
		t.Fatalf("an absent token produced a kept warning:\n%s", run.Report)
	}
	if run.Result.Kept != 0 {
		t.Fatalf("kept=%d, want 0\n%s", run.Result.Kept, run.Report)
	}
}

func TestControlPlaneRestoreOverwritesTheFirewallSnapshot(t *testing.T) {
	archivedRules := "table inet celikpanel { chain input { tcp dport 2083 accept } }\n"
	run := runControlPlanePolicyRestore(
		t,
		map[string]string{"firewall.nft": archivedRules},
		map[string]string{"firewall.nft": "table inet celikpanel {}\n"},
	)
	if got := controlPlaneTargetFile(t, run.Target, "firewall.nft"); got != archivedRules {
		t.Fatalf("the operator's ruleset was not restored:\n%s", got)
	}
	if strings.Count(run.Report, "firewall.nft: restored from the archive") != 1 {
		t.Fatalf("the restored-firewall line is missing or repeated:\n%s", run.Report)
	}
	if run.Result.Kept != 0 {
		t.Fatalf("kept=%d, want 0\n%s", run.Result.Kept, run.Report)
	}
}

func TestControlPlaneRestorePlacesTheFirewallSnapshotWhenAbsent(t *testing.T) {
	archivedRules := "table inet celikpanel { chain input { tcp dport 2083 accept } }\n"
	run := runControlPlanePolicyRestore(
		t,
		map[string]string{"firewall.nft": archivedRules},
		nil,
	)
	if got := controlPlaneTargetFile(t, run.Target, "firewall.nft"); got != archivedRules {
		t.Fatalf("the operator's ruleset was not restored:\n%s", got)
	}
	if strings.Count(run.Report, "firewall.nft: restored from the archive") != 1 {
		t.Fatalf("the restored-firewall line is missing or repeated:\n%s", run.Report)
	}
}

func TestControlPlaneRestoreRefusesANonRegularInstallerMember(t *testing.T) {
	source := newControlPlaneTestTree(t)
	archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
	if _, err := createControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		source.Root,
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("create the archive: %v", err)
	}
	target := newControlPlaneTargetRoots(t)
	if err := os.MkdirAll(filepath.Join(target.ConfDir, "agent.token"), 0o700); err != nil {
		t.Fatalf("create the impostor: %v", err)
	}
	_, err := restoreControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		target,
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error=%v, want a refusal to reason about a non-regular member", err)
	}
}

func TestControlPlaneMemberPolicyLookupFollowsTheConfigurationRoot(t *testing.T) {
	roots := controlPlaneRoots{ConfDir: filepath.Join("/", "srv", "celikpanel-etc")}
	for _, name := range []string{"panel.env", "agent.token", "firewall.nft"} {
		policy, ok := controlPlaneMemberPolicyFor(filepath.Join(roots.ConfDir, name), roots)
		if !ok || policy.Basename != name {
			t.Fatalf("%s has no policy under a moved configuration root", name)
		}
	}
	for _, path := range []string{
		filepath.Join("/", "etc", "celikpanel", "panel.env"),
		filepath.Join(roots.ConfDir, "secret.key"),
		filepath.Join(roots.ConfDir, "nested", "panel.env"),
	} {
		if _, ok := controlPlaneMemberPolicyFor(path, roots); ok {
			t.Fatalf("%s must not match a policy row", path)
		}
	}
	// Exactly one row per member, and no row outside the configuration root.
	seen := map[string]struct{}{}
	for _, policy := range controlPlaneMemberPolicies() {
		if _, duplicate := seen[policy.Basename]; duplicate {
			t.Fatalf("the precedence table has two rows for %s", policy.Basename)
		}
		seen[policy.Basename] = struct{}{}
	}
}

func TestControlPlaneConfigurationDifferences(t *testing.T) {
	installer := map[string]string{
		"SAME":           "1",
		"DIFFERENT":      "installer",
		"INSTALLER_ONLY": "1",
	}
	archived := map[string]string{
		"SAME":          "1",
		"DIFFERENT":     "archive",
		"ARCHIVED_ONLY": "1",
	}
	want := []string{"ARCHIVED_ONLY", "DIFFERENT", "INSTALLER_ONLY"}
	if got := controlPlaneConfigurationDifferences(installer, archived); !reflect.DeepEqual(got, want) {
		t.Fatalf("differences=%v, want %v", got, want)
	}
	if got := controlPlaneConfigurationDifferences(installer, installer); len(got) != 0 {
		t.Fatalf("identical files differ: %v", got)
	}
}

func TestParseControlPlaneConfigurationFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.env")
	content := "# a comment\n" +
		"\n" +
		"   \n" +
		"CELIKPANEL_LISTEN=:2083\n" +
		"CELIKPANEL_PANEL_DEMO_FLAG=\n" +
		"CELIKPANEL_TLS_CERT=/a/path=with=equals\n" +
		"not an assignment\n" +
		"CELIKPANEL_LISTEN=:9999\r\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	values, err := parseControlPlaneConfigurationFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := map[string]string{
		// The last assignment wins, exactly as an environment file is read.
		"CELIKPANEL_LISTEN":          ":9999",
		"CELIKPANEL_PANEL_DEMO_FLAG": "",
		"CELIKPANEL_TLS_CERT":        "/a/path=with=equals",
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("values=%v, want %v", values, want)
	}

	large := filepath.Join(t.TempDir(), "panel.env")
	if err := os.WriteFile(
		large,
		bytes.Repeat([]byte("A=1\n"), int(controlPlaneConfigurationMaxBytes/4)+8),
		0o600,
	); err != nil {
		t.Fatalf("write the oversized file: %v", err)
	}
	if _, err := parseControlPlaneConfigurationFile(large); err == nil ||
		!strings.Contains(err.Error(), "too large") {
		t.Fatalf("error=%v, want an oversized-file refusal", err)
	}
}
