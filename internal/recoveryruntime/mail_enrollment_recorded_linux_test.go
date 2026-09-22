//go:build linux

package recoveryruntime

import (
	"context"
	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Runs inside the real inherited-lock preparation subprocess fixture. The only
// producer here is explicit test admission through the common ledger encoder.
func exerciseRecordedEnrollment(t *testing.T, ctx context.Context, paths mailCapturePaths, agent *CompatibleMailAgent, prepared *PreparedMailEnrollment, binding MailEnrollmentBinding, scenario string) {
	t.Helper()
	stateDir := filepath.Join(filepath.Dir(paths.journals), "accepted-ledger")
	if err := os.Mkdir(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	binding.LedgerPath = filepath.Join(stateDir, "service-mutations.json")
	identity := prepared.Identity()
	empty := servicemutationledger.Ledger{Version: 1, Jobs: map[string]*servicemutationledger.ServiceMutationJob{}}
	ledger, err := servicemutationledger.AdmitMailEnrollment(&empty, identity, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	save := func() {
		raw, e := servicemutationledger.Encode(&ledger)
		if e != nil {
			t.Fatal(e)
		}
		capturePut(t, binding.LedgerPath, raw, 0600)
	}
	if scenario == "recorded-rollback" || scenario == "recorded-terminal" {
		next := "rollback"
		if scenario == "recorded-terminal" {
			next = "published"
		}
		ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, identity, next, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "recorded-owner" {
		binding.OwnerID = strings.Repeat("d", 32)
	}
	if scenario == "recorded-scope" {
		wrong := identity
		wrong.ScopeSHA256 = strings.Repeat("c", 64)
		ledger, err = servicemutationledger.AdmitMailEnrollment(&empty, wrong, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
	}
	save()
	switch scenario {
	case "recorded-missing":
		if err = os.Remove(binding.LedgerPath); err != nil {
			t.Fatal(err)
		}
	case "recorded-capture":
		capturePut(t, filepath.Join(paths.journals, identity.RequestID+".json"), []byte("{}\n"), 0600)
	case "recorded-plan":
		capturePut(t, filepath.Join(paths.journals, identity.RequestID+".files.json"), []byte("{}\n"), 0600)
	case "recorded-source":
		agent.Contract.MailRenewalGeneration = strings.Repeat("e", 64)
	}
	beforeLedger, _ := os.ReadFile(binding.LedgerPath)
	got, side, err := openRecordedMailEnrollmentAt(ctx, identity.RequestID, paths, agent, binding)
	good := scenario == "recorded-forward" || scenario == "recorded-rollback" || scenario == "recorded-terminal" || strings.HasPrefix(scenario, "recorded-late-")
	if (err == nil) != good {
		t.Fatalf("recorded open expected=%v: %v", good, err)
	}
	afterLedger, _ := os.ReadFile(binding.LedgerPath)
	if string(beforeLedger) != string(afterLedger) {
		t.Fatal("opening changed accepted evidence")
	}
	if !good {
		return
	}
	expected := "forward"
	if scenario == "recorded-rollback" {
		expected = "rollback"
	}
	if side != expected || got.Identity() != identity {
		t.Fatal("recorded selection changed")
	}
	if scenario == "recorded-terminal" {
		if err = got.Verify(ctx, side); err == nil {
			t.Fatal("unverified historical success became current native success")
		}
	}
	if scenario == "recorded-late-clear" || scenario == "recorded-late-inverse" {
		if scenario == "recorded-late-clear" {
			ledger = empty
		} else {
			ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, identity, "rollback", time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
		}
		save()
		if err = got.Resume(ctx, side); err == nil {
			t.Fatal("stale opener resumed a removed or reversed reservation")
		}
	}
}

func enrollmentRecordedAgent(t *testing.T, root, target string) *CompatibleMailAgent {
	t.Helper()
	binaryDir := filepath.Join(root, "recorded-agent")
	if err := os.Mkdir(binaryDir, 0755); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	agentBytes := []byte("recorded current-source agent fixture")
	c, _ := agentnativecontract.New(agentBytes, strings.Repeat("a", 40))
	bundle, err := readMailRenewalBundle(filepath.Join(root, "runtime", target))
	if err != nil {
		t.Fatal(err)
	}
	c, err = agentnativecontract.BindMailRenewal(c, bundle.manifest, mailBundleFiles(bundle))
	bundle.state.close()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := agentnativecontract.Encode(c)
	capturePut(t, filepath.Join(binaryDir, "agent"), agentBytes, 0755)
	capturePut(t, filepath.Join(binaryDir, agentnativecontract.FileName), raw, 0644)
	agent, err := InspectCompatibleMailAgent(binaryDir)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}
