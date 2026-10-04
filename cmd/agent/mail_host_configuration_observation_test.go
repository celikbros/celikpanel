package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailtlsconfig"
)

func TestMailHostConfigurationObservationRefusesDriftWithoutMutation(t *testing.T) {
	raw, err := os.ReadFile("../../internal/mailtlsartifact/testdata/alpha81-empty.json")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := decodeMailTLSSyncJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	previous := lookupMailTLSCommand
	t.Cleanup(func() { lookupMailTLSCommand = previous })
	lookupMailTLSCommand = func(name string) (string, error) { return "/fixture/" + name, nil }
	const cert = "/accepted/cert.pem"
	const key = "/accepted/key.pem"
	for _, scenario := range []string{"matching", "postfix-drift", "dovecot-drift", "sni-drift", "unknown-version", "failed-read", "unreadable-fragment"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			runner := func(name string, args ...string) ([]byte, error) {
				calls++
				switch filepath.Base(name) {
				case "dovecot":
					if strings.Join(args, " ") != "--version" {
						t.Fatalf("mutation: %s %v", name, args)
					}
					if scenario == "unknown-version" {
						return []byte("2.4.1 private-output"), errors.New("credential")
					}
					return []byte("2.4.1"), nil
				case "postconf":
					if len(args) != 2 || args[0] != "-h" {
						t.Fatalf("mutation: %s %v", name, args)
					}
					if scenario == "failed-read" {
						return []byte("private-output"), errors.New("credential")
					}
					if args[1] == "tls_server_sni_maps" {
						if scenario == "sni-drift" {
							return []byte("hash:/owner/map"), nil
						}
						return nil, nil
					}
					for _, setting := range mailtlsconfig.PostfixSettings(plan.Myhostname, cert, key) {
						if setting[0] == args[1] {
							if scenario == "postfix-drift" && args[1] == "smtpd_tls_security_level" {
								return []byte("encrypt"), nil
							}
							return []byte(setting[1] + "\n"), nil
						}
					}
				}
				t.Fatalf("unexpected action: %s %v", name, args)
				return nil, nil
			}
			read := func(path string) ([]byte, error) {
				if path != dovecotTLSConf {
					t.Fatalf("unexpected file: %s", path)
				}
				if scenario == "unreadable-fragment" {
					return nil, errors.New("owner-private-detail")
				}
				data := mailtlsconfig.Dovecot(true, cert, key, nil)
				if scenario == "dovecot-drift" {
					data += "# owner change\n"
				}
				return []byte(data), nil
			}
			err := verifyMailTLSConfiguration(plan, cert, key, runner, read)
			if scenario == "matching" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("unverified state accepted")
			}
			public := &mailHostConfigurationUnverified{cause: err}
			if strings.Contains(public.Error(), "private-output") || strings.Contains(public.Error(), "credential") || !strings.Contains(public.Error(), "server owner") || !strings.Contains(public.Error(), "preserved") {
				t.Fatal("unsafe/nonactionable guidance")
			}
			if scenario == "unknown-version" && (calls != 1 || !errors.Is(public, mailtlsconfig.ErrDovecotVersionUnknown)) {
				t.Fatalf("unknown observation lost: %d %v", calls, err)
			}
		})
	}
}
