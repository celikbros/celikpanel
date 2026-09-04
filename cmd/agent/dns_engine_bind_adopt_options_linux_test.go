//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The takeover's whole promise about a failure is that the operator's own
// configuration comes back exactly. R-039 said the snapshot machinery is
// content-based and already covers a configuration the panel never wrote; this
// proves it for the case R-042 adds, where the takeover does not merely add a
// block but removes directives the operator wrote. Both files are hashed before
// the mutation, changed, and hashed again after the restore.
//
// Devralmanın bir başarısızlık hakkındaki bütün sözü, operatörün kendi
// yapılandırmasının birebir geri gelmesidir. R-039, anlık görüntü düzeneğinin
// içerik temelli olduğunu ve panelin hiç yazmadığı bir yapılandırmayı zaten
// kapsadığını söylüyordu; bu, R-042'nin eklediği durum için - devralmanın
// yalnız bir blok eklemeyip operatörün yazdığı direktifleri kaldırdığı durum
// için - bunu kanıtlar. İki dosya da mutasyondan önce özetlenir, değiştirilir
// ve geri yüklemeden sonra yeniden özetlenir.
func TestBINDTakeoverReplacesOnDiskAndRestoresByteForByte(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the BIND configuration mutation writes managed files and requires root")
	}
	directory := t.TempDir()
	layout := bindHostLayout{
		GenerationRoot: filepath.Join(directory, "generations"),
		OptionsConfig:  filepath.Join(directory, "named.conf.options"),
		AnchorConfig:   filepath.Join(directory, "named.conf.local"),
	}
	const anchor = "// the operator's own zones\nzone \"example.test\" {\n" +
		"\ttype master;\n\tfile \"/var/lib/bind/example.test.zone\";\n};\n"
	before := map[string][]byte{
		layout.OptionsConfig: []byte(handConfiguredBINDOptions),
		layout.AnchorConfig:  []byte(anchor),
	}
	digests := map[string]string{}
	for path, content := range before {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		digests[path] = hex.EncodeToString(sum[:])
	}
	reader := func(
		path string, mode os.FileMode, allowAbsent bool,
	) (dnsFileSnapshot, error) {
		if allowAbsent {
			return dnsFileSnapshot{}, errors.New("unexpected absent BIND config")
		}
		return captureBINDConfigSnapshot(path, mode)
	}

	// The exclusive authority is the behaviour before this change, and it is
	// still the behaviour everywhere but a takeover: a hand-configured server
	// is refused.
	//
	// Dışlayıcı yetki, bu değişiklikten önceki davranıştır ve devralma dışında
	// hâlâ her yerdeki davranıştır: elle yapılandırılmış bir sunucu reddedilir.
	if _, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, "", bindOptionsExclusive, reader,
	); err == nil {
		t.Fatal("a path without the operator's consent must still refuse")
	}

	mutation, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, "", bindOptionsTakeover, reader,
	)
	if err != nil {
		t.Fatalf("the takeover still refuses a hand-configured server: %v", err)
	}
	if len(mutation.adopted) != 3 {
		t.Fatalf("the takeover recorded %d directives, want 3: %+v",
			len(mutation.adopted), mutation.adopted)
	}
	ctx := context.Background()
	if err := mutation.apply(ctx); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(layout.OptionsConfig)
	if err != nil {
		t.Fatal(err)
	}
	options := string(written)
	for _, gone := range []string{
		"recursion no;\n", "allow-transfer { 203.0.113.7; };",
	} {
		if strings.Contains(strings.Split(options, bindOptionsMarkerBegin)[0], gone) {
			t.Fatalf("%q is still outside CelikPanel's block on disk", gone)
		}
	}
	if !strings.Contains(options, bindOptionsMarkerBegin) ||
		!strings.Contains(options, "allow-transfer { none; };") ||
		!strings.Contains(options, "directory \"/var/cache/bind\";") {
		t.Fatalf("the written options are not the takeover's:\n%s", options)
	}

	if err := mutation.restore(ctx); err != nil {
		t.Fatal(err)
	}
	for path, digest := range digests {
		restored, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		sum := sha256.Sum256(restored)
		if hex.EncodeToString(sum[:]) != digest {
			t.Fatalf("%s came back as\n%s\nwant digest %s", path, restored, digest)
		}
	}
}
