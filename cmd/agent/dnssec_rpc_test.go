package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func createDNSSECTestZone(t *testing.T, kind string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", path)

	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO domains (name, type) VALUES ('example.test', ?)`, kind); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return "example.test"
}

func stubDNSSECCommands(t *testing.T, run func(string, ...string) ([]byte, error)) {
	t.Helper()
	oldLookPath, oldCommand := dnssecLookPath, dnssecCommand
	dnssecLookPath = func(file string) (string, error) {
		if file == "pdnsutil" {
			return file, nil
		}
		return exec.LookPath(file)
	}
	dnssecCommand = run
	t.Cleanup(func() {
		dnssecLookPath, dnssecCommand = oldLookPath, oldCommand
	})
}

func TestSecureDNSZoneRejectsReadOnlyZone(t *testing.T) {
	for _, kind := range []string{"SLAVE", "SECONDARY"} {
		t.Run(kind, func(t *testing.T) {
			zone := createDNSSECTestZone(t, kind)
			stubDNSSECCommands(t, func(name string, args ...string) ([]byte, error) {
				t.Fatalf("unexpected command for read-only zone: %s %v", name, args)
				return nil, nil
			})

			var resp DNSSECStatusResponse
			if err := (&Agent{}).SecureDNSZone(&DNSSECRequest{Zone: zone}, &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Secured || len(resp.DS) != 0 {
				t.Fatalf("read-only zone reported secured: %+v", resp)
			}
			if !strings.Contains(strings.ToLower(resp.Error), "secondary") {
				t.Fatalf("error = %q, want read-only secondary explanation", resp.Error)
			}
		})
	}
}

func TestSecureDNSZoneUsesCurrentPDNSUtilSyntax(t *testing.T) {
	zone := createDNSSECTestZone(t, "NATIVE")
	var calls []string
	showCount := 0
	stubDNSSECCommands(t, func(name string, args ...string) ([]byte, error) {
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
			return []byte("ID = 1 (CSK), tag = 12345\nDS = example.test. IN DS 12345 13 2 AABBCC\n"), nil
		case "zone secure " + zone, "zone rectify " + zone:
			return nil, nil
		default:
			return nil, errors.New("unexpected command")
		}
	})

	var resp DNSSECStatusResponse
	if err := (&Agent{}).SecureDNSZone(&DNSSECRequest{Zone: zone}, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Secured || !reflect.DeepEqual(resp.DS, []string{"12345 13 2 AABBCC"}) {
		t.Fatalf("unexpected response: %+v", resp)
	}
	for _, call := range calls {
		if strings.Contains(call, "secure-zone") || strings.Contains(call, "rectify-zone") || strings.Contains(call, "show-zone") {
			t.Fatalf("legacy syntax used despite current syntax succeeding: %v", calls)
		}
	}
}

func TestSecureDNSZoneFallsBackForLegacyPDNSUtil(t *testing.T) {
	zone := createDNSSECTestZone(t, "MASTER")
	showCount := 0
	var legacy []string
	stubDNSSECCommands(t, func(name string, args ...string) ([]byte, error) {
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
			return []byte("ID = 1 (CSK), tag = 23456\nDS = example.test. IN DS 23456 13 2 DDEEFF\n"), nil
		case "secure-zone " + zone, "rectify-zone " + zone:
			return nil, nil
		default:
			return nil, errors.New("unexpected legacy command")
		}
	})

	var resp DNSSECStatusResponse
	if err := (&Agent{}).SecureDNSZone(&DNSSECRequest{Zone: zone}, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Secured || len(resp.DS) != 1 {
		t.Fatalf("legacy fallback did not secure zone: %+v", resp)
	}
	for _, want := range []string{"secure-zone " + zone, "rectify-zone " + zone} {
		if !containsString(legacy, want) {
			t.Fatalf("legacy calls %v do not contain %q", legacy, want)
		}
	}
}

func TestSecureDNSZoneDoesNotFallbackOnOperationalFailure(t *testing.T) {
	zone := createDNSSECTestZone(t, "NATIVE")
	legacyCalled := false
	stubDNSSECCommands(t, func(name string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch joined {
		case "zone show " + zone:
			return []byte("Zone is not secured\n"), nil
		case "zone secure " + zone:
			return []byte("backend is read-only\n"), errors.New("exit status 1")
		default:
			if strings.HasPrefix(joined, "secure-zone") {
				legacyCalled = true
			}
			return nil, errors.New("unexpected command")
		}
	})

	var resp DNSSECStatusResponse
	if err := (&Agent{}).SecureDNSZone(&DNSSECRequest{Zone: zone}, &resp); err != nil {
		t.Fatal(err)
	}
	if legacyCalled {
		t.Fatal("operational failure incorrectly retried with legacy syntax")
	}
	if !strings.Contains(resp.Error, "backend is read-only") {
		t.Fatalf("error detail was discarded: %q", resp.Error)
	}
}

func TestSecureDNSZonePreservesRectifyFailure(t *testing.T) {
	zone := createDNSSECTestZone(t, "NATIVE")
	stubDNSSECCommands(t, func(name string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "zone show " + zone:
			return []byte("Zone is not secured\n"), nil
		case "zone secure " + zone:
			return nil, nil
		case "zone rectify " + zone:
			return []byte("unable to update ordername\n"), errors.New("exit status 1")
		default:
			return nil, errors.New("unexpected command")
		}
	})

	var resp DNSSECStatusResponse
	if err := (&Agent{}).SecureDNSZone(&DNSSECRequest{Zone: zone}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Secured {
		t.Fatalf("rectify failure reported secured: %+v", resp)
	}
	if !strings.Contains(resp.Error, "rectify zone") || !strings.Contains(resp.Error, "unable to update ordername") {
		t.Fatalf("rectify error was discarded: %q", resp.Error)
	}
}

func TestSecureDNSZoneRejectsMissingDS(t *testing.T) {
	zone := createDNSSECTestZone(t, "NATIVE")
	showCount := 0
	stubDNSSECCommands(t, func(name string, args ...string) ([]byte, error) {
		if name == "pdns_control" {
			return nil, nil
		}
		switch strings.Join(args, " ") {
		case "zone show " + zone:
			showCount++
			if showCount == 1 {
				return []byte("Zone is not secured\n"), nil
			}
			return []byte("ID = 1 (CSK), tag = 34567\n"), nil
		case "zone secure " + zone, "zone rectify " + zone:
			return nil, nil
		default:
			return nil, errors.New("unexpected command")
		}
	})

	var resp DNSSECStatusResponse
	if err := (&Agent{}).SecureDNSZone(&DNSSECRequest{Zone: zone}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Secured || len(resp.DS) != 0 {
		t.Fatalf("missing DS reported secured: %+v", resp)
	}
	if !strings.Contains(resp.Error, "no DS") {
		t.Fatalf("error = %q, want missing DS explanation", resp.Error)
	}
}

func TestDNSSECStatusReportsMissingPDNSUtil(t *testing.T) {
	oldLookPath := dnssecLookPath
	dnssecLookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}
	t.Cleanup(func() { dnssecLookPath = oldLookPath })

	var resp DNSSECStatusResponse
	if err := (&Agent{}).DNSSECStatus(&DNSSECRequest{Zone: "example.test"}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "pdnsutil is not installed" {
		t.Fatalf("error = %q, want pdnsutil error", resp.Error)
	}
}
