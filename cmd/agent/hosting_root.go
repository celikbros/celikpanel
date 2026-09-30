package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Hosting root preparation before a site is created (native finding P3,
// 1 Oct 2026). The directories above the hosting base (/var/www/celikpanel)
// decide whether nginx and the site users can reach any site file at all.
//
//   - Missing ones are the product's to create: exactly 0755 root:root,
//     set explicitly after mkdir because the Agent runs with UMask=0027 and
//     Group=celikpanel (mkdir alone produced 0750 root:celikpanel /var/www on
//     Arch, and every site answered 404). Each directory it creates is
//     recorded in a root-only receipt.
//   - Existing ones are the owner's. They are never changed. When one blocks
//     the web server account or the site users, the site is refused before
//     any change with HostingRootNotTraversable, naming the directory, its
//     mode and the owner's command (D-022, D-024).
//
// A receipt never authorizes a repair: a directory the product created at
// 0755 that is no longer traversable was changed afterwards, which is an
// owner change to report, not to overwrite.
//
// Site oluşturulmadan önce barındırma kökünün hazırlanması (yerel bulgu P3).
// Eksik üst dizinleri ürün oluşturur: tam olarak 0755 root:root, mkdir'den
// sonra açıkça ayarlanır (Agent UMask=0027 ve Group=celikpanel ile çalışır).
// Oluşturduğu her dizini yalnız root'un okuyabildiği bir makbuza yazar. Var
// olan dizinler sahibindir ve asla değiştirilmez; engellediklerinde site hiçbir
// değişiklikten önce dizin, kip ve sahibin komutuyla reddedilir. Makbuz asla
// onarım yetkisi vermez.

const (
	hostingRootReceiptName   = "hosting-root-v1.json"
	hostingRootReceiptSchema = "celikpanel/hosting-root-directories/v1"
)

type hostingRootReceipt struct {
	Schema      string                        `json:"schema"`
	Directories []hostingRootReceiptDirectory `json:"directories"`
}

type hostingRootReceiptDirectory struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	UID       int    `json:"uid"`
	GID       int    `json:"gid"`
	CreatedAt string `json:"created_at"`
}

type hostingRootPreparer struct {
	base         string
	receiptPath  string
	uid, gid     int
	stat         func(string) (hostingpath.DirectoryState, error)
	accounts     func() []hostingpath.Account
	nameOwners   func(*hostingpath.TraversalBlock)
	readReceipt  func(string) ([]byte, error)
	writeReceipt func(string, []byte) error
	now          func() time.Time
}

func defaultHostingRootPreparer() hostingRootPreparer {
	return hostingRootPreparer{
		base:        hostingpath.HostingBase(),
		receiptPath: filepath.Join(serviceMutationStateDirectory(), hostingRootReceiptName),
		uid:         0,
		gid:         0,
		stat:        hostingpath.StatDirectory,
		accounts:    hostingpath.TraversalAccounts,
		nameOwners:  hostingpath.NameOwners,
		readReceipt: secureReadConfig,
		writeReceipt: func(path string, content []byte) error {
			return secureWriteConfigOwnedBy(path, content, 0o600, 0, 0)
		},
		now: time.Now,
	}
}

// prepareHostingRoot runs the production preparation.
func prepareHostingRoot() (*hostingpath.TraversalBlock, error) {
	return defaultHostingRootPreparer().prepare()
}

// prepare proves the existing directories first, so a refusal changes
// nothing, then creates what is missing. The walk stops at the first missing
// directory; everything below it is created here at 0755.
// prepare önce var olan dizinleri kanıtlar, böylece ret hiçbir şeyi
// değiştirmez; sonra eksikleri oluşturur.
func (p hostingRootPreparer) prepare() (*hostingpath.TraversalBlock, error) {
	ancestors := hostingpath.HostingBaseAncestors(p.base)
	if len(ancestors) == 0 {
		return nil, fmt.Errorf("hosting base %q is not an absolute directory", p.base)
	}
	block, err := hostingpath.ProveTraversal(ancestors, p.stat, p.accounts())
	if err != nil {
		return nil, err
	}
	if block != nil {
		if p.nameOwners != nil {
			p.nameOwners(block)
		}
		return block, nil
	}

	var created []hostingRootReceiptDirectory
	for _, dir := range append(ancestors[1:], filepath.Clean(p.base)) {
		made, err := p.createIfMissing(dir)
		if err != nil {
			p.record(created)
			return nil, err
		}
		if made {
			created = append(created, hostingRootReceiptDirectory{
				Path:      dir,
				Mode:      "0755",
				UID:       p.uid,
				GID:       p.gid,
				CreatedAt: p.now().UTC().Format(time.RFC3339),
			})
		}
	}
	p.record(created)
	return nil, nil
}

