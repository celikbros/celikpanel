//go:build !linux

package main

func readPanelCertificateActivationState() (
	panelCertificateActivationState,
	bool,
	error,
) {
	return panelCertificateActivationState{}, false, errPanelCertificateActivationUnsupported
}

func writePanelCertificateActivationState(
	panelCertificateActivationState,
) error {
	return errPanelCertificateActivationUnsupported
}

func removePanelCertificateActivationState() error {
	return errPanelCertificateActivationUnsupported
}
