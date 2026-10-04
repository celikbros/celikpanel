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
	for _, scenario := range []string{"success", "preflight", "owner-before", "owner-between", "owner-after", "stopped", "unknown-version", "parser-failed", "reload-first-failed", "reload-second-failed", "cancel-before", "cancel-between"} {
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
					if args[1] == "tls_server_sni_maps" {
						return nil, nil
					}
					for _, item := range mailtlsconfig.PostfixSettings(plan.Myhostname, cert, key) {
						if args[1] == item[0] {
							return []byte(item[1]), nil
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
			err := observeOrReloadMailHostTLS(ctx, plan, cert, key, run, read, scenario != "preflight")
			want := []string{}
			switch scenario {
			case "success", "owner-after", "reload-second-failed":
				want = []string{"postfix.service", "dovecot.service"}
			case "owner-between", "reload-first-failed", "cancel-between":
				want = []string{"postfix.service"}
			}
			if !reflect.DeepEqual(reloads, want) {
				t.Fatalf("reloads=%v want=%v", reloads, want)
			}
			success := scenario == "success" || scenario == "preflight"
			if (err == nil) != success {
				t.Fatalf("outcome=%v", err)
			}
		})
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
