//go:build linux

package pdnspeerinspector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/pdnsmanagedconf"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"github.com/alicelik/celikpanel/internal/transport"
)

// debianPackageConfig is /etc/powerdns/pdns.conf as the Debian 13 package
// (PowerDNS 4.9.17) installs it, taken from the batch 6b switch journal's
// config_before (deploy/e2e/dns-kill-matrix/evidence/
// batch6b-pdns-secondary-20260930). The same bytes were measured on the
// Ubuntu-labelled r019 guest. Its only active lines are include-dir, launch=
// and security-poll-suffix=.
func debianPackageConfig(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "debian13-package-pdns.conf"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != "8b46927e48be83b2f0afc38efabdc2e2237daa5281b1a07ea44989ffa41f262a" || len(raw) != 20579 {
		t.Fatal("measured Debian package pdns.conf test data changed")
	}
	return raw
}

const debianPDNSGroup = 104

// managedSecondaryTree renders both drop-ins through the product's own
// renderer, exactly as the Agent writes them on a managed secondary.
func managedSecondaryTree(t *testing.T, p OwnerPolicyV1) configTree {
	t.Helper()
	managed, err := pdnsmanagedconf.Standalone(DatabasePath, []string{"10.0.2.15", p.PeerIP})
	if err != nil {
		t.Fatal(err)
	}
	cluster, err := pdnsmanagedconf.DirectionalCluster(transport.DNSPairRoleSecondary, p.PeerIP, p.PrimaryIP)
	if err != nil {
		t.Fatal(err)
	}
	return configTree{main: debianPackageConfig(t), mainGID: debianPDNSGroup, includes: map[string][]byte{
		pdnsmanagedconf.ManagedFile: managed, pdnsmanagedconf.ClusterFile: []byte(cluster),
	}}
}

func fixtureConfigText(p OwnerPolicyV1) string {
	lines := []string{"launch=gsqlite3", "gsqlite3-database=" + DatabasePath, "local-address=127.0.0.1," + p.PeerIP, "local-port=53", "primary=no", "secondary=yes", "xfr-cycle-interval=1", "autosecondary=no", "allow-axfr-ips=" + p.PrimaryIP + "/32,127.0.0.1/32", "disable-axfr=no", "setuid=powerdns", "setgid=powerdns"}
	return strings.Join(lines, "\n") + "\n"
}

func cloneTree(tree configTree) configTree {
	clone := configTree{main: append([]byte(nil), tree.main...), mainGID: tree.mainGID}
	if tree.includes != nil {
		clone.includes = map[string][]byte{}
		for name, data := range tree.includes {
			clone.includes[name] = append([]byte(nil), data...)
		}
	}
	return clone
}

// editDirective adds (old == ""), changes or removes (replacement == "") the
// first line exactly equal to old; commented copies are left alone.
func editDirective(raw []byte, old, replacement string) []byte {
	if old == "" {
		return append(append([]byte(nil), raw...), []byte(replacement+"\n")...)
	}
	lines := strings.Split(string(raw), "\n")
	for index, line := range lines {
		if line != old {
			continue
		}
		if replacement == "" {
			lines = append(lines[:index], lines[index+1:]...)
		} else {
			lines[index] = replacement
		}
		return []byte(strings.Join(lines, "\n"))
	}
	return append([]byte(nil), raw...)
}

func TestFixtureShapeStillAccepted(t *testing.T) {
	_, p, _ := fixture(t)
	tree := configTree{main: []byte(fixtureConfigText(p))}
	shape, ok := reviewedConfigTree(tree, p)
	if !ok || shape != shapeFixture || !shape.requiresLoopbackListeners() {
		t.Fatalf("fixture shape: %v %t", shape, ok)
	}
	if tree.digest() != sha256.Sum256(tree.main) {
		t.Fatal("fixture configuration digest changed meaning")
	}
	grouped := cloneTree(tree)
	grouped.mainGID = debianPDNSGroup
	if _, ok := reviewedConfigTree(grouped, p); ok {
		t.Fatal("fixture pdns.conf owned by a service group accepted")
	}
	large := cloneTree(tree)
	large.main = append([]byte(strings.Repeat("#\n", 2100)), large.main...)
	if _, ok := reviewedConfigTree(large, p); ok {
		t.Fatal("fixture pdns.conf above its reviewed size accepted")
	}
}

