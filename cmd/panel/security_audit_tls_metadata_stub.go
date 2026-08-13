//go:build !linux

package main

import "errors"

func readPinnedPanelTLSFiles(_, _ string, _, _ int64) ([]byte, []byte, error) {
	return nil, nil, errors.New("panel TLS metadata audit is unsupported on this platform")
}
