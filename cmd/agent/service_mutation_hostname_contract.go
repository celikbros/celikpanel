package main

import (
	"github.com/alicelik/celikpanel/internal/hostname"
)

func serviceMutationCanonicalFQDN(value string) bool {
	canonical, err := hostname.CanonicalFQDN(value)
	return err == nil && canonical == value
}
