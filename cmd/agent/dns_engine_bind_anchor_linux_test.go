//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// openAnchorFixture opens a directory the way the real walkers do, so the test
// exercises the same descriptor shape production uses.
// openAnchorFixture, bir dizini gerçek yürüyücülerin açtığı gibi açar; böylece
// test, üretimin kullandığı betimleyici biçimini sınar.
func openAnchorFixture(t *testing.T, path string, mode uint32) int {
	t.Helper()
	// unix.Chmod, not os.Chmod: os.FileMode encodes setuid, setgid and sticky
	// as high bits of its own, so os.Chmod would silently drop 0o4000, 0o2000
	// and 0o1000 and the special-bit cases below would assert nothing.
	// os.Chmod değil unix.Chmod: os.FileMode setuid, setgid ve sticky bitlerini
	// kendi üst bitlerinde kodlar; bu yüzden os.Chmod 0o4000, 0o2000 ve 0o1000
	// değerlerini sessizce düşürür ve aşağıdaki özel-bit durumları hiçbir şey
	// sınamamış olurdu.
	if err := unix.Chmod(path, mode&0o7777); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	fd, err := unix.Open(
		path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	return fd
}

// The defect this fixes: the official Arch image ships `/` as 0555, and every
// DNS engine walk demanded exactly 0755 from it, so neither BIND nor PowerDNS
// could reach its intent journal on a stock Arch host (risk R-018).
// Düzeltilen kusur: resmi Arch imajı `/` dizinini 0555 sunar ve her DNS motoru
// yürüyüşü ondan tam 0755 istiyordu; bu yüzden standart bir Arch sunucusunda ne
// BIND ne PowerDNS intent günlüğüne ulaşabiliyordu (risk R-018).
func TestInheritedBINDAnchorAcceptsAStockArchRootMode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("inherited BIND anchor tests require root")
	}
	for _, mode := range []uint32{0o555, 0o755, 0o511, 0o551} {
		directory := t.TempDir()
		fd := openAnchorFixture(t, directory, mode)
		if _, err := validateInheritedBINDAnchorFD(fd, "test anchor"); err != nil {
			t.Fatalf("mode %04o must be accepted as an inherited anchor: %v", mode, err)
		}
	}
}

// The property the anchor policy actually protects: nobody but root may
// substitute an entry along the path. Anything that lets a non-root user write
// into the directory, or that carries a special bit, must still be refused.
// Çıpa politikasının gerçekten koruduğu özellik: yol üzerindeki bir girdiyi
// root'tan başkası değiştiremez. Root olmayan bir kullanıcının dizine
// yazmasına izin veren ya da özel bit taşıyan her şey yine reddedilmelidir.
func TestInheritedBINDAnchorRefusesEveryWritableOrSpecialShape(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("inherited BIND anchor tests require root")
	}
	for name, mode := range map[string]uint32{
		"group writable":        0o775,
		"world writable":        0o757,
		"both writable":         0o777,
		"group write only":      0o720,
		"not world traversable": 0o700,
		"owner only":            0o750,
		"setuid":                0o4755,
		"setgid":                0o2755,
		"sticky":                0o1755,
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			fd := openAnchorFixture(t, directory, mode)
			if _, err := validateInheritedBINDAnchorFD(fd, "test anchor"); err == nil {
				t.Fatalf("mode %04o must be refused", mode)
			}
		})
	}
}

// Root ownership is not relaxed. A directory owned by anyone else is a path an
// attacker can rearrange, whatever its mode says.
// Root sahipliği gevşetilmez. Başkasına ait bir dizin, kipi ne derse desin, bir
// saldırganın yeniden düzenleyebileceği bir yoldur.
func TestInheritedBINDAnchorRequiresRootOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("inherited BIND anchor tests require root")
	}
	for _, owner := range []struct {
		name     string
		uid, gid int
	}{
		{"non-root user", 1207, 0},
		{"non-root group", 0, 1208},
		{"neither root", 1207, 1208},
	} {
		t.Run(owner.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chown(directory, owner.uid, owner.gid); err != nil {
				t.Fatalf("chown: %v", err)
			}
			fd := openAnchorFixture(t, directory, 0o755)
			if _, err := validateInheritedBINDAnchorFD(fd, "test anchor"); err == nil {
				t.Fatalf("%s must be refused", owner.name)
			}
		})
	}
}

// A file is not an anchor, and neither is a symlink to one. The open flags
// already refuse the symlink; this pins the directory requirement itself.
// Bir dosya çıpa değildir, ona giden bir sembolik bağ da değildir. Açma
// bayrakları sembolik bağı zaten reddeder; bu test dizin şartını sabitler.
func TestInheritedBINDAnchorRequiresADirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("inherited BIND anchor tests require root")
	}
	regular := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(regular, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := validateInheritedBINDAnchorFD(fd, "test anchor"); err == nil {
		t.Fatal("a regular file must be refused as an inherited anchor")
	}
}

// The directories this product creates keep their exact-mode assertion. If the
// anchor policy ever leaked into them, a managed BIND root could drift from
// 0755 unnoticed, which is the opposite of what R-018 asked for.
// Bu ürünün oluşturduğu dizinler tam-kip dayatmasını korur. Çıpa politikası
// oraya sızarsa, yönetilen bir BIND kökü 0755'ten fark edilmeden kayabilir; bu
// da R-018'in istediğinin tam tersidir.
func TestManagedBINDDirectoriesKeepTheirExactModeAssertion(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("exact BIND ownership tests require root")
	}
	directory := t.TempDir()
	fd := openAnchorFixture(t, directory, 0o555)
	if _, err := validateInheritedBINDAnchorFD(fd, "inherited"); err != nil {
		t.Fatalf("0555 must pass the inherited anchor policy: %v", err)
	}
	if _, err := validateExactBINDDirectoryFD(
		fd, 0, 0, bindManagedRootMode, "managed",
	); err == nil {
		t.Fatal("0555 must still fail the exact managed-directory assertion")
	}
}
