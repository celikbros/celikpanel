package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// D-031 component tests: the classifier, the render rule per state, the
// owner's three choices, a removed file, the frozen earlier releases' texts,
// write discipline and per-site isolation. The three platforms of the eighth
// native record (set8) differ in the generator only by the PHP-FPM socket
// path (internal/services/php_layout.go); every table below carries them.

type managedVhostPlatform struct {
	name   string
	socket string
}

var managedVhostPlatforms = []managedVhostPlatform{
	{name: "Debian 13", socket: "/var/run/php/php8.4-fpm-site7.sock"},
	{name: "Ubuntu 24.04", socket: "/var/run/php/php8.3-fpm-site7.sock"},
	{name: "Arch", socket: "/run/php-fpm/php8.5-fpm-site7.sock"},
}

type managedVhostHarness struct {
	t         *testing.T
	ng        *NginxGenerator
	validates int
	reloads   int
	refuse    error
}

func newManagedVhostHarness(t *testing.T) *managedVhostHarness {
	t.Helper()
	previous := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previous })
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	h := &managedVhostHarness{t: t, ng: ng}
	ng.validateNginx = func() error {
		h.validates++
		return h.refuse
	}
	ng.reloadNginx = func() error {
		h.reloads++
		return nil
	}
	return h
}

func managedTestData(domain, socket string) VhostData {
	return VhostData{
		SiteID: 7, Domain: domain, ProjectType: "php", PHPSocket: socket,
		DocumentRoot:      "/var/www/celikpanel/subscriptions/1/sites/7/public_html",
		ACMEChallengeRoot: testACMEChallengeRoot, SSLType: "none",
		ServerNames: []string{domain, "www." + domain},
	}
}

func (h *managedVhostHarness) item(data VhostData, trigger string) ManagedVhostItem {
	h.t.Helper()
	body, err := h.ng.Render(data)
	if err != nil {
		h.t.Fatal(err)
	}
	legacyData := data
	return ManagedVhostItem{
		Domain: data.Domain, Body: body, Trigger: trigger,
		Legacy: func() ([]LegacyVhostRender, error) { return LegacyVhostRenders(legacyData) },
	}
}

func (h *managedVhostHarness) apply(items ...ManagedVhostItem) []transport.SiteFileResult {
	h.t.Helper()
	results, err := h.ng.ApplyManagedVhosts(items)
	if err != nil {
		h.t.Fatalf("apply: %v", err)
	}
	return results
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Ino
}

func digestOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestManagedHeaderSealsTheBodyDigest(t *testing.T) {
	sealed := SealManagedText("server {}\n")
	if !strings.HasPrefix(sealed, "# celikpanel-render v2 sha256="+digestOf("server {}\n")+"\n") {
		t.Fatalf("header: %q", sealed)
	}
	declared, body, ok := splitManagedHeader([]byte(sealed))
	if !ok || declared != digestOf("server {}\n") || string(body) != "server {}\n" {
		t.Fatalf("split: %v %q %q", ok, declared, body)
	}
	// An editor that wrote CRLF still leaves a header: the edit is owner-edited.
	crlf := strings.Replace(sealed, "\n", "\r\n", 1)
	if _, _, ok := splitManagedHeader([]byte(crlf)); !ok {
		t.Fatal("a header line ending in CR was not read as the header")
	}
	for _, broken := range []string{"# celikpanel-render v2 sha256=xyz\nserver {}\n", "server {}\n", ""} {
		if _, _, ok := splitManagedHeader([]byte(broken)); ok {
			t.Fatalf("not a header: %q", broken)
		}
	}
}

