package services

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// D-031 step 1b: the ACME HTTP-01 location lives in the Panel's own include
// directory of the site, so a certificate can be validated for a site whose
// vhost the owner kept, as long as the kept file still includes the
// directory. The file's lifecycle is the location's: written with the vhost,
// kept as long as the site exists (certbot's own tokens in the challenge root
// come and go with each validation, as before), removed with the site.

func (h *managedVhostHarness) certItem(data VhostData, trigger string) ManagedVhostItem {
	h.t.Helper()
	item := h.item(data, trigger)
	item.ACMEChallengeRoot = data.ACMEChallengeRoot
	if data.SSLType != "none" {
		item.SSLCert = data.SSLCert
	}
	block, err := h.ng.RenderACMENamesBlock(data)
	if err != nil {
		h.t.Fatal(err)
	}
	item.ValidationBlock = block
	return item
}

// ownerEdit is an owner's edit under the header that leaves the include lines.
func ownerEdit(t *testing.T, path string) string {
	t.Helper()
	edited := strings.Replace(readTestFile(t, path), "index index.php", "index owner.html index.php", 1)
	writeTestFile(t, path, edited, 0o644)
	return edited
}

func TestEveryServerBlockIncludesThePanelDirectoryAndNoneCarriesTheLocation(t *testing.T) {
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	certificate := func(data VhostData) VhostData {
		data.SSLType, data.SSLCert, data.SSLKey = "letsencrypt", "/etc/letsencrypt/live/x/fullchain.pem", "/etc/letsencrypt/live/x/privkey.pem"
		return data
	}
	base := managedTestData("blocks.example", "/var/run/php/php8.4-fpm-site7.sock")
	forwarding := base
	forwarding.ProjectType, forwarding.ForwardTo = "forwarding", "https://target.example"
	forced := certificate(base)
	forced.ForceHTTPS = true
	mail := base
	mail.ACMEChallengeNames = []string{"mail.blocks.example"}
	for name, data := range map[string]VhostData{
		"http only": base, "with certificate": certificate(base), "forced https": forced,
		"forwarding": forwarding, "forwarding with certificate": certificate(forwarding), "validation names": mail,
	} {
		t.Run(name, func(t *testing.T) {
			out, err := ng.Render(data)
			if err != nil {
				t.Fatal(err)
			}
			include := PanelManagedIncludeLine(data.Domain)
			if blocks, includes := strings.Count(out, "server {"), strings.Count(out, include); blocks == 0 || blocks != includes {
				t.Fatalf("%d server blocks, %d include lines\n%s", blocks, includes, out)
			}
			if strings.Contains(out, "location ^~ /.well-known/acme-challenge/") {
				t.Fatalf("the vhost carries the challenge location itself\n%s", out)
			}
			if !fileHasDirective([]byte(out), include) {
				t.Fatal("the include line is not found as a directive")
			}
		})
	}
	file := RenderACMEChallengeFile("blocks.example", testACMEChallengeRoot)
	if !strings.Contains(file, "location ^~ /.well-known/acme-challenge/ {") ||
		!strings.Contains(file, "root "+testACMEChallengeRoot+";") || !strings.Contains(file, "try_files $uri =404;") {
		t.Fatalf("challenge file:\n%s", file)
	}
}

func TestTheChallengeFileIsWrittenWithTheVhostAndNotRewritten(t *testing.T) {
	for _, platform := range managedVhostPlatforms {
		t.Run(platform.name, func(t *testing.T) {
			h := newManagedVhostHarness(t)
			data := managedTestData("challenge.example", platform.socket)
			result := h.apply(h.certItem(data, transport.SiteFileTriggerCreate))[0]
			if result.Outcome != transport.SiteFileOutcomeWritten || result.ChallengeFile != transport.SiteFileChallengeWritten {
				t.Fatalf("create: %+v", result)
			}
			if result.ManagedDir != PanelManagedDir(data.Domain) || result.ManagedInclude != PanelManagedIncludeLine(data.Domain) {
				t.Fatalf("managed directory: %+v", result)
			}
			info, err := os.Stat(PanelManagedDir(data.Domain))
			if err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf("managed directory: %v %v", info, err)
			}
			path := ACMEChallengeFilePath(data.Domain)
			if readTestFile(t, path) != SealManagedText(RenderACMEChallengeFile(data.Domain, testACMEChallengeRoot)) {
				t.Fatal("the challenge file is not CelikPanel's sealed text")
			}
			inode := inodeOf(t, path)
			result = h.apply(h.certItem(data, transport.SiteFileTriggerStartup))[0]
			if result.Outcome != transport.SiteFileOutcomeUnchanged || result.ChallengeFile != transport.SiteFileChallengeUnchanged ||
				result.Reloaded || h.reloads != 1 || inodeOf(t, path) != inode {
				t.Fatalf("start with nothing changed: %+v reloads=%d", result, h.reloads)
			}
			// A removed challenge file is written again with one check and reload.
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			result = h.apply(h.certItem(data, transport.SiteFileTriggerStartup))[0]
			if result.ChallengeFile != transport.SiteFileChallengeWritten || !result.Reloaded || h.reloads != 2 {
				t.Fatalf("restored challenge file: %+v", result)
			}
		})
	}
}

