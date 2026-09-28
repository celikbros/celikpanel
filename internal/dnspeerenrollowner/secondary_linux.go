//go:build linux

// Package dnspeerenrollowner installs an optional, owner-operated, read-only
// BIND peer inspector channel on the secondary host. It never changes BIND.
package dnspeerenrollowner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindpeerinspector"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

const (
	Account            = "celikpeer"
	HomePath           = "/var/lib/bind-peer-inspector"
	InspectorPath      = "/usr/local/libexec/celikpanel-bind-peer-inspect"
	WrapperPath        = "/usr/local/libexec/celikpanel-bind-peer-inspect-wrapper"
	PolicyPath         = bindpeerinspector.OwnerPolicyPath
	AuthorizedKeysPath = "/etc/ssh/bind-peer-inspector/authorized_keys"
	SSHDConfigPath     = "/etc/bind-peer-inspector/sshd-match.conf"
	SSHDMainPath       = "/etc/ssh/sshd_config"
	SSHDBackupPath     = "/etc/bind-peer-inspector/sshd-config-original"
	SSHDReceiptPath    = "/etc/bind-peer-inspector/sshd-config-receipt.json"
	SudoersPath        = "/etc/sudoers.d/celikpanel-bind-peer-inspector"
	maxBinaryBytes     = 64 << 20
)

type SecondaryOptions struct {
	PrimaryIP            string `json:"primary_ip,omitempty"`
	PeerIP               string `json:"peer_ip,omitempty"`
	CatalogName          string `json:"catalog_name,omitempty"`
	PrimaryPublicKeyPath string
	InspectorBinaryPath  string
}

type Status struct {
	State            string `json:"state"` // disabled, incomplete, configured, unknown
	Reason           string `json:"reason"`
	NextAction       string `json:"next_action"`
	PrimaryIP        string `json:"primary_ip,omitempty"`
	PeerIP           string `json:"peer_ip,omitempty"`
	CatalogName      string `json:"catalog_name,omitempty"`
	PrimaryKeySHA256 string `json:"primary_key_sha256,omitempty"`
}