func TestClassifierStates(t *testing.T) {
	for _, platform := range managedVhostPlatforms {
		t.Run(platform.name, func(t *testing.T) {
			h := newManagedVhostHarness(t)
			data := managedTestData("classify.example", platform.socket)
			item := h.item(data, transport.SiteFileTriggerStartup)
			path := SiteVhostPath(data.Domain)
			sealed := SealManagedText(item.Body)

			check := func(name, want, adopted string, recorded string) {
				t.Helper()
				got := inspectSiteFile(path, item.Legacy, recorded)
				if got.state != want || got.adoptedFrom != adopted {
					t.Fatalf("%s: state=%s adopted=%q reason=%s, want %s %q", name, got.state, got.adoptedFrom, got.reason, want, adopted)
				}
			}
			check("absent", transport.SiteFileAbsent, "", "")
			writeTestFile(t, path, sealed, 0o644)
			check("sealed", transport.SiteFileManagedUnchanged, "", "")
			writeTestFile(t, path, strings.Replace(sealed, "index index.php", "index owner.html index.php", 1), 0o644)
			check("edited under the header", transport.SiteFileOwnerEdited, "", "")
			writeTestFile(t, path, "server { listen 80; server_name classify.example; }\n", 0o644)
			check("replaced, with a record", transport.SiteFileForeign, "", digestOf(item.Body))
			check("replaced, without a record", transport.SiteFileUnknownOrigin, "", "")

			legacy, err := LegacyVhostRenders(data)
			if err != nil {
				t.Fatal(err)
			}
			for _, render := range legacy {
				writeTestFile(t, path, render.Text, 0o644)
				check("legacy "+render.Release, transport.SiteFileManagedUnchanged, render.Release, "")
			}
			// One byte off an earlier release's text: unknown origin.
			writeTestFile(t, path, legacy[0].Text+"# mine\n", 0o644)
			check("legacy plus an owner line", transport.SiteFileUnknownOrigin, "", "")

			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "owner.conf")
			writeTestFile(t, target, sealed, 0o644)
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			got := inspectSiteFile(path, item.Legacy, "")
			if got.state != transport.SiteFileUnreadable || got.reason != transport.SiteFileReasonSymlink {
				t.Fatalf("symlink: %+v", got)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			got = inspectSiteFile(path, item.Legacy, "")
			if got.state != transport.SiteFileUnreadable || got.reason != transport.SiteFileReasonNotRegular {
				t.Fatalf("directory: %+v", got)
			}
		})
	}
}

// The frozen templates are the exact texts of the two releases.
func TestLegacyVhostTemplatesAreTheFrozenReleaseTexts(t *testing.T) {
	for release, want := range map[string]string{
		legacyVhostTemplateAlpha81: "4b4f3bdc4333a505a9749c77dae4cac748193f2d597ec63dd64f98b1eaaadbd7",
		legacyVhostTemplateAlpha82: "8236662bfb77ed52d65039b7521858be6e7c8d89017ce492c49d52e77a8b1aff",
	} {
		if got := digestOf(release); got != want {
			t.Fatalf("frozen template digest %s, want %s", got, want)
		}
	}
	if strings.Contains(legacyVhostTemplateAlpha81, "\r") || strings.Contains(legacyVhostTemplateAlpha82, "\r") {
		t.Fatal("a frozen template carries CR bytes")
	}
	// alpha.81 included the Debian snippet, alpha.82 wrote it out.
	const snippetDirective = "        include snippets/fastcgi-php.conf;\n"
	if !strings.Contains(legacyVhostTemplateAlpha81, snippetDirective) ||
		strings.Contains(legacyVhostTemplateAlpha82, snippetDirective) {
		t.Fatal("the frozen templates are not the alpha.81 and alpha.82 texts")
	}
}

