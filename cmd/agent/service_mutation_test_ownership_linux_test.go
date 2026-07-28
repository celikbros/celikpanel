//go:build linux

package main

import "os"

// installServiceMutationTestOwnership aligns the test-only ownership contract
// before package init functions run, including re-executed supervisor helpers.
// installServiceMutationTestOwnership, yeniden çalıştırılan supervisor
// yardımcıları dahil paket init işlevlerinden önce test sahiplik sözleşmesini
// çalıştıran kullanıcıyla eşleştirir.
var installServiceMutationTestOwnership = func() struct{} {
	serviceMutationRequiredOwnerUID = uint32(os.Getuid())
	serviceMutationRequiredOwnerGID = uint32(os.Getgid())
	return struct{}{}
}()
