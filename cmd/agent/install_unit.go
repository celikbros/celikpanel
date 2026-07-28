package main

import "github.com/alicelik/celikpanel/internal/core"

// exactInstallUnit returns the unit that a version-picked install must target.
// The boolean says the request is exact-targeted; an empty unit with true is
// an invalid exact request and must fail rather than fall back to another unit.
//
// exactInstallUnit, sürümü seçilmiş bir kurulumun hedeflemesi gereken tam
// unit'i döndürür. Boolean, isteğin exact-targeted olduğunu söyler; true ile
// birlikte boş unit geçersiz bir exact istektir ve başka unit'e düşmek yerine
// başarısız olmalıdır.
func exactInstallUnit(serviceID, family, packageName string) (string, bool) {
	if family != "apt" || packageName == "" {
		return "", false
	}
	switch serviceID {
	case "php-fpm":
		// Debian/Sury versioned PHP-FPM packages intentionally use their
		// package name as the systemd unit name (php8.3-fpm).
		// Debian/Sury sürümlü PHP-FPM paketleri paket adını bilerek systemd
		// unit adı olarak kullanır (php8.3-fpm).
		return packageName, true
	case "postgresql":
		unit, ok := core.PostgreSQLClusterUnitForPackage(packageName)
		if !ok {
			return "", true
		}
		return unit, true
	default:
		return "", false
	}
}
