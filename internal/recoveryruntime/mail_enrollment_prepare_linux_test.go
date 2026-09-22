//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

func TestMailEnrollmentPreparation(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, kind := range []string{"absent", "legacy", "independent"} {
		for _, scenario := range []string{"ok", "missing-binding", "wrong-generation", "denied", "cancelled", "unknown-native", "owner-hook", "late-source-change", "late-denial", "no-host-lock", "late-prepared-source-change"} {
			if kind == "independent" && scenario != "ok" {
				continue
			}
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				if kind == "independent" {
					scenario = "already-enrolled"
				}
				root, target, host, release := enrollmentFixture(t, kind)
				c := exec.Command(os.Args[0], "-test.run=^TestMailEnrollmentPreparationChild$", "-test.v")
				c.Env = append(os.Environ(), "CP_MAIL_PREPARE_ROOT="+root, "CP_MAIL_PREPARE_TARGET="+target, "CP_MAIL_PREPARE_CASE="+scenario)
				if scenario == "no-host-lock" {
					host = nil
				}
				c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, host, release}
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatalf("%v %s", err, out)
				}
			})
		}
	}
}
func TestMailEnrollmentPreparationChild(t *testing.T) {
	root := os.Getenv("CP_MAIL_PREPARE_ROOT")
	if root == "" {
		t.Skip("descriptor subprocess")
	}
	target, scenario := os.Getenv("CP_MAIL_PREPARE_TARGET"), os.Getenv("CP_MAIL_PREPARE_CASE")
	paths := mailCapturePaths{filepath.Join(root, "hooks", mailrenewalkit.HookName), filepath.Join(root, "units"), filepath.Join(root, "runtime"), filepath.Join(root, "journals"), filepath.Join(root, "transaction")}
	beforeHook, beforeErr := os.ReadFile(paths.hook)
	beforeUnits := map[string][]byte{}
	for _, name := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		beforeUnits[name], _ = os.ReadFile(filepath.Join(paths.units, name))
	}
	binaryDir := filepath.Join(root, "agent-bin")
	if err := os.Mkdir(binaryDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentBytes := []byte("read-only current-source Agent fixture")
	c, _ := agentnativecontract.New(agentBytes, strings.Repeat("a", 40))
	bundle, err := readMailRenewalBundle(filepath.Join(paths.runtime, target))
	if err != nil {
		t.Fatal(err)
	}
	c, err = agentnativecontract.BindMailRenewal(c, bundle.manifest, mailBundleFiles(bundle))
	bundle.state.close()
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "missing-binding" {
		c.MailRenewalGeneration = ""
	}
	if scenario == "wrong-generation" {
		c.MailRenewalGeneration = strings.Repeat("e", 64)
	}
	raw, _ := agentnativecontract.Encode(c)
	capturePut(t, filepath.Join(binaryDir, "agent"), agentBytes, 0755)
	capturePut(t, filepath.Join(binaryDir, agentnativecontract.FileName), raw, 0644)
	agent, err := InspectCompatibleMailAgent(binaryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	denied := scenario == "denied"
	observations, mutations := 0, 0
	native := enrollmentNativeFixture{mailEnrollmentCommands{
		loaded: mailLoadedCommands{observe: func(_ context.Context, unit string) ([]byte, error) {
			observations++
			if scenario == "unknown-native" {
				return nil, errors.New("native transport unavailable")
			}
			if observations == 2 {
				if scenario == "late-source-change" {
					capturePut(t, filepath.Join(binaryDir, "agent"), []byte("owner replacement"), 0755)
				}
				if scenario == "late-denial" {
					denied = true
				}
			}
			return []byte("LoadState=not-found\nFragmentPath=\nDropInPaths=\nNeedDaemonReload=no\nActiveState=inactive\nUnitFileState=\n"), nil
		}, reload: func(context.Context) error { mutations++; return errors.New("unexpected reload") }},
		activity: mailActivityCommands{startTimer: func(context.Context) error { mutations++; return errors.New("unexpected start") }, stopTimer: func(context.Context) error { mutations++; return errors.New("unexpected stop") }},
	}}
	binding := MailEnrollmentBinding{OwnerID: strings.Repeat("b", 32), HostLock: filepath.Join(root, "run", "mutation.lock"), Native: native, VerifyAuthority: func() error {
		if denied {
			return errors.New("owner operation refused")
		}
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if scenario == "cancelled" {
		cancel()
	}
	if scenario == "owner-hook" {
		capturePut(t, paths.hook, []byte("owner hook"), 0755)
		beforeHook = []byte("owner hook")
		beforeErr = nil
	}
	prepared, scope, err := prepareMailEnrollmentAt(ctx, mailCaptureTestOperation, paths, agent, binding)
	good := scenario == "ok" || scenario == "late-prepared-source-change"
	if (err == nil) != good {
		t.Fatalf("expected prepared=%v: %v", good, err)
	}
	if mutations != 0 {
		t.Fatal("preparation changed native services")
	}
	afterHook, afterErr := os.ReadFile(paths.hook)
	if !bytes.Equal(beforeHook, afterHook) || (beforeErr == nil) != (afterErr == nil) {
		t.Fatal("preparation changed hook")
	}
	for _, name := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		after, e := os.ReadFile(filepath.Join(paths.units, name))
		if !bytes.Equal(after, beforeUnits[name]) || (beforeUnits[name] == nil && !os.IsNotExist(e)) {
			t.Fatal("preparation changed native unit", e)
		}
	}
	if !good {
		entries, e := os.ReadDir(paths.journals)
		if e != nil || len(entries) != 0 {
			t.Fatal("unverified admission wrote evidence", e)
		}
		return
	}
	if prepared.Identity().ScopeSHA256 != Digest(scope) {
		t.Fatal("scope differs from reservation identity")
	}
	second, again, err := prepareMailEnrollmentAt(ctx, mailCaptureTestOperation, paths, agent, binding)
	if err != nil || !bytes.Equal(scope, again) || second.Identity() != prepared.Identity() {
		t.Fatal("retry changed prepared plan", err)
	}
	if scenario == "late-prepared-source-change" {
		capturePut(t, filepath.Join(binaryDir, "agent"), []byte("later owner replacement"), 0755)
		if err = prepared.verifyBoundary(ctx); err == nil {
			t.Fatal("prepared execution lost source binding")
		}
	}
}