func TestFixtureShapeRefusesOneAddedChangedOrRemovedKey(t *testing.T) {
	_, p, _ := fixture(t)
	raw := []byte(fixtureConfigText(p))
	for _, edit := range [][2]string{
		{"", "loglevel=5"},
		{"", "include-dir=" + pdnsmanagedconf.IncludeDir},
		{"primary=no", "primary=yes"},
		{"allow-axfr-ips=" + p.PrimaryIP + "/32,127.0.0.1/32", "allow-axfr-ips=" + p.PrimaryIP},
		{"setgid=powerdns", ""},
		// PowerDNS would join setgid into the comment above it.
		{"setgid=powerdns", "# comment \\\nsetgid=powerdns"},
	} {
		tree := configTree{main: editDirective(raw, edit[0], edit[1])}
		if _, ok := reviewedConfigTree(tree, p); ok {
			t.Fatalf("fixture edit %q accepted", edit)
		}
	}
}

func TestManagedSecondaryShapeAccepted(t *testing.T) {
	_, p, _ := fixture(t)
	tree := managedSecondaryTree(t, p)
	shape, ok := reviewedConfigTree(tree, p)
	if !ok || shape != shapeManagedSecondary {
		t.Fatalf("managed secondary shape refused: %v %t", shape, ok)
	}
	// The product renders only public listen addresses: no loopback listener
	// is expected. The pair drop-in allows AXFR to the primary alone; that it
	// lacks loopback is irrelevant, because this inspector performs no AXFR.
	if shape.requiresLoopbackListeners() {
		t.Fatal("managed secondary required loopback listeners it does not configure")
	}
	cluster, _ := parseStrictDirectives(string(tree.includes[pdnsmanagedconf.ClusterFile]))
	if cluster["allow-axfr-ips"] != p.PrimaryIP || strings.Contains(string(tree.includes[pdnsmanagedconf.ClusterFile]), "127.0.0.1") {
		t.Fatalf("test premise: managed pair drop-in names loopback: %q", cluster["allow-axfr-ips"])
	}
	if tree.digest() == sha256.Sum256(tree.main) {
		t.Fatal("managed digest does not bind the drop-ins")
	}
	// The earlier product rendering (autosecondary=yes) is the product's own.
	earlier := cloneTree(tree)
	autosecondary, err := pdnsmanagedconf.DirectionalClusterAutosecondary(transport.DNSPairRoleSecondary, p.PeerIP, p.PrimaryIP)
	if err != nil {
		t.Fatal(err)
	}
	earlier.includes[pdnsmanagedconf.ClusterFile] = []byte(autosecondary)
	if shape, ok := reviewedConfigTree(earlier, p); !ok || shape != shapeManagedSecondary {
		t.Fatal("earlier product rendering of the pair drop-in refused")
	}
	// A root-owned main file, the owner's comments, and a main file to which
	// the Agent only appended include-dir are all the same shape.
	for _, main := range [][]byte{
		[]byte("include-dir=" + pdnsmanagedconf.IncludeDir + "\n"),
		[]byte("# owner note\n\n# Managed by CelikPanel.\ninclude-dir=" + pdnsmanagedconf.IncludeDir + "\n"),
		append(debianPackageConfig(t), []byte("# owner comment\n")...),
	} {
		variant := cloneTree(tree)
		variant.main, variant.mainGID = main, 0
		if _, ok := reviewedConfigTree(variant, p); !ok {
			t.Fatalf("managed main accepted by the product refused: %q", main)
		}
	}
	// A single public address equal to the peer is the product's rendering too.
	single := cloneTree(tree)
	rendered, err := pdnsmanagedconf.Standalone(DatabasePath, []string{p.PeerIP})
	if err != nil {
		t.Fatal(err)
	}
	single.includes[pdnsmanagedconf.ManagedFile] = rendered
	if _, ok := reviewedConfigTree(single, p); !ok {
		t.Fatal("single-address managed drop-in refused")
	}
}

