//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Corrections from the second native measurement (11 Oct 2026): the directory
// members tar writes are extracted, every hostile name is still refused, and
// the archive's mailbox password hashes are handed over only to an apply.
// İkinci yerel ölçümün düzeltmeleri.

// tarDirectoryName is the name archive/tar itself gives a directory member:
// tar.FileInfoHeader appends the slash, as GNU tar and Python's tarfile do.
func tarDirectoryName(t *testing.T, name string) string {
	t.Helper()
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		t.Fatal(err)
	}
	if header.Typeflag != tar.TypeDir || !strings.HasSuffix(header.Name, "/") {
		t.Fatalf("archive/tar wrote the directory member %q (typeflag %q): the premise of this test does not hold", header.Name, header.Typeflag)
	}
	return name + "/"
}

// The measured defect (Debian 13 and Ubuntu 24.04): an archive that holds the
// directory member `homedir/public_html/` failed the whole files step with
// "unsafe cpmove member path". This is an archive of a directory as tar writes
// it: every directory is a member of its own, with a trailing slash.
func TestExtractCpmoveFilesAcceptsTheDirectoryMembersTarWrites(t *testing.T) {
	archiveRoot, siteHome := configureCpmoveTestRoots(t)
	if err := os.WriteFile(filepath.Join(siteHome, "public_html", "placeholder.html"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := func(name string) cpmoveTestMember {
		return cpmoveTestMember{name: tarDirectoryName(t, name), typeflag: tar.TypeDir, mode: 0o755}
	}
	archivePath := writeCpmoveSiteArchive(t, archiveRoot, []cpmoveTestMember{
		dir("cpmove-user"),
		dir("cpmove-user/cp"),
		{name: "cpmove-user/cp/user", typeflag: tar.TypeReg, content: "DNS=imported.example\n"},
		dir("cpmove-user/homedir"),
		dir("cpmove-user/homedir/public_html"),
		{name: "cpmove-user/homedir/public_html/index.html", typeflag: tar.TypeReg, content: "home"},
		dir("cpmove-user/homedir/public_html/assets"),
		dir("cpmove-user/homedir/public_html/assets/css"),
		{name: "cpmove-user/homedir/public_html/assets/css/site.css", typeflag: tar.TypeReg, content: "body{}"},
		dir("cpmove-user/homedir/public_html/uploads"),
		dir("cpmove-user/homedir/public_html/uploads/2026/10"),
		dir("cpmove-user/homedir/mail"),
		{name: "cpmove-user/homedir/mail/outside.txt", typeflag: tar.TypeReg, content: "not site payload"},
	})

	response, err := runCpmoveExtraction(t, archivePath)
	if err != nil {
		t.Fatalf("an archive with directory members was refused: %v", err)
	}
	if !response.Complete || response.Files != 2 || response.Bytes != int64(len("home")+len("body{}")) {
		t.Fatalf("response = %+v", response)
	}
	root := filepath.Join(siteHome, "public_html")
	for path, want := range map[string]string{"index.html": "home", "assets/css/site.css": "body{}"} {
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(content) != want {
			t.Fatalf("%s = %q, err = %v", path, content, err)
		}
	}
	// Directories are created as members of their own, the empty ones too.
	for _, path := range []string{"assets", "assets/css", "uploads", "uploads/2026/10"} {
		if info, err := os.Lstat(filepath.Join(root, path)); err != nil || !info.IsDir() {
			t.Fatalf("directory %s: %v", path, err)
		}
	}
	for _, absent := range []string{"placeholder.html", "outside.txt", "mail", "public_html", "homedir"} {
		if _, err := os.Lstat(filepath.Join(root, absent)); !os.IsNotExist(err) {
			t.Fatalf("%s is in the document root: %v", absent, err)
		}
	}
	assertNoCpmoveStage(t, siteHome)

	// The same archive is inspected without complaint, as before.
	var preview CpmoveInspectResponse
	if err := (&Agent{}).InspectCpmove(&CpmoveInspectRequest{Path: archivePath}, &preview); err != nil || preview.Error != "" {
		t.Fatalf("inspect: %v %q", err, preview.Error)
	}
	if !preview.PublicHTML || preview.SiteBytes != 10 {
		t.Fatalf("preview = %+v", preview)
	}
}

// Every refusal that existed before the directory members were accepted still
// stands, and the one new acceptance is no wider than "one trailing slash on a
// directory".
func TestCpmovePayloadRelativeKeepsEveryRefusal(t *testing.T) {
	type answer struct {
		relative string
		payload  bool
		refused  bool
	}
	for _, c := range []struct {
		name      string
		member    string
		directory bool
		want      answer
	}{
		// What is accepted.
		{"the document root as tar writes it", "cpmove-user/homedir/public_html/", true, answer{"", true, false}},
		{"the document root without a slash", "cpmove-user/homedir/public_html", true, answer{"", true, false}},
		{"a backup-named archive", "backup-10.9.2026_user/homedir/public_html/", true, answer{"", true, false}},
		{"a leading ./", "./cpmove-user/homedir/public_html/", true, answer{"", true, false}},
		{"a nested directory", "cpmove-user/homedir/public_html/a/b/", true, answer{"a/b", true, false}},
		{"a file", "cpmove-user/homedir/public_html/a/b/c.php", false, answer{"a/b/c.php", true, false}},
		{"a file whose path cleans", "homedir/public_html/a//b/./c.php", false, answer{"a/b/c.php", true, false}},
		// What is not site payload is skipped, as it always was.
		{"another part of the archive", "cpmove-user/mysql/", true, answer{"", false, false}},
		{"the archive's top directory", "cpmove-user/", true, answer{"", false, false}},
		{"an absolute name", "/etc/passwd", false, answer{"", false, false}},
		{"an absolute name that looks like payload", "/homedir/public_html/x.php", false, answer{"", false, false}},
		{"a sibling with the same prefix", "homedir/public_html_old/x.php", false, answer{"", false, false}},
		{"a file outside payload with a slash", "cpmove-user/cp/user/", false, answer{"", false, false}},
		// What is refused.
		{"an empty name", "", false, answer{"", false, true}},
		{"a NUL", "homedir/public_html/a\x00b", false, answer{"", false, true}},
		{"a backslash", `homedir/public_html/a\..\b`, false, answer{"", false, true}},
		{"a parent component in the payload", "homedir/public_html/../../etc/passwd", false, answer{"", false, true}},
		{"a parent component before the payload", "cpmove-user/../homedir/public_html/x", false, answer{"", false, true}},
		{"a parent component at the end", "homedir/public_html/a/..", true, answer{"", false, true}},
		{"a parent component at the end with a slash", "homedir/public_html/a/../", true, answer{"", false, true}},
		{"only a parent component", "..", true, answer{"", false, true}},
		{"a doubled slash at the end of the root", "homedir/public_html//", true, answer{"", false, true}},
		{"a dot below the root", "homedir/public_html/.", true, answer{"", false, true}},
		{"a dot below the root with a slash", "homedir/public_html/./", true, answer{"", false, true}},
		{"a file named like the document root", "cpmove-user/homedir/public_html/", false, answer{"", false, true}},
		{"a file with a trailing slash", "homedir/public_html/index.php/", false, answer{"", false, true}},
	} {
		relative, payload, err := cpmovePayloadRelative(c.member, c.directory)
		got := answer{relative, payload, err != nil}
		if got != c.want {
			t.Fatalf("%s (%q, directory=%t): got %+v, want %+v", c.name, c.member, c.directory, got, c.want)
		}
		if strings.HasPrefix(relative, "/") || strings.Contains(relative, "..") || strings.HasSuffix(relative, "/") {
			t.Fatalf("%s: the payload path %q can leave the document root", c.name, relative)
		}
	}
}

// Each hostile archive fails closed: nothing is published and the site that
// was there is unchanged.
func TestExtractCpmoveFilesRefusesHostileMembersWithoutChangingTheSite(t *testing.T) {
	for name, members := range map[string][]cpmoveTestMember{
		"a parent component": {
			{name: "cpmove-user/homedir/public_html/", typeflag: tar.TypeDir, mode: 0o755},
			{name: "cpmove-user/homedir/public_html/ok.html", typeflag: tar.TypeReg, content: "ok"},
			{name: "cpmove-user/homedir/public_html/../../../../etc/cron.d/x", typeflag: tar.TypeReg, content: "* * * * * root id"},
		},
		"a directory that climbs": {
			{name: "cpmove-user/homedir/public_html/a/../../", typeflag: tar.TypeDir, mode: 0o755},
		},
		"a symbolic link": {
			{name: "cpmove-user/homedir/public_html/", typeflag: tar.TypeDir, mode: 0o755},
			{name: "cpmove-user/homedir/public_html/link", typeflag: tar.TypeSymlink},
		},
		"a hard link": {
			{name: "cpmove-user/homedir/public_html/hard", typeflag: tar.TypeLink},
		},
		"a device": {
			{name: "cpmove-user/homedir/public_html/dev", typeflag: tar.TypeChar},
		},
		"a backslash": {
			{name: `cpmove-user/homedir/public_html/a\b.php`, typeflag: tar.TypeReg, content: "x"},
		},
		"a file below a file": {
			{name: "cpmove-user/homedir/public_html/a", typeflag: tar.TypeReg, content: "x"},
			{name: "cpmove-user/homedir/public_html/a/b", typeflag: tar.TypeReg, content: "y"},
		},
	} {
		archiveRoot, siteHome := configureCpmoveTestRoots(t)
		current := filepath.Join(siteHome, "public_html", "index.html")
		if err := os.WriteFile(current, []byte("current"), 0o600); err != nil {
			t.Fatal(err)
		}
		response, err := runCpmoveExtraction(t, writeCpmoveSiteArchive(t, archiveRoot, members))
		if err == nil || response.Complete {
			t.Fatalf("%s: response = %+v, err = %v; want the archive refused", name, response, err)
		}
		content, readErr := os.ReadFile(current)
		if readErr != nil || string(content) != "current" {
			t.Fatalf("%s: the current site changed: %q, %v", name, content, readErr)
		}
		entries, _ := os.ReadDir(filepath.Join(siteHome, "public_html"))
		if len(entries) != 1 {
			t.Fatalf("%s: the document root holds %d entries", name, len(entries))
		}
		assertNoCpmoveStage(t, siteHome)
	}
}

var set2CryptHashShaped = regexp.MustCompile(`\$(?:1|2[abxy]?|5|6|y|argon2(?:id|i|d)?)\$[^\s"']{4,}`)

func writeCpmoveShadowArchive(t *testing.T, archiveRoot string) string {
	t.Helper()
	archivePath := filepath.Join(archiveRoot, "cpmove-mail.tar.gz")
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range map[string]string{
		"cpmove-user/cp/user": "DNS=imported.example\n",
		"cpmove-user/homedir/etc/imported.example/shadow": "info:$6$rounds=5000$Xo3pQ1saltSALT$u9q0n3h8Jx2m4tT1wYb7s5kPzQ2vN8rL0cD6fG4hJ1aS3dF5gH7jK9lZ1xC3vB5nM7qW9eR2tY:19500::::::\n" +
			"sales:$2y$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ0123456:19500::::::\n" +
			"suspended:!!$6$salt$abcdefghijklmnopqrstuvwxyz0123456789:19500::::::\n" +
			"locked:*LOCKED*:19500::::::\n" +
			"# a comment line:x\n" +
			"\n",
		"cpmove-user/homedir/etc/imported.example/quota": "info:262144000\n",
	} {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Typeflag: tar.TypeReg, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tarWriter, content); err != nil {
			t.Fatal(err)
		}
	}
	for _, closer := range []io.Closer{tarWriter, gzipWriter, file} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return archivePath
}

// S1. The Agent hands a mailbox's password hash over only when the Panel asks
// for it to apply an import. A preview gets one fact per mailbox: whether the
// archive holds a password for it.
func TestInspectCpmoveHandsHashesOverOnlyToAnApply(t *testing.T) {
	archiveRoot, _ := configureCpmoveTestRoots(t)
	archivePath := writeCpmoveShadowArchive(t, archiveRoot)

	var preview CpmoveInspectResponse
	if err := (&Agent{}).InspectCpmove(&CpmoveInspectRequest{Path: archivePath}, &preview); err != nil || preview.Error != "" {
		t.Fatalf("inspect: %v %q", err, preview.Error)
	}
	byUser := map[string]transport.CpmoveMailAccount{}
	for _, account := range preview.MailAccounts {
		if account.CryptHash != "" {
			t.Fatalf("a preview holds the password hash of %s", account.User)
		}
		byUser[account.User] = account
	}
	if len(byUser) != 4 {
		t.Fatalf("mail accounts = %+v", preview.MailAccounts)
	}
	for user, hasPassword := range map[string]bool{"info": true, "sales": true, "suspended": false, "locked": false} {
		account, listed := byUser[user]
		if !listed || account.HasPassword != hasPassword || account.Domain != "imported.example" {
			t.Fatalf("%s: %+v, listed %t, want has_password=%t", user, account, listed, hasPassword)
		}
	}
	if byUser["info"].QuotaMB != 250 {
		t.Fatalf("quota = %d", byUser["info"].QuotaMB)
	}
	// However the preview is encoded for a browser, a log or a stored answer,
	// no hash is in it.
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	if set2CryptHashShaped.Match(encoded) || strings.Contains(string(encoded), "crypt_hash") || !strings.Contains(string(encoded), `"has_password":true`) {
		t.Fatalf("encoded preview: %s", encoded)
	}

	// An apply asks, and gets the hashes of the mailboxes that have one.
	var apply CpmoveInspectResponse
	if err := (&Agent{}).InspectCpmove(&CpmoveInspectRequest{Path: archivePath, IncludeMailHashes: true}, &apply); err != nil || apply.Error != "" {
		t.Fatalf("inspect for an apply: %v %q", err, apply.Error)
	}
	for _, account := range apply.MailAccounts {
		if account.HasPassword != (account.CryptHash != "") {
			t.Fatalf("%s: has_password=%t, hash present=%t", account.User, account.HasPassword, account.CryptHash != "")
		}
		if account.HasPassword && !strings.HasPrefix(account.CryptHash, "$") {
			t.Fatalf("%s: hash %q", account.User, account.CryptHash[:3])
		}
		if account.User == "info" {
			if err := validateImportedCryptHash(account.CryptHash); err != nil {
				t.Fatalf("the hash handed to an apply is not importable: %v", err)
			}
		}
	}
	// Even then it is not encoded as JSON.
	if encoded, _ = json.Marshal(apply); set2CryptHashShaped.Match(encoded) {
		t.Fatalf("the Agent's answer encodes a hash: %s", encoded)
	}
}
