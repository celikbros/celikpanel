package main

import (
	"bytes"
	"testing"
)

func TestEmitPanelBuildIdentityIsExactAndExplicit(t *testing.T) {
	oldVersion, oldCommit := buildVersion, buildCommit
	buildVersion = "v1.2.3-alpha.4"
	buildCommit = "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() {
		buildVersion, buildCommit = oldVersion, oldCommit
	})

	var output bytes.Buffer
	if emitPanelBuildIdentity([]string{"--other-mode"}, &output) || output.Len() != 0 {
		t.Fatal("unrelated mode was handled")
	}
	if !emitPanelBuildIdentity([]string{"--inspect-build-identity"}, &output) {
		t.Fatal("build identity probe was not handled")
	}
	const want = "version=v1.2.3-alpha.4\ncommit=0123456789abcdef0123456789abcdef01234567\n"
	if got := output.String(); got != want {
		t.Fatalf("build identity output = %q, want %q", got, want)
	}
}
