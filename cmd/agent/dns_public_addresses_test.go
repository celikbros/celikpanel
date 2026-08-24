package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testManagedPowerDNSStandaloneConfig(t *testing.T) []byte {
	t.Helper()
	config, err := managedPowerDNSStandaloneConfigForAddresses(
		[]string{"192.0.2.10"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func preservePublicListenAddressSeams(t *testing.T) {
	t.Helper()
	oldTimeout := publicListenAddressTimeout
	oldResolver := publicListenAddressExecutableResolver
	oldRunner := publicListenAddressCommandRunner
	t.Cleanup(func() {
		publicListenAddressTimeout = oldTimeout
		publicListenAddressExecutableResolver = oldResolver
		publicListenAddressCommandRunner = oldRunner
	})
}

func installPublicListenAddressOutput(t *testing.T, out string) {
	t.Helper()
	preservePublicListenAddressSeams(t)
	publicListenAddressExecutableResolver = func(string) (string, error) {
		return "/usr/bin/ip", nil
	}
	publicListenAddressCommandRunner = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(out), nil
	}
}

func TestParsePublicListenAddressesCanonicalizesAndDeduplicatesIPv4IPv6(t *testing.T) {
	out := []byte("" +
		"2: eth0 inet 192.0.2.10/24 brd 192.0.2.255 scope global eth0\n" +
		"3: eth1 inet6 2001:0db8:0:0::10/64 scope global dynamic eth1\n" +
		"4: eth2 inet 192.0.2.10/32 scope global eth2\n" +
		"5: eth3 inet6 2001:db8::10/128 scope global eth3\n")
	got, err := parsePublicListenAddresses(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.10", "2001:db8::10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses=%v want=%v", got, want)
	}
}

func TestParsePublicListenAddressesOrderIndependentConfigBytes(t *testing.T) {
	forwardOutput := []byte("" +
		"2: eth0 inet6 2001:db8::20/64 scope global dynamic eth0\n" +
		"3: eth0 inet 198.51.100.20/24 scope global eth0\n" +
		"4: eth0 inet6 2001:0db8:0:0::10/64 scope global dynamic eth0\n" +
		"5: eth0 inet 192.0.2.10/24 scope global eth0\n")
	reversedOutput := []byte("" +
		"5: eth0 inet 192.0.2.10/24 scope global eth0\n" +
		"4: eth0 inet6 2001:0db8:0:0::10/64 scope global dynamic eth0\n" +
		"3: eth0 inet 198.51.100.20/24 scope global eth0\n" +
		"2: eth0 inet6 2001:db8::20/64 scope global dynamic eth0\n")

	forwardAddresses, err := parsePublicListenAddresses(forwardOutput)
	if err != nil {
		t.Fatal(err)
	}
	reversedAddresses, err := parsePublicListenAddresses(reversedOutput)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.10", "198.51.100.20", "2001:db8::10", "2001:db8::20"}
	if !reflect.DeepEqual(forwardAddresses, want) {
		t.Fatalf("forward addresses=%v want=%v", forwardAddresses, want)
	}
	if !reflect.DeepEqual(reversedAddresses, want) {
		t.Fatalf("reversed addresses=%v want=%v", reversedAddresses, want)
	}

	forwardConfig, err := managedPowerDNSStandaloneConfigForAddresses(forwardAddresses)
	if err != nil {
		t.Fatal(err)
	}
	reversedConfig, err := managedPowerDNSStandaloneConfigForAddresses(reversedAddresses)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(forwardConfig, reversedConfig) {
		t.Fatalf("config bytes depend on ip output order:\nforward=%q\nreversed=%q", forwardConfig, reversedConfig)
	}
}

func TestParsePublicListenAddressesRejectsMalformedAndNoUsableOutput(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{name: "malformed", out: "2: eth0 inet not-a-prefix scope global eth0\n", want: "malformed address prefix"},
		{name: "wrong scope", out: "2: eth0 inet 192.0.2.10/24 scope host eth0\n", want: "does not have global scope"},
		{name: "empty", out: "", want: "no usable global unicast"},
		{name: "unsafe only", out: "1: lo inet 127.0.0.1/8 scope global lo\n", want: "no usable global unicast"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parsePublicListenAddresses([]byte(test.out))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
}

func TestPublicListenAddressesUsesPinnedResolverAndIgnoresPATHPoison(t *testing.T) {
	preservePublicListenAddressSeams(t)
	fakeIP := filepath.Join(t.TempDir(), "ip")
	if err := os.WriteFile(fakeIP, []byte("poison"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(fakeIP))
	resolverCalls := 0
	publicListenAddressExecutableResolver = func(name string) (string, error) {
		resolverCalls++
		if name != "ip" {
			t.Fatalf("resolver name=%q", name)
		}
		return "/usr/bin/ip", nil
	}
	publicListenAddressCommandRunner = func(_ context.Context, path string, args ...string) ([]byte, error) {
		if path != "/usr/bin/ip" || path == fakeIP {
			t.Fatalf("command path=%q fake=%q", path, fakeIP)
		}
		wantArgs := []string{"-o", "addr", "show", "scope", "global"}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Fatalf("args=%v want=%v", args, wantArgs)
		}
		return []byte("2: eth0 inet 192.0.2.10/24 scope global eth0\n"), nil
	}
	addresses, err := publicListenAddresses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resolverCalls != 1 || !reflect.DeepEqual(addresses, []string{"192.0.2.10"}) {
		t.Fatalf("resolverCalls=%d addresses=%v", resolverCalls, addresses)
	}
	if got := trustedFirewallCommandPaths["ip"]; !reflect.DeepEqual(got, []string{"/usr/bin/ip"}) {
		t.Fatalf("trusted ip paths=%v", got)
	}
}

func TestPublicListenAddressesRejectsMissingAndUntrustedExecutableBeforeRun(t *testing.T) {
	for _, detail := range []string{"no trusted ip executable", "untrusted ip executable"} {
		t.Run(detail, func(t *testing.T) {
			preservePublicListenAddressSeams(t)
			publicListenAddressExecutableResolver = func(string) (string, error) {
				return "", errors.New(detail)
			}
			runs := 0
			publicListenAddressCommandRunner = func(context.Context, string, ...string) ([]byte, error) {
				runs++
				return nil, nil
			}
			_, err := publicListenAddresses(context.Background())
			if err == nil || !strings.Contains(err.Error(), detail) || runs != 0 {
				t.Fatalf("error=%v runs=%d", err, runs)
			}
		})
	}
}

func TestPublicListenAddressesReportsTimeoutNonzeroMalformedAndEmpty(t *testing.T) {
	tests := []struct {
		name   string
		runner func(context.Context, string, ...string) ([]byte, error)
		want   string
	}{
		{
			name: "timeout",
			runner: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			want: "timed out",
		},
		{
			name: "nonzero",
			runner: func(context.Context, string, ...string) ([]byte, error) {
				return []byte("permission denied\n"), errors.New("exit status 1")
			},
			want: "permission denied",
		},
		{
			name: "malformed",
			runner: func(context.Context, string, ...string) ([]byte, error) {
				return []byte("not ip output\n"), nil
			},
			want: "not canonical",
		},
		{
			name: "no usable",
			runner: func(context.Context, string, ...string) ([]byte, error) {
				return nil, nil
			},
			want: "no usable global unicast",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preservePublicListenAddressSeams(t)
			publicListenAddressTimeout = 15 * time.Millisecond
			publicListenAddressExecutableResolver = func(string) (string, error) {
				return "/usr/bin/ip", nil
			}
			publicListenAddressCommandRunner = test.runner
			_, err := publicListenAddresses(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
}

func TestManagedPowerDNSStandaloneConfigRejectsWildcardAndDuplicates(t *testing.T) {
	config, err := managedPowerDNSStandaloneConfigForAddresses(
		[]string{"192.0.2.10", "2001:db8::10"},
	)
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	if !strings.Contains(text, "local-address=192.0.2.10,2001:db8::10\n") ||
		strings.Contains(text, "local-address=0.0.0.0") {
		t.Fatalf("config=%q", text)
	}
	for _, addresses := range [][]string{{}, {"0.0.0.0"}, {"192.0.2.10", "192.0.2.10"}} {
		if _, err := managedPowerDNSStandaloneConfigForAddresses(addresses); err == nil {
			t.Fatalf("addresses %v were accepted", addresses)
		}
	}
}
