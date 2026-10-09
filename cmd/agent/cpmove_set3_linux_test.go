//go:build linux

package main

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// O17 (final native round, 12 Oct 2026). Measured on Debian 13 and Ubuntu
// 24.04: an archive with a member named by an absolute path was imported, the
// member was left out, and the answer did not mention it. The site's files are
// still imported and nothing is written for the member; the answer now names
// it and counts what else of the archive this step did not copy.
func TestExtractCpmoveFilesNamesWhatItLeavesOut(t *testing.T) {
	archiveRoot, siteHome := configureCpmoveTestRoots(t)
	// An absolute name that would be writable if it were ever followed.
	escape := filepath.Join(t.TempDir(), "set3-escape-absolute.txt")
	archivePath := writeCpmoveSiteArchive(t, archiveRoot, []cpmoveTestMember{
		{name: "cpmove-user/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "cpmove-user/homedir/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "cpmove-user/homedir/public_html/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "cpmove-user/homedir/public_html/index.php", typeflag: tar.TypeReg, content: "<?php echo 1;"},
		{name: escape, typeflag: tar.TypeReg, content: "hostile"},
		{name: "/homedir/public_html/looks-like-payload.php", typeflag: tar.TypeReg, content: "hostile"},
		{name: "/etc/set3-link", typeflag: tar.TypeSymlink},
		{name: "cpmove-user/homedir/public_html/about.html", typeflag: tar.TypeReg, content: "about"},
		{name: "cpmove-user/homedir/mail/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "cpmove-user/homedir/mail/example.com/info/cur/1", typeflag: tar.TypeReg, content: "mail"},
		{name: "cpmove-user/homedir/mail/example.com/info/cur/2", typeflag: tar.TypeReg, content: "mail"},
		{name: "cpmove-user/homedir/etc/example.com/shadow", typeflag: tar.TypeReg, content: "info:x\n"},
		{name: "cpmove-user/mysql/shop.sql", typeflag: tar.TypeReg, content: "-- dump"},
		{name: "cpmove-user/homedir/www", typeflag: tar.TypeSymlink},
	})

	response, err := runCpmoveExtraction(t, archivePath)
	if err != nil {
		t.Fatalf("the archive's own site files were not imported: %v", err)
	}
	if !response.Complete || response.Files != 2 {
		t.Fatalf("response = %+v", response)
	}
	root := filepath.Join(siteHome, "public_html")
	for path, want := range map[string]string{"index.php": "<?php echo 1;", "about.html": "about"} {
		if content, err := os.ReadFile(filepath.Join(root, path)); err != nil || string(content) != want {
			t.Fatalf("%s = %q, err = %v", path, content, err)
		}
	}
	// Nothing was written for a refused member: not at its absolute path, not
	// in the document root.
	if _, err := os.Lstat(escape); !os.IsNotExist(err) {
		t.Fatalf("the absolute member was written: %v", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 2 {
		t.Fatalf("the document root holds %d entries", len(entries))
	}
	assertNoCpmoveStage(t, siteHome)

	// Every refused member is counted and named, with the reason.
	if response.RefusedCount != 3 || len(response.Refused) != 3 {
		t.Fatalf("refused = %+v (%d)", response.Refused, response.RefusedCount)
	}
	for index, name := range []string{escape, "/homedir/public_html/looks-like-payload.php", "/etc/set3-link"} {
		if response.Refused[index] != (transport.CpmoveRefusedMember{Name: name, Reason: transport.CpmoveRefusedAbsolutePath}) {
			t.Fatalf("refused[%d] = %+v", index, response.Refused[index])
		}
	}
	// What is outside the site folder is counted, by where it is.
	groups := map[string]int{}
	for _, group := range response.OutsideGroups {
		groups[group.Name] = group.Count
	}
	if response.OutsideCount != 5 || groups["homedir/mail"] != 2 || groups["homedir/etc"] != 1 || groups["mysql"] != 1 || groups["homedir"] != 1 || len(groups) != 4 {
		t.Fatalf("outside = %d, groups = %+v", response.OutsideCount, response.OutsideGroups)
	}

	// An archive that leaves nothing out says nothing.
	archiveRoot, _ = configureCpmoveTestRoots(t)
	response, err = runCpmoveExtraction(t, writeCpmoveSiteArchive(t, archiveRoot, []cpmoveTestMember{
		{name: "cpmove-user/homedir/public_html/index.html", typeflag: tar.TypeReg, content: "x"},
	}))
	if err != nil || response.RefusedCount != 0 || response.Refused != nil || response.OutsideCount != 0 || len(response.OutsideGroups) != 0 {
		t.Fatalf("response = %+v, err = %v", response, err)
	}
}

// A files step that is refused whole says which member refused it and what
// that member is, instead of only "unsafe" or "unsupported".
func TestRefusedFilesStepNamesTheMember(t *testing.T) {
	for name, c := range map[string]struct {
		member cpmoveTestMember
		want   []string
	}{
		"a symbolic link": {cpmoveTestMember{name: "cpmove-user/homedir/public_html/uploads", typeflag: tar.TypeSymlink},
			[]string{"unsupported cpmove site entry type", "cpmove-user/homedir/public_html/uploads", "a symbolic link"}},
		"a hard link": {cpmoveTestMember{name: "cpmove-user/homedir/public_html/hard", typeflag: tar.TypeLink},
			[]string{"unsupported cpmove site entry type", "public_html/hard", "a hard link"}},
		"a device node": {cpmoveTestMember{name: "cpmove-user/homedir/public_html/dev", typeflag: tar.TypeChar},
			[]string{"unsupported cpmove site entry type", "public_html/dev", "a device node"}},
		"a named pipe": {cpmoveTestMember{name: "cpmove-user/homedir/public_html/pipe", typeflag: tar.TypeFifo},
			[]string{"unsupported cpmove site entry type", "public_html/pipe", "a named pipe"}},
		"a parent component": {cpmoveTestMember{name: "cpmove-user/homedir/public_html/../../x", typeflag: tar.TypeReg, content: "x"},
			[]string{"unsafe cpmove member path", "cpmove-user/homedir/public_html/../../x"}},
	} {
		archiveRoot, siteHome := configureCpmoveTestRoots(t)
		response, err := runCpmoveExtraction(t, writeCpmoveSiteArchive(t, archiveRoot, []cpmoveTestMember{
			{name: "cpmove-user/homedir/public_html/ok.html", typeflag: tar.TypeReg, content: "ok"},
			c.member,
		}))
		if err == nil || response.Complete {
			t.Fatalf("%s: response = %+v, err = %v; want the step refused", name, response, err)
		}
		for _, part := range c.want {
			if !strings.Contains(err.Error(), part) {
				t.Fatalf("%s: the refusal %q does not say %q", name, err.Error(), part)
			}
		}
		if entries, _ := os.ReadDir(filepath.Join(siteHome, "public_html")); len(entries) != 0 {
			t.Fatalf("%s: the document root holds %d entries", name, len(entries))
		}
		assertNoCpmoveStage(t, siteHome)
	}
}
