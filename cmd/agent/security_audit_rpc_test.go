package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestSecurityAuditRPCIsExactNoInputContract(t *testing.T) {
	method, ok := reflect.TypeOf(&Agent{}).MethodByName("SecurityAudit")
	if !ok {
		t.Fatal("Agent.SecurityAudit is missing")
	}
	wantRequest := reflect.TypeOf((*transport.Empty)(nil))
	wantResponse := reflect.TypeOf((*transport.SecurityAuditAgentResponse)(nil))
	if method.Type.NumIn() != 3 || method.Type.In(1) != wantRequest || method.Type.In(2) != wantResponse ||
		method.Type.NumOut() != 1 || method.Type.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
		t.Fatalf("SecurityAudit signature = %s", method.Type)
	}
	if wantRequest.Elem().NumField() != 0 {
		t.Fatal("SecurityAudit request gained an input field")
	}
}

func TestSecurityAuditLinuxCollectorDoesNotUseMutatingFirewallStore(t *testing.T) {
	raw, err := os.ReadFile("security_audit_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, forbidden := range []string{
		"firewallStatusLocked(", "fileFirewallStateStore", "os.Mkdir", "os.WriteFile",
		"os.OpenFile", "os.Remove", "os.Rename", "os.Chmod", "os.Chown",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("read-only SecurityAudit collector contains forbidden mutating path %q", forbidden)
		}
	}
}
