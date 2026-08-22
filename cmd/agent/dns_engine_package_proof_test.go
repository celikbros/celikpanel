package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

type dnsPackageProofExitError struct{ code int }

func (dnsPackageProofExitError) Error() string   { return "exit" }
func (e dnsPackageProofExitError) ExitCode() int { return e.code }

func TestDNSPackageProofIsExactAndCallerDeadlineBound(t *testing.T) {
	for _, test := range []struct {
		name       string
		profile    hostplatform.Profile
		output     string
		commandErr error
		want       bool
		wantArgs   []string
	}{
		{name: "apt-installed", profile: testUbuntuBINDProfile(), output: "install ok installed", want: true, wantArgs: []string{"-W", "-f", "${Status}", "--", "bind9"}},
		{name: "apt-absent", profile: testUbuntuBINDProfile(), output: "dpkg-query: no packages found matching bind9\n", commandErr: dnsPackageProofExitError{code: 1}, wantArgs: []string{"-W", "-f", "${Status}", "--", "bind9"}},
		{name: "pacman-installed", profile: hostplatform.Profile{PackageManager: hostplatform.PackageManagerPacman}, output: "bind 9.20.4-1\n", want: true, wantArgs: []string{"-Q", "--", "bind"}},
		{name: "pacman-absent", profile: hostplatform.Profile{PackageManager: hostplatform.PackageManagerPacman}, output: "error: package 'bind' was not found\n", commandErr: dnsPackageProofExitError{code: 1}, wantArgs: []string{"-Q", "--", "bind"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			packageName := "bind9"
			if test.profile.PackageManager == hostplatform.PackageManagerPacman {
				packageName = "bind"
			}
			got, err := exactDNSEnginePackageInstalledWithRunner(
				context.Background(), test.profile, "/trusted/package-query", packageName,
				func(_ context.Context, _ string, args ...string) ([]byte, error) {
					if !reflect.DeepEqual(args, test.wantArgs) {
						t.Fatalf("args=%#v want=%#v", args, test.wantArgs)
					}
					return []byte(test.output), test.commandErr
				},
			)
			if err != nil || got != test.want {
				t.Fatalf("installed=%v err=%v", got, err)
			}
		})
	}
	for _, output := range []string{
		"install ok installed\n",
		"prefix install ok installed",
		"dpkg-query: no packages found matching other\n",
	} {
		if _, err := exactDNSEnginePackageInstalledWithRunner(
			context.Background(), testUbuntuBINDProfile(), "/trusted/dpkg-query", "bind9",
			func(context.Context, string, ...string) ([]byte, error) {
				return []byte(output), dnsPackageProofExitError{code: 1}
			},
		); err == nil {
			t.Fatalf("ambiguous APT package result accepted: %q", output)
		}
	}
	for _, test := range []struct {
		name   string
		output string
		err    error
	}{
		{name: "missing-without-exit-status", output: "dpkg-query: no packages found matching bind9\n", err: errors.New("transport failed")},
		{name: "missing-with-wrong-exit-status", output: "dpkg-query: no packages found matching bind9\n", err: dnsPackageProofExitError{code: 2}},
		{name: "missing-with-joined-exit-status", output: "dpkg-query: no packages found matching bind9\n", err: errors.Join(dnsPackageProofExitError{code: 1}, errors.New("tracker clear failed"))},
		{name: "pacman-duplicate-output", output: "error: package 'bind' was not found\nerror: package 'bind' was not found\n", err: dnsPackageProofExitError{code: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := testUbuntuBINDProfile()
			packageName := "bind9"
			if test.name == "pacman-duplicate-output" {
				profile = hostplatform.Profile{PackageManager: hostplatform.PackageManagerPacman}
				packageName = "bind"
			}
			if _, err := exactDNSEnginePackageInstalledWithRunner(
				context.Background(), profile, "/trusted/package-query", packageName,
				func(context.Context, string, ...string) ([]byte, error) {
					return []byte(test.output), test.err
				},
			); err == nil {
				t.Fatal("ambiguous missing package proof was accepted")
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := exactDNSEnginePackageInstalledWithRunner(
		ctx, testUbuntuBINDProfile(), "/trusted/dpkg-query", "bind9",
		func(commandCtx context.Context, _ string, _ ...string) ([]byte, error) {
			<-commandCtx.Done()
			return nil, commandCtx.Err()
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) ||
		time.Since(started) > time.Second {
		t.Fatalf("DNS package proof ignored caller deadline: %v", err)
	}
	expired, expire := context.WithCancel(context.Background())
	expire()
	if _, err := exactDNSEnginePackageInstalledWithRunner(
		expired, testUbuntuBINDProfile(), "/trusted/dpkg-query", "bind9",
		func(context.Context, string, ...string) ([]byte, error) {
			return []byte("dpkg-query: no packages found matching bind9\n"), dnsPackageProofExitError{code: 1}
		},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled missing-looking package proof was accepted: %v", err)
	}
}
