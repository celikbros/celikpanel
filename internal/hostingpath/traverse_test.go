package hostingpath

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestHostingBaseAncestors(t *testing.T) {
	if got := HostingBaseAncestors(HostingBase()); !reflect.DeepEqual(got, []string{"/", "/var", "/var/www"}) {
		t.Fatalf("ancestors = %v", got)
	}
	if got := HostingBaseAncestors("relative/base"); got != nil {
		t.Fatalf("relative base ancestors = %v", got)
	}
}

// The traversal proof for the web server account and a site user, including
// the P3 state (0750 root:celikpanel /var/www on Arch).
func TestProveTraversalForWebServerAndSiteUsers(t *testing.T) {
	http := Account{Name: "http", UID: 33, GIDs: []uint32{33}}
	const celikpanel = 969
	dir := func(mode os.FileMode, uid, gid uint32) DirectoryState {
		return DirectoryState{Mode: os.ModeDir | mode, UID: uid, GID: gid}
	}
	for _, test := range []struct {
		name        string
		www         DirectoryState
		wantBlocked bool
		wantAccount string
	}{
		{"P3 umask-created 0750 root:celikpanel", dir(0o750, 0, celikpanel), true, "http"},
		{"conventional 0755 root:root", dir(0o755, 0, 0), false, ""},
		{"traverse-only 0711", dir(0o711, 0, 0), false, ""},
		{"web group only 0750 root:http", dir(0o750, 0, 33), true, ""},
		{"owned by web user without owner x", dir(0o605, 33, 33), true, "http"},
		{"root only 0700", dir(0o700, 0, 0), true, "http"},
	} {
		t.Run(test.name, func(t *testing.T) {
			states := map[string]DirectoryState{"/": dir(0o755, 0, 0), "/var": dir(0o755, 0, 0), "/var/www": test.www}
			block, err := ProveTraversal(HostingBaseAncestors(HostingBase()), func(path string) (DirectoryState, error) {
				return states[path], nil
			}, []Account{http, SiteAccounts()})
			if err != nil {
				t.Fatal(err)
			}
			if (block != nil) != test.wantBlocked {
				t.Fatalf("block = %+v, want blocked %v", block, test.wantBlocked)
			}
			if block != nil && (block.Directory != "/var/www" || block.Account != test.wantAccount || block.ModeText() != "0"+formatPerm(test.www.Mode)) {
				t.Fatalf("block = %+v (mode %s)", block, block.ModeText())
			}
		})
	}
}

func formatPerm(mode os.FileMode) string {
	const digits = "01234567"
	perm := uint32(mode.Perm())
	return string([]byte{digits[perm>>6&7], digits[perm>>3&7], digits[perm&7]})
}

func TestProveTraversalStopsAtMissingDirectoryAndReportsErrors(t *testing.T) {
	calls := []string{}
	block, err := ProveTraversal([]string{"/", "/var", "/var/www"}, func(path string) (DirectoryState, error) {
		calls = append(calls, path)
		if path == "/var" {
			return DirectoryState{}, ErrDirectoryMissing
		}
		return DirectoryState{Mode: os.ModeDir | 0o755}, nil
	}, []Account{SiteAccounts()})
	if err != nil || block != nil || !reflect.DeepEqual(calls, []string{"/", "/var"}) {
		t.Fatalf("block %+v err %v calls %v", block, err, calls)
	}
	failure := errors.New("input/output error")
	if _, err := ProveTraversal([]string{"/"}, func(string) (DirectoryState, error) {
		return DirectoryState{}, failure
	}, []Account{SiteAccounts()}); !errors.Is(err, failure) {
		t.Fatalf("inspection error = %v", err)
	}
	if _, err := ProveTraversal([]string{"/"}, func(string) (DirectoryState, error) {
		return DirectoryState{Mode: 0o644}, nil
	}, []Account{SiteAccounts()}); err == nil {
		t.Fatal("a regular file passed as a directory")
	}
}

func TestTraversalBlockTexts(t *testing.T) {
	block := TraversalBlock{Directory: "/var/www", Mode: os.ModeDir | 0o750, Owner: "root", Group: "celikpanel"}
	if block.ModeText() != "0750" || block.OwnerText() != "root:celikpanel" || block.Command() != "sudo chmod 755 /var/www" {
		t.Fatalf("texts %q %q %q", block.ModeText(), block.OwnerText(), block.Command())
	}
}
