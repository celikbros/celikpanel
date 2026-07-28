//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

// platformRepoFileOwnerUID returns the Unix owner used to enforce that apt
// trust files and transaction journals remain root-owned.
// platformRepoFileOwnerUID, apt güven dosyaları ile işlem günlüklerinin root
// sahipliğinde kalmasını zorlamak için kullanılan Unix sahibini döndürür.
func platformRepoFileOwnerUID(info os.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// Linux ownership metadata must fail closed: an unknown owner can never
		// be accepted as root for an apt trust file.
		// Linux sahiplik metaverisi kapali basarisiz olmalidir: bilinmeyen bir
		// sahip, apt guven dosyasi icin asla root olarak kabul edilemez.
		return ^uint32(0), true
	}
	return stat.Uid, true
}

// platformSyncRepoDirectory makes preceding renames and removals durable across
// power loss by synchronizing the parent directory entry changes.
// platformSyncRepoDirectory, üst dizin girdisi değişikliklerini eşitleyerek
// önceki rename ve kaldırmaları güç kaybına karşı kalıcı hâle getirir.
func platformSyncRepoDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}
