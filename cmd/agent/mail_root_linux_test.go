//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// stockMailSpoolLayout recreates Arch's layout under a temporary root:
// var/mail -> spool/mail (relative), var/spool/mail a real directory.
func stockMailSpoolLayout(t *testing.T, linkText string) (string, string) {
	t.Helper()
	return stockMailSpoolLayoutAt(t, t.TempDir(), linkText)
}

func TestManagedMailRootResolvesOnlyTheStockMailLink(t *testing.T) {
	configureMailDomainDeletionTest(t)
	for _, text := range []string{"spool/mail", "<absolute>"} {
		t.Run(text, func(t *testing.T) {
			base := t.TempDir()
			linkText := text
			if text == "<absolute>" {
				linkText = filepath.Join(base, "var", "spool", "mail")
			}
			// The absolute form must name the exact target path.
			_, target := stockMailSpoolLayoutAt(t, base, linkText)
			root, err := managedMailRootPath()
			if err != nil {
				t.Fatal(err)
			}
			if root != filepath.Join(target, "vhosts") {
				t.Fatalf("root = %q, want %q", root, filepath.Join(target, "vhosts"))
			}
		})
	}
}

func stockMailSpoolLayoutAt(t *testing.T, base, linkText string) (string, string) {
	t.Helper()
	link := filepath.Join(base, "var", "mail")
	target := filepath.Join(base, "var", "spool", "mail")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkText, link); err != nil {
		t.Fatal(err)
	}
	previousLink, previousTarget, previousOwner, previousRoot := mailSpoolLinkPath, mailSpoolTargetPath, mailSpoolLinkOwner, mailRootDir
	mailSpoolLinkPath, mailSpoolTargetPath, mailSpoolLinkOwner = link, target, os.Geteuid()
	mailRootDir = filepath.Join(link, "vhosts")
	t.Cleanup(func() {
		mailSpoolLinkPath, mailSpoolTargetPath, mailSpoolLinkOwner, mailRootDir = previousLink, previousTarget, previousOwner, previousRoot
	})
	return link, target
}

// P4-2: a symlinked /var/mail with no mail root is nothing to clean.
func TestDeleteMailDomainOnStockLinkWithoutMailRootIsClean(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	_, target := stockMailSpoolLayout(t, "spool/mail")
	var response transport.DeleteMailDomainResponse
	if err := (&Agent{}).DeleteMailDomain(&request, &response); err != nil {
		t.Fatalf("delete on a stock /var/mail link: %v", err)
	}
	if !response.Applied || response.Quarantined {
		t.Fatalf("response = %+v", response)
	}
	if _, err := os.Stat(filepath.Join(target, "vhosts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup created a mail root: %v", err)
	}
}

// A domain with a mail runtime is quarantined under the resolved root.
func TestDeleteMailDomainQuarantinesUnderResolvedRoot(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	_, target := stockMailSpoolLayout(t, "spool/mail")
	message := filepath.Join(target, "vhosts", request.Domain, "user", "new", "message")
	if err := os.MkdirAll(filepath.Dir(message), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(message, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	var response transport.DeleteMailDomainResponse
	if err := (&Agent{}).DeleteMailDomain(&request, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Applied || !response.Quarantined {
		t.Fatalf("response = %+v", response)
	}
	moved := filepath.Join(target, "vhosts", mailDomainQuarantineDirectory,
		mailDomainQuarantineName(request.Domain, request.DomainID), "user", "new", "message")
	if content, err := os.ReadFile(moved); err != nil || string(content) != "kept" {
		t.Fatalf("quarantined message = %q, %v", content, err)
	}
}

func TestManagedMailRootRefusesOtherSymlinks(t *testing.T) {
	configureMailDomainDeletionTest(t)
	cases := map[string]func(t *testing.T){
		"link elsewhere": func(t *testing.T) {
			stockMailSpoolLayout(t, t.TempDir())
		},
		"stock text but target is a link": func(t *testing.T) {
			base := t.TempDir()
			elsewhere := t.TempDir()
			link := filepath.Join(base, "var", "mail")
			spool := filepath.Join(base, "var", "spool")
			if err := os.MkdirAll(spool, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, filepath.Join(spool, "mail")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("spool/mail", link); err != nil {
				t.Fatal(err)
			}
			previousLink, previousTarget, previousOwner, previousRoot := mailSpoolLinkPath, mailSpoolTargetPath, mailSpoolLinkOwner, mailRootDir
			mailSpoolLinkPath, mailSpoolTargetPath, mailSpoolLinkOwner = link, filepath.Join(spool, "mail"), os.Geteuid()
			mailRootDir = filepath.Join(link, "vhosts")
			t.Cleanup(func() {
				mailSpoolLinkPath, mailSpoolTargetPath, mailSpoolLinkOwner, mailRootDir = previousLink, previousTarget, previousOwner, previousRoot
			})
		},
		"link owned by someone else": func(t *testing.T) {
			stockMailSpoolLayout(t, "spool/mail")
			mailSpoolLinkOwner = os.Geteuid() + 1
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			arrange(t)
			if _, err := managedMailRootPath(); !errors.Is(err, errMailPathSymlinkRefused) {
				t.Fatalf("error = %v, want typed refusal", err)
			}
		})
	}
}

// A symbolic link inside a non-default root path is refused during a real
// cleanup, with the typed reason, and nothing outside is touched.
func TestDeleteMailDomainRefusesSymlinkedRootWithTypedReason(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	real := t.TempDir()
	if err := os.MkdirAll(filepath.Join(real, "vhosts", request.Domain), 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(real, linked); err != nil {
		t.Fatal(err)
	}
	mailRootDir = filepath.Join(linked, "vhosts")
	var response transport.DeleteMailDomainResponse
	err := (&Agent{}).DeleteMailDomain(&request, &response)
	if !errors.Is(err, errMailPathSymlinkRefused) || response.Applied {
		t.Fatalf("error = %v response = %+v", err, response)
	}
	if !strings.Contains(err.Error(), "mail storage refused") {
		t.Fatalf("error text = %q", err)
	}
	if _, statErr := os.Stat(filepath.Join(real, "vhosts", request.Domain)); statErr != nil {
		t.Fatalf("source behind the link changed: %v", statErr)
	}
}

func TestDeleteMailDomainRefusesSymlinkedDomainWithTypedReason(t *testing.T) {
	request := configureMailDomainDeletionTest(t)
	if err := os.MkdirAll(mailRootDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(mailRootDir, request.Domain)); err != nil {
		t.Fatal(err)
	}
	var response transport.DeleteMailDomainResponse
	if err := (&Agent{}).DeleteMailDomain(&request, &response); !errors.Is(err, errMailPathSymlinkRefused) {
		t.Fatalf("error = %v, want typed refusal", err)
	}
}

func TestDovecotVirtualConfRecordsResolvedRoot(t *testing.T) {
	for _, modern := range []bool{false, true} {
		conf := buildDovecotVirtualConfAt(modern, "/var/spool/mail/vhosts")
		if !strings.Contains(conf, "/var/spool/mail/vhosts/") || strings.Contains(conf, "/var/mail/vhosts") {
			t.Fatalf("modern=%v conf does not record the resolved root:\n%s", modern, conf)
		}
	}
	if buildDovecotVirtualConf(true) != buildDovecotVirtualConfAt(true, mailRootDir) {
		t.Fatal("default builder must render the configured root")
	}
}
