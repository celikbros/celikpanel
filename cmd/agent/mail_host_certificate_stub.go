//go:build !linux

package main

import (
	"context"
	"errors"
	"github.com/alicelik/celikpanel/internal/transport"
	"time"
)

func mailHostLinuxOnly() error { return errors.New("mail host certificates require Linux") }
func readMailHostCertificateSource(string) ([]byte, []byte, []byte, time.Time, error) {
	return nil, nil, nil, time.Time{}, mailHostLinuxOnly()
}
func stageMailHostCertificateMaterial(string, string, []byte, []byte, mailHostCertificateReceipt) (*mailHostCertificateStage, error) {
	return nil, mailHostLinuxOnly()
}
func verifyPublishedMailHostCertificateReceipt(string, string, string) (bool, error) {
	return false, mailHostLinuxOnly()
}
func stabilizePublishedMailHostCertificate() error { return mailHostLinuxOnly() }
func reconcilePersistedMailHostCertificateHost(context.Context, string, string, string) (bool, error) {
	return false, mailHostLinuxOnly()
}
func selectedMailHostCertificate(string) (string, string, error) {
	return defaultMailCert, defaultMailKey, nil
}
func runMailHostCertificateCommand(context.Context, string, ...string) ([]byte, error) {
	return nil, mailHostLinuxOnly()
}
func writeMailHostCertificateDeployHook() error                   { return mailHostLinuxOnly() }
func currentMailHostCertificateIdentity() (string, string, error) { return "", "", mailHostLinuxOnly() }
func queueMailHostCertificateRenewal(string) error                { return mailHostLinuxOnly() }
func clearMailHostCertificateRenewal(mailHostRenewal) error       { return mailHostLinuxOnly() }
func (a *Agent) MailHostCertificateStatus(_ *transport.MailHostCertificateStatusRequest, resp *transport.MailHostCertificateStatusResponse) error {
	*resp = transport.MailHostCertificateStatusResponse{Error: mailHostLinuxOnly().Error()}
	return nil
}
