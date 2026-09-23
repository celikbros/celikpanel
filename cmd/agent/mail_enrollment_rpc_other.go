//go:build !linux

package main

import "github.com/alicelik/celikpanel/internal/transport"

func (a *Agent) StartMailEnrollmentV1(req *transport.MailEnrollmentStartRequest, _ *transport.MailEnrollmentStartResponse) error {
	if req != nil {
		if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "mail renewal enrollment"); err != nil {
			return err
		}
	}
	return mailHostLinuxOnly()
}
func (a *Agent) MailEnrollmentStatusV1(*transport.MailEnrollmentRequest, *transport.MailEnrollmentStatusResponse) error {
	return mailHostLinuxOnly()
}

func (a *Agent) ContinueMailEnrollmentV1(req *transport.MailEnrollmentStartRequest, resp *transport.MailEnrollmentStartResponse) error {
	return a.StartMailEnrollmentV1(req, resp)
}

func (a *Agent) MailEnrollmentSourceV1(*transport.MailEnrollmentSourceRequest, *transport.MailEnrollmentSourceResponse) error {
	return mailHostLinuxOnly()
}

func (a *Agent) MailEnrollmentPreviewV1(*transport.MailEnrollmentSourceRequest, *transport.MailEnrollmentPreviewResponse) error {
	return mailHostLinuxOnly()
}
