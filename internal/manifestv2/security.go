package manifestv2

import (
	"errors"
	"fmt"
	"runtime"
)

var (
	// ErrCatalogFilesystemSecurityUnavailable means the current platform cannot
	// prove the private catalog ACL and durability guarantees required by V2.
	// ErrCatalogFilesystemSecurityUnavailable, geçerli platformun V2'nin
	// gerektirdiği özel katalog ACL ve kalıcılık güvencelerini kanıtlayamadığını
	// belirtir.
	ErrCatalogFilesystemSecurityUnavailable = errors.New("secure catalog filesystem guarantees are unavailable")

	// ErrCatalogDigestChanged means the signing input no longer matches the
	// digest returned by BuildCatalog.
	// ErrCatalogDigestChanged, imzalama girdisinin artık BuildCatalog tarafından
	// döndürülen özetle eşleşmediğini belirtir.
	ErrCatalogDigestChanged = errors.New("catalog digest changed after build")

	// ErrCatalogPublishDurability means publication linked a destination but
	// could not prove its directory entry durable.
	// ErrCatalogPublishDurability, yayımın bir hedef bağladığını ancak dizin
	// girdisinin kalıcılığını kanıtlayamadığını belirtir.
	ErrCatalogPublishDurability = errors.New("catalog publication durability is uncertain")
)

// CatalogPublishError reports whether a failed post-link cleanup may have left
// a visible destination that requires operator inspection.
// CatalogPublishError, bağlantı sonrası başarısız temizliğin operatör incelemesi
// gerektiren görünür bir hedef bırakmış olabileceğini bildirir.
type CatalogPublishError struct {
	Path                 string
	DestinationMayRemain bool
	Cause                error
	CleanupError         error
}

func (err *CatalogPublishError) Error() string {
	message := fmt.Sprintf("%s: %v", ErrCatalogPublishDurability, err.Cause)
	if err.CleanupError != nil {
		message += fmt.Sprintf("; cleanup failed: %v", err.CleanupError)
	}
	return message
}

func (err *CatalogPublishError) Unwrap() error {
	return ErrCatalogPublishDurability
}

func requireSecureCatalogFilesystem() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf(
			"%w: catalog filesystem activation is audited only on Linux; %s is not accepted",
			ErrCatalogFilesystemSecurityUnavailable,
			runtime.GOOS,
		)
	}
	return nil
}
