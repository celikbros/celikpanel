//go:build !linux

package main

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func inspectHostPDNSVendorUnit(
	context.Context,
	hostplatform.Profile,
) (bindSecureFileIdentity, error) {
	return bindSecureFileIdentity{},
		errors.New("secure PowerDNS vendor unit proof requires Linux")
}
