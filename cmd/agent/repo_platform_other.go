//go:build !linux

package main

import "os"

// platformRepoFileOwnerUID reports no Unix owner on non-Linux development
// hosts; managed apt repositories are Linux-only in production.
// platformRepoFileOwnerUID, Linux dışı geliştirme makinelerinde Unix sahibi
// bildirmez; yönetilen apt depoları üretimde yalnız Linux'ta çalışır.
func platformRepoFileOwnerUID(os.FileInfo) (uint32, bool) {
	return 0, false
}

// platformSyncRepoDirectory is a development-host no-op outside Linux. The
// production durability boundary is implemented in repo_platform_linux.go.
// platformSyncRepoDirectory, Linux dışı geliştirme makinelerinde işlem yapmaz.
// Üretim kalıcılık sınırı repo_platform_linux.go içinde uygulanır.
func platformSyncRepoDirectory(string) error {
	return nil
}