// SecondaryHostKeySHA256 returns the pinned digest of the local OpenSSH
// Ed25519 host identity. The owner must compare it through a trusted channel.
func SecondaryHostKeySHA256() (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("host identity inspection requires local root")
	}
	publicRaw, err := readTrustedFile("/etc/ssh/ssh_host_ed25519_key.pub", 4096, false)
	if err != nil {
		return "", err
	}
	privateRaw, err := readTrustedFile("/etc/ssh/ssh_host_ed25519_key", 16384, false)
	if err != nil {
		return "", err
	}
	defer func() {
		for i := range privateRaw {
			privateRaw[i] = 0
		}
	}()
	public, _, options, rest, err := ssh.ParseAuthorizedKey(publicRaw)
	if err != nil || public.Type() != ssh.KeyAlgoED25519 || len(options) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return "", errors.New("native Ed25519 host public key is invalid")
	}
	private, err := ssh.ParsePrivateKey(privateRaw)
	if err != nil || private.PublicKey().Type() != ssh.KeyAlgoED25519 || !bytes.Equal(private.PublicKey().Marshal(), public.Marshal()) {
		return "", errors.New("native Ed25519 host private key does not match public key")
	}
	digest := sha256.Sum256(public.Marshal())
	return hex.EncodeToString(digest[:]), nil
}
func validateOptions(o SecondaryOptions) (ssh.PublicKey, []byte, error) {
	catalog, err := binddns.CatalogDomain(o.PrimaryIP)
	peer := net.ParseIP(o.PeerIP)
	if err != nil || catalog != o.CatalogName || peer == nil || peer.To4() == nil ||
		peer.String() != o.PeerIP || !peer.IsGlobalUnicast() || o.PeerIP == o.PrimaryIP {
		return nil, nil, errors.New("reviewed primary, peer and exact catalog are invalid")
	}
	if !filepath.IsAbs(o.PrimaryPublicKeyPath) || !filepath.IsAbs(o.InspectorBinaryPath) {
		return nil, nil, errors.New("public key and inspector binary paths must be absolute")
	}
	keyRaw, err := readTrustedFile(o.PrimaryPublicKeyPath, 4096, false)
	if err != nil {
		return nil, nil, fmt.Errorf("primary public key: %w", err)
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey(keyRaw)
	_ = comment
	if err != nil || key.Type() != ssh.KeyAlgoED25519 || len(options) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, nil, errors.New("primary key must be one unqualified Ed25519 public key")
	}
	bin, err := readTrustedFile(o.InspectorBinaryPath, maxBinaryBytes, true)
	if err != nil {
		return nil, nil, fmt.Errorf("inspector binary: %w", err)
	}
	if len(bin) < 4 || !bytes.Equal(bin[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		return nil, nil, errors.New("inspector binary must be a reviewed Linux ELF")
	}
	return key, bin, nil
}

func renderFiles(o SecondaryOptions, key ssh.PublicKey) (policy, wrapper, sudoers, sshd, authorized []byte) {
	p := bindpeerinspector.OwnerPolicyV1{Schema: bindpeerinspector.PolicySchemaV1,
		PrimaryIP: o.PrimaryIP, PeerIP: o.PeerIP, CatalogName: o.CatalogName, View: dnspeerproof.DefaultView}
	policy, _ = json.Marshal(p)
	wrapper = []byte("#!/bin/sh\n[ \"${SSH_ORIGINAL_COMMAND-}\" = \"celikpanel-bind-peer-inspect-v1\" ] || exit 1\nunset SSH_ORIGINAL_COMMAND\nexec /usr/bin/sudo -n -- " + InspectorPath + "\n")
	sudoers = []byte(Account + " ALL=(root) NOPASSWD: " + InspectorPath + " \"\"\n")
	sshd = []byte("Match User " + Account + "\n" +
		"    AuthenticationMethods publickey\n    PubkeyAuthentication yes\n" +
		"    PasswordAuthentication no\n    KbdInteractiveAuthentication no\n" +
		"    AuthorizedKeysFile " + AuthorizedKeysPath + "\n" +
		"    AuthorizedKeysCommand /usr/bin/false\n    AuthorizedKeysCommandUser nobody\n    PubkeyAcceptedAlgorithms ssh-ed25519\n" +
		"    ForceCommand " + WrapperPath + "\n" +
		"    DisableForwarding yes\n    PermitTTY no\n    PermitUserRC no\n")
	authorized = []byte("restrict,from=\"" + o.PrimaryIP + "\",command=\"" + WrapperPath + "\" " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + "\n")
	return
}

// InstallSecondary is an explicit local root action. The authorized key is
// installed last, after the restrictive sshd configuration is effective.
// A retry can reuse only exact staged files before the SSH receipt/account exists.
// Existing conflicting owner state is never overwritten.
func InstallSecondary(o SecondaryOptions) error {
	return enrollSecondary(o, false)
}

// ResumeSecondary is an explicit owner action with reviewed inputs. It reuses
// exact staged files and a restricted dedicated account, never a changed key,
// owner configuration or inspector binary.
func ResumeSecondary(o SecondaryOptions) error {
	return enrollSecondary(o, true)
}

func enrollSecondary(o SecondaryOptions, resume bool) error {
	if os.Geteuid() != 0 {
		return errors.New("secondary enrollment requires local root")
	}
	key, bin, err := validateOptions(o)
	if err != nil {
		return err
	}
	policy, wrapper, sudoers, sshd, authorized := renderFiles(o, key)
	keyPresent := false
	if resume {
		staged, err := readManagedFile(PolicyPath, 4096, 0600)
		if err != nil || !bytes.Equal(staged, policy) {
			return errors.New("resume requires the exact staged pair policy; preserve existing files for owner review")
		}
		if raw, err := readManagedFile(AuthorizedKeysPath, 4096, 0644); err == nil {
			keyPresent = true
			if !bytes.Equal(raw, authorized) {
				return errors.New("resume cannot replace an existing authorized primary key")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		for _, name := range []string{AuthorizedKeysPath, SSHDBackupPath, SSHDReceiptPath} {
			if _, err := os.Lstat(name); err == nil {
				return fmt.Errorf("activation or receipt exists at %s; inspect secondary-status, then use secondary-resume with the reviewed pair, key and inspector", name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	for _, file := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{InspectorPath, bin, 0755}, {WrapperPath, wrapper, 0755}, {PolicyPath, policy, 0600}, {SudoersPath, sudoers, 0440}, {SSHDConfigPath, sshd, 0644},
	} {
		if _, err := os.Lstat(file.path); err == nil {
			if raw, readErr := readManagedFile(file.path, len(file.data)+1, file.mode); readErr != nil || !bytes.Equal(raw, file.data) {
				return fmt.Errorf("staged owner file %s changed; refusing to resume", file.path)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if keyPresent {
		status, err := SecondaryStatus()
		if err != nil || status.State != "configured" {
			return errors.New("the existing key is still authorized but channel state is unverified; run secondary-revoke and review owner changes before resuming")
		}
		return nil
	}
	if _, err := os.Stat("/usr/bin/getent"); err != nil {
		return errors.New("getent is required to verify account absence")
	}
	accountExists := false
	lookupContext, cancelLookup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelLookup()
	lookup := exec.CommandContext(lookupContext, "/usr/bin/getent", "passwd", Account)
	lookup.Env = []string{"PATH=/usr/sbin:/usr/bin:/bin", "LC_ALL=C"}
	if output, err := lookup.CombinedOutput(); err == nil {
		if !resume {
			return errors.New("dedicated account already exists; inspect enrollment before explicit secondary-resume")
		}
		accountExists = true
		if err := checkLockedAccount(); err != nil {
			return err
		}
	} else {
		var exit *exec.ExitError
		if len(output) != 0 || !errors.As(err, &exit) || exit.ExitCode() != 2 {
			return errors.New("dedicated account absence could not be verified")
		}
	}
	if _, err := nativePath("sshd"); err != nil {
		return errors.New("OpenSSH server is required")
	}
	if _, err := nativePath("visudo"); err != nil {
		return errors.New("visudo is required")
	}
	if _, err := os.Stat("/usr/bin/sudo"); err != nil {
		return errors.New("/usr/bin/sudo is required")
	}
	if _, err := readTrustedFile("/usr/bin/false", 1<<20, true); err != nil {
		return errors.New("root-owned /usr/bin/false is required to disable alternate key lookup")
	}
	if _, err := run("getent", "passwd", "nobody"); err != nil {
		return errors.New("nobody account is required for disabled alternate key lookup")
	}
	unit, err := activeSSHUnit()
	if err != nil {
		return err
	}
	if out, err := run("systemctl", "is-active", "named.service"); err != nil || strings.TrimSpace(string(out)) != "active" {
		return errors.New("active native named.service is required before enrollment")
	}
	if err := ensureDir("/etc/bind-peer-inspector", 0750); err != nil {
		return err
	}
	if err := ensureDir("/etc/ssh/bind-peer-inspector", 0755); err != nil {
		return err
	}
	if err := checkPublicKeyTraversal(); err != nil {
		return err
	}
	if err := ensureExistingDir("/etc/sudoers.d"); err != nil {
		return err
	}
	if err := ensureDir("/usr/local/libexec", 0755); err != nil {
		return err
	}
	if err := ensureDir(HomePath, 0755); err != nil {
		return err
	}
	for _, file := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{InspectorPath, bin, 0755}, {WrapperPath, wrapper, 0755},
		{PolicyPath, policy, 0600}, {SudoersPath, sudoers, 0440}, {SSHDConfigPath, sshd, 0644},
	} {
		if err := writeExactOrCreate(file.path, file.data, file.mode); err != nil {
			return err
		}
	}
	if _, err := run("visudo", "-cf", SudoersPath); err != nil {
		return fmt.Errorf("sudoers validation failed: %w", err)
	}
	if err := installSSHDInclude(o.PrimaryIP, resume); err != nil {
		return err
	}
	if _, err := run("sshd", "-t"); err != nil {
		return fmt.Errorf("sshd rejected the managed Match drop-in; the key remains disabled. Preserve staged files and review sshd_config Include ordering before secondary-resume: %w", err)
	}
	if err := checkSSHD(o.PrimaryIP); err != nil {
		return err
	}
	if !accountExists {
		if _, err := run("useradd", "--system", "--no-create-home", "--home-dir", HomePath, "--shell", "/bin/sh", "--user-group", Account); err != nil {
			return fmt.Errorf("dedicated account creation failed: %w", err)
		}
		if _, err := run("usermod", "--lock", Account); err != nil {
			return fmt.Errorf("account password lock failed: %w", err)
		}
	}
	if err := checkLockedAccount(); err != nil {
		return err
	}
	if _, err := run("systemctl", "reload", unit); err != nil {
		return fmt.Errorf("SSH reload failed; key remains disabled: %w", err)
	}
	if err := checkSSHD(o.PrimaryIP); err != nil {
		return err
	}
	// Recheck owner-controlled bytes immediately before granting the key.
	if err := verifySSHDInclude(); err != nil {
		return err
	}
	for _, file := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{InspectorPath, bin, 0755}, {WrapperPath, wrapper, 0755},
		{PolicyPath, policy, 0600}, {SudoersPath, sudoers, 0440}, {SSHDConfigPath, sshd, 0644},
	} {
		raw, err := readManagedFile(file.path, len(file.data)+1, file.mode)
		if err != nil || !bytes.Equal(raw, file.data) {
			return fmt.Errorf("staged owner file %s changed before key activation", file.path)
		}
	}
	if err := checkLockedAccount(); err != nil {
		return err
	}
	return writeExactOrCreate(AuthorizedKeysPath, authorized, 0644)
}

// RevokeSecondary removes only the fixed root-owned authorized key file. It
// does not change native BIND replication, the SSH service, or owner policy.
func RevokeSecondary() error {
	if os.Geteuid() != 0 {
		return errors.New("revocation requires local root")
	}
	policy, _, err := (bindpeerinspector.OwnerPolicyReader{}).Read(context.Background())
	if err != nil {
		return errors.New("reviewed owner policy is required before revocation")
	}

	keyRaw, err := readManagedFile(AuthorizedKeysPath, 4096, 0644)
	if err != nil {
		return err
	}
	if _, err := enrolledPrimaryKey(keyRaw, policy.PrimaryIP); err != nil {
		return err
	}
	if err := os.Remove(AuthorizedKeysPath); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(AuthorizedKeysPath))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func SecondaryStatus() (Status, error) {
	if os.Geteuid() != 0 {
		return Status{State: "unknown", Reason: "local root required", NextAction: "Run status as root on the secondary"}, nil
	}
	policy, _, err := (bindpeerinspector.OwnerPolicyReader{}).Read(context.Background())
	if err != nil {
		if _, statErr := os.Lstat(PolicyPath); errors.Is(statErr, os.ErrNotExist) {
			return Status{State: "disabled", Reason: "reviewed BIND inspector policy is absent", NextAction: "Run secondary-install locally with reviewed pair and key"}, nil
		}
		return Status{State: "unknown", Reason: "reviewed BIND inspector policy cannot be trusted", NextAction: "Review owner policy ownership, mode and canonical content locally"}, nil
	}
	result := Status{State: "incomplete", PrimaryIP: policy.PrimaryIP, PeerIP: policy.PeerIP, CatalogName: policy.CatalogName,
		Reason: "inspector SSH channel is incomplete", NextAction: "Run secondary-resume locally with the reviewed pair, key and inspector; changed files require owner review"}
	derived, pairErr := binddns.CatalogDomain(policy.PrimaryIP)
	peer := net.ParseIP(policy.PeerIP)
	if pairErr != nil || derived != policy.CatalogName || peer == nil || peer.To4() == nil || peer.String() != policy.PeerIP || policy.PeerIP == policy.PrimaryIP || !peer.IsGlobalUnicast() || policy.View != dnspeerproof.DefaultView {
		result.State = "unknown"
		result.Reason = "owner policy authority is invalid"
		return result, nil
	}
	if err := checkPublicKeyTraversal(); err != nil {
		result.Reason = "authorized-key path is unreadable by the dedicated account"
		result.NextAction = "Restore root-owned directory traversal permissions and review the enrolled key"
		return result, nil
	}
	keyRaw, err := readManagedFile(AuthorizedKeysPath, 4096, 0644)
	if errors.Is(err, os.ErrNotExist) {
		result.State = "disabled"
		result.Reason = "primary key is not authorized"
		result.NextAction = "Run secondary-resume with the reviewed pair, key and inspector to finish enrollment; leave it disabled if revocation was intentional"
		return result, nil
	}
	if err != nil {
		return result, nil
	}
	managed := map[string][]byte{}
	for _, item := range []struct {
		path string
		max  int
		mode os.FileMode
	}{
		{InspectorPath, maxBinaryBytes, 0755}, {WrapperPath, 4096, 0755}, {SudoersPath, 4096, 0440}, {SSHDConfigPath, 4096, 0644},
	} {
		raw, err := readManagedFile(item.path, item.max, item.mode)
		if err != nil {
			return result, nil
		}
		managed[item.path] = raw
	}
	if err := verifySSHDInclude(); err != nil {
		result.State = "unknown"
		result.Reason = "owner sshd_config changed since enrollment"
		result.NextAction = "Review the retained sshd original and current file, then revoke the key if restrictions are uncertain"
		return result, nil
	}
	if err := checkSSHD(policy.PrimaryIP); err != nil {
		return result, nil
	}
	if _, err := activeSSHUnit(); err != nil {
		return result, nil
	}
	if out, err := run("systemctl", "is-active", "named.service"); err != nil || strings.TrimSpace(string(out)) != "active" {
		return result, nil
	}
	if err := checkLockedAccount(); err != nil {
		return result, nil
	}
	if err := checkHome(); err != nil {
		return result, nil
	}
	if _, err := readTrustedFile("/usr/bin/false", 1<<20, true); err != nil {
		return result, nil
	}
	if _, err := run("getent", "passwd", "nobody"); err != nil {
		return result, nil
	}
	if _, err := run("visudo", "-cf", SudoersPath); err != nil {
		return result, nil
	}
	key, err := enrolledPrimaryKey(keyRaw, policy.PrimaryIP)
	if err != nil {
		return result, nil
	}
	_, wrapper, sudoers, sshd, _ := renderFiles(SecondaryOptions{PrimaryIP: policy.PrimaryIP, PeerIP: policy.PeerIP, CatalogName: policy.CatalogName}, key)
	if !bytes.Equal(managed[WrapperPath], wrapper) || !bytes.Equal(managed[SudoersPath], sudoers) || !bytes.Equal(managed[SSHDConfigPath], sshd) || !bytes.HasPrefix(managed[InspectorPath], []byte{0x7f, 'E', 'L', 'F'}) {
		return result, nil
	}
	digest := sha256.Sum256(key.Marshal())
	result.PrimaryKeySHA256 = hex.EncodeToString(digest[:])
	result.State = "configured"
	result.Reason = "local policy and SSH configuration verified; live authentication is unproven"
	result.NextAction = "Run one authenticated read-only inspector exchange from the reviewed primary before relying on deletion proof"
	return result, nil
}

func enrolledPrimaryKey(raw []byte, primaryIP string) (ssh.PublicKey, error) {
	parts := bytes.SplitN(bytes.TrimSpace(raw), []byte(" ssh-ed25519 "), 2)
	if len(parts) != 2 || !bytes.Equal(parts[0], []byte("restrict,from=\""+primaryIP+"\",command=\""+WrapperPath+"\"")) {
		return nil, errors.New("authorized key is not bound to reviewed primary and wrapper")
	}
	key, _, _, rest, err := ssh.ParseAuthorizedKey(raw)
	if err != nil || key.Type() != ssh.KeyAlgoED25519 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("authorized key is malformed")
	}
	return key, nil
}
func checkHome() error {
	if err := secureComponents(filepath.Join(HomePath, "placeholder")); err != nil {
		return err
	}
	st, err := os.Lstat(HomePath)
	if err != nil {
		return err
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || owner.Uid != 0 || st.Mode().Perm() != 0755 {
		return errors.New("dedicated account home changed")
	}
	return nil
}
func activeSSHUnit() (string, error) {
	for _, unit := range []string{"ssh.service", "sshd.service"} {
		out, err := run("systemctl", "is-active", unit)
		if err == nil && strings.TrimSpace(string(out)) == "active" {
			return unit, nil
		}
	}
	return "", errors.New("active OpenSSH service required; resolve it before enrollment")
}

func checkSSHD(primaryIP string) error { return checkSSHDAt("", primaryIP) }

func run(name string, args ...string) ([]byte, error) {
	program, err := nativePath(name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/bin", "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if len(out) > 1<<16 {
		return nil, errors.New("native command output exceeded bound")
	}
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return out, nil
}

func nativePath(name string) (string, error) {
	paths := map[string][]string{
		"getent": {"/usr/bin/getent"}, "id": {"/usr/bin/id"}, "sshd": {"/usr/sbin/sshd", "/usr/bin/sshd"},
		"visudo": {"/usr/sbin/visudo", "/usr/bin/visudo"}, "systemctl": {"/usr/bin/systemctl"},
		"useradd": {"/usr/sbin/useradd", "/usr/bin/useradd"}, "usermod": {"/usr/sbin/usermod", "/usr/bin/usermod"},
	}
	for _, candidate := range paths[name] {
		st, err := os.Stat(candidate)
		if err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("required native command %s is unavailable", name)
}
func secureComponents(name string) error {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return errors.New("path is not absolute and canonical")
	}
	current := "/"
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(name), "/"), "/") {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || stat.Uid != 0 || st.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("unsafe owner directory %s", current)
		}
	}
	return nil
}

func ensureExistingDir(name string) error {
	return secureComponents(filepath.Join(name, "placeholder"))
}

func ensureDir(name string, mode os.FileMode) error {
	if err := secureComponents(filepath.Join(filepath.Dir(name), "placeholder")); err != nil {
		return err
	}
	if err := os.Mkdir(name, mode); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	st, err := os.Lstat(name)
	if err != nil {
		return err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || stat.Uid != 0 || st.Mode().Perm() != mode {
		return fmt.Errorf("unsafe existing owner directory %s", name)
	}
	return nil
}

func readTrustedFile(name string, max int, executable bool) ([]byte, error) {
	if err := secureComponents(name); err != nil {
		return nil, err
	}
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Nlink != 1 || before.Size <= 0 || before.Size > int64(max) || before.Mode&0022 != 0 || (executable && before.Mode&0100 == 0) {
		return nil, errors.New("source file is not a restricted root-owned regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil || len(raw) != int(before.Size) || unix.Fstat(fd, &after) != nil || before.Ino != after.Ino || before.Dev != after.Dev || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return nil, errors.New("source file changed during read")
	}
	return raw, nil
}

func readManagedFile(name string, max int, mode os.FileMode) ([]byte, error) {
	raw, err := readTrustedFile(name, max, false)
	if err != nil {
		return nil, err
	}
	st, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if st.Mode().Perm() != mode {
		return nil, errors.New("managed file mode changed")
	}
	return raw, nil
}

func checkPublicKeyTraversal() error {
	current := "/"
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(AuthorizedKeysPath), "/"), "/") {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if err != nil {
			return err
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || owner.Uid != 0 || st.Mode().Perm()&0022 != 0 || st.Mode().Perm()&0005 != 0005 || (current == filepath.Dir(AuthorizedKeysPath) && st.Mode().Perm() != 0755) {
			return fmt.Errorf("public key directory %s is not safely traversable", current)
		}
	}
	return nil
}
func writeExactOrCreate(name string, raw []byte, mode os.FileMode) error {
	if _, err := os.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return writeExclusive(name, raw, mode)
	} else if err != nil {
		return err
	}
	existing, err := readManagedFile(name, len(raw)+1, mode)
	if err != nil || !bytes.Equal(existing, raw) {
		return fmt.Errorf("staged owner file %s changed; refusing to resume", name)
	}
	return nil
}
func writeExclusive(name string, raw []byte, mode os.FileMode) error {
	if err := secureComponents(name); err != nil {
		return err
	}
	fd, err := unix.Open(name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode))
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	dir, err := os.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
