package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// D-031 at the Agent's RPC boundary. The generator is replaced by a recorder:
// a component test must not write the host's own /etc/nginx.

type recordedManagedApply struct {
	items   [][]services.ManagedVhostItem
	results func([]services.ManagedVhostItem) ([]transport.SiteFileResult, error)
}

func withRecordedManagedApply(t *testing.T, recorder *recordedManagedApply) {
	t.Helper()
	previous := applyManagedVhostsFor
	applyManagedVhostsFor = func(_ *Agent, items []services.ManagedVhostItem) ([]transport.SiteFileResult, error) {
		recorder.items = append(recorder.items, items)
		if recorder.results != nil {
			return recorder.results(items)
		}
		results := make([]transport.SiteFileResult, len(items))
		for index := range items {
			results[index] = transport.SiteFileResult{
				Kind: transport.SiteFileKindNginxVhost, Path: "/etc/nginx/sites-available/" + items[index].Domain + ".conf",
				State: transport.SiteFileManagedUnchanged, Outcome: transport.SiteFileOutcomeWritten,
			}
		}
		return results, nil
	}
	previousPrepare := prepareVhostChallengeRoot
	prepareVhostChallengeRoot = func(*ApplyVhostRequest) error { return nil }
	t.Cleanup(func() {
		applyManagedVhostsFor = previous
		prepareVhostChallengeRoot = previousPrepare
	})
}

func siteFileTestAgent(t *testing.T) *Agent {
	t.Helper()
	generator, err := services.NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	return &Agent{nginxGen: generator}
}

func startupTestVhostRequest(t *testing.T, siteID, domainID int, domain string) ApplyVhostRequest {
	t.Helper()
	documentRoot, err := hostingpath.DocumentRoot(1, domainID)
	if err != nil {
		t.Fatal(err)
	}
	return ApplyVhostRequest{
		SiteID: siteID, SubscriptionID: 1, DomainID: domainID, Domain: domain,
		DocumentRoot: documentRoot, ProjectType: "static", SSLType: "none",
		ServerNames: []string{domain, "www." + domain}, FileTrigger: transport.SiteFileTriggerStartup,
	}
}

