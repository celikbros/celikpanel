//go:build unix

package hostingpath

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// StatDirectory reads one directory's mode and numeric owner, following a
// final symbolic link like the kernel does during path traversal.
// StatDirectory, bir dizinin kipini ve sayısal sahibini okur.
func StatDirectory(dir string) (DirectoryState, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return DirectoryState{}, ErrDirectoryMissing
	}
	if err != nil {
		return DirectoryState{}, err
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return DirectoryState{}, fmt.Errorf("no owner information for %s", dir)
	}
	return DirectoryState{Mode: info.Mode(), UID: sys.Uid, GID: sys.Gid}, nil
}

// webServerAccountNames: Debian www-data · RHEL nginx · Arch http. The first
// that exists is the account nginx's workers run as on this server.
var webServerAccountNames = []string{"www-data", "nginx", "http"}

// WebServerAccount returns the web server's worker account, or false when
// none exists yet (nginx not installed).
// WebServerAccount, web sunucusunun işçi hesabını döndürür; henüz yoksa false.
func WebServerAccount() (Account, bool) {
	for _, name := range webServerAccountNames {
		account, err := user.Lookup(name)
		if err != nil {
			continue
		}
		uid, err := strconv.ParseUint(account.Uid, 10, 32)
		if err != nil {
			continue
		}
		result := Account{Name: name, UID: uint32(uid)}
		groups, _ := account.GroupIds()
		if len(groups) == 0 {
			groups = []string{account.Gid}
		}
		for _, group := range groups {
			if gid, err := strconv.ParseUint(group, 10, 32); err == nil {
				result.GIDs = append(result.GIDs, uint32(gid))
			}
		}
		return result, true
	}
	return Account{}, false
}

// TraversalAccounts is the web server account (when present) and every site
// user.
// TraversalAccounts, web sunucusu hesabı (varsa) ve her site kullanıcısıdır.
func TraversalAccounts() []Account {
	accounts := []Account{}
	if web, ok := WebServerAccount(); ok {
		accounts = append(accounts, web)
	}
	return append(accounts, SiteAccounts())
}

// NameOwners fills Owner and Group with names where they resolve.
// NameOwners, çözülebilen yerlerde Owner ve Group'u adlarla doldurur.
func NameOwners(block *TraversalBlock) {
	if block == nil {
		return
	}
	block.Owner = strconv.FormatUint(uint64(block.UID), 10)
	block.Group = strconv.FormatUint(uint64(block.GID), 10)
	if account, err := user.LookupId(block.Owner); err == nil {
		block.Owner = account.Username
	}
	if group, err := user.LookupGroupId(block.Group); err == nil {
		block.Group = group.Name
	}
}

// ProbeHostingRoot is the read-only proof over the directories above the
// product hosting base, for the web server account and every site user.
// ProbeHostingRoot, ürün barındırma kökünün üstündeki dizinler için web
// sunucusu hesabı ve her site kullanıcısı adına salt-okur kanıttır.
func ProbeHostingRoot() (*TraversalBlock, error) {
	block, err := ProveTraversal(HostingBaseAncestors(hostingBase), StatDirectory, TraversalAccounts())
	NameOwners(block)
	return block, err
}