// Both forms of both releases are offered: the creation text (only the
// domain as server_name; set8 measured it on every platform) and the
// start/save text.
func TestLegacyRendersOfferCreationAndStartTexts(t *testing.T) {
	data := managedTestData("legacy.example", managedVhostPlatforms[0].socket)
	renders, err := LegacyVhostRenders(data)
	if err != nil {
		t.Fatal(err)
	}
	releases := map[string]string{}
	for _, render := range renders {
		releases[render.Release] = render.Text
	}
	for _, release := range []string{
		"v0.1.0-alpha.82", "v0.1.0-alpha.81",
		"v0.1.0-alpha.82 (creation)", "v0.1.0-alpha.81 (creation)",
	} {
		if releases[release] == "" {
			t.Fatalf("no %s text; have %v", release, len(renders))
		}
	}
	if !strings.Contains(releases["v0.1.0-alpha.82"], "server_name legacy.example www.legacy.example;") {
		t.Fatal("start text does not carry www")
	}
	if !strings.Contains(releases["v0.1.0-alpha.82 (creation)"], "server_name legacy.example;") {
		t.Fatal("creation text is not the domain alone")
	}
	if strings.Contains(releases["v0.1.0-alpha.82"], "celikpanel-sites.d") {
		t.Fatal("an earlier release's text carries the new include point")
	}
}

func TestStartWithEveryFileUnchangedWritesNothingAndDoesNotReload(t *testing.T) {
	h := newManagedVhostHarness(t)
	var items []ManagedVhostItem
	for index, platform := range managedVhostPlatforms {
		data := managedTestData([]string{"a.example", "b.example", "c.example"}[index], platform.socket)
		items = append(items, h.item(data, transport.SiteFileTriggerCreate))
	}
	h.apply(items...)
	if h.reloads != 1 || h.validates != 1 {
		t.Fatalf("creation: validates=%d reloads=%d", h.validates, h.reloads)
	}
	before := map[string]uint64{}
	modified := map[string]time.Time{}
	for _, item := range items {
		path := SiteVhostPath(item.Domain)
		before[path] = inodeOf(t, path)
		info, _ := os.Stat(path)
		modified[path] = info.ModTime()
	}
	for index := range items {
		items[index].Trigger = transport.SiteFileTriggerStartup
	}
	results := h.apply(items...)
	for _, result := range results {
		if result.Outcome != transport.SiteFileOutcomeUnchanged || result.Reloaded {
			t.Fatalf("start: %+v", result)
		}
		info, _ := os.Stat(result.Path)
		if inodeOf(t, result.Path) != before[result.Path] || !info.ModTime().Equal(modified[result.Path]) {
			t.Fatalf("%s was rewritten", result.Path)
		}
	}
	if h.reloads != 1 || h.validates != 1 {
		t.Fatalf("a start with every file unchanged validated %d / reloaded %d times in all", h.validates, h.reloads)
	}
}

