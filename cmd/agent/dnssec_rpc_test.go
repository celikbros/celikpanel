//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func createDNSSECTestZone(t *testing.T, kind string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO domains (name, type) VALUES ('example.test', ?)`, kind,
	); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	prepareManagedDNSReadinessTest(t, path)
	return "example.test"
}

func installDNSSECV2TestLease(
	t *testing.T, zone string,
) (*serviceMutationManager, transport.ServiceMutationBinding) {
	t.Helper()
	manager, _ := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(t, manager, "dnssec_secure", zone, "")
	installGlobalMutationTestManager(t, manager)
	return manager, transport.ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	}
}

func stubDNSSECV2Commands(
	t *testing.T,
	run func(context.Context, string, ...string) ([]byte, error),
) {
	t.Helper()
	oldLookPath, oldCommand := dnssecLookPath, dnssecV2Command
	dnssecLookPath = func(file string) (string, error) {
		if file == "pdnsutil" {
			return file, nil
		}
		return exec.LookPath(file)
	}
	dnssecV2Command = run
	t.Cleanup(func() {
		dnssecLookPath, dnssecV2Command = oldLookPath, oldCommand
	})
}

func TestSecureDNSZoneLegacyEndpointIsStableZeroTouchStub(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	oldLookPath, oldCommand := dnssecLookPath, dnssecV2Command
	dnssecLookPath = func(string) (string, error) {
		t.Fatal("legacy DNSSEC endpoint performed host lookup")
		return "", nil
	}
	dnssecV2Command = func(
		context.Context, string, ...string,
	) ([]byte, error) {
		t.Fatal("legacy DNSSEC endpoint ran a command")
		return nil, nil
	}
	t.Cleanup(func() {
		dnssecLookPath, dnssecV2Command = oldLookPath, oldCommand
	})
	response := DNSSECStatusResponse{Secured: true, DS: []string{"stale"}}
	if err := (&Agent{}).SecureDNSZone(
		&DNSSECRequest{Zone: "example.test"}, &response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Secured || len(response.DS) != 0 ||
		response.Error != secureDNSZoneLegacyUnsupportedError {
		t.Fatalf("legacy response=%+v", response)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy endpoint touched PowerDNS DB: %v", err)
	}
}

func TestSecureDNSZoneV2RejectsInvalidDomainBeforeLeaseOrHost(t *testing.T) {
	oldLookPath := dnssecLookPath
	dnssecLookPath = func(string) (string, error) {
		t.Fatal("invalid V2 domain reached host lookup")
		return "", nil
	}
	t.Cleanup(func() { dnssecLookPath = oldLookPath })
	response := SecureDNSZoneV2Response{Secured: true, DS: []string{"stale"}}
	if err := (&Agent{}).SecureDNSZoneV2(
		&SecureDNSZoneV2Request{Zone: "NOT A DOMAIN"}, &response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Secured || len(response.DS) != 0 || response.Error == "" {
		t.Fatalf("invalid V2 response=%+v", response)
	}
}

func TestSecureDNSZoneV2ReadinessDriftRejectsBeforeHostCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detached-pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	sentinel := []byte("detached database sentinel")
	if err := os.WriteFile(path, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	prepareManagedDNSReadinessTest(t, path)
	if err := os.WriteFile(dnsMainConf, []byte("include-dir=/unmanaged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager, binding := installDNSSECV2TestLease(t, "example.test")
	t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })
	oldLookPath, oldCommand := dnssecLookPath, dnssecV2Command
	hostCalls := 0
	dnssecLookPath = func(string) (string, error) {
		hostCalls++
		return "pdnsutil", nil
	}
	dnssecV2Command = func(context.Context, string, ...string) ([]byte, error) {
		hostCalls++
		return nil, nil
	}
	t.Cleanup(func() { dnssecLookPath, dnssecV2Command = oldLookPath, oldCommand })
	var response SecureDNSZoneV2Response
	if err := (&Agent{}).SecureDNSZoneV2(
		&SecureDNSZoneV2Request{ServiceMutationBinding: binding, Zone: "example.test"},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Error == "" || response.Secured || len(response.DS) != 0 || hostCalls != 0 {
		t.Fatalf("readiness response=%+v hostCalls=%d", response, hostCalls)
	}
	got, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(got, sentinel) {
		t.Fatalf("readiness changed detached DB=%q err=%v", got, err)
	}
	manager.mu.Lock()
	steps := manager.active.steps
	manager.mu.Unlock()
	if steps != 0 {
		t.Fatalf("readiness retained %d active steps", steps)
	}
}

func TestSecureDNSZoneV2RejectsReadOnlyZone(t *testing.T) {
	for _, kind := range []string{"SLAVE", "SECONDARY"} {
		t.Run(kind, func(t *testing.T) {
			zone := createDNSSECTestZone(t, kind)
			_, binding := installDNSSECV2TestLease(t, zone)
			stubDNSSECV2Commands(t, func(
				context.Context, string, ...string,
			) ([]byte, error) {
				t.Fatal("read-only zone ran a command")
				return nil, nil
			})
			var response SecureDNSZoneV2Response
			if err := (&Agent{}).SecureDNSZoneV2(
				&SecureDNSZoneV2Request{
					ServiceMutationBinding: binding, Zone: zone,
				},
				&response,
			); err != nil {
				t.Fatal(err)
			}
			if response.Secured ||
				!strings.Contains(strings.ToLower(response.Error), "secondary") {
				t.Fatalf("read-only response=%+v", response)
			}
		})
	}
}

func TestSecureDNSZoneV2UsesTrackedCurrentSyntax(t *testing.T) {
	zone := createDNSSECTestZone(t, "NATIVE")
	manager, binding := installDNSSECV2TestLease(t, zone)
	showCount := 0
	var calls []string
	stubDNSSECV2Commands(t, func(
		ctx context.Context, name string, args ...string,
	) ([]byte, error) {
		tracker, _ := ctx.Value(
			serviceMutationExecutionTrackerKey{},
		).(*serviceMutationExecutionTracker)
		if tracker == nil || tracker.manager != manager {
			return nil, errors.New("DNSSEC command lacked durable tracker")
		}
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if name == "pdns_control" {
			return nil, nil
		}
		switch strings.Join(args, " ") {
		case "zone show " + zone:
			showCount++
			if showCount == 1 {
				return []byte("Zone is not secured\n"), nil
			}
			return []byte(
				"ID = 1 (CSK), tag = 12345\n" +
					"DS = example.test. IN DS 12345 13 2 AABBCC\n",
			), nil
		case "zone secure " + zone, "zone rectify " + zone:
			return nil, nil
		default:
			return nil, errors.New("unexpected command")
		}
	})
	var response SecureDNSZoneV2Response
	if err := (&Agent{}).SecureDNSZoneV2(
		&SecureDNSZoneV2Request{
			ServiceMutationBinding: binding, Zone: zone,
		},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if !response.Secured || !reflect.DeepEqual(
		response.DS, []string{"12345 13 2 AABBCC"},
	) {
		t.Fatalf("V2 response=%+v calls=%v", response, calls)
	}
	for _, call := range calls {
		if strings.Contains(call, "secure-zone") ||
			strings.Contains(call, "rectify-zone") ||
			strings.Contains(call, "show-zone") {
			t.Fatalf("legacy syntax used after current syntax success: %v", calls)
		}
	}
}

func TestSecureDNSZoneV2FallsBackOnlyForSyntaxMismatch(t *testing.T) {
	zone := createDNSSECTestZone(t, "MASTER")
	_, binding := installDNSSECV2TestLease(t, zone)
	showCount := 0
	var legacy []string
	stubDNSSECV2Commands(t, func(
		_ context.Context, name string, args ...string,
	) ([]byte, error) {
		if name == "pdns_control" {
			return nil, nil
		}
		joined := strings.Join(args, " ")
		if strings.HasPrefix(joined, "zone ") {
			return []byte("Unknown command 'zone'\n"), errors.New("exit status 1")
		}
		legacy = append(legacy, joined)
		switch joined {
		case "show-zone " + zone:
			showCount++
			if showCount == 1 {
				return []byte("Zone is not secured\n"), nil
			}
			return []byte(
				"ID = 1, tag = 23456\n" +
					"DS = example.test. IN DS 23456 13 2 DDEEFF\n",
			), nil
		case "secure-zone " + zone, "rectify-zone " + zone:
			return nil, nil
		default:
			return nil, errors.New("unexpected legacy command")
		}
	})
	var response SecureDNSZoneV2Response
	if err := (&Agent{}).SecureDNSZoneV2(
		&SecureDNSZoneV2Request{
			ServiceMutationBinding: binding, Zone: zone,
		}, &response,
	); err != nil {
		t.Fatal(err)
	}
	if !response.Secured || len(response.DS) != 1 {
		t.Fatalf("legacy fallback response=%+v", response)
	}
	for _, want := range []string{
		"secure-zone " + zone, "rectify-zone " + zone,
	} {
		if !containsString(legacy, want) {
			t.Fatalf("legacy calls=%v missing %q", legacy, want)
		}
	}
}

func TestDNSSECStatusReportsMissingPDNSUtil(t *testing.T) {
	oldLookPath := dnssecLookPath
	dnssecLookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}
	t.Cleanup(func() { dnssecLookPath = oldLookPath })
	var response DNSSECStatusResponse
	if err := (&Agent{}).DNSSECStatus(
		&DNSSECRequest{Zone: "example.test"}, &response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Error != "pdnsutil is not installed" {
		t.Fatalf("status response=%+v", response)
	}
}
