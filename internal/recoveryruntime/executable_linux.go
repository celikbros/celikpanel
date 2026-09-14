//go:build linux

package recoveryruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"golang.org/x/sys/unix"
	"io"
	"os"
)

// VerifyExecutingBinary binds privileged publication to the selected kit's
// recovery executable. The fixed launcher is an identical retained copy.
func (runtime *Runtime) VerifyExecutingBinary() error {
	if err := runtime.Revalidate(); err != nil {
		return err
	}
	raw, err := runtime.ManifestBytes()
	if err != nil {
		return err
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return err
	}
	// This one procfs link is kernel-owned and names this process's executable;
	// it is not a caller-supplied source path or a mutable candidate symlink.
	file, err := os.Open("/proc/self/exe")
	if err != nil {
		return fail(ReasonReadFailed)
	}
	defer file.Close()
	if err := verifyExecutableDescriptor(file, manifest.Files["bin/recovery"]); err != nil {
		return err
	}
	return runtime.Revalidate()
}
func verifyExecutableDescriptor(file *os.File, digest string) error {
	var before, after unix.Stat_t
	if unix.Fstat(int(file.Fd()), &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != 0755 || before.Uid != 0 || before.Gid != 0 || before.Nlink != 1 || before.Size <= 0 || before.Size > MaxBinarySize {
		return fail(ReasonUnsafeMetadata)
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, MaxBinarySize+1))
	if err != nil || count != before.Size {
		return fail(ReasonReadFailed)
	}
	if unix.Fstat(int(file.Fd()), &after) != nil || !sameFile(before, after) {
		return fail(ReasonChanged)
	}
	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return fail(ReasonContentMismatch)
	}
	return nil
}
