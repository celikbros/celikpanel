//go:build linux

package mailhoststore

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"golang.org/x/sys/unix"
)

const versionPrefix = ".panel-cert-"

// Stage is a prepared immutable generation. The caller holds publication
// exclusion through Close and supplies separate durable mutation authority.
// This API neither grants that authority nor proves service convergence.
// StageMaterialAt duplicates the trusted parent descriptor; caller retains its FD.
type Stage struct {
	publishAction func() (bool, error)
	cleanupAction func(bool) error
	published     bool
	closed        bool
}

// Publish returns whether the atomic link changed, even if its subsequent fsync
// failed. The caller must retain/reconcile the exact operation in that case.
func (stage *Stage) Publish() (bool, error) {
	if stage == nil || stage.closed || stage.publishAction == nil {
		return false, errors.New("invalid mail host certificate stage")
	}
	if stage.published {
		return true, errors.New("mail host certificate stage already published")
	}
	published, err := stage.publishAction()
	stage.published = published
	return published, err
}

// Close preserves a published/current generation and removes only a matching
// unpublished receipt. It is idempotent and does not roll back service state.
func (stage *Stage) Close() error {
	if stage == nil || stage.closed {
		return nil
	}
	stage.closed = true
	if stage.cleanupAction == nil {
		return errors.New("invalid mail host certificate stage cleanup")
	}
	return stage.cleanupAction(stage.published)
}

