// Package hostingpath owns the immutable filesystem layout shared by the
// unprivileged panel and the root agent. Paths are derived from database
// identities, never from tenant-controlled text.
package hostingpath

import (
	"fmt"
	"path"
	"strings"
)

const (
	subscriptionsRoot        = "/var/www/celikpanel/subscriptions"
	acmeChallengeBaseRoot    = "/var/lib/celikpanel-agent/acme-http-01"
	serviceMutationStateRoot = "/var/lib/celikpanel-agent-private"
)

// ServiceMutationStateRoot returns the root-only state directory used by privileged service mutations.
// ServiceMutationStateRoot, ayrıcalıklı servis işlemlerinin kullandığı yalnızca root erişimli durum dizinini döndürür.
func ServiceMutationStateRoot() string {
	return serviceMutationStateRoot
}

// SubscriptionsRoot returns the parent every tenant site home lives under.
// Callers use it to prove that an account named in a request is a tenant site
// account and not a system or administrative one, before acting as root on its
// behalf.
// SubscriptionsRoot, her kiracı site ev dizininin altında yaşadığı üst dizini
// döndürür. Çağıranlar, bir istekte adı geçen hesabın sistem ya da yönetim
// hesabı değil bir kiracı site hesabı olduğunu, onun adına root olarak işlem
// yapmadan önce kanıtlamak için kullanır.
func SubscriptionsRoot() string {
	return subscriptionsRoot
}

// IsSiteHome reports whether an account home directory lies inside the hosting
// root. It is a containment test on an already-cleaned absolute path, not a
// permission check.
// IsSiteHome, bir hesabın ev dizininin barındırma kökünün içinde olup
// olmadığını bildirir. Zaten temizlenmiş mutlak bir yol üzerinde bir kapsama
// testidir, izin denetimi değil.
func IsSiteHome(home string) bool {
	if home == "" {
		return false
	}
	cleaned := path.Clean(home)
	return cleaned != subscriptionsRoot &&
		strings.HasPrefix(cleaned, subscriptionsRoot+"/")
}

func SiteHome(subscriptionID, domainID int) (string, error) {
	if subscriptionID <= 0 || domainID <= 0 {
		return "", fmt.Errorf("subscription and domain IDs must be positive")
	}
	return path.Join(
		subscriptionsRoot,
		fmt.Sprintf("%d", subscriptionID),
		"sites",
		fmt.Sprintf("%d", domainID),
	), nil
}

func DocumentRoot(subscriptionID, domainID int) (string, error) {
	home, err := SiteHome(subscriptionID, domainID)
	if err != nil {
		return "", err
	}
	return path.Join(home, "public_html"), nil
}

func ValidateDocumentRoot(candidate string, subscriptionID, domainID int) error {
	expected, err := DocumentRoot(subscriptionID, domainID)
	if err != nil {
		return err
	}
	if candidate != expected {
		return fmt.Errorf("document root must be %s", expected)
	}
	return nil
}

// ACMEChallengeRoot returns the root-owned HTTP-01 webroot for one immutable
// subscription/domain identity. It deliberately lives outside every tenant
// home: certbot runs as root and must never write challenge files into
// tenant-owned public_html.
func ACMEChallengeRoot(subscriptionID, domainID int) (string, error) {
	if subscriptionID <= 0 || domainID <= 0 {
		return "", fmt.Errorf("subscription and domain IDs must be positive")
	}
	return path.Join(
		acmeChallengeBaseRoot,
		"subscriptions",
		fmt.Sprintf("%d", subscriptionID),
		"domains",
		fmt.Sprintf("%d", domainID),
	), nil
}

// PanelACMEChallengeRoot returns the fixed, root-owned HTTP-01 webroot used
// for the panel hostname. It is deliberately outside tenant document roots
// and has no user-controlled path component.
func PanelACMEChallengeRoot() string {
	return path.Join(acmeChallengeBaseRoot, "panel")
}

func ValidateACMEChallengeRoot(candidate string, subscriptionID, domainID int) error {
	expected, err := ACMEChallengeRoot(subscriptionID, domainID)
	if err != nil {
		return err
	}
	if candidate != expected {
		return fmt.Errorf("ACME challenge root must be %s", expected)
	}
	return nil
}
