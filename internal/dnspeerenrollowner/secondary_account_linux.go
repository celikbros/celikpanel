//go:build linux

package dnspeerenrollowner

import (
	"errors"
	"strconv"
	"strings"
)

func checkLockedAccount() error {
	pass, err := run("getent", "passwd", Account)
	if err != nil {
		return errors.New("dedicated account unavailable")
	}
	shadow, err := run("getent", "shadow", Account)
	if err != nil {
		return errors.New("dedicated account password state unavailable")
	}
	groups, err := run("id", "-G", Account)
	if err != nil {
		return errors.New("dedicated account group state unavailable")
	}
	group, err := run("getent", "group", Account)
	if err != nil {
		return errors.New("dedicated account primary group unavailable")
	}
	return validateRestrictedAccount(pass, shadow, groups, group)
}

func validateRestrictedAccount(pass, shadow, groups, group []byte) error {
	parts := strings.Split(strings.TrimSpace(string(pass)), ":")
	if len(parts) != 7 || parts[0] != Account || parts[5] != HomePath || parts[6] != "/bin/sh" {
		return errors.New("dedicated account identity changed")
	}
	for _, raw := range []string{parts[2], parts[3]} {
		value, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || value == 0 || strconv.FormatUint(value, 10) != raw {
			return errors.New("dedicated account has an unsafe user or group identity")
		}
	}
	fields := strings.Split(strings.TrimSpace(string(shadow)), ":")
	if len(fields) != 9 || fields[0] != Account || (!strings.HasPrefix(fields[1], "!") && !strings.HasPrefix(fields[1], "*")) {
		return errors.New("dedicated account password is not locked")
	}
	primaryGroup := strings.Split(strings.TrimSpace(string(group)), ":")
	if len(primaryGroup) != 4 || primaryGroup[0] != Account || primaryGroup[2] != parts[3] || primaryGroup[3] != "" {
		return errors.New("dedicated primary group differs from the reserved account group")
	}
	memberships := strings.Fields(string(groups))
	if len(memberships) != 1 || memberships[0] != parts[3] {
		return errors.New("dedicated account has unexpected supplementary groups; owner review is required")
	}
	return nil
}
