//go:build linux

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var removePDNSSwitchArtifactBeforeRename func(string) error

func samePDNSArtifact(left, right *unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode&unix.S_IFMT == unix.S_IFREG && right.Mode&unix.S_IFMT == unix.S_IFREG
}

func removePDNSSwitchArtifact(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || filepath.Base(clean) == "." || filepath.Base(clean) == string(filepath.Separator) {
		return errors.New("PowerDNS switch artifact path is not absolute and canonical")
	}
	parent, name := filepath.Dir(clean), filepath.Base(clean)
	parentFD, err := unix.Open(
		parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return fmt.Errorf("open PowerDNS switch artifact parent: %w", err)
	}
	defer unix.Close(parentFD)

	var initial unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &initial, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("inspect PowerDNS switch artifact: %w", err)
	}
	if initial.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("PowerDNS switch artifact is not a safe regular file")
	}
	artifactFD, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open exact PowerDNS switch artifact: %w", err)
	}
	defer unix.Close(artifactFD)
	var opened unix.Stat_t
	if err := unix.Fstat(artifactFD, &opened); err != nil {
		return fmt.Errorf("inspect opened PowerDNS switch artifact: %w", err)
	}
	if !samePDNSArtifact(&initial, &opened) {
		return errors.New("PowerDNS switch artifact changed before quarantine")
	}
	if removePDNSSwitchArtifactBeforeRename != nil {
		if err := removePDNSSwitchArtifactBeforeRename(clean); err != nil {
			return err
		}
	}

	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("generate PowerDNS artifact quarantine name: %w", err)
	}
	quarantine := ".cp-rm-" + hex.EncodeToString(random[:])
	if err := unix.Renameat2(
		parentFD, name, parentFD, quarantine, unix.RENAME_NOREPLACE,
	); err != nil {
		return fmt.Errorf("quarantine exact PowerDNS switch artifact: %w", err)
	}
	restore := func() error {
		return unix.Renameat2(
			parentFD, quarantine, parentFD, name, unix.RENAME_NOREPLACE,
		)
	}
	var quarantined unix.Stat_t
	if err := unix.Fstatat(parentFD, quarantine, &quarantined, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("reinspect quarantined PowerDNS switch artifact: %w", err)
	}
	if !samePDNSArtifact(&opened, &quarantined) {
		if restoreErr := restore(); restoreErr != nil {
			return errors.Join(
				errors.New("PowerDNS switch artifact was replaced before quarantine"),
				fmt.Errorf("restore replacement without deleting it: %w", restoreErr),
			)
		}
		return errors.New("PowerDNS switch artifact was replaced before quarantine")
	}
	if err := unix.Unlinkat(parentFD, quarantine, 0); err != nil {
		if restoreErr := restore(); restoreErr != nil {
			return errors.Join(
				fmt.Errorf("remove quarantined PowerDNS switch artifact: %w", err),
				fmt.Errorf("restore quarantined artifact: %w", restoreErr),
			)
		}
		return fmt.Errorf("remove quarantined PowerDNS switch artifact: %w", err)
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync PowerDNS switch artifact parent: %w", err)
	}
	return nil
}