func TestRenderRulePerState(t *testing.T) {
	for _, platform := range managedVhostPlatforms {
		t.Run(platform.name, func(t *testing.T) {
			h := newManagedVhostHarness(t)
			data := managedTestData("rule.example", platform.socket)
			item := h.item(data, transport.SiteFileTriggerStartup)
			path := SiteVhostPath(data.Domain)
			_, enabled := vhostPaths(data.Domain)

			// Absent at start: not recreated.
			result := h.apply(item)[0]
			if result.Outcome != transport.SiteFileOutcomeMissing || result.State != transport.SiteFileAbsent {
				t.Fatalf("absent at start: %+v", result)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("a removed file was recreated at start")
			}
			// Created: written, enabled, include directory made and empty.
			create := item
			create.Trigger = transport.SiteFileTriggerCreate
			result = h.apply(create)[0]
			if result.Outcome != transport.SiteFileOutcomeWritten || result.Enabled != "link" || !result.Reloaded {
				t.Fatalf("create: %+v", result)
			}
			if readTestFile(t, path) != SealManagedText(item.Body) {
				t.Fatal("created file is not the sealed text")
			}
			entries, err := os.ReadDir(OwnerIncludeDir(data.Domain))
			if err != nil || len(entries) != 0 {
				t.Fatalf("owner include directory: %v %d", err, len(entries))
			}
			if !strings.Contains(item.Body, "include "+OwnerIncludeDir(data.Domain)+"/*.conf;") {
				t.Fatal("the vhost does not include the owner directory")
			}
			// Managed and the Panel's text changed: written.
			changed := managedTestData("rule.example", platform.socket)
			changed.RedirectWWW = true
			changedItem := h.item(changed, transport.SiteFileTriggerChange)
			result = h.apply(changedItem)[0]
			if result.Outcome != transport.SiteFileOutcomeWritten {
				t.Fatalf("managed change: %+v", result)
			}
			// Owner-edited: kept, Panel's text held beside, no reload.
			owner := strings.Replace(readTestFile(t, path), "index index.php", "index owner.html index.php", 1)
			writeTestFile(t, path, owner, 0o640)
			reloads := h.reloads
			result = h.apply(item)[0]
			if result.Outcome != transport.SiteFileOutcomeKept || result.State != transport.SiteFileOwnerEdited {
				t.Fatalf("owner-edited: %+v", result)
			}
			if readTestFile(t, path) != owner || h.reloads != reloads {
				t.Fatal("the owner's file was touched or nginx reloaded")
			}
			if result.PendingPath != path+SiteFilePendingSuffix || readTestFile(t, result.PendingPath) != SealManagedText(item.Body) {
				t.Fatalf("pending: %+v", result)
			}
			if inspected := inspectSiteFile(result.PendingPath, nil, ""); inspected.state != transport.SiteFileManagedUnchanged {
				t.Fatal("the pending file is not a sealed Panel text")
			}
			// Foreign and unknown origin: kept.
			writeTestFile(t, path, "server { listen 80; }\n", 0o644)
			recorded := item
			recorded.RecordedSHA256 = digestOf(item.Body)
			if result = h.apply(recorded)[0]; result.Outcome != transport.SiteFileOutcomeKept || result.State != transport.SiteFileForeign {
				t.Fatalf("foreign: %+v", result)
			}
			if result = h.apply(item)[0]; result.Outcome != transport.SiteFileOutcomeKept || result.State != transport.SiteFileUnknownOrigin {
				t.Fatalf("unknown origin: %+v", result)
			}
			if readTestFile(t, path) != "server { listen 80; }\n" || h.reloads != reloads {
				t.Fatal("a foreign file was touched")
			}
			// The enabled link is never removed for a kept file.
			if state := enabledVhostState(enabled, path); state != "link" {
				t.Fatalf("enabled link: %s", state)
			}
			// Unreadable (a symlink): refused, target untouched.
			target := filepath.Join(t.TempDir(), "owner.conf")
			writeTestFile(t, target, "server {}\n", 0o644)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			result = h.apply(item)[0]
			if result.Outcome != transport.SiteFileOutcomeRefused || result.Reason != transport.SiteFileReasonSymlink {
				t.Fatalf("symlink: %+v", result)
			}
			if readTestFile(t, target) != "server {}\n" {
				t.Fatal("the symlink's target was written")
			}
			if info, _ := os.Lstat(path); info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("the symlink was replaced")
			}
		})
	}
}

