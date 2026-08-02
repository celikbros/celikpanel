// Package hostingpath owns the immutable filesystem layout shared by the
// unprivileged panel and the root agent. Paths are derived from database
// identities, never from tenant-controlled text.
package hostingpath

import (
	"fmt"
	"path"
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
