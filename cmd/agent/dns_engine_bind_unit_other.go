//go:build !linux

package main

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func inspectHostBINDVendorFiles(
	context.Context,
	hostplatform.Profile,
) (bindVendorFilesIdentity, error) {
	return bindVendorFilesIdentity{},
		errors.New("secure BIND vendor unit proof requires Linux")
}
