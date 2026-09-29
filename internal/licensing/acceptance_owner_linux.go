//go:build acceptance_license && linux

package licensing

import (
	"errors"
	"os"
	"syscall"
)

func acceptanceMarkerOwner(info os.FileInfo, want uint32) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != want {
		return errors.New("guest marker is not owned by root")
	}
	return nil
}
