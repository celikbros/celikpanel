//go:build linux

package pdnspeerinspector

import (
	"bytes"
	"context"
	"database/sql"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	_ "modernc.org/sqlite"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeParsersFailClosed(t *testing.T) {
	if pid, err := parseServicePID("ActiveState=active\nMainPID=17\n"); err != nil || pid != 17 {
		t.Fatal(pid, err)
	}
	for _, raw := range []string{"ActiveState=inactive\nMainPID=17", "ActiveState=active\nMainPID=0", "ActiveState=active\nMainPID=17\nMainPID=17"} {
		if _, err := parseServicePID(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if !reviewedInvocation("/usr/bin/pdns_server", []byte("/usr/bin/pdns_server\x00--daemon=no\x00--guardian=no\x00")) {
		t.Fatal("reviewed invocation rejected")
	}
	if reviewedInvocation("/usr/bin/pdns_server", []byte("/usr/bin/pdns_server\x00--config-dir=/tmp\x00")) {
		t.Fatal("alternate config admitted")
	}
	zones, err := parseLiveZones("catalog-c000020a.celikpanel.invalid.\nother.test.\nAll zonecount: 2\n")
	if err != nil || len(zones) != 2 {
		t.Fatal(zones, err)
	}
	for _, raw := range []string{"catalog-c000020a.celikpanel.invalid.\ncatalog-c000020a.celikpanel.invalid.\nAll zonecount: 2", "All zones: none", "catalog-c000020a.celikpanel.invalid.\nAll zonecount: 2", "catalog-c000020a.celikpanel.invalid.\nnot a zone\nAll zonecount: 2"} {
		if _, err := parseLiveZones(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
func TestOnlyReviewedFixtureConfiguration(t *testing.T) {
	_, p, _ := fixture(t)
	lines := []string{"launch=gsqlite3", "gsqlite3-database=" + DatabasePath, "local-address=127.0.0.1," + p.PeerIP, "local-port=53", "primary=no", "secondary=yes", "xfr-cycle-interval=1", "autosecondary=no", "allow-axfr-ips=" + p.PrimaryIP + "/32,127.0.0.1/32", "disable-axfr=no", "setuid=powerdns", "setgid=powerdns"}
	raw := strings.Join(lines, "\n") + "\n"
	if !reviewedConfig(raw, p) {
		t.Fatal("reviewed config rejected")
	}
	for _, extra := range []string{"include-dir=/tmp", "launch=gsqlite3,bind", "disable-axfr=yes", "socket-dir=/tmp"} {
		if reviewedConfig(raw+extra+"\n", p) {
			t.Fatalf("unreviewed option admitted: %s", extra)
		}
	}
}
func TestExactCatalogDatabaseAndMemberAbsence(t *testing.T) {
	r, p, _ := fixture(t)
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{"CREATE TABLE domains(id INTEGER PRIMARY KEY,name TEXT,type TEXT,master TEXT,account TEXT,catalog TEXT)", "CREATE TABLE records(domain_id INTEGER,name TEXT,type TEXT,content TEXT)", "INSERT INTO domains VALUES(1,'catalog-c000020a.celikpanel.invalid','CONSUMER','192.0.2.10','fixture-pdns-peer',NULL)", "INSERT INTO records VALUES(1,'catalog-c000020a.celikpanel.invalid','SOA','invalid invalid 2 60 30 3600 30')", "INSERT INTO records VALUES(1,'catalog-c000020a.celikpanel.invalid','NS','invalid')", "INSERT INTO records VALUES(1,'version.catalog-c000020a.celikpanel.invalid','TXT','\"2\"')"}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	st, _ := os.Stat(path)
	info := st.Sys().(*syscall.Stat_t)
	serial, members, present, err := readCatalogDatabase(context.Background(), path, uint64(info.Dev), info.Ino, r, p)
	if err != nil || serial != 2 || len(members) != 0 || present {
		t.Fatalf("absent: %d %v %t %v", serial, members, present, err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO domains VALUES(2,'s1-kill.test','SLAVE','192.0.2.10',NULL,'catalog-c000020a.celikpanel.invalid')")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	_, _, present, err = readCatalogDatabase(context.Background(), path, uint64(info.Dev), info.Ino, r, p)
	if err != nil || !present {
		t.Fatalf("loaded DB row not seen: %v %v", present, err)
	}
	if _, _, _, err = readCatalogDatabase(context.Background(), path, uint64(info.Dev), info.Ino+1, r, p); err == nil {
		t.Fatal("mismatched daemon inode accepted")
	}
}

var _ pdnspeerproof.RequestV1

func TestTrustedConfigDescriptorRejectsSwapAndOwnerEdit(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-only native config")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "etc", "powerdns")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pdns.conf")
	original := []byte("launch=gsqlite3\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := readTrustedConfigAt(root, nil)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("stable config rejected: %q %v", got, err)
	}
	_, err = readTrustedConfigAt(root, func() {
		if e := os.Rename(path, path+".old"); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, original, 0644); e != nil {
			t.Fatal(e)
		}
	})
	if err == nil {
		t.Fatal("renamed-and-restored owner config accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".old", path); err != nil {
		t.Fatal(err)
	}
	_, err = readTrustedConfigAt(root, func() {
		if e := os.WriteFile(path, []byte("launch=other\n"), 0644); e != nil {
			t.Fatal(e)
		}
	})
	if err == nil {
		t.Fatal("in-place owner edit accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".old", path); err != nil {
		t.Fatal(err)
	}
	if _, err := readTrustedConfigAt(root, nil); err == nil {
		t.Fatal("symlink config accepted")
	}
}

func TestAuthenticatedControlAndDatabaseMustBothShowAbsence(t *testing.T) {
	if got := classifyAuthenticatedControlZone(false, false); got != "unloaded" {
		t.Fatalf("native absence rejected: %s", got)
	}
	for _, state := range [][2]bool{{true, false}, {false, true}, {true, true}} {
		if got := classifyAuthenticatedControlZone(state[0], state[1]); got != "loaded" {
			t.Fatalf("positive presence ignored: %s", got)
		}
	}
}

func TestControlListenerInodeIgnoresAcceptedSamePathSocket(t *testing.T) {
	path := "/run/pdns/pdns.controlsocket"
	listener := "00000000f99547b5: 00000002 00000000 00010000 0001 01 7136 " + path
	accepted := "000000009f034f1b: 00000003 00000000 00000000 0001 03 11124 " + path
	inode, err := listeningUnixSocketInode(listener+"\n"+accepted+"\n", path)
	if err != nil || inode != "7136" {
		t.Fatalf("listener not separated from accepted connection: %q %v", inode, err)
	}
	for _, raw := range []string{accepted, listener + "\n" + listener, listener + "\n" + strings.Replace(accepted, "00000000 0001 03", "00020000 0001 03", 1)} {
		if _, err := listeningUnixSocketInode(raw, path); err == nil {
			t.Fatalf("accepted ambiguous or unknown control socket rows: %q", raw)
		}
	}
}

func TestControlPeerPIDMismatchSendsNoCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.controlsocket")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	gotRequest := make(chan bool, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			gotRequest <- false
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var buf [32]byte
		n, _ := conn.Read(buf[:])
		gotRequest <- n != 0
	}()
	_, err = readLiveZonesAuthenticated(context.Background(), path, "bogus", uint64(os.Getpid()+1), 1, "/usr/bin/pdns_server")
	if err == nil || !strings.Contains(err.Error(), "peer PID differs") {
		t.Fatalf("foreign control peer accepted: %v", err)
	}
	select {
	case sent := <-gotRequest:
		if sent {
			t.Fatal("sent control command to unverified peer")
		}
	case <-time.After(time.Second):
		t.Fatal("foreign peer connection did not close")
	}
}

func TestControlSocketPathSwapChangesListenerIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.controlsocket")
	first, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	inode, err := ownedControlSocketInode(uint64(os.Getpid()), path)
	if err != nil {
		t.Fatal(err)
	}
	if !controlSocketMatches(uint64(os.Getpid()), path, inode) {
		t.Fatal("stable listener rejected")
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	second, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if controlSocketMatches(uint64(os.Getpid()), path, inode) {
		t.Fatal("swapped listener accepted")
	}
}