func TestManagedSecondaryShapeRefusesOneAddedChangedOrRemovedKey(t *testing.T) {
	_, p, _ := fixture(t)
	base := managedSecondaryTree(t, p)
	type edit struct {
		file        string // "" is the main file
		old, change string
	}
	for _, e := range []edit{
		// main pdns.conf
		{"", "", "loglevel=5"},
		{"", "", "local-address=127.0.0.1"},
		{"", "", "include-dir=/etc/powerdns/other.d"},
		{"", "launch=", "launch=bind"},
		{"", "security-poll-suffix=", "security-poll-suffix=secpoll.powerdns.com."},
		{"", "include-dir=" + pdnsmanagedconf.IncludeDir, "include-dir=/etc/powerdns/other.d"},
		{"", "include-dir=" + pdnsmanagedconf.IncludeDir, ""},
		{"", "launch=", "# joined \\"},
		// backend drop-in
		{pdnsmanagedconf.ManagedFile, "", "api-key=x"},
		{pdnsmanagedconf.ManagedFile, "gsqlite3-dnssec=yes", "gsqlite3-dnssec=no"},
		{pdnsmanagedconf.ManagedFile, "gsqlite3-database=" + DatabasePath, "gsqlite3-database=/tmp/pdns.sqlite3"},
		{pdnsmanagedconf.ManagedFile, "local-address=10.0.2.15," + p.PeerIP, "local-address=10.0.2.15"},
		{pdnsmanagedconf.ManagedFile, "local-address=10.0.2.15," + p.PeerIP, "local-address=127.0.0.1," + p.PeerIP},
		{pdnsmanagedconf.ManagedFile, "webserver=no", ""},
		{pdnsmanagedconf.ManagedFile, "api=no", " api=no"},
		// pair drop-in
		{pdnsmanagedconf.ClusterFile, "", "also-notify=" + p.PrimaryIP},
		{pdnsmanagedconf.ClusterFile, "", "autoprimary=yes"},
		{pdnsmanagedconf.ClusterFile, "allow-axfr-ips=" + p.PrimaryIP, "allow-axfr-ips=" + p.PrimaryIP + ",127.0.0.1"},
		{pdnsmanagedconf.ClusterFile, "allow-axfr-ips=" + p.PrimaryIP, "allow-axfr-ips=192.0.2.99"},
		{pdnsmanagedconf.ClusterFile, "primary=yes", "primary=no"},
		{pdnsmanagedconf.ClusterFile, "secondary=yes", ""},
	} {
		tree := cloneTree(base)
		if e.file == "" {
			before := tree.main
			tree.main = editDirective(tree.main, e.old, e.change)
			if bytes.Equal(before, tree.main) {
				t.Fatalf("test edit %+v did not apply", e)
			}
		} else {
			before := tree.includes[e.file]
			tree.includes[e.file] = editDirective(before, e.old, e.change)
			if bytes.Equal(before, tree.includes[e.file]) {
				t.Fatalf("test edit %+v did not apply", e)
			}
		}
		if _, ok := reviewedConfigTree(tree, p); ok {
			t.Fatalf("managed edit %+v accepted", e)
		}
	}
	// Missing, extra and unreviewed drop-ins.
	for _, mutate := range []func(configTree){
		func(tree configTree) { delete(tree.includes, pdnsmanagedconf.ClusterFile) },
		func(tree configTree) { delete(tree.includes, pdnsmanagedconf.ManagedFile) },
		func(tree configTree) { tree.includes["zz-owner.conf"] = []byte("loglevel=5\n") },
	} {
		tree := cloneTree(base)
		mutate(tree)
		if _, ok := reviewedConfigTree(tree, p); ok {
			t.Fatalf("managed drop-in set accepted: %v", tree.includes)
		}
	}
	// The undirected (legacy) pair rendering carries no catalog and is not a
	// rendering of this secondary's drop-in; the primary-role rendering is not
	// a secondary's either.
	legacy := cloneTree(base)
	legacy.includes[pdnsmanagedconf.ClusterFile] = []byte("primary=yes\nsecondary=yes\nallow-axfr-ips=" + p.PrimaryIP + "\nalso-notify=" + p.PrimaryIP + "\n")
	primaryRole, err := pdnsmanagedconf.DirectionalCluster(transport.DNSPairRolePrimary, p.PeerIP, p.PrimaryIP)
	if err != nil {
		t.Fatal(err)
	}
	primary := cloneTree(base)
	primary.includes[pdnsmanagedconf.ClusterFile] = []byte(primaryRole)
	for _, tree := range []configTree{legacy, primary} {
		if _, ok := reviewedConfigTree(tree, p); ok {
			t.Fatalf("pair drop-in accepted: %s", tree.includes[pdnsmanagedconf.ClusterFile])
		}
	}
	// A managed tree for another peer is not this peer's.
	other := p
	other.PeerIP = "192.0.2.12"
	if _, ok := reviewedConfigTree(base, other); ok {
		t.Fatal("managed secondary of another peer accepted")
	}
}

