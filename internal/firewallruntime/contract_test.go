package firewallruntime

import (
	"bytes"
	"strings"
	"testing"
)

func TestRuntimeBindsBinaryUnitAndImmutablePath(t *testing.T) {
	binary := []byte("reviewed fixture binary")
	m, unit, err := Build(binary)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(raw, binary, unit); err != nil {
		t.Fatal(err)
	}
	path := InstalledRoot + "/" + m.Generation + "/restore"
	if strings.Count(string(unit), path) != 2 || strings.Contains(string(unit), "/opt/celikpanel/bin/agent") || strings.Contains(string(unit), "@RESTORE@") {
		t.Fatal("unit is not independently bound")
	}
	other, _, _ := Build([]byte("next reviewed binary"))
	if other.Generation == m.Generation {
		t.Fatal("versions collide")
	}
}

func TestRuntimeRefusesForeignVersionPayloadAndNoncanonicalEvidence(t *testing.T) {
	binary := []byte("reviewed fixture binary")
	m, unit, _ := Build(binary)
	raw, _ := Encode(m)
	for _, bad := range [][]byte{bytes.Replace(raw, []byte("/v1"), []byte("/v2"), 1), append([]byte(" "), raw...), bytes.Replace(raw, []byte("{"), []byte("{\"unknown\":true,"), 1), bytes.Replace(raw, []byte("{"), []byte("{\"schema\":\"other\","), 1)} {
		if _, err := Verify(bad, binary, unit); err == nil {
			t.Fatal("unsupported manifest accepted")
		}
	}
	if _, err := Verify(raw, []byte("foreign binary"), unit); err == nil {
		t.Fatal("foreign binary accepted")
	}
	changed := bytes.Replace(unit, []byte("--restore"), []byte("--other"), 1)
	if _, err := Verify(raw, binary, changed); err == nil {
		t.Fatal("changed unit accepted")
	}
	m.Generation = strings.Repeat("0", 64)
	if _, err := Encode(m); err == nil {
		t.Fatal("foreign generation accepted")
	}
}

func TestUnitGenerationClosedTemplate(t *testing.T) {
	manifest, unit, err := Build([]byte("helper"))
	if err != nil {
		t.Fatal(err)
	}
	if id, err := UnitGeneration(unit); err != nil || id != manifest.Generation {
		t.Fatal(id, err)
	}
	for _, raw := range [][]byte{nil, []byte("legacy unit"), append(append([]byte{}, unit...), '\n'), bytes.Replace(unit, []byte("--restore"), []byte("--other"), 1), bytes.Replace(unit, []byte(manifest.Generation), []byte(strings.Repeat("f", 64)), 1)} {
		if _, err := UnitGeneration(raw); err == nil {
			t.Fatal("foreign unit accepted")
		}
	}
}
