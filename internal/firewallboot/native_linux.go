//go:build linux

package firewallboot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alicelik/celikpanel/internal/firewalllock"
	"github.com/alicelik/celikpanel/internal/firewallpolicy"
	"github.com/alicelik/celikpanel/internal/hostcmd"
	"golang.org/x/sys/unix"
)

const policyPath = "/etc/celikpanel/firewall.nft"

type nativeHost struct{}

// Restore reads the fixed existing policy; it never creates or rewrites it.
func Restore(ctx context.Context, checkOnly bool) (Result, error) {
	if os.Geteuid() != 0 {
		return Result{}, errors.New("root or authorized sudo is required")
	}
	initial, err := (nativeHost{}).load()
	if err != nil || !initial.exists {
		return Result{}, err
	}
	lock, err := firewalllock.Acquire()
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	return restore(ctx, nativeHost{}, checkOnly)
}
func (nativeHost) load() (snapshot, error) { return readSnapshot(policyPath) }
func readSnapshot(path string) (snapshot, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return snapshot{}, errors.New("noncanonical policy path")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return snapshot{}, err
	}
	defer func() { unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	// Installer parents may be root:celikpanel 0750. Group read/traverse
	// does not confer write authority; require root ownership and no other writer.
	var st unix.Stat_t
	for _, name := range parts[:len(parts)-1] {
		if err = unix.Fstat(fd, &st); err != nil || st.Uid != 0 || st.Mode&0022 != 0 {
			return snapshot{}, errors.New("unsafe policy directory")
		}
		next, e := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) {
			return snapshot{}, nil
		}
		if e != nil {
			return snapshot{}, e
		}
		unix.Close(fd)
		fd = next
	}
	if err = unix.Fstat(fd, &st); err != nil || st.Uid != 0 || st.Mode&0022 != 0 {
		return snapshot{}, errors.New("unsafe policy parent")
	}
	filefd, err := unix.Openat(fd, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return snapshot{}, nil
	}
	if err != nil {
		return snapshot{}, err
	}
	file := os.NewFile(uintptr(filefd), "saved firewall policy")
	defer file.Close()
	if err = unix.Fstat(filefd, &st); err != nil {
		return snapshot{}, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&07777 != 0600 || st.Nlink != 1 || st.Size < 1 || st.Size > firewallpolicy.MaxSnapshotSize {
		return snapshot{}, errors.New("unsafe saved firewall policy metadata")
	}
	data, err := io.ReadAll(io.LimitReader(file, firewallpolicy.MaxSnapshotSize+1))
	if err != nil {
		return snapshot{}, err
	}
	var after unix.Stat_t
	if err = unix.Fstat(filefd, &after); err != nil || st.Dev != after.Dev || st.Ino != after.Ino || st.Mode != after.Mode || st.Uid != after.Uid || st.Gid != after.Gid || st.Nlink != after.Nlink || st.Size != after.Size || st.Mtim != after.Mtim || st.Ctim != after.Ctim || int64(len(data)) != st.Size {
		return snapshot{}, errors.New("saved firewall policy changed while reading")
	}
	return snapshot{data: data, exists: true, identity: fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d:%d", st.Dev, st.Ino, st.Mtim.Sec, st.Mtim.Nsec, st.Ctim.Sec, st.Ctim.Nsec, st.Mode, st.Size)}, nil
}

var commands = map[string][]string{"nft": {"/usr/sbin/nft", "/usr/bin/nft"}, "systemctl": {"/usr/bin/systemctl"}, "sshd": {"/usr/sbin/sshd", "/usr/bin/sshd", "/usr/local/sbin/sshd"}}

func trusted(name string) (string, error) {
	for _, path := range commands[name] {
		if trustedFile(path) == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no trusted %s executable is available", name)
}
func trustedFile(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || st.Gid != 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return errors.New("unsafe host executable ownership/path")
		}
		if current == path {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				return errors.New("host executable unavailable")
			}
		} else if !info.IsDir() {
			return errors.New("host executable parent is not a directory")
		}
		if current == "/" {
			break
		}
	}
	return nil
}

type boundedOutput struct {
	data     []byte
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 65536 {
		b.overflow = true
		return 0, errors.New("host output exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (nativeHost) command(ctx context.Context, name string, args []string, input string) ([]byte, error) {
	path, err := trusted(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C", "LC_ALL=C"}
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out := &boundedOutput{}
	cmd.Stdout = out
	cmd.Stderr = out
	err = cmd.Run()
	if out.overflow {
		return nil, errors.New("host command output exceeded limit")
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %s", name, hostcmd.Reason(out.data, err))
	}
	return out.data, nil
}
func (h nativeHost) ssh(ctx context.Context) ([]int, error) {
	out, err := h.command(ctx, "sshd", []string{"-T"}, "")
	if err != nil {
		// Unknown is not absence. Only a missing executable plus all four genuinely
		// absent native units permits a reviewed no-SSH host to restore its policy.
		for _, path := range commands["sshd"] {
			if _, e := os.Lstat(path); !errors.Is(e, os.ErrNotExist) {
				return nil, err
			}
		}
		for _, unit := range []string{"ssh.service", "sshd.service", "ssh.socket", "sshd.socket"} {
			state, e := h.command(ctx, "systemctl", []string{"show", "--no-pager", "--property=LoadState", "--value", unit}, "")
			if e != nil || strings.TrimSpace(string(state)) != "not-found" {
				return nil, err
			}
		}
		return nil, nil
	}
	ports, err := firewallpolicy.ParseSSHConfigPorts(out)
	if err != nil {
		return nil, err
	}
	for _, unit := range []string{"ssh.socket", "sshd.socket"} {
		state, e := h.command(ctx, "systemctl", []string{"show", "--no-pager", "--property=LoadState", "--value", unit}, "")
		if e != nil {
			return nil, e
		}
		switch strings.TrimSpace(string(state)) {
		case "not-found":
			continue
		case "loaded":
		default:
			return nil, errors.New("SSH socket state unavailable")
		}
		listeners, e := h.command(ctx, "systemctl", []string{"show", "--no-pager", "--property=Listen", "--value", unit}, "")
		if e != nil {
			return nil, e
		}
		socketPorts, e := firewallpolicy.ParseSocketListenPorts(listeners)
		if e != nil {
			return nil, e
		}
		if len(socketPorts) == 0 {
			return nil, fmt.Errorf("%s is loaded but exposes no Stream listener", unit)
		}
		ports = append(ports, socketPorts...)
	}
	return ports, nil
}