// ValidateMaterial checks only the staged pair/receipt agreement. The caller
// still verifies current trust and lifetime before authorizing publication.
func ValidateMaterial(certificate, privateKey []byte, receipt mailhostartifact.Receipt) ([]byte, error) {
	if len(certificate) == 0 || len(privateKey) == 0 || len(certificate) > PEMMaxSize || len(privateKey) > PEMMaxSize {
		return nil, errors.New("mail host certificate material exceeds its bounds")
	}
	domain := receipt.Domain
	if err := mailhostartifact.ValidateReceipt(receipt); err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(certificate, privateKey)
	if err != nil {
		return nil, fmt.Errorf("validate staged mail host certificate pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("validate staged mail host certificate pair: chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse staged mail host certificate leaf: %w", err)
	}
	if err := leaf.VerifyHostname(domain); err != nil {
		return nil, fmt.Errorf("validate staged mail host certificate identity: %w", err)
	}
	if mailhostartifact.LeafSHA256(pair.Certificate[0]) != receipt.LeafSHA256 {
		return nil, errors.New("staged mail host certificate leaf does not match receipt")
	}
	receiptRaw, err := mailhostartifact.CanonicalReceipt(receipt)
	if err != nil {
		return nil, err
	}
	return receiptRaw, nil
}

func StageMaterialAt(parentFD int, certificate, privateKey []byte, receipt mailhostartifact.Receipt) (*Stage, error) {
	receiptRaw, err := ValidateMaterial(certificate, privateKey, receipt)
	if err != nil {
		return nil, err
	}
	domain := receipt.Domain
	certificate = bytes.Clone(certificate)
	privateKey = bytes.Clone(privateKey)
	var parent unix.Stat_t
	if err := unix.Fstat(parentFD, &parent); err != nil || parent.Mode&unix.S_IFMT != unix.S_IFDIR || parent.Uid != 0 || parent.Gid != 0 || parent.Mode&0022 != 0 {
		return nil, errors.New("mail host publication requires a trusted root-owned directory")
	}
	dirFD, err := unix.FcntlInt(uintptr(parentFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	panelGID := 0
	initial, initialFound, err := readCurrentVersionAt(dirFD)
	if err != nil {
		unix.Close(dirFD)
		return nil, err
	}
	var initialStat unix.Stat_t
	if initialFound {
		if err = unix.Fstatat(dirFD, "current", &initialStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			unix.Close(dirFD)
			return nil, err
		}
	}
	versionName, err := randomEntry(versionPrefix)
	if err != nil {
		unix.Close(dirFD)
		return nil, err
	}
	if err := unix.Mkdirat(dirFD, versionName, 0o750); err != nil {
		unix.Close(dirFD)
		return nil, fmt.Errorf("create certificate issue version directory: %w", err)
	}
	versionFD, err := openDirectoryAt(dirFD, versionName)
	if err != nil {
		_ = unix.Unlinkat(dirFD, versionName, unix.AT_REMOVEDIR)
		unix.Close(dirFD)
		return nil, fmt.Errorf("open certificate issue version directory: %w", err)
	}
	staged := false
	defer func() {
		unix.Close(versionFD)
		if !staged {
			_ = removeVersionFilesAt(dirFD, versionName)
			unix.Close(dirFD)
		}
	}()
	if err := unix.Fchown(versionFD, 0, panelGID); err != nil {
		return nil, fmt.Errorf("own certificate issue version directory: %w", err)
	}
	if err := unix.Fchmod(versionFD, 0o750); err != nil {
		return nil, fmt.Errorf("protect certificate issue version directory: %w", err)
	}
	for _, file := range []struct {
		name    string
		gid     int
		mode    uint32
		content []byte
	}{
		{name: "fullchain.pem", gid: panelGID, mode: 0o600, content: certificate},
		{name: "privkey.pem", gid: panelGID, mode: 0o600, content: privateKey},
		{name: "mail.domain", gid: panelGID, mode: 0o600, content: []byte(domain + "\n")},
		{name: mailhostartifact.ReceiptName, gid: 0, mode: 0o600, content: receiptRaw},
	} {
		if err := writeVersionFile(
			versionFD, file.name, 0, file.gid, file.mode, file.content,
		); err != nil {
			return nil, err
		}
	}
	if err := unix.Fsync(versionFD); err != nil {
		return nil, fmt.Errorf("sync certificate issue version directory: %w", err)
	}
	if err := unix.Fsync(dirFD); err != nil {
		return nil, fmt.Errorf("sync staged mail host certificate directory: %w", err)
	}
	var versionStat unix.Stat_t
	if err := unix.Fstat(versionFD, &versionStat); err != nil {
		return nil, err
	}
	checkIdentity := func() error {
		var now, parentNow unix.Stat_t
		if unix.Fstatat(dirFD, versionName, &now, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameEvidenceStat(versionStat, now) || unix.Fstat(dirFD, &parentNow) != nil || parentNow.Uid != parent.Uid || parentNow.Gid != parent.Gid || parentNow.Mode != parent.Mode {
			return errors.New("staged mail host certificate authority changed; owner review required")
		}
		return nil
	}
	staged = true
	stage := &Stage{}
	stage.publishAction = func() (bool, error) {
		if err := checkIdentity(); err != nil {
			return false, err
		}
		if err := unchangedMaterialAt(dirFD, versionName, certificate, privateKey, receiptRaw, domain); err != nil {
			return false, err
		}
		selected, found, err := readCurrentVersionAt(dirFD)
		if err != nil {
			return false, err
		}
		var currentStat unix.Stat_t
		if found != initialFound || selected != initial || (found && (unix.Fstatat(dirFD, "current", &currentStat, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameEvidenceStat(initialStat, currentStat))) {
			return false, errors.New("mail host certificate selection changed after staging; owner review required")
		}
		return activateVersionAt(dirFD, versionName)
	}
	stage.cleanupAction = func(published bool) error {
		defer unix.Close(dirFD)
		if published {
			return nil
		}
		current, found, err := readCurrentVersionAt(dirFD)
		if err != nil {
			return err
		}
		if found && current == versionName {
			return nil
		}
		if err := checkIdentity(); err != nil {
			return err
		}
		if err := unchangedMaterialAt(dirFD, versionName, certificate, privateKey, receiptRaw, domain); err != nil {
			return err
		}
		if err := removeExactVersionAt(
			dirFD, versionName, receipt,
		); err != nil {
			return err
		}
		return unix.Fsync(dirFD)
	}
	return stage, nil
}

func activateVersionAt(
	dirFD int,
	versionName string,
) (published bool, err error) {
	if !validVersionName(versionName) {
		return false, errors.New("invalid staged mail host certificate version")
	}
	linkName, err := randomEntry(".current-")
	if err != nil {
		return false, err
	}
	if err := unix.Symlinkat(versionName, dirFD, linkName); err != nil {
		return false, fmt.Errorf("create staged mail host certificate link: %w", err)
	}
	linkPublished := false
	defer func() {
		if !linkPublished {
			_ = unix.Unlinkat(dirFD, linkName, 0)
		}
	}()
	if err := unix.Renameat(dirFD, linkName, dirFD, "current"); err != nil {
		return false, fmt.Errorf("activate mail host certificate atomically: %w", err)
	}
	linkPublished = true
	if err := unix.Fsync(dirFD); err != nil {
		return true, fmt.Errorf(
			"sync mail host TLS directory after certificate activation: %w", err,
		)
	}
	return true, nil
}

func removeExactVersionAt(
	dirFD int,
	versionName string,
	expected mailhostartifact.Receipt,
) error {
	if !validVersionName(versionName) {
		return errors.New("invalid mail host certificate issue cleanup version")
	}
	versionFD, err := openDirectoryAt(dirFD, versionName)
	if err != nil {
		return err
	}
	receipt, found, err :=
		ReadReceiptAt(versionFD)
	unix.Close(versionFD)
	if err != nil {
		return err
	}
	if !found || receipt != expected {
		return errors.New(
			"mail host certificate issue cleanup receipt mismatch",
		)
	}
	return removeVersionFilesAt(dirFD, versionName)
}

func removeVersionFilesAt(
	dirFD int,
	versionName string,
) error {
	versionFD, err := openDirectoryAt(dirFD, versionName)
	if err != nil {
		return err
	}
	for _, name := range []string{
		mailhostartifact.ReceiptName,
		"mail.domain",
		"privkey.pem",
		"fullchain.pem",
	} {
		if err := unix.Unlinkat(versionFD, name, 0); err != nil &&
			!errors.Is(err, unix.ENOENT) {
			unix.Close(versionFD)
			return err
		}
	}
	unix.Close(versionFD)
	if err := unix.Unlinkat(
		dirFD, versionName, unix.AT_REMOVEDIR,
	); err != nil {
		return err
	}
	return nil
}

func writeVersionFile(
	dirFD int,
	name string,
	ownerUID, ownerGID int,
	mode uint32,
	content []byte,
) error {
	fd, err := unix.Openat(
		dirFD,
		name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		mode,
	)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return fmt.Errorf("create %s: invalid file descriptor", name)
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := unix.Fchown(fd, ownerUID, ownerGID); err != nil {
		return fmt.Errorf("own %s: %w", name, err)
	}
	if err := unix.Fchmod(fd, mode); err != nil {
		return fmt.Errorf("protect %s: %w", name, err)
	}
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	closed = true
	return nil
}

func randomEntry(prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate certificate version name: %w", err)
	}
	return prefix + hex.EncodeToString(random), nil
}

// Verify the exact staged material before publication or unpublished cleanup.
// A valid receipt alone is not authority to remove later owner edits.
func unchangedMaterialAt(parent int, name string, certificate, key, receipt []byte, domain string) error {
	fd, err := openDirectoryAt(parent, name)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	duplicate, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(duplicate), "staged-mail-certificate")
	names, err := dir.Readdirnames(5)
	dir.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(names) != 4 {
		return errors.New("staged mail host certificate contents changed; owner review required")
	}
	expected := map[string][]byte{"fullchain.pem": certificate, "privkey.pem": key, "mail.domain": []byte(domain + "\n"), mailhostartifact.ReceiptName: receipt}
	for _, file := range names {
		want, ok := expected[file]
		if !ok {
			return errors.New("unexpected staged mail host certificate file; owner review required")
		}
		got, e := ReadRegularFileAt(fd, file, 0600, PEMMaxSize)
		if e != nil {
			return e
		}
		if !bytes.Equal(got, want) {
			return errors.New("staged mail host certificate material changed; owner review required")
		}
	}
	return nil
}
