//go:build !linux

package main

import (
	"context"
	"fmt"
)

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

func stagePanelCertificateIssueMaterial(
	string, string, []byte, []byte, panelCertificateIssueReceipt,
) (*panelCertificateIssueStage, error) {
	return nil, fmt.Errorf("panel certificate deployment requires Linux")
}

func verifyPublishedPanelCertificateIssueReceipt(string, string, string) (bool, error) {
	return false, fmt.Errorf("panel certificate deployment requires Linux")
}

func stabilizePublishedPanelCertificateIssue() error {
	return fmt.Errorf("panel certificate deployment requires Linux")
}

func reconcilePersistedPanelCertificateIssueHost(
	context.Context, string, string, string,
) (bool, error) {
	return false, fmt.Errorf("panel certificate deployment requires Linux")
}

func clearInterruptedPanelCertificateActivation(string, string, string) error {
	return fmt.Errorf("panel certificate deployment requires Linux")
}
