//go:build linux

package bindpeerinspector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const OwnerPolicyPath = "/etc/bind-peer-inspector/policy.json"

type OwnerPolicyReader struct{}

func (OwnerPolicyReader) Read(_ context.Context) (OwnerPolicyV1, string, error) {
	return readOwnerPolicyAt("/")
}

func readOwnerPolicyAt(root string) (OwnerPolicyV1, string, error) {
	if os.Geteuid() != 0 {
		return OwnerPolicyV1{}, "", errors.New("owner policy requires root")
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return OwnerPolicyV1{}, "", errors.New("owner policy root is unavailable")
	}
	defer unix.Close(rootFD)
	if !trustedParent(rootFD, false) {
		return OwnerPolicyV1{}, "", errors.New("owner policy root is unsafe")
	}
	etcFD, err := unix.Openat(rootFD, "etc", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return OwnerPolicyV1{}, "", errors.New("owner policy /etc is unavailable")
	}
	defer unix.Close(etcFD)
	if !trustedParent(etcFD, false) {
		return OwnerPolicyV1{}, "", errors.New("owner policy /etc is unsafe")
	}
	dirFD, err := unix.Openat(etcFD, "bind-peer-inspector", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return OwnerPolicyV1{}, "", errors.New("owner policy directory is unavailable")
	}
	defer unix.Close(dirFD)
	if !trustedParent(dirFD, true) {
		return OwnerPolicyV1{}, "", errors.New("owner policy directory is unsafe")
	}
	fd, err := unix.Openat(dirFD, "policy.json", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return OwnerPolicyV1{}, "", errors.New("owner policy file is unavailable")
	}
	file := os.NewFile(uintptr(fd), "owner-policy")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !trustedPolicyFile(before) {
		return OwnerPolicyV1{}, "", errors.New("owner policy file is unsafe")
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 4097))
	if readErr != nil || len(raw) == 0 || len(raw) > 4096 {
		return OwnerPolicyV1{}, "", errors.New("owner policy content is unavailable")
	}
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(dirFD, "policy.json", &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!samePolicyFile(before, after) || !samePolicyFile(after, named) || int64(len(raw)) != after.Size {
		return OwnerPolicyV1{}, "", errors.New("owner policy changed while reading")
	}
	raw = bytes.TrimSuffix(raw, []byte("\n"))
	var policy OwnerPolicyV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&policy) != nil {
		return OwnerPolicyV1{}, "", errors.New("owner policy JSON is invalid")
	}
	canonical, err := json.Marshal(policy)
	if err != nil || !bytes.Equal(raw, canonical) {
		return OwnerPolicyV1{}, "", errors.New("owner policy JSON is not canonical")
	}
	hash := sha256.Sum256(raw)
	return policy, hex.EncodeToString(hash[:]), nil
}

func trustedParent(fd int, exact bool) bool {
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 {
		return false
	}
	if exact {
		return st.Mode&0o7777 == 0o750
	}
	return st.Mode&0o022 == 0
}
func trustedPolicyFile(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&0o7777 == 0o600 &&
		st.Uid == 0 && st.Gid == 0 && st.Nlink == 1 && st.Size > 0 && st.Size <= 4096
}
func samePolicyFile(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid &&
		a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
