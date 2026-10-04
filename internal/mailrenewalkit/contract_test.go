package mailrenewalkit

import (
	"bytes"
	"strings"
	"testing"
)

func fixture(t *testing.T) (Manifest, []byte, map[string][]byte) {
	t.Helper()
	m, f, e := Payload([]byte("reviewed binary"))
	if e != nil {
		t.Fatal(e)
	}
	r, e := Encode(m)
	if e != nil {
		t.Fatal(e)
	}
	return m, r, f
}
func TestArtifactBindsNativeActionsAndIndependentCode(t *testing.T) {
	m, r, f := fixture(t)
	if _, e := Verify(r, f); e != nil {
		t.Fatal(e)
	}
	path := InstalledRoot + "/" + m.Generation + "/renew"
	if !strings.Contains(string(f[ServiceName]), "ExecStart="+path+" --process-pending\n") || !strings.Contains(string(f[HookName]), "exec "+path+" --queue ") {
		t.Fatal("entry not fixed")
	}
	for _, raw := range f {
		if bytes.Contains(raw, []byte("/opt/celikpanel")) || bytes.Contains(raw, []byte("celikpanel-agent.service")) || bytes.Contains(raw, []byte("@RENEW@")) {
			t.Fatal("management dependency")
		}
	}
	if id, e := ServiceGeneration(f[ServiceName]); e != nil || id != m.Generation {
		t.Fatal(id, e)
	}
	if !bytes.Contains(f[TimerName], []byte("OnUnitInactiveSec=5min\n")) || !bytes.Contains(f[ServiceName], []byte("TimeoutStartSec=180s\n")) {
		t.Fatal("retry not bounded")
	}
}
func TestArtifactRejectsSubstitutionAndUnsupportedEvidence(t *testing.T) {
	_, raw, f := fixture(t)
	for _, key := range []string{BinaryName, ServiceName, TimerName, HookName} {
		t.Run(key, func(t *testing.T) {
			copyFiles := map[string][]byte{}
			for k, v := range f {
				copyFiles[k] = v
			}
			copyFiles[key] = append(append([]byte{}, f[key]...), 'x')
			if _, e := Verify(raw, copyFiles); e == nil {
				t.Fatal("substituted file accepted")
			}
		})
	}
	for _, bad := range [][]byte{append([]byte(" "), raw...), bytes.Replace(raw, []byte("/v1"), []byte("/v2"), 1), bytes.Replace(raw, []byte("{"), []byte("{\"schema\":\"foreign\","), 1), bytes.Replace(raw, []byte("\"ledger_version\":1"), []byte("\"ledger_version\":2"), 1)} {
		if _, e := Verify(bad, f); e == nil {
			t.Fatal("unsupported evidence accepted")
		}
	}
	f["owner-note"] = []byte("retain")
	if _, e := Verify(raw, f); e == nil {
		t.Fatal("extra inventory accepted")
	}
}
func TestServiceRejectsChangedAuthority(t *testing.T) {
	_, _, f := fixture(t)
	for _, bad := range [][]byte{nil, []byte("old agent unit"), append(append([]byte{}, f[ServiceName]...), '\n'), bytes.Replace(f[ServiceName], []byte("--process-pending"), []byte("--self-update-worker"), 1)} {
		if _, e := ServiceGeneration(bad); e == nil {
			t.Fatal("foreign service accepted")
		}
	}
}

func TestHookIdentityCannotAcceptLegacyOrEditedIndependentHook(t *testing.T) {
	m, files, err := Payload([]byte("helper"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := HookGeneration(files[HookName]); err != nil || got != m.Generation {
		t.Fatal(got, err)
	}
	for _, raw := range [][]byte{LegacyHook(), append(files[HookName], []byte("# owner edit\n")...), []byte("/usr/libexec/celikpanel/mail-renewal/" + m.Generation + "/renew")} {
		if _, err := HookGeneration(raw); err == nil {
			t.Fatal("unsupported hook accepted")
		}
	}
}
