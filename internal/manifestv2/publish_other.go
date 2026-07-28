//go:build !linux && !windows

package manifestv2

import "os"

func openCatalogPublishDirectory(_ string) (*os.File, error) {
	return nil, ErrCatalogFilesystemSecurityUnavailable
}

func createCatalogBuildWorkspace(_ *os.File) (*catalogBuildWorkspace, error) {
	return nil, ErrCatalogFilesystemSecurityUnavailable
}

func cleanupCatalogBuildWorkspace(_ *catalogBuildWorkspace) error {
	return ErrCatalogFilesystemSecurityUnavailable
}

func openCatalogSigningArtifact(_ string) (*os.File, error) {
	return nil, ErrCatalogFilesystemSecurityUnavailable
}

func lockCatalogPublishDirectory(_ *os.File) error {
	return ErrCatalogFilesystemSecurityUnavailable
}

func unlockCatalogPublishDirectory(_ *os.File) {}

func linkCatalogFile(_ *os.File, _ *os.File, _ string) error {
	return ErrCatalogFilesystemSecurityUnavailable
}

func catalogFileAtMatches(_ *os.File, _ *os.File, _ string) (bool, error) {
	return false, ErrCatalogFilesystemSecurityUnavailable
}

func removeCatalogFileAt(_ *os.File, _ string) error {
	return ErrCatalogFilesystemSecurityUnavailable
}
