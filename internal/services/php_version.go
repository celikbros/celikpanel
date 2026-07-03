package services

import (
	"fmt"
	"regexp"
)

// phpVersionPattern matches a PHP version like "8.3". Anything else is
// rejected before it can be interpolated into a filesystem path.
// phpVersionPattern, "8.3" gibi bir PHP sürümüyle eşleşir. Başka her şey,
// bir dosya sistemi yoluna gömülmeden önce reddedilir.
var phpVersionPattern = regexp.MustCompile(`^[0-9]{1,2}\.[0-9]{1,2}$`)

// ValidatePHPVersion guards the version component of paths such as
// /etc/php/<version>/fpm/php.ini. Because the agent writes these as root,
// an unvalidated version (e.g. "../../../etc/cron.d/x") would be an
// arbitrary-file-write as root. Callers must validate before building the
// path.
//
// ValidatePHPVersion, /etc/php/<version>/fpm/php.ini gibi yolların sürüm
// bileşenini korur. Agent bunları root olarak yazdığından, doğrulanmamış
// bir sürüm (örn. "../../../etc/cron.d/x") root olarak keyfi-dosya-yazma
// olurdu. Çağıranlar yolu oluşturmadan önce doğrulamalıdır.
func ValidatePHPVersion(version string) error {
	if !phpVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid PHP version %q", version)
	}
	return nil
}
