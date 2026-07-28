package core

import (
	"regexp"
	"strings"
)

var postgresqlVersionPackage = regexp.MustCompile(`^postgresql-([0-9]+)$`)

// PostgreSQLClusterUnitForPackage maps one PGDG major package to the real
// Debian/Ubuntu cluster unit that owns that major. The apt package name is not
// a systemd unit: postgresql-17 installs postgresql@17-main. Callers must use
// the returned unit as an exact target and must not fall back to the aggregate
// postgresql.service wrapper or another installed major.
// PostgreSQLClusterUnitForPackage, tek bir PGDG major paketini o major'a ait
// gerçek Debian/Ubuntu cluster unit'ine eşler. Paket adı systemd unit'i
// değildir: postgresql-17, postgresql@17-main kurar. Çağıran tam hedefi
// kullanmalı; postgresql.service sarmalayıcısına veya başka major'a
// düşmemelidir.
func PostgreSQLClusterUnitForPackage(packageName string) (string, bool) {
	m := postgresqlVersionPackage.FindStringSubmatch(strings.TrimSpace(packageName))
	if len(m) != 2 {
		return "", false
	}
	return "postgresql@" + m[1] + "-main", true
}
