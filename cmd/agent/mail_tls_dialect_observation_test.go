package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailtlsconfig"
)

func TestMailTLSDialectUnknownStopsBeforeConfigurationOrSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		failure      error
		want         error
	}{
		{"failed-command", "2.4.1", errors.New("private credential output"), mailtlsconfig.ErrDovecotVersionUnknown},
		{"empty", "", nil, mailtlsconfig.ErrDovecotVersionUnknown},
		{"malformed", "not-a-version private credential", nil, mailtlsconfig.ErrDovecotVersionUnknown},
		{"unsupported", "3.0.0", nil, mailtlsconfig.ErrDovecotVersionUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := lookupMailTLSCommand
			t.Cleanup(func() { lookupMailTLSCommand = previous })
			lookupMailTLSCommand = func(name string) (string, error) { return filepath.Join(t.TempDir(), name), nil }
			t.Setenv("CELIKPANEL_MAIL_DIR", "")
			calls := 0
			run := func(name string, args ...string) ([]byte, error) {
				calls++
				if filepath.Base(name) != "dovecot" || strings.Join(args, " ") != "--version" {
					t.Fatalf("unknown dialect reached a host action: %s %v", name, args)
				}
				return []byte(tc.output), tc.failure
			}
			if _, err := preflightMailTLSCommands(true, run); !errors.Is(err, tc.want) {
				t.Fatalf("preflight classification: %v", err)
			}
			if err := configureDovecotTLSForHost("mail.example.test", nil, run); !errors.Is(err, tc.want) {
				t.Fatalf("direct TLS writer classification: %v", err)
			}
			var resp SecureMailTLSResponse
			outcome, err := reconcileMailTLSHost(&SecureMailTLSRequest{Myhostname: "mail.example.test"}, &resp, run)
			if err != nil || outcome != mailTLSHostUntouched || resp.Configured || resp.Error != tc.want.Error() {
				t.Fatalf("unknown became host mutation or success: %v %+v %v", outcome, resp, err)
			}
			if strings.Contains(resp.Error, "credential") || calls != 3 {
				t.Fatalf("unexpected observation/guidance: calls=%d %+v", calls, resp)
			}
		})
	}
}
func TestMailTLSDialectNilRunnerIsUnknown(t *testing.T) {
	if modern, err := dovecotIs24WithRunner(nil); modern || !errors.Is(err, mailtlsconfig.ErrDovecotVersionUnknown) {
		t.Fatalf("nil observer: %v %v", modern, err)
	}
}