func TestLegacyFileIsAdoptedAndUnknownOriginIsNeverWritten(t *testing.T) {
	for _, platform := range managedVhostPlatforms {
		t.Run(platform.name, func(t *testing.T) {
			h := newManagedVhostHarness(t)
			adopted := managedTestData("adopt.example", platform.socket)
			creationOnly := managedTestData("fresh.example", platform.socket)
			left := managedTestData("left.example", platform.socket)
			legacy, _ := LegacyVhostRenders(adopted)
			legacyCreation, _ := LegacyVhostRenders(creationOnly)
			legacyLeft, _ := LegacyVhostRenders(left)
			var creationText string
			for _, render := range legacyCreation {
				if render.Release == "v0.1.0-alpha.82"+LegacyCreationSuffix {
					creationText = render.Text
				}
			}
			writeTestFile(t, SiteVhostPath("adopt.example"), legacy[0].Text, 0o644)
			writeTestFile(t, SiteVhostPath("fresh.example"), creationText, 0o644)
			ownerText := legacyLeft[0].Text + "    # owner\n"
			writeTestFile(t, SiteVhostPath("left.example"), ownerText, 0o644)

			results := h.apply(
				h.item(adopted, transport.SiteFileTriggerStartup),
				h.item(creationOnly, transport.SiteFileTriggerStartup),
				h.item(left, transport.SiteFileTriggerStartup),
			)
			if results[0].Outcome != transport.SiteFileOutcomeWritten || results[0].AdoptedFrom != "v0.1.0-alpha.82" {
				t.Fatalf("adopt start text: %+v", results[0])
			}
			if results[1].Outcome != transport.SiteFileOutcomeWritten || results[1].AdoptedFrom != "v0.1.0-alpha.82 (creation)" {
				t.Fatalf("adopt creation text: %+v", results[1])
			}
			if results[2].Outcome != transport.SiteFileOutcomeKept || results[2].State != transport.SiteFileUnknownOrigin {
				t.Fatalf("unknown origin: %+v", results[2])
			}
			if readTestFile(t, SiteVhostPath("left.example")) != ownerText {
				t.Fatal("an unknown-origin file was written")
			}
			if _, _, ok := splitManagedHeader([]byte(readTestFile(t, SiteVhostPath("adopt.example")))); !ok {
				t.Fatal("the adopted file carries no header")
			}
			// Resumable: a second start finds the adopted files managed and
			// unchanged and leaves the unknown one alone.
			results = h.apply(
				h.item(adopted, transport.SiteFileTriggerStartup),
				h.item(creationOnly, transport.SiteFileTriggerStartup),
				h.item(left, transport.SiteFileTriggerStartup),
			)
			if results[0].Outcome != transport.SiteFileOutcomeUnchanged || results[1].Outcome != transport.SiteFileOutcomeUnchanged ||
				results[2].Outcome != transport.SiteFileOutcomeKept {
				t.Fatalf("second start: %+v", results)
			}
		})
	}
}

func TestTakeCelikPanelsKeepsADatedCopyAndPreservesModeAndOwner(t *testing.T) {
	h := newManagedVhostHarness(t)
	siteFileNow = func() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { siteFileNow = time.Now })
	data := managedTestData("take.example", managedVhostPlatforms[2].socket)
	item := h.item(data, transport.SiteFileTriggerTake)
	path := SiteVhostPath(data.Domain)
	owner := "server { listen 80; server_name take.example; }\n"
	writeTestFile(t, path, owner, 0o640)
	group := os.Getgid()
	if os.Geteuid() == 0 {
		group = 4242
		if err := os.Chown(path, 0, group); err != nil {
			t.Fatal(err)
		}
	}

	// A digest the owner was not shown: refused, nothing changed.
	stale := item
	stale.ExpectedFileSHA256 = digestOf("something else")
	if result := h.apply(stale)[0]; result.Outcome != transport.SiteFileOutcomeRefused || result.Reason != transport.SiteFileReasonChanged {
		t.Fatalf("stale take: %+v", result)
	}
	item.ExpectedFileSHA256 = digestOf(owner)
	item.ExpectedRenderSHA256 = digestOf(item.Body)
	result := h.apply(item)[0]
	if result.Outcome != transport.SiteFileOutcomeTaken || result.BackupPath != path+SiteFileBackupMarker+"20261010T120000Z" {
		t.Fatalf("take: %+v", result)
	}
	if readTestFile(t, result.BackupPath) != owner || readTestFile(t, path) != SealManagedText(item.Body) {
		t.Fatal("backup or file content")
	}
	for _, name := range []string{path, result.BackupPath} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if info.Mode().Perm() != 0o640 || int(stat.Gid) != group {
			t.Fatalf("%s: mode %v gid %d, want 0640 gid %d", name, info.Mode().Perm(), stat.Gid, group)
		}
	}
}

