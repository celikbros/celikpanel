package services

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// D-031 step 1b, second round: a kept file's readiness for certificate
// validation is measured by a probe, not read from its text.

// fakeNginx stands for nginx on port 80: it serves a file from the challenge
// directory under the names it is told to, answers others with a status, or
// does not answer at all.
type fakeNginx struct {
	root   string
	serve  map[string]bool
	status map[string]int
	down   bool
	asked  []string
	// seen records whether the probe file was on disk when it was asked for.
	seen []bool
}

func (f *fakeNginx) get(_ context.Context, name, path string) ValidationProbeAnswer {
	f.asked = append(f.asked, name)
	if f.down {
		return ValidationProbeAnswer{Err: errors.New("dial tcp 127.0.0.1:80: connect: connection refused")}
	}
	file := filepath.Join(ValidationProbeChallengeDir(f.root), strings.TrimPrefix(path, acmeChallengeURLPath))
	body, err := os.ReadFile(file)
	f.seen = append(f.seen, err == nil)
	if status, ok := f.status[name]; ok {
		return ValidationProbeAnswer{Answered: true, Status: status}
	}
	if !f.serve[name] || err != nil {
		return ValidationProbeAnswer{Answered: true, Status: http.StatusNotFound, Body: []byte("not found")}
	}
	return ValidationProbeAnswer{Answered: true, Status: http.StatusOK, Body: body}
}

