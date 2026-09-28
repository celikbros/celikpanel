package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

func TestBINDSourceInverseCapabilityExactSelectedCommand(t *testing.T) {
	for _, args := range [][]string{
		nil, {"check-bind-source-inverse-v1", "--json"}, {"check-bind-source-inverse-v1", "--request-id", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	} {
		var out, diag bytes.Buffer
		if code := dispatchBINDSourceInverseCapability(args, 0, &out, &diag); code != exitUsage || out.Len() != 0 {
			t.Fatalf("unexpected capability accepted %v: %d", args, code)
		}
	}
	var out, diag bytes.Buffer
	if runtime.GOOS == "linux" {
		if code := dispatchBINDSourceInverseCapability([]string{bindSourceInverseCapabilityCommand}, 1000, &out, &diag); code != exitNotOwner || out.Len() != 0 {
			t.Fatal("nonowner received capability marker")
		}
		out.Reset()
		diag.Reset()
		if code := dispatchBINDSourceInverseCapability([]string{bindSourceInverseCapabilityCommand}, 0, &out, &diag); code != exitOK || out.String() != bindSourceInverseCapabilityMarker+"\n" || diag.Len() != 0 {
			t.Fatalf("capability marker mismatch: %d %q %q", code, out.String(), diag.String())
		}
	} else if code := dispatchBINDSourceInverseCapability([]string{bindSourceInverseCapabilityCommand}, 0, &out, &diag); code != exitUnavailable || strings.Contains(out.String(), bindSourceInverseCapabilityMarker) {
		t.Fatal("unsupported platform advertised inverse")
	}
}
func TestBINDAdoptionInverseCapabilityExactSelectedCommand(t *testing.T) {
	for _, args := range [][]string{
		nil, {"check-bind-adoption-inverse-v1", "--json"}, {"check-bind-adoption-inverse-v1", "--request-id", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	} {
		var out, diag bytes.Buffer
		if code := dispatchBINDAdoptionInverseCapability(args, 0, &out, &diag); code != exitUsage || out.Len() != 0 {
			t.Fatalf("unexpected capability accepted %v: %d", args, code)
		}
	}
	var out, diag bytes.Buffer
	if runtime.GOOS == "linux" {
		if code := dispatchBINDAdoptionInverseCapability([]string{bindAdoptionInverseCapabilityCommand}, 1000, &out, &diag); code != exitNotOwner || out.Len() != 0 {
			t.Fatal("nonowner received capability marker")
		}
		out.Reset()
		diag.Reset()
		if code := dispatchBINDAdoptionInverseCapability([]string{bindAdoptionInverseCapabilityCommand}, 0, &out, &diag); code != exitOK || out.String() != bindAdoptionInverseCapabilityMarker+"\n" || diag.Len() != 0 {
			t.Fatalf("capability marker mismatch: %d %q %q", code, out.String(), diag.String())
		}
	} else if code := dispatchBINDAdoptionInverseCapability([]string{bindAdoptionInverseCapabilityCommand}, 0, &out, &diag); code != exitUnavailable || strings.Contains(out.String(), bindAdoptionInverseCapabilityMarker) {
		t.Fatal("unsupported platform advertised inverse")
	}
}
