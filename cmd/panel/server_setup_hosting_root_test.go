package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Plan tests must not depend on the test machine's /var/www: by default the
// hosting root proves traversable. Tests that exercise the proof set their
// own probe.
func init() {
	serverSetupHostingRootProbe = func() (*hostingpath.TraversalBlock, error) { return nil, nil }
}

func withHostingRootProbe(t *testing.T, probe func() (*hostingpath.TraversalBlock, error)) {
	t.Helper()
	previous := serverSetupHostingRootProbe
	serverSetupHostingRootProbe = probe
	t.Cleanup(func() { serverSetupHostingRootProbe = previous })
}

// Native finding P3: setup review names the owner's blocking directory before
// any site exists; the profile without sites is not held back.
func TestServerSetupReviewNamesABlockingHostingRoot(t *testing.T) {
	probed := 0
	withHostingRootProbe(t, func() (*hostingpath.TraversalBlock, error) {
		probed++
		return &hostingpath.TraversalBlock{Directory: "/var/www", Mode: os.ModeDir | 0o750, Owner: "root", Group: "celikpanel", Account: "http"}, nil
	})
	const want = "server_setup_hosting_root_not_traversable:0750:root:celikpanel:/var/www"
	for _, purpose := range []string{"web", "web_mail", "application", "dns"} {
		t.Run(purpose, func(t *testing.T) {
			probed = 0
			f, state := setupOperationFixture(t)
			state.Draft.Purpose = purpose
			if purpose == "web_mail" {
				state.Draft.MailHostname = "mail.example.test"
			}
			if purpose == "application" {
				state.Draft.NodeVersion = "22.1.0"
			}
			caps := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2, transport.AgentCapabilityFirewallApplyV2, transport.AgentCapabilityPanelCertificateIssueV2}
			f.agent.versionCapabilities = &caps
			plan, err := f.panel.buildServerSetupPlan(context.Background(), state, serviceOperationActor{UserID: f.userID})
			if err != nil {
				t.Fatal(err)
			}
			hostsSites := purpose != "dns"
			if slices.Contains(plan.Blockers, want) != hostsSites || (hostsSites && plan.CanStart) {
				t.Fatalf("%s blockers = %v can_start = %v", purpose, plan.Blockers, plan.CanStart)
			}
			if (probed > 0) != hostsSites {
				t.Fatalf("%s probed %d times", purpose, probed)
			}
		})
	}
}

// Missing or traversable parents are no blocker, and an inspection error is
// logged rather than presented as a verified block.
func TestServerSetupReviewPassesTraversableOrUnknownHostingRoot(t *testing.T) {
	for name, probe := range map[string]func() (*hostingpath.TraversalBlock, error){
		"traversable": func() (*hostingpath.TraversalBlock, error) { return nil, nil },
		"unknown": func() (*hostingpath.TraversalBlock, error) {
			return nil, errors.New("inspect /var: input/output error")
		},
	} {
		t.Run(name, func(t *testing.T) {
			withHostingRootProbe(t, probe)
			f, state := setupOperationFixture(t)
			plan, err := f.panel.buildServerSetupPlan(context.Background(), state, serviceOperationActor{UserID: f.userID})
			if err != nil {
				t.Fatal(err)
			}
			for _, blocker := range plan.Blockers {
				if strings.HasPrefix(blocker, serverSetupHostingRootBlocker) {
					t.Fatalf("unexpected blocker %q", blocker)
				}
			}
		})
	}
}

func TestDomainCreateAnswersTheHostingRootRefusal(t *testing.T) {
	refusal := &services.HostingRootNotTraversableError{Block: transport.HostingRootBlock{
		Directory: "/var/www", Mode: "0750", Owner: "root:celikpanel", Account: "http",
	}}
	found, ok := hostingRootNotTraversable(errors.Join(refusal, nil))
	if !ok || found != refusal {
		t.Fatalf("refusal not found: %v %v", found, ok)
	}
	if _, ok := hostingRootNotTraversable(errors.Join(refusal, errors.New("rollback site metadata failed"))); ok {
		t.Fatal("a refusal whose cleanup failed was answered as a clean refusal")
	}
	if _, ok := hostingRootNotTraversable(fmt.Errorf("site creation failed: %s", "other")); ok {
		t.Fatal("a generic failure was classified")
	}

	recorder := httptest.NewRecorder()
	writeHostingRootNotTraversable(recorder, refusal)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != errCodeHostingRootNotTraversable || body.Vars["directory"] != "/var/www" || body.Vars["mode"] != "0750" ||
		body.Vars["owner"] != "root:celikpanel" || body.Vars["command"] != "sudo chmod 755 /var/www" {
		t.Fatalf("body = %+v", body)
	}
	for _, want := range []string{"nothing was changed", "/var/www (mode 0750, owner root:celikpanel)", "The server owner", "sudo chmod 755 /var/www", "create the site again"} {
		if !strings.Contains(body.Error, want) {
			t.Fatalf("message %q lacks %q", body.Error, want)
		}
	}
}