func TestRecreateWritesAMissingFileOnlyWhenAsked(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("recreate.example", managedVhostPlatforms[1].socket)
	item := h.item(data, transport.SiteFileTriggerChange)
	if result := h.apply(item)[0]; result.Outcome != transport.SiteFileOutcomeMissing {
		t.Fatalf("a setting change recreated a missing file: %+v", result)
	}
	item.Trigger = transport.SiteFileTriggerRecreate
	result := h.apply(item)[0]
	if result.Outcome != transport.SiteFileOutcomeRecreated || result.Enabled != "link" {
		t.Fatalf("recreate: %+v", result)
	}
	// Recreate never replaces a file that is there.
	writeTestFile(t, SiteVhostPath(data.Domain), "server {}\n", 0o644)
	if result = h.apply(item)[0]; result.Outcome != transport.SiteFileOutcomeKept {
		t.Fatalf("recreate over a file: %+v", result)
	}
}

// set8 scenario g: an immutable file. Its rename is refused (EPERM); the site
// is reported and kept, its enabled link stays, the other sites are written
// and nginx is reloaded once.
func TestAnUnwritableFileIsKeptAndDoesNotStopTheOtherSites(t *testing.T) {
	h := newManagedVhostHarness(t)
	var items []ManagedVhostItem
	for index, domain := range []string{"before.example", "locked.example", "after.example"} {
		data := managedTestData(domain, managedVhostPlatforms[index].socket)
		items = append(items, h.item(data, transport.SiteFileTriggerCreate))
	}
	h.apply(items...)
	locked := SiteVhostPath("locked.example")
	lockedBefore := readTestFile(t, locked)
	siteFileRename = func(from, to string) error {
		if to == locked {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EPERM}
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { siteFileRename = os.Rename })
	for index := range items {
		changed := managedTestData(items[index].Domain, managedVhostPlatforms[index].socket)
		changed.RedirectWWW = true
		items[index] = h.item(changed, transport.SiteFileTriggerStartup)
	}
	reloads := h.reloads
	results := h.apply(items...)
	if results[0].Outcome != transport.SiteFileOutcomeWritten || results[2].Outcome != transport.SiteFileOutcomeWritten {
		t.Fatalf("other sites: %+v", results)
	}
	if results[1].Outcome != transport.SiteFileOutcomeRefused || results[1].Reason != transport.SiteFileReasonWriteRefused ||
		!strings.Contains(results[1].Detail, "operation not permitted") {
		t.Fatalf("locked site: %+v", results[1])
	}
	if readTestFile(t, locked) != lockedBefore {
		t.Fatal("the locked file changed")
	}
	for _, domain := range []string{"before.example", "locked.example", "after.example"} {
		available, enabled := vhostPaths(domain)
		if enabledVhostState(enabled, available) != "link" {
			t.Fatalf("%s lost its enabled link", domain)
		}
	}
	if h.reloads != reloads+1 {
		t.Fatalf("reloads %d, want one more than %d", h.reloads, reloads)
	}
	if entries, _ := os.ReadDir(filepath.Dir(locked)); len(entries) != 3 {
		t.Fatalf("a temporary file was left: %d entries", len(entries))
	}
}

func TestNginxRefusalPutsBackEveryWrittenFileExactlyAndLeavesKeptFiles(t *testing.T) {
	h := newManagedVhostHarness(t)
	first := managedTestData("one.example", managedVhostPlatforms[0].socket)
	second := managedTestData("two.example", managedVhostPlatforms[1].socket)
	h.apply(h.item(first, transport.SiteFileTriggerCreate), h.item(second, transport.SiteFileTriggerCreate))
	before := readTestFile(t, SiteVhostPath("one.example"))
	owner := strings.Replace(readTestFile(t, SiteVhostPath("two.example")), "index index.php", "index mine.html", 1)
	writeTestFile(t, SiteVhostPath("two.example"), owner, 0o644)
	h.refuse = &NginxConfigRefusedError{Output: "nginx: [emerg] owner include broken", Cause: errors.New("exit status 1")}
	first.RedirectWWW = true
	results, err := h.ng.ApplyManagedVhosts([]ManagedVhostItem{
		h.item(first, transport.SiteFileTriggerChange), h.item(second, transport.SiteFileTriggerChange),
	})
	var refused *NginxConfigRefusedError
	if err == nil || !errors.As(err, &refused) {
		t.Fatalf("refusal error: %v", err)
	}
	if results[0].Outcome != transport.SiteFileOutcomeFailed || results[0].Reason != transport.SiteFileReasonNginxRefused ||
		results[0].Detail != "nginx: [emerg] owner include broken" {
		t.Fatalf("written then refused: %+v", results[0])
	}
	if results[1].Outcome != transport.SiteFileOutcomeKept {
		t.Fatalf("kept file: %+v", results[1])
	}
	if readTestFile(t, SiteVhostPath("one.example")) != before || readTestFile(t, SiteVhostPath("two.example")) != owner {
		t.Fatal("files were not put back exactly")
	}
}

