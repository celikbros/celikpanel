//go:build acceptance_license && !linux

package licensing

import (
	"errors"
	"os"
)

// Acceptance fixture guests are Linux QEMU guests; elsewhere nothing is granted.
func acceptanceMarkerOwner(os.FileInfo, uint32) error {
	return errors.New("acceptance fixture guests are Linux only")
}
