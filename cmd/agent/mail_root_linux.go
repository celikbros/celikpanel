//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// The product's mail root has always been the fixed path mailRootDir
// ("/var/mail/vhosts"): every release wrote it into Postfix
// virtual_mailbox_base, the Dovecot drop-in and the vmail home. There is no
// other stored root. Some distributions (Arch's filesystem package, the Fedora
// family) ship /var/mail as a symbolic link to spool/mail, which the
// symlink-free open below refuses with ELOOP. Deletion and installation
// therefore resolve that one path through exactly one permitted link - the
// distribution's stock /var/mail -> spool/mail - and open the result with no
// symbolic link anywhere. Every other link in the path is a typed refusal.
//
// Ürünün posta kökü her zaman sabit mailRootDir'dir. Bazı dağıtımlar
// /var/mail'i spool/mail'e sembolik bağlantı olarak getirir; kök yalnız bu
// dağıtım bağlantısı üzerinden çözülür, başka her bağlantı reddedilir.
var (
	mailSpoolLinkPath   = "/var/mail"
	mailSpoolTargetPath = "/var/spool/mail"
	mailSpoolLinkOwner  = 0
)

// errMailPathSymlinkRefused is the typed refusal for a mail storage path that
// crosses a symbolic link other than the distribution's stock /var/mail link.
var errMailPathSymlinkRefused = errors.New("mail storage refused: a symbolic link is in the path and it is not the distribution's stock /var/mail -> spool/mail link")

// managedMailRootPath returns the symlink-free absolute mail root. The dev
// override and any non-default root are returned unchanged; the secure open
// still refuses every symbolic link in them.
func managedMailRootPath() (string, error) {
	root := filepath.Clean(mailRootDir)
	if root != filepath.Join(mailSpoolLinkPath, "vhosts") {
		return root, nil
	}
	fd, err := openMailAbsoluteDirectory(mailSpoolLinkPath)
	if err == nil {
		unix.Close(fd)
		return root, nil
	}
	if errors.Is(err, unix.ENOENT) {
		// Nothing exists there; the secure open of the root reports ENOENT
		// and cleanup treats it as nothing to clean.
		return root, nil
	}
	// A symbolic link as the last component yields ENOTDIR (O_NOFOLLOW with
	// O_DIRECTORY); one earlier in the path yields ELOOP.
	if !errors.Is(err, unix.ELOOP) && !errors.Is(err, unix.ENOTDIR) {
		return "", fmt.Errorf("inspect mail spool %s: %w", mailSpoolLinkPath, err)
	}
	target, err := stockMailSpoolTarget()
	if err != nil {
		return "", err
	}
	return filepath.Join(target, "vhosts"), nil
}

// stockMailSpoolTarget accepts the refused open only when the last component of
// mailSpoolLinkPath itself is the distribution's link: its parent opens with
// no symbolic link, the entry is a symlink owned by root whose text is exactly
// "spool/mail" (relative) or "/var/spool/mail", and the target is a real,
// root-owned directory reachable with no symbolic link.
func stockMailSpoolTarget() (string, error) {
	parent := filepath.Dir(mailSpoolLinkPath)
	base := filepath.Base(mailSpoolLinkPath)
	parentFD, err := openMailAbsoluteDirectory(parent)
	if err != nil {
		if mailOpenCrossesLink(parent, err) {
			return "", fmt.Errorf("%w (%s)", errMailPathSymlinkRefused, parent)
		}
		return "", fmt.Errorf("inspect mail spool parent %s: %w", parent, err)
	}
	defer unix.Close(parentFD)
	var link unix.Stat_t
	if err := unix.Fstatat(parentFD, base, &link, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", fmt.Errorf("inspect mail spool %s: %w", mailSpoolLinkPath, err)
	}
	if link.Mode&unix.S_IFMT != unix.S_IFLNK {
		return "", fmt.Errorf("mail spool %s is neither a directory nor the distribution's link", mailSpoolLinkPath)
	}
	if int(link.Uid) != mailSpoolLinkOwner {
		return "", fmt.Errorf("%w (%s)", errMailPathSymlinkRefused, mailSpoolLinkPath)
	}
	buffer := make([]byte, unix.PathMax)
	size, err := unix.Readlinkat(parentFD, base, buffer)
	if err != nil {
		return "", fmt.Errorf("read mail spool link %s: %w", mailSpoolLinkPath, err)
	}
	text := string(buffer[:size])
	relative, relErr := filepath.Rel(parent, mailSpoolTargetPath)
	if relErr != nil || (text != relative && text != mailSpoolTargetPath) {
		return "", fmt.Errorf("%w (%s -> %s)", errMailPathSymlinkRefused, mailSpoolLinkPath, text)
	}
	targetFD, err := openMailAbsoluteDirectory(mailSpoolTargetPath)
	if err != nil {
		if mailOpenCrossesLink(mailSpoolTargetPath, err) {
			return "", fmt.Errorf("%w (%s)", errMailPathSymlinkRefused, mailSpoolTargetPath)
		}
		return "", fmt.Errorf("open mail spool %s: %w", mailSpoolTargetPath, err)
	}
	defer unix.Close(targetFD)
	var target unix.Stat_t
	if err := unix.Fstat(targetFD, &target); err != nil {
		return "", fmt.Errorf("inspect mail spool %s: %w", mailSpoolTargetPath, err)
	}
	if target.Mode&unix.S_IFMT != unix.S_IFDIR || int(target.Uid) != mailSpoolLinkOwner {
		return "", fmt.Errorf("%w (%s is not a root-owned directory)", errMailPathSymlinkRefused, mailSpoolTargetPath)
	}
	return mailSpoolTargetPath, nil
}

// mailOpenCrossesLink reports whether a refused symlink-free open of path
// failed because of a symbolic link: ELOOP for an earlier component, or
// ENOTDIR when the last component itself is a link. Lstat only classifies the
// error; nothing is opened through it.
func mailOpenCrossesLink(path string, err error) bool {
	if errors.Is(err, unix.ELOOP) {
		return true
	}
	if !errors.Is(err, unix.ENOTDIR) {
		return false
	}
	info, statErr := os.Lstat(path)
	return statErr == nil && info.Mode()&os.ModeSymlink != 0
}

// mailEntryIsLinkAt classifies an ENOTDIR/ELOOP from opening name under dirFD.
func mailEntryIsLinkAt(dirFD int, name string, err error) bool {
	if errors.Is(err, unix.ELOOP) {
		return true
	}
	if !errors.Is(err, unix.ENOTDIR) {
		return false
	}
	var stat unix.Stat_t
	return unix.Fstatat(dirFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW) == nil && stat.Mode&unix.S_IFMT == unix.S_IFLNK
}