func TestRemovingASiteKeepsACopyOfAnEditedVhostAndTheOwnerDirectory(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("gone.example", managedVhostPlatforms[0].socket)
	h.apply(h.item(data, transport.SiteFileTriggerCreate))
	path := SiteVhostPath(data.Domain)
	writeTestFile(t, filepath.Join(OwnerIncludeDir(data.Domain), "mine.conf"), "location /mine/ { return 204; }\n", 0o644)
	owner := readTestFile(t, path) + "# owner\n"
	writeTestFile(t, path, owner, 0o644)
	backup, err := h.ng.RemoveSiteVhost(data.Domain)
	if err != nil || backup == "" || readTestFile(t, backup) != owner {
		t.Fatalf("remove: %q %v", backup, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("vhost not removed")
	}
	if readTestFile(t, filepath.Join(OwnerIncludeDir(data.Domain), "mine.conf")) == "" {
		t.Fatal("the owner's include file was removed")
	}
}

func TestInspectionDiffIsBoundedAndHidesCredentials(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("diff.example", managedVhostPlatforms[0].socket)
	item := h.item(data, transport.SiteFileTriggerChange)
	path := SiteVhostPath(data.Domain)
	owner := strings.Replace(SealManagedText(item.Body), "    index index.php index.html index.htm;\n",
		"    index index.php index.html index.htm;\n    proxy_set_header Authorization \"Bearer s3cr3t-token\";\n", 1)
	writeTestFile(t, path, owner, 0o644)
	result, diff, truncated := h.ng.InspectManagedVhost(item)
	if result.State != transport.SiteFileOwnerEdited || result.Outcome != transport.SiteFileOutcomeInspected || truncated {
		t.Fatalf("inspect: %+v %v", result, truncated)
	}
	if strings.Contains(diff, "s3cr3t") || !strings.Contains(diff, "-    proxy_set_header Authorization [hidden by CelikPanel") {
		t.Fatalf("diff:\n%s", diff)
	}
	if !strings.HasPrefix(diff, "--- "+path+" (on this server)\n+++ CelikPanel's text\n@@ ") {
		t.Fatalf("diff header:\n%s", diff)
	}
	if readTestFile(t, path) != owner {
		t.Fatal("inspection wrote the file")
	}
	if _, err := os.Lstat(path + SiteFilePendingSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection wrote a pending file")
	}
	// A large file: the diff is cut and says so.
	var big strings.Builder
	for line := 0; line < 6000; line++ {
		big.WriteString("    # owner line with some words to make it long enough\n")
	}
	writeTestFile(t, path, big.String(), 0o644)
	_, diff, truncated = h.ng.InspectManagedVhost(item)
	if !truncated || len(diff) > maxSiteFileDiffBytes {
		t.Fatalf("large diff: truncated=%v bytes=%d", truncated, len(diff))
	}
}

func TestUnifiedDiffHunks(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"
	new := "a\nb\nC\nd\ne\nf\ng\nh\ni\nj\nk\n"
	diff, truncated := UnifiedSiteFileDiff("old", "new", []byte(old), []byte(new))
	want := "--- old\n+++ new\n@@ -1,6 +1,6 @@\n a\n b\n-c\n+C\n d\n e\n f\n@@ -8,3 +8,4 @@\n h\n i\n j\n+k\n"
	if diff != want || truncated {
		t.Fatalf("diff:\n%s\nwant:\n%s", diff, want)
	}
}