// probeHarness is a managed-vhost harness whose challenge root is a real
// directory and whose nginx is a fakeNginx.
func probeHarness(t *testing.T) (*managedVhostHarness, *fakeNginx) {
	t.Helper()
	h := newManagedVhostHarness(t)
	root := t.TempDir()
	if err := os.MkdirAll(ValidationProbeChallengeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	nginx := &fakeNginx{root: root, serve: map[string]bool{}, status: map[string]int{}}
	h.ng.probeGet = nginx.get
	return h, nginx
}

func probeData(nginx *fakeNginx, domain string) VhostData {
	data := managedTestData(domain, managedVhostPlatforms[0].socket)
	data.ACMEChallengeRoot = nginx.root
	return data
}

func (h *managedVhostHarness) probeItem(data VhostData, trigger string) ManagedVhostItem {
	item := h.certItem(data, trigger)
	item.Probe = &ValidationProbe{SiteNames: data.ServerNames}
	for _, name := range data.ACMEChallengeNames {
		if name != data.Domain && name != "www."+data.Domain {
			item.Probe.ExtraNames = append(item.Probe.ExtraNames, name)
		}
	}
	return item
}

func challengeDirEntries(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(ValidationProbeChallengeDir(root))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestTheProbeIsServedUnderEveryNameThenRemoved(t *testing.T) {
	h, nginx := probeHarness(t)
	data := probeData(nginx, "probe-ready.example")
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	path := SiteVhostPath(data.Domain)
	edited := ownerEdit(t, path)
	nginx.serve[data.Domain], nginx.serve["www."+data.Domain] = true, true

	result := h.apply(h.probeItem(data, transport.SiteFileTriggerChange))[0]
	if result.Outcome != transport.SiteFileOutcomeKept || result.Validation != transport.SiteFileValidationReady ||
		result.ValidationName != "" || result.ValidationStatus != 0 {
		t.Fatalf("kept, served: %+v", result)
	}
	if strings.Join(nginx.asked, ",") != data.Domain+",www."+data.Domain {
		t.Fatalf("asked %v", nginx.asked)
	}
	for index, seen := range nginx.seen {
		if !seen {
			t.Fatalf("the probe file was not on disk for request %d", index)
		}
	}
	if left := challengeDirEntries(t, nginx.root); len(left) != 0 {
		t.Fatalf("the probe file was left: %v", left)
	}
	if readTestFile(t, path) != edited {
		t.Fatal("the owner's file was touched")
	}
	// The include line is not what decides: a file that has it, served by an
	// nginx that does not answer the probe for the site, is not ready.
	nginx.asked, nginx.serve[data.Domain] = nil, false
	result = h.apply(h.probeItem(data, transport.SiteFileTriggerChange))[0]
	if result.Validation != transport.SiteFileValidationIncludeMissing ||
		result.ValidationName != data.Domain || result.ValidationStatus != http.StatusNotFound {
		t.Fatalf("include line present, probe not served: %+v", result)
	}
	// Without a probe asked for, a kept file's validation is not evaluated.
	if result = h.apply(h.certItem(data, transport.SiteFileTriggerChange))[0]; result.Validation != "" {
		t.Fatalf("no probe asked for: %+v", result)
	}
}

func TestAProbeAnsweredOtherwiseNamesTheFirstNameAndItsStatus(t *testing.T) {
	h, nginx := probeHarness(t)
	data := probeData(nginx, "probe-redirect.example")
	writeTestFile(t, SiteVhostPath(data.Domain), "server {\n    listen 80;\n    return 301 https://$host$request_uri;\n}\n", 0o644)
	nginx.status[data.Domain] = http.StatusMovedPermanently
	nginx.serve["www."+data.Domain] = true
	result := h.apply(h.probeItem(data, transport.SiteFileTriggerChange))[0]
	if result.Validation != transport.SiteFileValidationIncludeMissing || result.ValidationName != data.Domain ||
		result.ValidationStatus != http.StatusMovedPermanently {
		t.Fatalf("redirect: %+v", result)
	}
	// The probe makes CelikPanel's challenge file available whatever the text
	// of the kept file says, with one check and reload.
	if result.ChallengeFile != transport.SiteFileChallengeWritten || !result.Reloaded {
		t.Fatalf("challenge file for a probed kept file: %+v", result)
	}
	if left := challengeDirEntries(t, nginx.root); len(left) != 0 {
		t.Fatalf("the probe file was left: %v", left)
	}
}

func TestValidationOnlyNamesThatAreNotServedAreNamesMissing(t *testing.T) {
	h, nginx := probeHarness(t)
	data := probeData(nginx, "probe-mail.example")
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	ownerEdit(t, SiteVhostPath(data.Domain))
	withMail := data
	withMail.ACMEChallengeNames = []string{"mail.probe-mail.example"}
	nginx.serve[data.Domain], nginx.serve["www."+data.Domain] = true, true
	result := h.apply(h.probeItem(withMail, transport.SiteFileTriggerChange))[0]
	if result.Validation != transport.SiteFileValidationNamesMissing || result.ValidationName != "mail.probe-mail.example" ||
		result.ValidationStatus != http.StatusNotFound {
		t.Fatalf("mail not served: %+v", result)
	}
	nginx.serve["mail.probe-mail.example"] = true
	if result = h.apply(h.probeItem(withMail, transport.SiteFileTriggerChange))[0]; result.Validation != transport.SiteFileValidationReady {
		t.Fatalf("mail served: %+v", result)
	}
	// A site name that fails outranks a validation-only one.
	nginx.serve[data.Domain], nginx.serve["mail.probe-mail.example"] = false, false
	if result = h.apply(h.probeItem(withMail, transport.SiteFileTriggerChange))[0]; result.Validation != transport.SiteFileValidationIncludeMissing {
		t.Fatalf("site and mail not served: %+v", result)
	}
}

func TestNoAnswerOnPort80IsUnknownNotNotReady(t *testing.T) {
	h, nginx := probeHarness(t)
	data := probeData(nginx, "probe-down.example")
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	ownerEdit(t, SiteVhostPath(data.Domain))
	nginx.down = true
	result := h.apply(h.probeItem(data, transport.SiteFileTriggerChange))[0]
	if result.Validation != transport.SiteFileValidationUnknown || !strings.Contains(result.ValidationDetail, "did not answer") ||
		result.ValidationStatus != 0 {
		t.Fatalf("nginx down: %+v", result)
	}
	if len(nginx.asked) != 1 {
		t.Fatalf("asked again after no answer: %v", nginx.asked)
	}
	if left := challengeDirEntries(t, nginx.root); len(left) != 0 {
		t.Fatalf("the probe file was left: %v", left)
	}
	// A challenge directory that is not there: the probe cannot be made.
	nginx.down = false
	if err := os.RemoveAll(filepath.Join(nginx.root, ".well-known")); err != nil {
		t.Fatal(err)
	}
	if result = h.apply(h.probeItem(data, transport.SiteFileTriggerChange))[0]; result.Validation != transport.SiteFileValidationUnknown {
		t.Fatalf("no challenge directory: %+v", result)
	}
}

func TestTheSiteConfigReadMeasuresAndPublishesTheChallengeFile(t *testing.T) {
	h, nginx := probeHarness(t)
	data := probeData(nginx, "probe-inspect.example")
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	path := SiteVhostPath(data.Domain)
	edited := ownerEdit(t, path)
	if err := os.Remove(ACMEChallengeFilePath(data.Domain)); err != nil {
		t.Fatal(err)
	}
	nginx.serve[data.Domain], nginx.serve["www."+data.Domain] = true, true
	reloads := h.reloads
	inspected, _, _ := h.ng.InspectManagedVhost(h.probeItem(data, transport.SiteFileTriggerChange))
	if inspected.Validation != transport.SiteFileValidationReady || inspected.ChallengeFile != transport.SiteFileChallengeWritten ||
		h.reloads != reloads+1 {
		t.Fatalf("inspect with probe: %+v reloads=%d", inspected, h.reloads-reloads)
	}
	if readTestFile(t, path) != edited {
		t.Fatal("the owner's file was touched")
	}
	// Again: the challenge file is there, nothing reloaded.
	inspected, _, _ = h.ng.InspectManagedVhost(h.probeItem(data, transport.SiteFileTriggerChange))
	if inspected.Validation != transport.SiteFileValidationReady || inspected.ChallengeFile != transport.SiteFileChallengeUnchanged ||
		h.reloads != reloads+1 {
		t.Fatalf("inspect again: %+v", inspected)
	}
	// Without the probe, inspection writes nothing and draws no verdict.
	before := len(nginx.asked)
	inspected, _, _ = h.ng.InspectManagedVhost(h.certItem(data, transport.SiteFileTriggerChange))
	if inspected.Validation != "" || len(nginx.asked) != before {
		t.Fatalf("inspect without probe: %+v", inspected)
	}
	// nginx refusing the challenge file on a read: put back, challenge_failed.
	if err := os.Remove(ACMEChallengeFilePath(data.Domain)); err != nil {
		t.Fatal(err)
	}
	h.refuse = &NginxConfigRefusedError{Output: "nginx: [emerg] something", Cause: errors.New("exit status 1")}
	inspected, _, _ = h.ng.InspectManagedVhost(h.probeItem(data, transport.SiteFileTriggerChange))
	if inspected.Validation != transport.SiteFileValidationChallengeFailed || inspected.ChallengeFile != transport.SiteFileChallengeFailed {
		t.Fatalf("refused on read: %+v", inspected)
	}
	if _, err := os.Lstat(ACMEChallengeFilePath(data.Domain)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the refused challenge file was not put back")
	}
}

// The real request: 127.0.0.1 on the URL's port, the name as Host, a
// redirect followed only to the same name, no answer is unknown.
func TestLocalProbeRequestAsksLoopbackWithTheNameAsHost(t *testing.T) {
	var hosts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hosts = append(hosts, r.Host)
		switch r.URL.Path {
		case "/.well-known/acme-challenge/ok":
			_, _ = w.Write([]byte("body"))
		case "/.well-known/acme-challenge/same":
			http.Redirect(w, r, "http://"+r.Host+"/.well-known/acme-challenge/ok", http.StatusMovedPermanently)
		case "/.well-known/acme-challenge/other":
			http.Redirect(w, r, "http://elsewhere.example/.well-known/acme-challenge/ok", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	name := "probe.example:" + port
	ctx := context.Background()

	if answer := localValidationProbeGet(ctx, name, acmeChallengeURLPath+"ok"); !answer.Answered ||
		answer.Status != http.StatusOK || string(answer.Body) != "body" {
		t.Fatalf("ok: %+v", answer)
	}
	if hosts[0] != name {
		t.Fatalf("Host %q", hosts[0])
	}
	if answer := localValidationProbeGet(ctx, name, acmeChallengeURLPath+"same"); answer.Status != http.StatusOK {
		t.Fatalf("same-name redirect: %+v", answer)
	}
	if answer := localValidationProbeGet(ctx, name, acmeChallengeURLPath+"other"); !answer.Answered || answer.Status != http.StatusFound {
		t.Fatalf("redirect to another name: %+v", answer)
	}
	if answer := localValidationProbeGet(ctx, name, acmeChallengeURLPath+"missing"); answer.Status != http.StatusNotFound {
		t.Fatalf("missing: %+v", answer)
	}
	server.Close()
	if answer := localValidationProbeGet(ctx, name, acmeChallengeURLPath+"ok"); answer.Answered || answer.Err == nil {
		t.Fatalf("closed: %+v", answer)
	}
}
