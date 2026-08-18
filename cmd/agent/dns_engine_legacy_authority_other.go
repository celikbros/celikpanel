//go:build !linux

package main

// DNS engine host switching is Linux-only. Non-Linux builds retain the
// historical unresolved compatibility state so cross-platform validation can
// exercise the pure guard contract without pretending a durable host receipt
// exists.
func inspectLegacyPowerDNSDurableAuthorityOnHost(requireResolved bool) error {
	return validateLegacyPowerDNSDurableAuthority(
		dnsEngineStateReceipt{}, false, false, requireResolved,
	)
}

func inspectLegacyPowerDNSMutationAuthorityOnHost(requireResolved bool) error {
	return validateLegacyPowerDNSMutationAuthority(
		dnsEngineStateReceipt{}, false, false, requireResolved,
	)
}
