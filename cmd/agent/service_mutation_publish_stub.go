//go:build !linux

package main

import "errors"

// publishInitialServiceMutationLedger fails closed where an atomic no-replace
// rename implementation has not been audited yet.
// publishInitialServiceMutationLedger, atomik üzerine-yazmayan rename uygulaması
// henüz denetlenmemiş platformlarda güvenli biçimde başarısız olur.
func publishInitialServiceMutationLedger(string, string) error {
	return errors.New("initial service mutation ledger publication is supported only on Linux")
}
