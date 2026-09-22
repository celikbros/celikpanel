//go:build linux

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

func TestMailEnrollmentNativeCommandBoundary(t *testing.T) {
	for _, pair := range [][2]string{{"restart", mailrenewalkit.TimerName}, {"start", mailrenewalkit.ServiceName}, {"stop", "postfix.service"}, {"daemon-reload", mailrenewalkit.TimerName}, {"show", "sshd.service"}, {"show", "--all"}, {"enable", mailrenewalkit.TimerName}} {
		if _, err := mailEnrollmentSystemctlArgs(pair[0], pair[1]); err == nil {
			t.Fatal("unexpected native authority", pair)
		}
	}
	for _, pair := range [][2]string{{"show", mailrenewalkit.ServiceName}, {"show", mailrenewalkit.TimerName}, {"daemon-reload", ""}, {"start", mailrenewalkit.TimerName}, {"stop", mailrenewalkit.TimerName}} {
		if _, err := mailEnrollmentSystemctlArgs(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
}
func TestMailEnrollmentNativeObservationAndFailureGuidance(t *testing.T) {
	absent := []byte("LoadState=not-found\nFragmentPath=\nDropInPaths=\nNeedDaemonReload=no\nActiveState=inactive\nUnitFileState=\n")
	if _, err := readMailEnrollmentSystemctlResult("show", mailrenewalkit.TimerName, absent, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := readMailEnrollmentSystemctlResult("show", mailrenewalkit.TimerName, []byte("LoadState=not-found\n"), nil, nil); err == nil {
		t.Fatal("incomplete observation became absence")
	}
	secret := "password=do-not-repeat"
	_, err := readMailEnrollmentSystemctlResult("start", mailrenewalkit.TimerName, nil, []byte(secret+" permission denied"), errors.New("could not start"))
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "denied") || !strings.Contains(err.Error(), "same operation") {
		t.Fatal("unsafe or unactionable native error", err)
	}
}
