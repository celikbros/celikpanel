//go:build !linux

package main

import "fmt"

func installPanelCertFiles(domain, tlsDir string) error {
	return fmt.Errorf("panel certificate deployment requires Linux")
}

func writePanelCertDeployHook(domain, tlsDir string) error {
	return fmt.Errorf("panel certificate renewal hooks require Linux")
}

func activePanelCertificateIdentity(tlsDir string) (string, bool, error) {
	return "", false, fmt.Errorf("panel certificate deployment requires Linux")
}

func deployRenewedPanelCertFiles(domain, tlsDir string) (bool, error) {
	return false, fmt.Errorf("panel certificate deployment requires Linux")
}

func withPanelCertPublishLock(action func() error) error {
	return fmt.Errorf("panel certificate deployment requires Linux")
}
