package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailtlsconfig"
)

func TestMailHostReloadOnlyCommandBoundary(t *testing.T) {
	raw, err := os.ReadFile("../../internal/mailtlsartifact/testdata/alpha81-empty.json")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := decodeMailTLSSyncJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	previous := lookupMailTLSCommand
	lookupMailTLSCommand = func(name string) (string, error) { return "/fixture/" + name, nil }
	t.Cleanup(func() { lookupMailTLSCommand = previous })
	const cert, key = "/accepted/current/fullchain.pem", "/accepted/current/privkey.pem"
	for _, scenario := range []string{"success", "preflight", "owner-before", "owner-between", "owner-after", "stopped", "unknown-version", "parser-failed", "reload-first-failed", "reload-second-failed", "cancel-before", "cancel-between", "not-served-first", "not-served-second", "served-unknown"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reloads := []string{}
			run := func(name string, args ...string) ([]byte, error) {
				switch name {
				case "dovecot":
					if strings.Join(args, " ") != "--version" {
						t.Fatalf("unexpected command: %s %v", name, args)
					}
					if scenario == "unknown-version" {
						return []byte("2.4.1 private-output"), errors.New("private-error")
					}
					return []byte("2.4.1"), nil
				case "doveconf":
					if strings.Join(args, " ") != "-n" {
						t.Fatalf("unexpected command: %s %v", name, args)
					}
					if scenario == "parser-failed" {
						return []byte("private-output"), errors.New("private-error")
					}
					return []byte("private native configuration; never reported"), nil
				case "postconf":
					if len(args) != 2 || args[0] != "-h" {
						t.Fatalf("configuration write: %s %v", name, args)
					}
					// As postconf prints them: one line per value, an empty
					// line for a setting that is not set.
					if args[1] == "tls_server_sni_maps" {
						return []byte("\n"), nil
					}
					for _, item := range mailtlsconfig.PostfixSettings(plan.Myhostname, cert, key) {
						if args[1] == item[0] {
							return []byte(item[1] + "\n"), nil
						}
					}
				case "systemctl":
					if len(args) == 3 && args[0] == "is-active" && args[1] == "--quiet" && (args[2] == "postfix.service" || args[2] == "dovecot.service") {
						if scenario == "stopped" {
							return nil, errors.New("inactive")
						}
						if scenario == "cancel-before" {
							cancel()
						}
						return nil, nil
					}
					if len(args) == 2 && args[0] == "reload" && (args[1] == "postfix.service" || args[1] == "dovecot.service") {
						reloads = append(reloads, args[1])
						if scenario == "reload-first-failed" || (scenario == "reload-second-failed" && len(reloads) == 2) {
							return nil, errors.New("reload failed")
						}
						if scenario == "cancel-between" {
							cancel()
						}
						return nil, nil
					}
				}
				t.Fatalf("forbidden renewal command: %s %v", name, args)
				return nil, nil
			}
			read := func(path string) ([]byte, error) {
				if path != dovecotTLSConf {
					t.Fatalf("unexpected configuration read: %s", path)
				}
				value := mailtlsconfig.Dovecot(true, cert, key, plan.SNI)
				if scenario == "owner-before" || (scenario == "owner-between" && len(reloads) > 0) || (scenario == "owner-after" && len(reloads) > 1) {
					value += "# native owner edit\n"
				}
				return []byte(value), nil
			}
			// Every reload in these scenarios exits 0 and both units answer
			// "active", as a wrapper unit does. What the listeners present
			// afterwards is asked separately, once per reloaded service.
			asked := []string{}
			served := func(_ context.Context, unit string) error {
				if len(reloads) == 0 || reloads[len(reloads)-1] != unit {
					t.Fatalf("listeners of %s asked before its reload: %v", unit, reloads)
				}
				asked = append(asked, unit)
				switch {
				case scenario == "not-served-first" && unit == "postfix.service":
					return &mailServedCertificateError{service: "Postfix"}
				case scenario == "not-served-second" && unit == "dovecot.service":
					return &mailServedCertificateError{service: "Dovecot"}
				case scenario == "served-unknown" && unit == "postfix.service":
					return &mailServedCertificateError{service: "Postfix", unknown: true}
				}
				return nil
			}
			err := observeOrReloadMailHostTLS(ctx, plan, cert, key, run, read, scenario != "preflight", served)
			want := []string{}
			switch scenario {
			case "success", "owner-after", "reload-second-failed", "not-served-second":
				want = []string{"postfix.service", "dovecot.service"}
			case "owner-between", "reload-first-failed", "cancel-between", "not-served-first", "served-unknown":
				want = []string{"postfix.service"}
			}
			if !reflect.DeepEqual(reloads, want) {
				t.Fatalf("reloads=%v want=%v", reloads, want)
			}
			wantAsked := []string{}
			switch scenario {
			case "success", "not-served-second":
				wantAsked = []string{"postfix.service", "dovecot.service"}
			case "reload-second-failed", "owner-after", "not-served-first", "served-unknown":
				// A service whose reload or observation failed is not asked.
				wantAsked = []string{"postfix.service"}
			}
			if !reflect.DeepEqual(asked, wantAsked) {
				t.Fatalf("listeners asked=%v want=%v", asked, wantAsked)
			}
			var notServed *mailServedCertificateError
			if strings.Contains(scenario, "served") != errors.As(err, &notServed) {
				t.Fatalf("served-certificate cause=%v for %s", err, scenario)
			}
			success := scenario == "success" || scenario == "preflight"
			if (err == nil) != success {
				t.Fatalf("outcome=%v", err)
			}
		})
	}
}

// A reload without the listener check would be the old inference from
// systemctl's exit status; it is refused rather than run.
func TestMailHostReloadRequiresTheServedCertificateCheck(t *testing.T) {
	raw, err := os.ReadFile("../../internal/mailtlsartifact/testdata/alpha81-empty.json")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := decodeMailTLSSyncJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	const cert, key = "/accepted/current/fullchain.pem", "/accepted/current/privkey.pem"
	run := func(name string, args ...string) ([]byte, error) {
		switch {
		case name == "systemctl" && len(args) > 0 && args[0] == "reload":
			t.Fatalf("reload sent without a listener check: %v", args)
		case name == "postconf" && len(args) == 2:
			if args[1] == "tls_server_sni_maps" {
				return []byte("\n"), nil
			}
			for _, item := range mailtlsconfig.PostfixSettings(plan.Myhostname, cert, key) {
				if args[1] == item[0] {
					return []byte(item[1] + "\n"), nil
				}
			}
		case name == "dovecot":
			return []byte("2.4.1"), nil
		}
		return nil, nil
	}
	read := func(string) ([]byte, error) {
		return []byte(mailtlsconfig.Dovecot(true, cert, key, plan.SNI)), nil
	}
	if err := observeOrReloadMailHostTLS(context.Background(), plan, cert, key, run, read, true, nil); err == nil {
		t.Fatal("a reload without the served certificate check was accepted")
	}
}

func TestMailHostReloadGuidanceDoesNotExposeNativeOutput(t *testing.T) {
	cause := errors.New("private credential and native command output")
	err := &mailHostReloadUnverified{cause: cause}
	if strings.Contains(err.Error(), cause.Error()) || !errors.Is(err, cause) {
		t.Fatal("public detail leaked or typed cause lost")
	}
	for _, fragment := range []string{"server owner", "Postfix/Dovecot", "retry the same operation", "preserved"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("missing recovery guidance: %s", fragment)
		}
	}
}
