//go:build linux

package hostplatform

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	selinuxEnforcePath         = "/sys/fs/selinux/enforce"
	selinuxEnforceContentLimit = 4096
)

// InspectLiveSecurityPolicy reads the live SELinux state without changing it.
func InspectLiveSecurityPolicy() (SecurityPolicyState, error) {
	return inspectSELinuxStateAt(selinuxEnforcePath)
}

func verifySELinuxInactiveAt(path string) error {
	state, err := inspectSELinuxStateAt(path)
	if err != nil {
		return err
	}
	switch state {
	case SecurityPolicyInactive:
		return nil
	case SecurityPolicyPermissive:
		return errors.New("SELinux is active in permissive mode but no certified label lifecycle is available")
	case SecurityPolicyEnforcing:
		return errors.New("SELinux is active in enforcing mode but no certified label lifecycle is available")
	default:
		return fmt.Errorf("unknown live security-policy state %q", state)
	}
}

// VerifyLiveSecurityPolicy is the central mutation gate. Every mutation needs
// an inactive SELinux filesystem until a complete label lifecycle is certified.
func VerifyLiveSecurityPolicy() error {
	return verifySELinuxInactiveAt(selinuxEnforcePath)
}

func inspectSELinuxStateAt(path string) (SecurityPolicyState, error) {
	entry, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return SecurityPolicyInactive, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect SELinux enforcement state: %w", err)
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("SELinux enforcement state path must not be symbolic")
	}
	if !entry.Mode().IsRegular() {
		return "", errors.New("SELinux enforcement state is unavailable or unreadable")
	}
	if entry.Mode().Perm()&0o444 == 0 {
		return "", errors.New("SELinux enforcement state has no readable permission bit")
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open SELinux enforcement state: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect opened SELinux enforcement state: %w", err)
	}
	if !os.SameFile(entry, opened) {
		return "", errors.New("SELinux enforcement state changed while it was being verified")
	}
	if !opened.Mode().IsRegular() || opened.Mode().Perm()&0o444 == 0 {
		return "", errors.New("SELinux enforcement state is unavailable or unreadable")
	}
	raw, err := io.ReadAll(io.LimitReader(file, selinuxEnforceContentLimit+1))
	if err != nil {
		return "", fmt.Errorf("read SELinux enforcement state: %w", err)
	}
	if len(raw) > selinuxEnforceContentLimit {
		return "", errors.New("SELinux enforcement state is malformed")
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return "", errors.New("SELinux enforcement state contains a NUL byte")
	}
	if !bytes.HasSuffix(raw, []byte{'\n'}) {
		return "", errors.New("SELinux enforcement state must be newline-terminated")
	}
	if bytes.Count(raw, []byte{'\n'}) != 1 {
		return "", errors.New("SELinux enforcement state must contain exactly one line")
	}
	switch string(raw) {
	case "0\n":
		return SecurityPolicyPermissive, nil
	case "1\n":
		return SecurityPolicyEnforcing, nil
	default:
		return "", errors.New("SELinux enforcement state is malformed")
	}
}