// One site whose input is refused is that site's result; the others are
// classified and written in the same batch, and the counts say so.
func TestApplyVhostsIsolatesOneSitesRefusedInput(t *testing.T) {
	withLifecycleTestBuild(t)
	recorder := &recordedManagedApply{}
	withRecordedManagedApply(t, recorder)
	first := startupTestVhostRequest(t, 21, 31, "first.example")
	bad := startupTestVhostRequest(t, 22, 32, "bad.example")
	bad.ProjectType = "php"
	bad.PHPSocket = "/run/php/php8.3-fpm-site99.sock" // another site's socket
	third := startupTestVhostRequest(t, 23, 33, "third.example")
	var resp ApplyVhostsResponse
	if err := siteFileTestAgent(t).ApplyVhosts(&ApplyVhostsRequest{
		ExpectedBuildCommit: "lifecycle-test", Vhosts: []ApplyVhostRequest{first, bad, third},
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" || len(resp.Items) != 3 || resp.Counts == nil {
		t.Fatalf("response: %+v", resp)
	}
	if len(recorder.items) != 1 || len(recorder.items[0]) != 2 ||
		recorder.items[0][0].Domain != "first.example" || recorder.items[0][1].Domain != "third.example" {
		t.Fatalf("generator items: %+v", recorder.items)
	}
	if recorder.items[0][0].Trigger != transport.SiteFileTriggerStartup || recorder.items[0][0].Legacy == nil {
		t.Fatal("the start item carries no trigger or no earlier releases' texts")
	}
	if resp.Items[1].Error == "" || resp.Items[1].File.Outcome != transport.SiteFileOutcomeFailed ||
		resp.Items[1].File.Reason != transport.SiteFileReasonRenderFailed {
		t.Fatalf("refused item: %+v", resp.Items[1])
	}
	if resp.Counts.Written != 2 || resp.Counts.Failed != 1 || resp.Applied != 2 {
		t.Fatalf("counts: %+v applied %d", *resp.Counts, resp.Applied)
	}
}

func TestSiteFileCountsNameEveryOutcome(t *testing.T) {
	item := func(outcome, state, reason, adopted string) transport.SiteFileBatchItem {
		return transport.SiteFileBatchItem{File: transport.SiteFileResult{Outcome: outcome, State: state, Reason: reason, AdoptedFrom: adopted}}
	}
	counts := countSiteFileResults([]transport.SiteFileBatchItem{
		item(transport.SiteFileOutcomeWritten, transport.SiteFileManagedUnchanged, "", "v0.1.0-alpha.82"),
		item(transport.SiteFileOutcomeWritten, transport.SiteFileManagedUnchanged, "", ""),
		item(transport.SiteFileOutcomeUnchanged, transport.SiteFileManagedUnchanged, "", ""),
		item(transport.SiteFileOutcomeKept, transport.SiteFileOwnerEdited, "", ""),
		item(transport.SiteFileOutcomeKept, transport.SiteFileForeign, "", ""),
		item(transport.SiteFileOutcomeKept, transport.SiteFileUnknownOrigin, "", ""),
		item(transport.SiteFileOutcomeRefused, transport.SiteFileUnreadable, transport.SiteFileReasonSymlink, ""),
		item(transport.SiteFileOutcomeRefused, transport.SiteFileManagedUnchanged, transport.SiteFileReasonWriteRefused, ""),
		item(transport.SiteFileOutcomeMissing, transport.SiteFileAbsent, "", ""),
		item(transport.SiteFileOutcomeFailed, transport.SiteFileManagedUnchanged, transport.SiteFileReasonNginxRefused, ""),
	})
	want := transport.SiteFileCounts{Written: 2, Unchanged: 1, Kept: 1, Foreign: 1, Unknown: 1, Unreadable: 2, Missing: 1, Adopted: 1, Failed: 1}
	if counts != want {
		t.Fatalf("counts %+v, want %+v", counts, want)
	}
}

// A render the classifier kept answers with the typed file and an error
// sentence, never as a success.
func TestApplyVhostReturnsTheTypedKeptFile(t *testing.T) {
	withLifecycleTestBuild(t)
	recorder := &recordedManagedApply{results: func(items []services.ManagedVhostItem) ([]transport.SiteFileResult, error) {
		return []transport.SiteFileResult{{
			Kind: transport.SiteFileKindNginxVhost, Path: "/etc/nginx/sites-available/kept.example.conf",
			State: transport.SiteFileOwnerEdited, Outcome: transport.SiteFileOutcomeKept,
			PendingPath: "/etc/nginx/sites-available/kept.example.conf.celikpanel-pending",
		}}, nil
	}}
	withRecordedManagedApply(t, recorder)
	request := startupTestVhostRequest(t, 41, 31, "kept.example")
	request.FileTrigger = ""
	request.ExpectedBuildCommit = "lifecycle-test"
	var resp ApplyVhostResponse
	if err := siteFileTestAgent(t).ApplyVhost(&request, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.File == nil || resp.File.Outcome != transport.SiteFileOutcomeKept || !strings.Contains(resp.Error, "was kept") {
		t.Fatalf("response: %+v", resp)
	}
	if !strings.HasPrefix(resp.Config, "# celikpanel-render v2 sha256=") {
		t.Fatal("the answered text is not the sealed text")
	}
	if recorder.items[0][0].Trigger != "" {
		t.Fatalf("an empty trigger is passed as is (the generator reads it as a change): %q", recorder.items[0][0].Trigger)
	}
}

// set8 measured that alpha.82 created a site with `server_name X;` and the
// next start rendered `server_name X www.X;`. With the Panel's host names in
// the request, the creation render is the start render, byte for byte.
func TestCreationRenderIsTheStartRender(t *testing.T) {
	agent := siteFileTestAgent(t)
	for _, projectType := range []string{"php", "static"} {
		req := validLifecycleCreateRequest(t)
		req.ProjectType = projectType
		req.TempDomain = "example-test.celik.panel"
		req.ServerNames = []string{"example.test", "www.example.test"}
		_, vhostReq, created, err := agent.validatedCreateSiteRequest(req)
		if err != nil {
			t.Fatalf("%s: %v", projectType, err)
		}
		start := ApplyVhostRequest{
			SiteID: req.SiteID, SubscriptionID: req.SubscriptionID, DomainID: req.DomainID,
			Domain: req.Domain, DocumentRoot: req.DocumentRoot, PHPSocket: vhostReq.PHPSocket,
			SSLType: "none", ProjectType: projectType, ServerNames: []string{"example.test", "www.example.test"},
		}
		started, err := agent.renderValidatedVhost(&start)
		if err != nil {
			t.Fatalf("%s start: %v", projectType, err)
		}
		if created.Config != started.Config {
			t.Fatalf("%s: creation and start renders differ", projectType)
		}
		if !strings.Contains(created.Config, "server_name example.test www.example.test;") {
			t.Fatalf("%s: creation server_name", projectType)
		}
		// An older Panel sends no host names: the domain alone, as before.
		req.ServerNames = nil
		req.TempDomain = ""
		_, _, older, err := agent.validatedCreateSiteRequest(req)
		if err != nil || !strings.Contains(older.Config, "server_name example.test;") {
			t.Fatalf("%s older Panel: %v", projectType, err)
		}
	}
}

// A file at the new site's path that is not CelikPanel's unchanged text is
// kept; the site is refused with a typed code and its parts are removed, and
// the Agent never asks for the vhost to be removed (it is the owner's file).
func TestCreateSiteKeepsAnExistingOwnerFileAndRefuses(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	fake := &fakeSiteLifecycle{
		home:     home,
		failures: map[string]error{},
		expectedSocket: services.PHPFPMSocketPath(
			req.PHPVersion, "site13",
		),
	}
	agent := lifecycleTestAgent(t, fake)
	var seen services.ManagedVhostItem
	agent.siteOps.applySiteVhost = func(item services.ManagedVhostItem) (transport.SiteFileResult, error) {
		seen = item
		fake.calls = append(fake.calls, "apply-site-vhost")
		return transport.SiteFileResult{
			Kind: transport.SiteFileKindNginxVhost, Path: "/etc/nginx/sites-available/example.test.conf",
			State: transport.SiteFileUnknownOrigin, Outcome: transport.SiteFileOutcomeRefused,
			Reason: transport.SiteFileReasonNotApplicable,
		}, nil
	}
	reply := &transport.CreateSiteResponse{}
	if err := agent.CreateSite(req, reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || reply.ErrorCode != transport.SiteConfigExists ||
		reply.ErrorDetail != "/etc/nginx/sites-available/example.test.conf" || reply.SiteFile == nil {
		t.Fatalf("reply: %+v", reply)
	}
	if seen.Trigger != transport.SiteFileTriggerCreate || seen.Legacy != nil {
		t.Fatalf("creation item: trigger %q legacy %v", seen.Trigger, seen.Legacy != nil)
	}
	calls := strings.Join(fake.calls, ",")
	for _, want := range []string{"delete-pool", "delete-user", "remove-all"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("rollback did not run %s: %s", want, calls)
		}
	}
	if strings.Contains(calls, "remove-vhost") || strings.Contains(calls, "apply-vhost,") {
		t.Fatalf("the owner's vhost was touched: %s", calls)
	}
}

func TestCreateSiteNginxRefusalKeepsItsTypedAnswer(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	fake := &fakeSiteLifecycle{home: home, failures: map[string]error{},
		expectedSocket: services.PHPFPMSocketPath(req.PHPVersion, "site13")}
	agent := lifecycleTestAgent(t, fake)
	agent.siteOps.applySiteVhost = func(item services.ManagedVhostItem) (transport.SiteFileResult, error) {
		refused := &services.NginxConfigRefusedError{Output: "nginx: [emerg] unknown directive", Cause: errors.New("exit status 1")}
		return transport.SiteFileResult{Outcome: transport.SiteFileOutcomeFailed, Reason: transport.SiteFileReasonNginxRefused},
			&services.VhostRestoredError{Cause: refused}
	}
	reply := &transport.CreateSiteResponse{}
	if err := agent.CreateSite(req, reply); err != nil {
		t.Fatal(err)
	}
	if reply.ErrorCode != transport.WebServerRefusedConfig || reply.ErrorDetail != "nginx: [emerg] unknown directive" {
		t.Fatalf("reply: %+v", reply)
	}
}

// D-031 step 1b: every render item carries the site's challenge root (the
// generator keeps the Panel's challenge file for it), the certificate path
// of the render and the validation-only server block of the extra names, so
// a kept file can be checked without the Panel's text being applied.
func TestRenderItemsCarryWhatTheCertificateValidationNeeds(t *testing.T) {
	withLifecycleTestBuild(t)
	recorder := &recordedManagedApply{}
	withRecordedManagedApply(t, recorder)
	request := startupTestVhostRequest(t, 51, 61, "cert.example")
	request.FileTrigger = ""
	request.ExpectedBuildCommit = "lifecycle-test"
	request.ACMEChallengeNames = []string{"mail.cert.example"}
	var resp ApplyVhostResponse
	if err := siteFileTestAgent(t).ApplyVhost(&request, &resp); err != nil {
		t.Fatal(err)
	}
	item := recorder.items[0][0]
	root, err := hostingpath.ACMEChallengeRoot(1, 61)
	if err != nil {
		t.Fatal(err)
	}
	if item.ACMEChallengeRoot != root || item.SSLCert != "" {
		t.Fatalf("item: root %q cert %q", item.ACMEChallengeRoot, item.SSLCert)
	}
	if !strings.Contains(item.ValidationBlock, "server_name mail.cert.example;") ||
		!strings.Contains(item.ValidationBlock, services.PanelManagedIncludeLine("cert.example")) ||
		!strings.Contains(item.Body, item.ValidationBlock) {
		t.Fatalf("validation block:\n%s", item.ValidationBlock)
	}
}
