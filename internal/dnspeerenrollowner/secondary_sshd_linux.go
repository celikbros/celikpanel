//go:build linux

package dnspeerenrollowner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const sshdInclude = "\nInclude " + SSHDConfigPath + "\n"
const sshdReceiptSchema = "celikpanel-bind-peer-sshd-include/v1"

type sshdReceipt struct {
	Schema         string `json:"schema"`
	OriginalSHA256 string `json:"original_sha256"`
	ActiveSHA256   string `json:"active_sha256"`
}

// installSSHDInclude stages and validates a complete replacement of the
// existing sshd_config before one atomic rename. The original bytes and a
// versioned digest receipt remain root-only for owner review and drift checks.
func installSSHDInclude(primaryIP string, resume bool) error {
	if err := trustedSSHDOriginal(); err != nil {
		return err
	}
	active, err := readTrustedFile(SSHDMainPath, 1<<20, false)
	if err != nil {
		return fmt.Errorf("reviewed sshd_config unavailable: %w", err)
	}
	original := active
	var priorReceipt []byte
	backup, backupErr := readManagedFile(SSHDBackupPath, 1<<20, 0600)
	if backupErr == nil {
		if !resume {
			return errors.New("sshd backup exists; explicit secondary-resume is required")
		}
		original = backup
	} else if !errors.Is(backupErr, os.ErrNotExist) {
		return backupErr
	}
	priorReceipt, err = readManagedFile(SSHDReceiptPath, 4096, 0600)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if backupErr != nil && len(priorReceipt) != 0 {
		return errors.New("sshd receipt exists without its original; preserve owner evidence")
	}
	published, err := classifySSHDPublication(original, active, priorReceipt)
	if err != nil {
		return err
	}
	if published {
		return nil
	}
	st, err := os.Lstat(SSHDMainPath)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 {
		return errors.New("sshd_config ownership or mode is unsafe")
	}
	updated := append(append([]byte(nil), original...), []byte(sshdInclude)...)
	temp, err := os.CreateTemp(filepath.Dir(SSHDMainPath), ".celikpanel-sshd-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(st.Mode().Perm()); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(updated); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if _, err := run("sshd", "-t", "-f", tempPath); err != nil {
		return fmt.Errorf("staged end-of-file Include is rejected; key remains disabled: %w", err)
	}
	if err := checkSSHDAt(tempPath, primaryIP); err != nil {
		return err
	}
	oldHash := sha256.Sum256(original)
	newHash := sha256.Sum256(updated)
	receipt, _ := json.Marshal(sshdReceipt{Schema: sshdReceiptSchema, OriginalSHA256: hex.EncodeToString(oldHash[:]), ActiveSHA256: hex.EncodeToString(newHash[:])})
	if err := writeExactOrCreate(SSHDBackupPath, original, 0600); err != nil {
		return err
	}
	if err := writeExactOrCreate(SSHDReceiptPath, receipt, 0600); err != nil {
		return err
	}
	current, err := readTrustedFile(SSHDMainPath, 1<<20, false)
	if err != nil || !bytes.Equal(current, original) {
		return errors.New("sshd_config changed during staging; key remains disabled")
	}
	if err := os.Rename(tempPath, SSHDMainPath); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(SSHDMainPath))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// classifySSHDPublication admits only the recorded before-image or the exact
// published image. A backup-only cut can continue while the live file is still
// original; a published image always needs its durable receipt.
func classifySSHDPublication(original, active, receipt []byte) (bool, error) {
	if bytes.Contains(original, []byte(SSHDConfigPath)) {
		return false, errors.New("sshd original already references the managed Match path")
	}
	updated := append(append([]byte(nil), original...), []byte(sshdInclude)...)
	if len(receipt) != 0 {
		if err := validateSSHDReceipt(original, updated, receipt); err != nil {
			return false, err
		}
	}
	if bytes.Equal(active, original) {
		return false, nil
	}
	if len(receipt) != 0 && bytes.Equal(active, updated) {
		return true, nil
	}
	return false, errors.New("sshd_config differs from the recorded original and publication; preserve the owner edit")
}

func verifySSHDInclude() error {
	backup, err := readManagedFile(SSHDBackupPath, 1<<20, 0600)
	if err != nil {
		return err
	}
	receiptRaw, err := readManagedFile(SSHDReceiptPath, 4096, 0600)
	if err != nil {
		return err
	}
	active, err := readTrustedFile(SSHDMainPath, 1<<20, false)
	if err != nil {
		return err
	}
	return validateSSHDReceipt(backup, active, receiptRaw)
}

func validateSSHDReceipt(backup, active, receiptRaw []byte) error {
	var receipt sshdReceipt
	dec := json.NewDecoder(bytes.NewReader(receiptRaw))
	dec.DisallowUnknownFields()
	if dec.Decode(&receipt) != nil {
		return errors.New("sshd receipt is malformed")
	}
	canonical, _ := json.Marshal(receipt)
	if !bytes.Equal(canonical, receiptRaw) || receipt.Schema != sshdReceiptSchema {
		return errors.New("sshd receipt is not canonical")
	}
	oldHash := sha256.Sum256(backup)
	activeHash := sha256.Sum256(active)
	if receipt.OriginalSHA256 != hex.EncodeToString(oldHash[:]) || receipt.ActiveSHA256 != hex.EncodeToString(activeHash[:]) || !bytes.Equal(active, append(append([]byte(nil), backup...), []byte(sshdInclude)...)) {
		return errors.New("owner sshd_config changed since enrollment")
	}
	return nil
}
func checkSSHDAt(config, primaryIP string) error {
	args := []string{"-T"}
	if config != "" {
		args = append(args, "-f", config)
	}
	args = append(args, "-C", "user="+Account+",host=localhost,addr="+primaryIP)
	out, err := run("sshd", args...)
	if err != nil {
		return fmt.Errorf("effective sshd policy unavailable: %w", err)
	}
	got := parseSSHDSettings(out)
	for key, want := range map[string]string{
		"authenticationmethods": "publickey", "pubkeyauthentication": "yes", "passwordauthentication": "no",
		"kbdinteractiveauthentication": "no", "authorizedkeysfile": AuthorizedKeysPath, "authorizedkeyscommand": "/usr/bin/false", "authorizedkeyscommanduser": "nobody", "pubkeyacceptedalgorithms": "ssh-ed25519", "forcecommand": WrapperPath,
		"disableforwarding": "yes", "permittty": "no", "permituserrc": "no",
	} {
		if got[key] != want {
			return fmt.Errorf("effective sshd %s is not restricted as required; key remains disabled", key)
		}
	}
	return nil
}

func parseSSHDSettings(out []byte) map[string]string {
	got := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 {
			got[strings.ToLower(fields[0])] = strings.Join(fields[1:], " ")
		}
	}
	return got
}
func trustedSSHDOriginal() error {
	st, err := os.Lstat(SSHDMainPath)
	if err != nil {
		return err
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != 0 || owner.Gid != 0 || owner.Nlink != 1 {
		return errors.New("sshd_config is not a sole root-owned file")
	}
	xattrBytes, err := unix.Listxattr(SSHDMainPath, nil)
	if err != nil || xattrBytes != 0 {
		return errors.New("sshd_config has metadata this installer cannot preserve")
	}
	return nil
}
