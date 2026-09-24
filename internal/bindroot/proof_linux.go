//go:build linux

package bindroot

import (
	"errors"
	"fmt"
	"strings"
)

const (
	APTExactStatOverrideLine = "root bind 1775 /var/cache/bind\n"
	APTExactPackageOwnerLine = "bind9: /var/cache/bind\n"
)

type APTStatOverrideState uint8

const (
	APTStatOverrideAbsent APTStatOverrideState = iota
	APTStatOverrideExact
)

type commandExitCoder interface {
	ExitCode() int
}

// ClassifyAPTStatOverride distinguishes a genuinely absent override from a
// conflicting or failed query. Only the exact durable override is sufficient
// for the read-only root proof; the caller decides whether absence can be fixed.
func ClassifyAPTStatOverride(output []byte, commandErr error) (APTStatOverrideState, error) {
	if commandErr == nil && string(output) == APTExactStatOverrideLine {
		return APTStatOverrideExact, nil
	}
	var exitCoder commandExitCoder
	if len(output) == 0 && errors.As(commandErr, &exitCoder) && exitCoder.ExitCode() == 1 {
		return APTStatOverrideAbsent, nil
	}
	return APTStatOverrideAbsent, errors.New(
		"dpkg-statoverride returned a conflicting, redirected, or non-canonical /var/cache/bind result",
	)
}

func VerifyAPTPackageOwner(output []byte, commandErr error) error {
	if commandErr != nil {
		return fmt.Errorf("verify /var/cache/bind package ownership: %w", commandErr)
	}
	if string(output) != APTExactPackageOwnerLine {
		return errors.New("/var/cache/bind is not the exact bind9 package-owned directory")
	}
	return nil
}

// VerifyPacmanPackageOwner accepts exactly one canonical pacman ownership line.
func VerifyPacmanPackageOwner(output []byte, commandErr error) error {
	if commandErr != nil {
		return fmt.Errorf("verify /var/named package ownership: %w", commandErr)
	}
	line := string(output)
	const prefix = "/var/named/ is owned by bind "
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		return errors.New("/var/named is not the exact bind package-owned directory")
	}
	version := strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\n")
	if version == "" || strings.ContainsAny(version, " \t") ||
		strings.Trim(version, "0123456789.:-+abcdefghijklmnopqrstuvwxyz_") != "" {
		return errors.New("/var/named package ownership version is not canonical")
	}
	return nil
}
