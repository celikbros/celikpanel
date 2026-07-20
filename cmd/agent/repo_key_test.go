package main

import "testing"

// A repo key is written to /usr/share/keyrings and then TRUSTED by apt, so what
// counts as a key is a security decision. PGDG publishes ASCII-armoured, Sury
// publishes a binary keyring; accepting only one silently locked out every
// vendor using the other, and accepting anything would let an HTML error page
// become a trusted keyring.
//
// Depo anahtarı /usr/share/keyrings'e yazılır ve apt tarafından GÜVENİLİR
// sayılır; bu yüzden neyin anahtar sayıldığı bir güvenlik kararıdır. PGDG
// ASCII-zırhlı, Sury ikili keyring yayınlar; yalnız birini kabul etmek diğerini
// kullanan her vendor'ı sessizce dışarıda bırakıyordu, her şeyi kabul etmek ise
// bir HTML hata sayfasının güvenilir keyring olmasına izin verirdi.
func TestIsBinaryPublicKey(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		// Sury's apt.gpg begins 0x99 0x01 0x8d — old-format packet, tag 6.
		// Sury'nin apt.gpg'si 0x99 0x01 0x8d ile başlar — eski biçim, tag 6.
		{"sury old-format public key", []byte{0x99, 0x01, 0x8d, 0x04}, true},
		{"new-format public key", []byte{0xC6, 0x01, 0x02}, true},
		// Tag 2 is a signature, not a public key — a detached signature file
		// must not be installed as a keyring.
		// Tag 2 imzadır, açık anahtar değil — ayrık imza dosyası keyring olarak
		// kurulmamalıdır.
		{"signature packet", []byte{0x89, 0x01, 0x02}, false},
		{"html error page", []byte("<!DOCTYPE html><h1>404</h1>"), false},
		{"empty", []byte{}, false},
		{"armoured text is not binary", []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isBinaryPublicKey(c.in); got != c.want {
				t.Errorf("isBinaryPublicKey = %v, want %v", got, c.want)
			}
		})
	}
}

// The keyring filename must state the format, because apt reads the file by
// content but the source line points at a name — writing binary bytes into a
// .asc leaves a repo apt refuses to verify.
// Keyring dosya adı biçimi bildirmelidir; çünkü apt dosyayı içeriğine göre okur
// ama kaynak satırı bir adı gösterir — ikili baytları .asc içine yazmak, apt'ın
// doğrulamayı reddettiği bir depo bırakır.
func TestRepoKeyringPathNamesTheFormat(t *testing.T) {
	if got := repoKeyringPath("pgdg", true); got != "/usr/share/keyrings/celikpanel-pgdg.asc" {
		t.Errorf("armoured key path = %q", got)
	}
	if got := repoKeyringPath("sury", false); got != "/usr/share/keyrings/celikpanel-sury.gpg" {
		t.Errorf("binary key path = %q", got)
	}
	// Both names must be reachable for status/removal, or switching formats
	// would strand a trusted keyring nothing points at.
	// Durum/kaldırma için iki ad da erişilebilir olmalı; yoksa biçim değiştirmek,
	// hiçbir kaynağın göstermediği güvenilir bir keyring bırakırdı.
	if got := repoKeyringCandidates("sury"); len(got) != 2 {
		t.Errorf("candidates = %v, want both formats", got)
	}
}

// Version ordering decides which version the drawer shows first and which unit
// firstPresentUnit enables. The previous implementation read the TRAILING
// integer, which is 0 for every php8.x-fpm ("fpm" is not a number) — so all PHP
// versions tied and the order was whatever apt-cache happened to print.
// Sürüm sıralaması, çekmecenin hangi sürümü başta göstereceğine ve
// firstPresentUnit'in hangi unit'i etkinleştireceğine karar verir. Önceki
// uygulama SONDAKİ tam sayıyı okuyordu; bu her php8.x-fpm için 0'dır ("fpm"
// sayı değil) — yani tüm PHP sürümleri berabere kalıyor ve sıra apt-cache ne
// bastıysa o oluyordu.
func TestVersionOf(t *testing.T) {
	cases := []struct {
		pkg              string
		wantMaj, wantMin int
	}{
		{"postgresql-17", 17, 0},
		{"postgresql-9", 9, 0},
		{"php8.3-fpm", 8, 3},
		{"php8.4-fpm", 8, 4},
		{"php5.6-fpm", 5, 6},
		{"php-fpm", 0, 0},
	}
	for _, c := range cases {
		maj, min := versionOf(c.pkg)
		if maj != c.wantMaj || min != c.wantMin {
			t.Errorf("versionOf(%q) = (%d,%d), want (%d,%d)", c.pkg, maj, min, c.wantMaj, c.wantMin)
		}
	}
	// The property that matters: 8.4 must outrank 8.3, and 8.3 must outrank 5.6.
	// Önemli olan özellik: 8.4, 8.3'ü; 8.3 de 5.6'yı geçmelidir.
	newer := func(a, b string) bool {
		ai, an := versionOf(a)
		bi, bn := versionOf(b)
		if ai != bi {
			return ai > bi
		}
		return an > bn
	}
	if !newer("php8.4-fpm", "php8.3-fpm") {
		t.Error("8.4 must sort before 8.3")
	}
	if !newer("php8.3-fpm", "php5.6-fpm") {
		t.Error("8.3 must sort before 5.6")
	}
	if !newer("postgresql-17", "postgresql-9") {
		t.Error("PGDG ordering regressed")
	}
}
