package recoveryruntime

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func sampleManifest() Manifest {
	result := Manifest{Files: map[string]string{}}
	for _, spec := range ExpectedFiles() {
		result.Files[spec.Path] = Digest([]byte(spec.Path))
	}
	return result
}
func TestManifestAndSelectionCanonicalRoundTrip(t *testing.T) {
	source := sampleManifest()
	raw, err := EncodeManifest(source)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := EncodeManifest(parsed)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatalf("canonical round trip: %v", err)
	}
	selection, err := EncodeSelection(Digest(raw))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ParseSelection(selection)
	if err != nil || digest != Digest(raw) {
		t.Fatalf("selection round trip: %v", err)
	}
	files := ExpectedFiles()
	if len(files) != 12 {
		t.Fatalf("fixed inventory contains %d entries", len(files))
	}
	files[0].Path = "evil"
	if ExpectedFiles()[0].Path == "evil" {
		t.Fatal("caller changed shared allowlist")
	}
}
func TestManifestRejectsNoncanonicalOrUnsupportedInventory(t *testing.T) {
	raw, _ := EncodeManifest(sampleManifest())
	lines := strings.Split(string(raw), "\n")
	swapped := append([]string(nil), lines...)
	swapped[3], swapped[4] = swapped[4], swapped[3]
	for name, broken := range map[string][]byte{
		"empty":           nil,
		"too large":       bytes.Repeat([]byte("x"), MaxManifestSize+1),
		"new format":      bytes.Replace(raw, []byte("runtime-v1"), []byte("runtime-v2"), 1),
		"new protocol":    bytes.Replace(raw, []byte("protocol=1"), []byte("protocol=2"), 1),
		"old snapshot":    bytes.Replace(raw, []byte("snapshot=6"), []byte("snapshot=5"), 1),
		"crlf":            bytes.ReplaceAll(raw, []byte("\n"), []byte("\r\n")),
		"missing newline": raw[:len(raw)-1],
		"trailing line":   append(append([]byte(nil), raw...), []byte("extra\n")...),
		"unsorted":        []byte(strings.Join(swapped, "\n")),
		"alias":           bytes.Replace(raw, []byte("bin/recovery"), []byte("bin/../recovery"), 1),
		"digest case":     bytes.Replace(raw, []byte(lines[3][:64]), []byte(strings.ToUpper(lines[3][:64])), 1),
		"one space":       bytes.Replace(raw, []byte("  bin/"), []byte(" bin/"), 1),
		"duplicate":       []byte(strings.Replace(string(raw), lines[4], lines[3], 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest(broken); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("accepted malformed manifest: %v", err)
			}
		})
	}
	bad := sampleManifest()
	bad.Files["../arbitrary"] = Digest([]byte("x"))
	if _, err := EncodeManifest(bad); err == nil {
		t.Fatal("encoded extra arbitrary source")
	}
	bad = sampleManifest()
	delete(bad.Files, "bin/recovery")
	if _, err := EncodeManifest(bad); err == nil {
		t.Fatal("encoded missing executor")
	}
}
func TestSelectionRejectsUnknownOrAmbiguousIdentity(t *testing.T) {
	raw, _ := EncodeSelection(strings.Repeat("a", 64))
	for _, broken := range [][]byte{nil, raw[:len(raw)-1], append(append([]byte(nil), raw...), raw...), bytes.Replace(raw, []byte("v1"), []byte("v2"), 1), bytes.Replace(raw, []byte(strings.Repeat("a", 64)), []byte(strings.Repeat("A", 64)), 1), bytes.Repeat([]byte("x"), MaxSelectionSize+1)} {
		if _, err := ParseSelection(broken); err == nil {
			t.Fatal("accepted noncanonical selection")
		}
	}
	if _, err := EncodeSelection("../untrusted"); err == nil {
		t.Fatal("encoded path as runtime identity")
	}
}
