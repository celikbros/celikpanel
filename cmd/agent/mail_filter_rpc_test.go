package main

import (
	"strings"
	"testing"
)

func TestWireMailFiltersRequiresPostfix(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var resp WireMailFiltersResponse
	if err := (&Agent{}).WireMailFilters(&struct{}{}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Wired {
		t.Fatal("mail filter must not report wired without Postfix")
	}
	if !strings.Contains(strings.ToLower(resp.Error), "postfix") {
		t.Fatalf("error = %q, want a Postfix prerequisite error", resp.Error)
	}
}
