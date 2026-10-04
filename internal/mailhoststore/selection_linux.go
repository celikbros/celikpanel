//go:build linux

package mailhoststore

import (
	"encoding/json"
	"errors"
	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"golang.org/x/sys/unix"
)

// CurrentObservation is read-only evidence, never renewal authority. Its v1
// identity binds the selected link, generation, receipt and four material inodes.
// It contains no certificate private bytes. Recovery must compare a before-image
// written before admission; it must not reconstruct one after interruption.
type CurrentObservation struct {
	Version        string
	Receipt        mailhostartifact.Receipt
	IdentitySHA256 string
}

type selectionNodeV1 struct {
	Name      string `json:"name"`
	Dev       uint64 `json:"dev"`
	Ino       uint64 `json:"ino"`
	Mode      uint32 `json:"mode"`
	UID       uint32 `json:"uid"`
	GID       uint32 `json:"gid"`
	Links     uint64 `json:"links"`
	Size      int64  `json:"size"`
	MtimeSec  int64  `json:"mtime_sec"`
	MtimeNsec int64  `json:"mtime_nsec"`
	CtimeSec  int64  `json:"ctime_sec"`
	CtimeNsec int64  `json:"ctime_nsec"`
}

func selectionNode(name string, s unix.Stat_t) selectionNodeV1 {
	return selectionNodeV1{name, uint64(s.Dev), s.Ino, s.Mode, s.Uid, s.Gid, uint64(s.Nlink), s.Size, s.Mtim.Sec, s.Mtim.Nsec, s.Ctim.Sec, s.Ctim.Nsec}
}

// ObserveCurrentAt requires a verified current certificate and the caller's
// native publication lock and pinned trusted parent. Absence remains an error:
// unattended renewal cannot acquire an initial mail identity from missing proof.
func ObserveCurrentAt(dirFD int, verify PairVerifier) (CurrentObservation, error) {
	version, found, err := readCurrentVersionAt(dirFD)
	if err != nil {
		return CurrentObservation{}, err
	}
	if !found {
		return CurrentObservation{}, errors.New("selected mail certificate is absent; no renewal before-image is established")
	}
	versionFD, err := openDirectoryAt(dirFD, version)
	if err != nil {
		return CurrentObservation{}, err
	}
	defer unix.Close(versionFD)
	observe := func() ([]selectionNodeV1, error) {
		var parent, opened, named unix.Stat_t
		if unix.Fstat(dirFD, &parent) != nil || unix.Fstat(versionFD, &opened) != nil || unix.Fstatat(dirFD, version, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameEvidenceStat(opened, named) {
			return nil, errors.New("selected mail generation identity changed")
		}
		// Sibling staging legitimately changes parent timestamps/link count. Retain
		// its ownership/identity, while all selected entries retain complete metadata.
		parent.Size = 0
		parent.Nlink = 0
		parent.Mtim = unix.Timespec{}
		parent.Ctim = unix.Timespec{}
		nodes := []selectionNodeV1{selectionNode("parent", parent), selectionNode("generation", opened)}
		for _, entry := range []struct {
			fd   int
			name string
		}{{dirFD, "current"}, {versionFD, mailhostartifact.ReceiptName}, {versionFD, "mail.domain"}, {versionFD, "fullchain.pem"}, {versionFD, "privkey.pem"}} {
			var st unix.Stat_t
			if unix.Fstatat(entry.fd, entry.name, &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
				return nil, errors.New("selected mail material identity unavailable")
			}
			nodes = append(nodes, selectionNode(entry.name, st))
		}
		return nodes, nil
	}
	before, err := observe()
	if err != nil {
		return CurrentObservation{}, err
	}
	got, receipt, _, _, present, err := ReadCurrentAt(dirFD, verify)
	if err != nil {
		return CurrentObservation{}, err
	}
	if !present || got != version {
		return CurrentObservation{}, errors.New("selected mail certificate changed during observation")
	}
	after, err := observe()
	if err != nil {
		return CurrentObservation{}, err
	}
	if len(before) != len(after) {
		return CurrentObservation{}, errors.New("selected mail inventory changed")
	}
	for i := range before {
		if before[i] != after[i] {
			return CurrentObservation{}, errors.New("selected mail material changed during observation")
		}
	}
	payload, err := json.Marshal(struct {
		Schema  string                   `json:"schema"`
		Version string                   `json:"version"`
		Receipt mailhostartifact.Receipt `json:"receipt"`
		Nodes   []selectionNodeV1        `json:"nodes"`
	}{"mail-host-selection-identity/v1", version, receipt, after})
	if err != nil {
		return CurrentObservation{}, err
	}
	return CurrentObservation{Version: version, Receipt: receipt, IdentitySHA256: mailhostartifact.LeafSHA256(payload)}, nil
}
