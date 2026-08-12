//go:build !linux

package main

import "errors"

func newPlatformSystemUpdateService() (*systemUpdateService, error) {
	platformOS, platformArch := runtimeSystemUpdatePlatform()
	return newSystemUpdateService(nil, nil, platformOS, platformArch), nil
}

func runSystemUpdateWorker(string) error {
	return errors.New("system updates are supported only on Linux")
}
