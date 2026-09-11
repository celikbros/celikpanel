//go:build linux

package main

import "os"

// installServiceMutationTestOwnership aligns the ownership contract only in
// re-executed supervisor test helpers, before package init functions run.
// installServiceMutationTestOwnership, sahiplik sözleşmesini yalnız yeniden
// çalıştırılan supervisor test yardımcılarında, paket init işlevlerinden önce
// çalıştıran kullanıcıyla eşleştirir.
var installServiceMutationTestOwnership = func() struct{} {
	if len(os.Args) > 1 && os.Args[1] == serviceMutationSupervisorMode && os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM") != "debian13-20260911" {
		serviceMutationRequiredOwnerUID = uint32(os.Getuid())
		serviceMutationRequiredOwnerGID = uint32(os.Getgid())
	}
	return struct{}{}
}()