// createIfMissing makes one directory at exactly 0755 uid:gid. An existing
// entry is left as it is: a directory (or a link the owner made) is the
// owner's, anything else stops preparation.
func (p hostingRootPreparer) createIfMissing(dir string) (bool, error) {
	info, err := os.Lstat(dir)
	if err == nil {
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, nil
		}
		return false, fmt.Errorf("%s exists and is not a directory", dir)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect %s: %w", dir, err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("create %s: %w", dir, err)
	}
	// The unit umask and group apply to mkdir; set both explicitly.
	// Birim umask'i ve grubu mkdir'e uygulanır; ikisi de açıkça ayarlanır.
	if err := os.Chown(dir, p.uid, p.gid); err != nil {
		return true, fmt.Errorf("set owner of %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return true, fmt.Errorf("set mode of %s: %w", dir, err)
	}
	return true, nil
}

// record appends created directories to the receipt. A receipt that cannot be
// written is logged, not fatal: the directory is already traversable, and a
// missing receipt only means a later check treats it as the owner's.
// record, oluşturulan dizinleri makbuza ekler. Yazılamayan makbuz günlüğe
// yazılır, ölümcül değildir.
func (p hostingRootPreparer) record(created []hostingRootReceiptDirectory) {
	if len(created) == 0 || p.writeReceipt == nil {
		return
	}
	receipt := hostingRootReceipt{Schema: hostingRootReceiptSchema}
	if p.readReceipt != nil {
		if data, err := p.readReceipt(p.receiptPath); err == nil {
			var previous hostingRootReceipt
			if json.Unmarshal(data, &previous) == nil && previous.Schema == hostingRootReceiptSchema {
				receipt.Directories = previous.Directories
			} else {
				log.Printf("hosting root receipt %s is unreadable; starting a new one", p.receiptPath)
			}
		}
	}
	for _, entry := range created {
		if !slices.ContainsFunc(receipt.Directories, func(existing hostingRootReceiptDirectory) bool {
			return existing.Path == entry.Path
		}) {
			receipt.Directories = append(receipt.Directories, entry)
		}
		log.Printf("hosting root: created %s at 0755 %d:%d", entry.Path, entry.UID, entry.GID)
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err == nil {
		err = p.writeReceipt(p.receiptPath, append(data, '\n'))
	}
	if err != nil {
		log.Printf("hosting root: record created directories in %s: %v", p.receiptPath, err)
	}
}

// hostingRootRefusal is the Agent's English line for logs and older Panels;
// the Panel answers with its own guidance from the structured block.
// hostingRootRefusal, günlükler ve eski Paneller için Agent'ın İngilizce
// satırıdır; Panel yapılandırılmış bloktan kendi yönlendirmesini üretir.
func hostingRootRefusal(block *hostingpath.TraversalBlock) string {
	who := "the site users"
	if block.Account != "" {
		who = "the web server (" + block.Account + ")"
	}
	return fmt.Sprintf(
		"site refused before any change: %s cannot pass %s (mode %s, owner %s); "+
			"CelikPanel does not change this directory; the server owner can allow it with: %s",
		who, block.Directory, block.ModeText(), block.OwnerText(), block.Command(),
	)
}

func hostingRootTransportBlock(block *hostingpath.TraversalBlock) *transport.HostingRootBlock {
	return &transport.HostingRootBlock{
		Directory: block.Directory,
		Mode:      block.ModeText(),
		Owner:     block.OwnerText(),
		Account:   block.Account,
	}
}