func TestAKeptFileThatIncludesThePanelDirectoryLetsTheValidationRun(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("kept-ready.example", managedVhostPlatforms[0].socket)
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	path := SiteVhostPath(data.Domain)
	edited := ownerEdit(t, path)
	if err := os.Remove(ACMEChallengeFilePath(data.Domain)); err != nil {
		t.Fatal(err)
	}
	reloads := h.reloads
	result := h.apply(h.certItem(data, transport.SiteFileTriggerChange))[0]
	if result.Outcome != transport.SiteFileOutcomeKept || result.Validation != transport.SiteFileValidationReady ||
		result.ChallengeFile != transport.SiteFileChallengeWritten || !result.Reloaded || h.reloads != reloads+1 {
		t.Fatalf("kept, ready: %+v", result)
	}
	if readTestFile(t, path) != edited {
		t.Fatal("the owner's file was touched")
	}
	// The same again: nothing written, nothing reloaded, still ready.
	result = h.apply(h.certItem(data, transport.SiteFileTriggerChange))[0]
	if result.Validation != transport.SiteFileValidationReady || result.ChallengeFile != transport.SiteFileChallengeUnchanged ||
		result.Reloaded || h.reloads != reloads+1 {
		t.Fatalf("kept, ready, again: %+v", result)
	}
	// Inspection says the same without writing.
	inspected, _, _ := h.ng.InspectManagedVhost(h.certItem(data, transport.SiteFileTriggerChange))
	if inspected.Validation != transport.SiteFileValidationReady {
		t.Fatalf("inspection: %+v", inspected)
	}
}

func TestAKeptFileWithoutThePanelDirectoryCannotValidate(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("kept-old.example", managedVhostPlatforms[2].socket)
	path := SiteVhostPath(data.Domain)
	// A file from before step 1b: the location written inline, no include.
	writeTestFile(t, path, "server {\n    listen 80;\n    location ^~ /.well-known/acme-challenge/ { root /x; }\n}\n", 0o644)
	result := h.apply(h.certItem(data, transport.SiteFileTriggerChange))[0]
	if result.Outcome != transport.SiteFileOutcomeKept || result.Validation != transport.SiteFileValidationIncludeMissing ||
		result.ChallengeFile != "" || h.reloads != 0 {
		t.Fatalf("kept without the include: %+v", result)
	}
	if _, err := os.Lstat(ACMEChallengeFilePath(data.Domain)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a challenge file was written for a file that does not read it")
	}
	// A commented-out include line is not an include.
	writeTestFile(t, path, "server {\n    # "+PanelManagedIncludeLine(data.Domain)+"\n}\n", 0o644)
	if result = h.apply(h.certItem(data, transport.SiteFileTriggerChange))[0]; result.Validation != transport.SiteFileValidationIncludeMissing {
		t.Fatalf("commented include: %+v", result)
	}
	// Re-indented by an editor, it still is one.
	writeTestFile(t, path, "server {\n\t\tinclude   "+PanelManagedDir(data.Domain)+"/*.conf;\r\n}\n", 0o644)
	if result = h.apply(h.certItem(data, transport.SiteFileTriggerChange))[0]; result.Validation != transport.SiteFileValidationReady {
		t.Fatalf("re-indented include: %+v", result)
	}
}

func TestValidationNamesMustBeInTheKeptFileAsCelikPanelWritesThem(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("kept-mail.example", managedVhostPlatforms[1].socket)
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	path := SiteVhostPath(data.Domain)
	ownerEdit(t, path)
	withMail := data
	withMail.ACMEChallengeNames = []string{"mail.kept-mail.example"}
	result := h.apply(h.certItem(withMail, transport.SiteFileTriggerChange))[0]
	if result.Validation != transport.SiteFileValidationNamesMissing {
		t.Fatalf("names missing: %+v", result)
	}
	// The owner's file rendered with the names, then edited: ready.
	h2 := newManagedVhostHarness(t)
	h2.apply(h2.certItem(withMail, transport.SiteFileTriggerCreate))
	ownerEdit(t, SiteVhostPath(data.Domain))
	if result = h2.apply(h2.certItem(withMail, transport.SiteFileTriggerChange))[0]; result.Validation != transport.SiteFileValidationReady {
		t.Fatalf("names present: %+v", result)
	}
}

