//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/transport"
)

// hostingRootTestPreparer roots the hosting base in a temporary directory.
// Directories above that root are reported as ordinary 0755 root:root ones so
// the test's own 0700 temporary parents do not stand in for the server's.
func hostingRootTestPreparer(t *testing.T) (hostingRootPreparer, string, *[]byte) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var receipt []byte
	preparer := hostingRootPreparer{
		base:        filepath.Join(root, "var", "www", "celikpanel"),
		receiptPath: filepath.Join(root, "state", hostingRootReceiptName),
		uid:         os.Getuid(),
		gid:         os.Getgid(),
		stat: func(dir string) (hostingpath.DirectoryState, error) {
			if dir != root && !strings.HasPrefix(dir, root+"/") {
				return hostingpath.DirectoryState{Mode: os.ModeDir | 0o755}, nil
			}
			return hostingpath.StatDirectory(dir)
		},
		accounts: func() []hostingpath.Account {
			return []hostingpath.Account{{Name: "http", UID: 33, GIDs: []uint32{33}}, hostingpath.SiteAccounts()}
		},
		readReceipt: func(string) ([]byte, error) {
			if receipt == nil {
				return nil, os.ErrNotExist
			}
			return receipt, nil
		},
		writeReceipt: func(_ string, content []byte) error {
			receipt = append([]byte(nil), content...)
			return nil
		},
		now: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) },
	}
	return preparer, root, &receipt
}

func statMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// P3: under the Agent's UMask=0027, mkdir alone made /var/www 0750. The
// directories the Agent creates must come out 0755 whatever the umask.
func TestHostingRootCreatesMissingParentsAt0755UnderRestrictiveUmask(t *testing.T) {
	preparer, root, receipt := hostingRootTestPreparer(t)
	previous := syscall.Umask(0o027)
	defer syscall.Umask(previous)

	block, err := preparer.prepare()
	if err != nil || block != nil {
		t.Fatalf("prepare = %+v, %v", block, err)
	}
	created := []string{
		filepath.Join(root, "var"),
		filepath.Join(root, "var", "www"),
		filepath.Join(root, "var", "www", "celikpanel"),
	}
	for _, dir := range created {
		if mode := statMode(t, dir); mode != 0o755 {
			t.Fatalf("%s mode = %04o, want 0755", dir, mode)
		}
	}
	var recorded hostingRootReceipt
	if err := json.Unmarshal(*receipt, &recorded); err != nil {
		t.Fatalf("receipt: %v (%s)", err, *receipt)
	}
	if recorded.Schema != hostingRootReceiptSchema || len(recorded.Directories) != len(created) {
		t.Fatalf("receipt = %+v", recorded)
	}
	for index, dir := range created {
		entry := recorded.Directories[index]
		if entry.Path != dir || entry.Mode != "0755" || entry.UID != os.Getuid() || entry.GID != os.Getgid() || entry.CreatedAt != "2026-10-01T12:00:00Z" {
			t.Fatalf("receipt entry %d = %+v", index, entry)
		}
	}

	// A second site: nothing is created, the receipt keeps its entries.
	// İkinci site: hiçbir şey oluşturulmaz, makbuz kayıtlarını korur.
	before := string(*receipt)
	if block, err := preparer.prepare(); err != nil || block != nil {
		t.Fatalf("second prepare = %+v, %v", block, err)
	}
	if string(*receipt) != before {
		t.Fatalf("receipt changed without a new directory:\n%s\n%s", before, *receipt)
	}
}