func writeConfigTree(t *testing.T, tree configTree, mainMode os.FileMode) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "etc", "powerdns")
	if err := os.MkdirAll(filepath.Join(dir, "pdns.d"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "etc"), dir, filepath.Join(dir, "pdns.d")} {
		if err := os.Chmod(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "pdns.conf")
	if err := os.WriteFile(main, tree.main, mainMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(main, 0, int(tree.mainGID)); err != nil {
		t.Fatal(err)
	}
	for name, data := range tree.includes {
		if err := os.WriteFile(filepath.Join(dir, "pdns.d", name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestManagedSecondaryFilesReadThroughDescriptors(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-only native config")
	}
	_, p, _ := fixture(t)
	want := managedSecondaryTree(t, p)
	root := writeConfigTree(t, want, 0640)
	includeDir := filepath.Join(root, "etc", "powerdns", "pdns.d")
	// PowerDNS loads neither hidden entries nor names without ".conf".
	for _, name := range []string{".staged.conf", "celikpanel.conf.tmp", "README"} {
		if err := os.WriteFile(filepath.Join(includeDir, name), []byte("loglevel=9\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	shape, digest, err := readReviewedConfigurationAt(root, debianPDNSGroup, p)
	if err != nil || shape != shapeManagedSecondary || digest != want.digest() {
		t.Fatalf("managed secondary on disk: %v %x %v", shape, digest, err)
	}
	// The package's group ownership is accepted only for the daemon's group.
	if _, _, err := readReviewedConfigurationAt(root, 0, p); Reason(err) != transport.DNSPeerInspectorReasonConfigUnreviewed {
		t.Fatalf("main file of a group other than the daemon's accepted: %v", err)
	}
	for name, mutate := range map[string]func() error{
		"owner drop-in": func() error {
			return os.WriteFile(filepath.Join(includeDir, "zz-owner.conf"), []byte("loglevel=5\n"), 0644)
		},
		"group-writable drop-in": func() error {
			return os.Chmod(filepath.Join(includeDir, pdnsmanagedconf.ClusterFile), 0664)
		},
		"symlinked drop-in": func() error {
			path := filepath.Join(includeDir, pdnsmanagedconf.ManagedFile)
			if err := os.Rename(path, path+".real"); err != nil {
				return err
			}
			return os.Symlink(path+".real", path)
		},
		"group-writable include-dir": func() error { return os.Chmod(includeDir, 0775) },
		"edited drop-in": func() error {
			return os.WriteFile(filepath.Join(includeDir, pdnsmanagedconf.ClusterFile), []byte("primary=yes\nsecondary=yes\nallow-axfr-ips=192.0.2.99\n"), 0644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := writeConfigTree(t, want, 0640)
			includeDir = filepath.Join(root, "etc", "powerdns", "pdns.d")
			if err := mutate(); err != nil {
				t.Fatal(err)
			}
			_, _, err := readReviewedConfigurationAt(root, debianPDNSGroup, p)
			if err == nil || Reason(err) != transport.DNSPeerInspectorReasonConfigUnreviewed {
				t.Fatalf("%s: %v (reason %q)", name, err, Reason(err))
			}
		})
	}
}

func TestFixtureFileStillReadWithoutIncludeDir(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-only native config")
	}
	_, p, _ := fixture(t)
	tree := configTree{main: []byte(fixtureConfigText(p))}
	root := writeConfigTree(t, tree, 0644)
	// An include directory the fixture does not load is not read at all.
	if err := os.WriteFile(filepath.Join(root, "etc", "powerdns", "pdns.d", "zz-owner.conf"), []byte("loglevel=5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	shape, digest, err := readReviewedConfigurationAt(root, debianPDNSGroup, p)
	if err != nil || shape != shapeFixture || digest != sha256.Sum256(tree.main) {
		t.Fatalf("fixture on disk: %v %v", shape, err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "powerdns", "pdns.conf"), []byte(fixtureConfigText(p)+"loglevel=5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readReviewedConfigurationAt(root, debianPDNSGroup, p); Reason(err) != transport.DNSPeerInspectorReasonConfigUnreviewed {
		t.Fatalf("unreviewed fixture edit: %v", err)
	}
	if _, _, err := readReviewedConfigurationAt(filepath.Join(root, "missing"), 0, p); Reason(err) != transport.DNSPeerInspectorReasonConfigUnreviewed {
		t.Fatalf("missing configuration: %v", err)
	}
}

func TestProcessGIDReadsAgreeingGroups(t *testing.T) {
	gid, ok := processGID(uint64(os.Getpid()))
	if os.Getgid() == os.Getegid() && (!ok || int(gid) != os.Getgid()) {
		t.Fatalf("own process group: %d %t", gid, ok)
	}
	if _, ok := processGID(0); ok {
		t.Fatal("PID 0 has a group")
	}
}

type failingReader struct{ err error }

func (f failingReader) Read(context.Context, pdnspeerproof.RequestV1, OwnerPolicyV1) (Snapshot, error) {
	return Snapshot{}, f.err
}

func TestServeBindsTheConfigReasonToTheRequestDigest(t *testing.T) {
	r, p, _ := fixture(t)
	raw, err := pdnspeerproof.EncodeRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := pdnspeerproof.RequestSHA256(r)
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Unix(1800000003, 0) }
	var out bytes.Buffer
	err = Serve(context.Background(), bytes.NewReader(append(raw, '\n')), &out, &testPolicy{p: p, hash: "policy"},
		failingReader{err: reasonError(transport.DNSPeerInspectorReasonConfigUnreviewed, "local text /etc/powerdns")}, now)
	line, ok := ReasonLine(err)
	if !ok || line != dnspeerproof.InspectorReasonPrefixV1+" "+digest+" config_unreviewed" || out.Len() != 0 {
		t.Fatalf("reason line: %q %t %v", line, ok, err)
	}
	if dnspeerproof.ParseInspectorReason([]byte(line+"\n"), digest) != "config_unreviewed" {
		t.Fatal("the primary's parser does not accept the line")
	}
	// An unclassified failure, and a failure before a request is decoded,
	// never produce a reason line.
	err = Serve(context.Background(), bytes.NewReader(append(raw, '\n')), &out, &testPolicy{p: p, hash: "policy"},
		failingReader{err: errors.New("PowerDNS listeners do not match service")}, now)
	if _, ok := ReasonLine(err); ok || err == nil {
		t.Fatalf("unclassified failure produced a reason: %v", err)
	}
	err = Serve(context.Background(), strings.NewReader("{}"), &out, &testPolicy{p: p, hash: "policy"},
		failingReader{err: reasonError(transport.DNSPeerInspectorReasonConfigUnreviewed, "x")}, now)
	if _, ok := ReasonLine(err); ok || err == nil {
		t.Fatalf("undecoded request produced a reason: %v", err)
	}
}