func TestAnOwnersEditOfTheChallengeFileIsKept(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("kept-challenge.example", managedVhostPlatforms[0].socket)
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	ownerEdit(t, SiteVhostPath(data.Domain))
	challenge := ACMEChallengeFilePath(data.Domain)
	writeTestFile(t, challenge, "location ^~ /.well-known/acme-challenge/ { root /owner; }\n", 0o644)
	result := h.apply(h.certItem(data, transport.SiteFileTriggerChange))[0]
	if result.ChallengeFile != transport.SiteFileChallengeKept || result.Validation != transport.SiteFileValidationChallengeKept {
		t.Fatalf("owner's challenge file: %+v", result)
	}
	if readTestFile(t, challenge) != "location ^~ /.well-known/acme-challenge/ { root /owner; }\n" {
		t.Fatal("the owner's challenge file was replaced")
	}
}

func TestNginxRefusingTheChallengeFilePutsItBack(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("refused-challenge.example", managedVhostPlatforms[0].socket)
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	edited := ownerEdit(t, SiteVhostPath(data.Domain))
	challenge := ACMEChallengeFilePath(data.Domain)
	if err := os.Remove(challenge); err != nil {
		t.Fatal(err)
	}
	h.refuse = &NginxConfigRefusedError{Output: `nginx: [emerg] duplicate location "/.well-known/acme-challenge/"`, Cause: errors.New("exit status 1")}
	results, err := h.ng.ApplyManagedVhosts([]ManagedVhostItem{h.certItem(data, transport.SiteFileTriggerChange)})
	if err == nil {
		t.Fatal("a refused configuration was not reported")
	}
	result := results[0]
	if result.Outcome != transport.SiteFileOutcomeKept || result.ChallengeFile != transport.SiteFileChallengeFailed ||
		result.Validation != transport.SiteFileValidationChallengeFailed || !strings.Contains(result.Detail, "duplicate location") {
		t.Fatalf("refused: %+v", result)
	}
	if _, err := os.Lstat(challenge); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the refused challenge file was not put back (removed)")
	}
	if readTestFile(t, SiteVhostPath(data.Domain)) != edited {
		t.Fatal("the owner's file was touched")
	}
}

func TestAKeptFileThatNamesTheCertificateIsSaidToDoSo(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("kept-cert.example", managedVhostPlatforms[0].socket)
	data.SSLType, data.SSLCert, data.SSLKey = "custom", "/certs/new/fullchain.pem", "/certs/new/privkey.pem"
	path := SiteVhostPath(data.Domain)
	writeTestFile(t, path, "server {\n    ssl_certificate /certs/old/fullchain.pem;\n}\n", 0o644)
	if result := h.apply(h.certItem(data, transport.SiteFileTriggerChange))[0]; result.CertificateReferenced {
		t.Fatalf("old certificate: %+v", result)
	}
	writeTestFile(t, path, "server {\n    ssl_certificate   /certs/new/fullchain.pem;\n}\n", 0o644)
	if result := h.apply(h.certItem(data, transport.SiteFileTriggerChange))[0]; !result.CertificateReferenced {
		t.Fatalf("new certificate: %+v", result)
	}
}

func TestRemovingASiteRemovesThePanelsChallengeFileOnly(t *testing.T) {
	h := newManagedVhostHarness(t)
	data := managedTestData("removed.example", managedVhostPlatforms[0].socket)
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	if _, err := h.ng.RemoveSiteVhost(data.Domain); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(PanelManagedDir(data.Domain)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the Panel's directory of a removed site was left")
	}
	// An owner's file in it keeps the directory.
	h.apply(h.certItem(data, transport.SiteFileTriggerCreate))
	writeTestFile(t, PanelManagedDir(data.Domain)+"/owner.conf", "# mine\n", 0o644)
	if _, err := h.ng.RemoveSiteVhost(data.Domain); err != nil {
		t.Fatal(err)
	}
	if readTestFile(t, PanelManagedDir(data.Domain)+"/owner.conf") != "# mine\n" {
		t.Fatal("an owner's file in the Panel's directory was removed")
	}
	if _, err := os.Lstat(ACMEChallengeFilePath(data.Domain)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the Panel's challenge file was left")
	}
}