// An existing directory is the owner's: it is never changed and never
// recorded, even when it is not 0755 but still traversable.
func TestHostingRootLeavesExistingTraversableParentsUntouched(t *testing.T) {
	preparer, root, receipt := hostingRootTestPreparer(t)
	www := filepath.Join(root, "var", "www")
	if err := os.MkdirAll(www, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(www, 0o711); err != nil {
		t.Fatal(err)
	}
	if block, err := preparer.prepare(); err != nil || block != nil {
		t.Fatalf("prepare = %+v, %v", block, err)
	}
	if mode := statMode(t, www); mode != 0o711 {
		t.Fatalf("owner directory changed to %04o", mode)
	}
	var recorded hostingRootReceipt
	if err := json.Unmarshal(*receipt, &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded.Directories) != 1 || recorded.Directories[0].Path != preparer.base {
		t.Fatalf("receipt = %+v, want only the hosting base", recorded)
	}
}

// P3 on an existing server: /var/www 0750 from an earlier build (or the
// owner). The site is refused with the directory named; nothing changes.
func TestHostingRootRefusesExistingBlockingParentWithoutChange(t *testing.T) {
	preparer, root, receipt := hostingRootTestPreparer(t)
	www := filepath.Join(root, "var", "www")
	if err := os.MkdirAll(www, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(www, 0o750); err != nil {
		t.Fatal(err)
	}
	var named *hostingpath.TraversalBlock
	preparer.nameOwners = func(block *hostingpath.TraversalBlock) {
		named = block
		block.Owner, block.Group = "root", "celikpanel"
	}
	block, err := preparer.prepare()
	if err != nil || block == nil {
		t.Fatalf("prepare = %+v, %v", block, err)
	}
	if block.Directory != www || block.ModeText() != "0750" || block.Account != "http" || named != block {
		t.Fatalf("block = %+v", block)
	}
	if block.Command() != "sudo chmod 755 "+www || block.OwnerText() != "root:celikpanel" {
		t.Fatalf("command %q owner %q", block.Command(), block.OwnerText())
	}
	if mode := statMode(t, www); mode != 0o750 {
		t.Fatalf("blocking directory changed to %04o", mode)
	}
	if _, err := os.Lstat(preparer.base); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hosting base created under a blocking parent: %v", err)
	}
	if *receipt != nil {
		t.Fatalf("receipt written on refusal: %s", *receipt)
	}
}

func TestCreateSiteRefusesBlockedHostingRootBeforeAnyChange(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	fake := &fakeSiteLifecycle{home: home, failures: map[string]error{}}
	agent := lifecycleTestAgent(t, fake)
	agent.siteOps.prepareHostingRoot = func() (*hostingpath.TraversalBlock, error) {
		fake.calls = append(fake.calls, "prepare-hosting-root")
		return &hostingpath.TraversalBlock{
			Directory: "/var/www", Mode: os.ModeDir | 0o750, Owner: "root", Group: "celikpanel", Account: "http",
		}, nil
	}
	reply := &transport.CreateSiteResponse{}
	if err := agent.CreateSite(req, reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || reply.ErrorCode != transport.HostingRootNotTraversable || reply.HostingRoot == nil {
		t.Fatalf("reply = %#v", reply)
	}
	want := transport.HostingRootBlock{Directory: "/var/www", Mode: "0750", Owner: "root:celikpanel", Account: "http"}
	if *reply.HostingRoot != want {
		t.Fatalf("block = %+v, want %+v", *reply.HostingRoot, want)
	}
	if !strings.Contains(reply.ErrorMessage, "sudo chmod 755 /var/www") || !strings.Contains(reply.ErrorMessage, "before any change") {
		t.Fatalf("message = %q", reply.ErrorMessage)
	}
	for _, call := range fake.calls {
		switch call {
		case "path-exists", "lookup-user", "prepare-hosting-root":
		default:
			t.Fatalf("mutation %q ran after the refusal: %v", call, fake.calls)
		}
	}
}

func TestCreateSiteReportsHostingRootInspectionFailure(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	fake := &fakeSiteLifecycle{home: home, failures: map[string]error{}}
	agent := lifecycleTestAgent(t, fake)
	agent.siteOps.prepareHostingRoot = func() (*hostingpath.TraversalBlock, error) {
		return nil, errors.New("inspect /var: input/output error")
	}
	reply := &transport.CreateSiteResponse{}
	if err := agent.CreateSite(req, reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || reply.ErrorCode != "" || !strings.Contains(reply.ErrorMessage, "hosting root preparation") {
		t.Fatalf("reply = %#v", reply)
	}
	if strings.Contains(strings.Join(fake.calls, ","), "mkdir-all") {
		t.Fatalf("site directories created after a failed proof: %v", fake.calls)
	}
}
