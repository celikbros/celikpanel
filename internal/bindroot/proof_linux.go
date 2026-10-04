//go:build linux

package bindroot

import (
	"errors"
	"fmt"
	"strings"
)

// APTExactStatOverrideLine is the dpkg statoverride earlier releases
// registered for the package-owned BIND working directory. Current releases
// never register it. The entry names the package's `bind` group; bind9's postrm
// deletes that user and group on purge, and from then on dpkg refuses every
// package operation on the host ("unknown system group 'bind' in statoverride
// file"). A `#GID` registration does not avoid this: dpkg-statoverride stores
// it by name whenever the id resolves. dpkg keeps an existing directory's
// owner and mode across package upgrades without an override, and the
// product re-asserts the mode itself on every mutating root proof. Hosts
// installed by earlier releases keep the entry: those releases require it
// after an owner rollback. It is accepted on every path and left in place;
// only the package preflight removes it, once its group is gone. Any other
// entry belongs to the owner and is never changed.
//
// APTExactStatOverrideLine, önceki sürümlerin paketin BIND çalışma dizini için
// kaydettiği dpkg statoverride satırıdır. Güncel sürümler bunu hiç kaydetmez.
// Satır paketin `bind` grubunu adlandırır; bind9'un postrm betiği purge
// sırasında bu kullanıcıyı ve grubu siler ve o andan sonra dpkg sunucudaki her
// paket işlemini reddeder. `#GID` biçimi bunu önlemez: dpkg-statoverride kimlik
// çözülebildiğinde onu adla saklar. dpkg, var olan bir dizinin sahibini ve
// kipini paket yükseltmelerinde geçersiz kılma olmadan da korur; ürün kipi her
// değiştiren kök kanıtında kendisi yeniden kurar. Önceki sürümlerin kurduğu
// sunucular girdiyi korur; her yolda kabul edilir ve yerinde bırakılır. Yalnız
// paket ön denetimi, grubu silinmişse onu kaldırır. Başka her girdi sahibindir.
const (
	APTExactStatOverrideLine = "root bind 1775 /var/cache/bind\n"
	APTExactPackageOwnerLine = "bind9: /var/cache/bind\n"
	APTStatOverridePath      = "/var/cache/bind"
)

type APTStatOverrideState uint8

const (
	// APTStatOverrideAbsent is the current supported state.
	APTStatOverrideAbsent APTStatOverrideState = iota
	// APTStatOverrideExact is the product's own legacy entry, accepted on
	// every path and left in place.
	APTStatOverrideExact
)

type commandExitCoder interface {
	ExitCode() int
}

// ClassifyAPTStatOverride distinguishes a genuinely absent override and the
// product's exact legacy override from a conflicting or failed query. Both
// classified states satisfy the read-only root proof; a conflicting entry
// belongs to the owner and is never changed by the product.
func ClassifyAPTStatOverride(output []byte, commandErr error) (APTStatOverrideState, error) {
	if commandErr == nil && string(output) == APTExactStatOverrideLine {
		return APTStatOverrideExact, nil
	}
	var exitCoder commandExitCoder
	if len(output) == 0 && errors.As(commandErr, &exitCoder) && exitCoder.ExitCode() == 1 {
		return APTStatOverrideAbsent, nil
	}
	return APTStatOverrideAbsent, errors.New(
		"dpkg-statoverride returned a conflicting, redirected, or non-canonical /var/cache/bind result",
	)
}

func VerifyAPTPackageOwner(output []byte, commandErr error) error {
	if commandErr != nil {
		return fmt.Errorf("verify /var/cache/bind package ownership: %w", commandErr)
	}
	if string(output) != APTExactPackageOwnerLine {
		return errors.New("/var/cache/bind is not the exact bind9 package-owned directory")
	}
	return nil
}

// VerifyPacmanPackageOwner accepts exactly one canonical pacman ownership line.
func VerifyPacmanPackageOwner(output []byte, commandErr error) error {
	if commandErr != nil {
		return fmt.Errorf("verify /var/named package ownership: %w", commandErr)
	}
	line := string(output)
	const prefix = "/var/named/ is owned by bind "
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		return errors.New("/var/named is not the exact bind package-owned directory")
	}
	version := strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\n")
	if version == "" || strings.ContainsAny(version, " \t") ||
		strings.Trim(version, "0123456789.:-+abcdefghijklmnopqrstuvwxyz_") != "" {
		return errors.New("/var/named package ownership version is not canonical")
	}
	return nil
}
