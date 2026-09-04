package main

import "testing"

func journalConfigSnapshot(path string, mode, gid uint32) dnsFileSnapshot {
	snapshot := dnsFileSnapshot{
		Path: path, Exists: true, Mode: mode, OwnerKnown: true, UID: 0, GID: gid,
		Data: []byte("managed\n"),
	}
	snapshot.SHA256 = digestDNSBytes(snapshot.Data)
	return snapshot
}

// The switch journal records the vendor configuration exactly as the package
// ships it: Debian's pair at 0644 root:bind, Arch's single /etc/named.conf at
// 0640 root:named. The first live BIND switch on Arch was refused here with
// "BIND switch journal config snapshot set is incomplete" because the journal
// still demanded 0644 root:root for pacman (register R-018).
// Geçiş günlüğü satıcı yapılandırmasını tam paketin gönderdiği gibi kaydeder:
// Debian'ın çifti 0644 root:bind, Arch'ın tek /etc/named.conf'u 0640
// root:named. Arch'taki ilk canlı BIND geçişi burada "BIND switch journal
// config snapshot set is incomplete" ile reddedildi; çünkü günlük pacman için
// hâlâ 0644 root:root istiyordu (defter R-018).
func TestBINDJournalConfigSetAcceptsEachLayoutsVendorShape(t *testing.T) {
	apt := []dnsFileSnapshot{
		journalConfigSnapshot("/etc/bind/named.conf.local", 0o644, 109),
		journalConfigSnapshot("/etc/bind/named.conf.options", 0o644, 109),
	}
	if !validBINDConfigSnapshotSet(apt) {
		t.Fatal("Debian pair at 0644 root:bind rejected")
	}
	pacman := []dnsFileSnapshot{journalConfigSnapshot("/etc/named.conf", 0o640, 40)}
	if !validBINDConfigSnapshotSet(pacman) {
		t.Fatal("Arch /etc/named.conf at 0640 root:named rejected")
	}
	for name, set := range map[string][]dnsFileSnapshot{
		"pacman world-readable": {journalConfigSnapshot("/etc/named.conf", 0o644, 40)},
		"apt group-only":        {journalConfigSnapshot("/etc/bind/named.conf.local", 0o640, 109), journalConfigSnapshot("/etc/bind/named.conf.options", 0o640, 109)},
		"apt mixed groups":      {journalConfigSnapshot("/etc/bind/named.conf.local", 0o644, 109), journalConfigSnapshot("/etc/bind/named.conf.options", 0o644, 0)},
		"pacman nonroot uid": func() []dnsFileSnapshot {
			s := journalConfigSnapshot("/etc/named.conf", 0o640, 40)
			s.UID = 1
			return []dnsFileSnapshot{s}
		}(),
		"unknown path": {journalConfigSnapshot("/etc/named.conf.d/extra", 0o640, 40)},
		"empty":        {},
	} {
		t.Run(name, func(t *testing.T) {
			if validBINDConfigSnapshotSet(set) {
				t.Fatal("non-vendor config snapshot set was accepted")
			}
		})
	}
}
