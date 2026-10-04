//go:build linux

package recoverypublication

import (
	"bytes"
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

// A v2 no-op publication records the exact existing tree without replacing it.
// The empty Stage and identical Before/After are admitted only under material v2.
// Interrupted receipt publication resumes the same proof; recovery never invents
// this intent from a semantically matching live resource.
func (v *inputs) finishNoopPublication(journal *os.File, raw []byte, record intent, checkJournal func() error) error {
	current, err := openAt(v.prefix, v.request.Resource, true)
	if err != nil {
		return ErrOwnerChanged
	}
	defer current.Close()
	verify := func() error {
		if err := v.revalidate(); err != nil {
			return err
		}
		if err := checkJournal(); err != nil {
			return err
		}
		if err := validateJournalObjects(journal); err != nil {
			return err
		}
		recorded, err := readPrivateFile(journal, "intent.json", maxIntent)
		if err != nil || !bytes.Equal(recorded, raw) {
			return ErrOwnerChanged
		}
		actual, err := scan(current)
		if err != nil || !actual.equal(record.After) || !samePath(v.prefix, v.request.Resource, current) {
			return ErrOwnerChanged
		}
		return nil
	}
	if err = verify(); err != nil {
		return err
	}
	expected := []byte("format=celikpanel-resource-publication-v1\nintent=" + digest(raw) + "\n")
	receipt, err := readPrivateFile(journal, "published", 4096)
	if errors.Is(err, unix.ENOENT) {
		name, err := randomName(".published-")
		if err != nil {
			return err
		}
		if err = writeNew(journal, name, expected); err != nil {
			return err
		}
		v.c.point("receipt_staged")
		if err = verify(); err != nil {
			return err
		}
		if unix.Renameat2(int(journal.Fd()), name, int(journal.Fd()), "published", unix.RENAME_NOREPLACE) != nil {
			return ErrUnavailable
		}
		v.c.point("receipt_published")
	} else if err != nil || !bytes.Equal(receipt, expected) {
		return ErrOwnerChanged
	}
	if unix.Fsync(int(journal.Fd())) != nil {
		return ErrUnavailable
	}
	v.c.point("noop_verified")
	if err = verify(); err != nil {
		return err
	}
	receipt, err = readPrivateFile(journal, "published", 4096)
	if err != nil || !bytes.Equal(receipt, expected) {
		return ErrOwnerChanged
	}
	return nil
}
