//go:build !linux

package main

import (
	"errors"
	"time"
)

func readPanelCertificateSource(string) (
	certificate, privateKey, leafDER []byte,
	notAfter time.Time,
	err error,
) {
	return nil, nil, nil, time.Time{}, errors.New(
		"secure panel certificate source access requires Linux openat2",
	)
}
