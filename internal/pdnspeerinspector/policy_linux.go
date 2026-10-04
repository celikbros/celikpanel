//go:build linux

package pdnspeerinspector

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

const OwnerPolicyPath = "/etc/pdns-peer-inspector/policy.json"

type OwnerPolicyReader struct{}

func (OwnerPolicyReader) Read(_ context.Context) (OwnerPolicyV1, string, error) {
	return readOwnerPolicyAt("/")
}
func readOwnerPolicyAt(root string) (OwnerPolicyV1, string, error) {
	fail := func() (OwnerPolicyV1, string, error) {
		return OwnerPolicyV1{}, "", errors.New("PowerDNS owner policy unavailable or unsafe")
	}
	if os.Geteuid() != 0 {
		return fail()
	}
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail()
	}
	defer unix.Close(rootFD)
	etcFD, err := unix.Openat(rootFD, "etc", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail()
	}
	defer unix.Close(etcFD)
	dirFD, err := unix.Openat(etcFD, "pdns-peer-inspector", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail()
	}
	defer unix.Close(dirFD)
	for _, item := range []struct {
		fd    int
		exact bool
	}{{rootFD, false}, {etcFD, false}, {dirFD, true}} {
		var st unix.Stat_t
		if unix.Fstat(item.fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 || (item.exact && st.Mode&07777 != 0750) {
			return fail()
		}
	}
	fd, err := unix.Openat(dirFD, "policy.json", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fail()
	}
	file := os.NewFile(uintptr(fd), "pdns-owner-policy")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !securePolicyStat(before) {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return fail()
	}
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(dirFD, "policy.json", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !samePolicyStat(before, after) || !samePolicyStat(after, named) || int64(len(raw)) != after.Size {
		return fail()
	}
	canonicalRaw := bytes.TrimSuffix(raw, []byte("\n"))
	var policy OwnerPolicyV1
	decoder := json.NewDecoder(bytes.NewReader(canonicalRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&policy) != nil {
		return fail()
	}
	canonical, err := json.Marshal(policy)
	if err != nil || !bytes.Equal(canonicalRaw, canonical) {
		return fail()
	}
	hash := sha256.Sum256(canonicalRaw)
	return policy, hex.EncodeToString(hash[:]), nil
}
func securePolicyStat(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&07777 == 0600 && st.Uid == 0 && st.Gid == 0 && st.Nlink == 1 && st.Size > 0 && st.Size <= 4096
}
func samePolicyStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
