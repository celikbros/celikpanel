//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
)

var lookupDovecotUsersGroup = user.LookupGroup
var dovecotUsersEffectiveIdentity = func() (int, int) {
	return os.Geteuid(), os.Getegid()
}

// readDovecotUsersFileForMutation is the central fail-closed boundary for
// mutations of Dovecot's secret-bearing passwd-file. Content and metadata come
// from the same securely-opened descriptor; ConfigureMailStack is responsible
// for explicit create/repair and deliberately bypasses this helper.
func readDovecotUsersFileForMutation(path string, requireExists bool) ([]byte, bool, error) {
	content, mode, uid, gid, err := secureSnapshotMailFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if requireExists {
			return nil, false, fmt.Errorf("dovecot passwd-file %s is missing; run ConfigureMailStack", path)
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect dovecot passwd-file %s: %w", path, err)
	}

	expectedUID, expectedGID, err := expectedDovecotUsersOwnership(path)
	if err != nil {
		return nil, false, err
	}
	if mode != 0o640 || uid != expectedUID || gid != expectedGID {
		return nil, false, fmt.Errorf("dovecot passwd-file %s has unsafe metadata; run ConfigureMailStack", path)
	}
	return content, true, nil
}

func validateDovecotUsersFileMetadata(path string, requireExists bool) error {
	_, _, err := readDovecotUsersFileForMutation(path, requireExists)
	return err
}

func expectedDovecotUsersOwnership(path string) (int, int, error) {
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		uid, gid := dovecotUsersEffectiveIdentity()
		return uid, gid, nil
	}
	group, err := lookupDovecotUsersGroup("dovecot")
	if err != nil {
		return 0, 0, fmt.Errorf("resolve dovecot passwd-file group for %s: %w", path, err)
	}
	if group == nil {
		return 0, 0, fmt.Errorf("resolve dovecot passwd-file group for %s: empty result", path)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil || gid < 0 {
		return 0, 0, fmt.Errorf("resolve dovecot passwd-file group for %s: invalid group id", path)
	}
	return 0, gid, nil
}
