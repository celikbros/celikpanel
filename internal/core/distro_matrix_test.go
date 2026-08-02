package core

import (
	"os"
	"strings"
	"testing"
)

// The committed matrix must always match the catalogue. A generated document
// nobody regenerates is worse than none: it reads as authoritative and lies.
// This guard turns "forgot to regenerate" from silent drift into a red test
// with the exact command to run.
//
// Commit'li matris her zaman katalogla uyuşmalıdır. Kimsenin yeniden
// üretmediği üretilmiş belge, hiç olmamasından kötüdür: yetkili görünür ve
// yalan söyler. Bu bekçi, "yeniden üretmeyi unuttum"u sessiz kaymadan,
// çalıştırılacak komutu söyleyen kırmızı bir teste çevirir.
func TestDistroMatrixIsCurrent(t *testing.T) {
	for path, lang := range map[string]string{
		"../../docs/DISTRO-SUPPORT.md":    "en",
		"../../docs/DISTRO-SUPPORT.tr.md": "tr",
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v — run `make distro-matrix` from the repo root", path, err)
		}
		want := RenderDistroMatrix(lang)
		// The repo may check files out with CRLF on Windows; compare content,
		// not line endings. / Depo Windows'ta CRLF ile checkout edebilir;
		// satır sonlarını değil içeriği karşılaştır.
		if strings.ReplaceAll(string(got), "\r\n", "\n") != want {
			t.Errorf("%s is stale: the catalogue changed without regenerating — run `make distro-matrix`", path)
		}
	}
}

// Every offered cell must be honest both ways: a component whose install path
// is distro packages must name packages for at least ONE family (otherwise it
// is offered nowhere and the row is dead weight), and the portable section is
// exactly the components with no package mapping at all.
// Sunulan her hücre iki yönde dürüst olmalı: kurulum yolu dağıtım paketi olan
// bileşen en az BİR aile için paket saymalıdır (yoksa hiçbir yerde sunulmuyor
// demektir, satır ölü ağırlıktır); taşınabilir bölüm de tam olarak hiç paket
// eşlemesi olmayan bileşenlerdir.
func TestEveryPackagedComponentIsOfferedSomewhere(t *testing.T) {
	for i := range ManagedServices {
		svc := &ManagedServices[i]
		if len(svc.Packages) == 0 {
			continue
		}
		total := 0
		for fam, pkgs := range svc.Packages {
			if len(pkgs) == 0 {
				t.Errorf("%s: family %q declared with zero packages — delete the key instead; an empty list reads as offered", svc.ID, fam)
			}
			total += len(pkgs)
		}
		if total == 0 {
			t.Errorf("%s: has a Packages map but is offered on no distro", svc.ID)
		}
	}
}
