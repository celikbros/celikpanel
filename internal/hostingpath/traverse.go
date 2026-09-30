package hostingpath

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

// The hosting root and the directories above it (native finding P3, 1 Oct
// 2026). A site is served only when the web server's worker account can walk
// from "/" down to its document root, and its scheduled tasks run only when
// the site account can enter its own home. On a fresh Arch server /var/www
// does not exist; the first site creation made it with the Agent's unit
// umask and group (0750 root:celikpanel), so nginx (http) answered 404 and
// crond logged "chdir failed … Permission denied" for the site user.
//
// The product owns only what it creates. Directories above the hosting base
// that already exist belong to the server owner: CelikPanel never changes
// their mode. It proves they can be traversed and, when they cannot, names
// the directory, its mode and the owner's command instead of guessing.
//
// Barındırma kökü ve üstündeki dizinler (yerel bulgu P3, 1 Eki 2026). Bir
// site ancak web sunucusunun işçi hesabı "/"den belge köküne kadar
// yürüyebildiğinde sunulur; zamanlanmış görevleri de ancak site hesabı kendi
// ev dizinine girebildiğinde çalışır. Ürün yalnız kendi oluşturduğunun
// sahibidir: var olan üst dizinlerin kipini asla değiştirmez; geçilebilir
// olduklarını kanıtlar, olmadıklarında dizini, kipini ve sahibin komutunu
// adlandırır.

const hostingBase = "/var/www/celikpanel"

// HostingBase returns the product-owned directory every site home lives
// below.
// HostingBase, her site ev dizininin altında yaşadığı ürüne ait dizini
// döndürür.
func HostingBase() string {
	return hostingBase
}

// HostingBaseAncestors returns the directories from "/" down to the parent of
// base, in walking order. For the product base that is "/", "/var" and
// "/var/www".
// HostingBaseAncestors, "/"den base'in üst dizinine kadar olan dizinleri
// yürüme sırasıyla döndürür.
func HostingBaseAncestors(base string) []string {
	clean := path.Clean(base)
	if !path.IsAbs(clean) || clean == "/" {
		return nil
	}
	ancestors := []string{"/"}
	current := ""
	parts := strings.Split(strings.TrimPrefix(path.Dir(clean), "/"), "/")
	for _, part := range parts {
		if part == "" {
			continue
		}
		current += "/" + part
		ancestors = append(ancestors, current)
	}
	return ancestors
}

// DirectoryState is what traversal needs from one directory's metadata.
// DirectoryState, geçiş denetiminin bir dizinin üst verisinden ihtiyacıdır.
type DirectoryState struct {
	Mode os.FileMode // permission bits (and the type)
	UID  uint32
	GID  uint32
}

// Account is one identity that must walk to the hosting base. AnyTenant
// stands for every site account CelikPanel creates: none of them owns a
// directory above the hosting base or belongs to its group, so only the
// "other" execute bit lets them through.
// Account, barındırma köküne yürümesi gereken bir kimliktir. AnyTenant,
// CelikPanel'in oluşturduğu her site hesabını temsil eder.
type Account struct {
	Name      string
	UID       uint32
	GIDs      []uint32
	AnyTenant bool
}

// SiteAccounts is the Account for every site user.
// SiteAccounts, her site kullanıcısı için Account'tur.
func SiteAccounts() Account {
	return Account{AnyTenant: true}
}

// CanSearch reports whether account may enter (search) a directory with this
// metadata, by the owner/group/other permission classes. POSIX ACLs are not
// read; a directory that relies on an ACL for traversal is reported blocked.
// CanSearch, hesabın bu üst veriye sahip bir dizine girip giremeyeceğini
// sahip/grup/diğer izin sınıflarıyla bildirir. POSIX ACL okunmaz.
func CanSearch(state DirectoryState, account Account) bool {
	perm := state.Mode.Perm()
	if account.AnyTenant {
		return perm&0o001 != 0
	}
	if account.UID == 0 {
		return true
	}
	if state.UID == account.UID {
		return perm&0o100 != 0
	}
	for _, gid := range account.GIDs {
		if gid == state.GID {
			return perm&0o010 != 0
		}
	}
	return perm&0o001 != 0
}

// TraversalBlock names the first directory above the hosting base that an
// account cannot traverse.
// TraversalBlock, bir hesabın geçemediği, barındırma kökünün üstündeki ilk
// dizini adlandırır.
type TraversalBlock struct {
	Directory string
	Mode      os.FileMode
	UID       uint32
	GID       uint32
	// Owner and Group are names when they resolve, otherwise numbers.
	Owner string
	Group string
	// Account is the web server account's name, or "" for site users.
	Account string
}

// ModeText is the octal permission text an owner reads in ls/stat ("0750").
// ModeText, sahibin ls/stat'ta okuduğu sekizlik izin metnidir ("0750").
func (b TraversalBlock) ModeText() string {
	return fmt.Sprintf("%04o", uint32(b.Mode.Perm()))
}

// OwnerText is "owner:group".
func (b TraversalBlock) OwnerText() string {
	return b.Owner + ":" + b.Group
}

// Command is the owner's command that allows traversal of this directory.
// Command, bu dizinden geçişe izin veren sahip komutudur.
func (b TraversalBlock) Command() string {
	return "sudo chmod 755 " + b.Directory
}

// ErrDirectoryMissing is returned by a stat function for a directory that
// does not exist yet.
var ErrDirectoryMissing = errors.New("directory does not exist")

// ProveTraversal walks dirs top-down and returns the first directory one of
// accounts cannot search. A missing directory ends the walk without a block:
// whatever creates it (the Agent, at 0755 root:root) makes it traversable.
// Any other stat error is returned; the proof is then unknown, not passed.
// ProveTraversal, dizinleri yukarıdan aşağı yürür ve hesaplardan birinin
// giremediği ilk dizini döndürür. Eksik dizin yürüyüşü engelsiz bitirir.
// Başka bir stat hatası döndürülür; kanıt o zaman bilinmez, geçmiş sayılmaz.
func ProveTraversal(
	dirs []string,
	stat func(string) (DirectoryState, error),
	accounts []Account,
) (*TraversalBlock, error) {
	for _, dir := range dirs {
		state, err := stat(dir)
		if errors.Is(err, ErrDirectoryMissing) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", dir, err)
		}
		if !state.Mode.IsDir() {
			return nil, fmt.Errorf("inspect %s: not a directory", dir)
		}
		for _, account := range accounts {
			if !CanSearch(state, account) {
				return &TraversalBlock{
					Directory: dir,
					Mode:      state.Mode,
					UID:       state.UID,
					GID:       state.GID,
					Account:   account.Name,
				}, nil
			}
		}
	}
	return nil, nil
}
