// gen-distro-matrix writes docs/DISTRO-SUPPORT.md and docs/DISTRO-SUPPORT.tr.md
// from the component catalogue. Run it from the repository root after any
// catalogue change; the guard test in internal/core fails until you do.
//
// gen-distro-matrix, bileşen kataloğundan docs/DISTRO-SUPPORT.md ve
// docs/DISTRO-SUPPORT.tr.md dosyalarını yazar. Her katalog değişikliğinden
// sonra depo kökünden çalıştırın; çalıştırana dek internal/core'daki bekçi
// test düşer.
package main

import (
	"fmt"
	"os"

	"github.com/alicelik/celikpanel/internal/core"
)

func main() {
	for _, f := range []struct{ path, lang string }{
		{"docs/DISTRO-SUPPORT.md", "en"},
		{"docs/DISTRO-SUPPORT.tr.md", "tr"},
	} {
		if err := os.WriteFile(f.path, []byte(core.RenderDistroMatrix(f.lang)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f.path, err)
			os.Exit(1)
		}
		fmt.Println("wrote", f.path)
	}
}
