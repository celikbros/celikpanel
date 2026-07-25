package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
)

// Managed vendor repositories. A distro freezes one major version of a
// database/runtime per release; the vendor's own apt repo (PGDG for PostgreSQL,
// and later others) carries every current major at once. Enabling one lets the
// operator pick the version they need instead of whatever the OS shipped.
//
// This widens the trust boundary, so it gets the same discipline as installing
// a service: it is opt-in, the panel only ever passes catalog-declared repos
// (the UI can never inject a URL), the signing key is pinned per-repo with apt's
// signed-by= (no global apt-key trust), and DisableRepo removes the source and
// key cleanly. The key is written in whichever form the vendor publishes —
// armoured (.asc) or binary keyring (.gpg); apt reads both directly, so no gpg
// is pulled in to convert between them and the minimal-install promise holds.
//
// Yönetilen vendor depoları. Dağıtım her sürümde bir veritabanı/çalışma
// zamanının tek major'unu dondurur; vendor'ın kendi apt deposu (PostgreSQL için
// PGDG, ileride diğerleri) tüm güncel major'ları aynı anda taşır. Birini açmak,
// operatörün OS'un getirdiğiyle yetinmek yerine ihtiyacı olan sürümü seçmesini
// sağlar.
//
// Bu güven sınırını genişletir; bu yüzden servis kurmakla aynı disipline tabidir:
// opt-in'dir, panel yalnız katalogda tanımlı depoları geçirir (UI asla URL
// enjekte edemez), imza anahtarı depo başına apt signed-by= ile sabitlenir
// (küresel apt-key güveni yok) ve DisableRepo kaynağı + anahtarı temizce
// kaldırır. Anahtar, vendor hangi biçimde yayınlıyorsa o biçimde yazılır —
// zırhlı (.asc) ya da ikili keyring (.gpg); apt ikisini de doğrudan okur, bu
// yüzden dönüştürmek için gpg çekilmez ve minimal kurulum sözü korunur.

// validRepoID bounds the id to a filename-safe token, since it names the
// keyring and source files under /etc and /usr/share.
// validRepoID, id'yi dosya-adı güvenli bir dizgeye sınırlar; çünkü /etc ve
// /usr/share altındaki keyring ve kaynak dosyalarını adlandırır.
var validRepoID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// A signing key ships either ASCII-armoured (.asc) or as a binary keyring
// (.gpg) and apt's signed-by= accepts both — but only if the file is named for
// what it actually contains. PGDG publishes armoured; Sury publishes binary, so
// assuming one format silently locked out every vendor using the other.
// İmza anahtarı ya ASCII-zırhlı (.asc) ya da ikili keyring (.gpg) olarak
// yayınlanır ve apt'ın signed-by='ı ikisini de kabul eder — ama yalnız dosya
// gerçekte içerdiği şeye göre adlandırılmışsa. PGDG zırhlı, Sury ikili yayınlar;
// tek biçim varsaymak, diğerini kullanan her vendor'ı sessizce dışarıda bırakıyordu.
func repoKeyringPath(id string, armored bool) string {
	if armored {
		return "/usr/share/keyrings/celikpanel-" + id + ".asc"
	}
	return "/usr/share/keyrings/celikpanel-" + id + ".gpg"
}

// repoKeyringCandidates lists both names so status and removal do not depend on
// knowing which format was used when the repo was enabled.
// repoKeyringCandidates iki adı da listeler; böylece durum ve kaldırma, depo
// açılırken hangi biçimin kullanıldığını bilmeye bağlı kalmaz.
func repoKeyringCandidates(id string) []string {
	return []string{repoKeyringPath(id, true), repoKeyringPath(id, false)}
}
func repoSourcePath(id string) string { return "/etc/apt/sources.list.d/celikpanel-" + id + ".list" }

// EnableRepoRequest names WHICH repository, never WHAT it points at.
//
// It used to carry KeyURL and SourceTemplate straight from the caller, and the
// agent wrote them to disk after checking only the "https://" and "deb https://"
// prefixes. That made the panel an AUTHORITY on what apt trusts: anything able
// to reach this RPC could pin a signing key of its choosing and add a matching
// source, and the next install of a whitelisted package could then be satisfied
// from it — defeating the compiled package whitelist that is the whole point of
// the catalogue. Agent.InstallService never had this hole: it resolves req.ID
// against core.ManagedServices and installs only the packages IT finds there.
//
// The invariant, now enforced here too: the agent re-derives every fact it acts
// on from its own compiled catalogue. The panel is a courier, not an authority.
//
// EnableRepoRequest HANGİ deponun olduğunu söyler, neyi gösterdiğini asla.
//
// Eskiden KeyURL ve SourceTemplate'i doğrudan çağırandan taşıyordu ve agent
// yalnız "https://" ile "deb https://" öneklerini denetleyip bunları diske
// yazıyordu. Bu, panelin apt'ın neye güveneceği konusunda YETKİLİ olması
// demekti: bu RPC'ye ulaşabilen herhangi bir şey kendi seçtiği imza anahtarını
// sabitleyip eşleşen bir kaynak ekleyebilir, sonra whitelist'teki herhangi bir
// paketin kurulumu oradan karşılanabilirdi — kataloğun bütün varlık sebebi olan
// derlenmiş paket whitelist'i böylece çökerdi. Agent.InstallService'te bu açık
// hiç yoktu: req.ID'yi core.ManagedServices'e karşı çözer ve yalnız ORADA
// bulduğu paketleri kurar.
//
// Artık burada da geçerli olan değişmez kural: agent, üzerine iş yaptığı her
// olguyu kendi derlenmiş kataloğundan yeniden türetir. Panel taşıyıcıdır,
// yetkili değil.
type EnableRepoRequest struct {
	RepoID string `json:"repo_id"`
}

// repoFromCatalogue finds the catalogue repo with this id. Nothing outside the
// binary can add one.
// repoFromCatalogue, bu id'ye sahip katalog deposunu bulur. Binary dışından
// hiçbir şey yeni bir tane ekleyemez.
func repoFromCatalogue(id string) *core.ManagedRepo {
	for i := range core.ManagedServices {
		if r := core.ManagedServices[i].Repo; r != nil && r.ID == id {
			return r
		}
	}
	return nil
}

type RepoStatusResponse struct {
	Enabled bool   `json:"enabled"`
	Source  string `json:"source,omitempty"`
	Error   string `json:"error,omitempty"`
}

// EnableRepo pins the vendor's signing key and writes an apt source that trusts
// only that key, then refreshes just this source's package list. It is
// idempotent: re-enabling simply rewrites the key and source.
// EnableRepo, vendor'ın imza anahtarını sabitler ve yalnız o anahtara güvenen
// bir apt kaynağı yazar, sonra yalnız bu kaynağın paket listesini tazeler.
// İdempotenttir: yeniden açmak anahtarı ve kaynağı yeniden yazar.
func (a *Agent) EnableRepo(req *EnableRepoRequest, resp *RepoStatusResponse) error {
	if detectPkgFamily() != "apt" {
		resp.Error = "managed repositories are only supported on apt (Debian/Ubuntu) systems yet"
		return nil
	}
	if !validRepoID.MatchString(req.RepoID) {
		resp.Error = "invalid repo id"
		return nil
	}
	repo := repoFromCatalogue(req.RepoID)
	if repo == nil {
		resp.Error = "unknown repository"
		return nil
	}
	if !strings.HasPrefix(repo.KeyURL, "https://") {
		resp.Error = "repo key URL must be https"
		return nil
	}

	codename := osCodename()
	if codename == "" {
		resp.Error = "could not determine the distribution codename (/etc/os-release)"
		return nil
	}
	source := strings.ReplaceAll(repo.SourceTemplate, "{codename}", codename)
	// Only a plain "deb https://…" line is accepted, and signed-by= is injected
	// so the source trusts our pinned key and nothing else.
	// Yalnız düz bir "deb https://…" satırı kabul edilir ve signed-by= enjekte
	// edilir; böylece kaynak yalnız sabitlediğimiz anahtara güvenir.
	if !strings.HasPrefix(source, "deb https://") {
		resp.Error = "repo source must be a deb https:// line"
		return nil
	}
	// The key is fetched BEFORE the source line is built: its format decides
	// the keyring filename, and signed-by= must point at the name we actually
	// write. / Anahtar, kaynak satırından ÖNCE indirilir: biçimi keyring dosya
	// adını belirler ve signed-by= gerçekten yazdığımız adı göstermelidir.
	key, armored, err := fetchRepoKey(repo.KeyURL)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	keyring := repoKeyringPath(req.RepoID, armored)
	signed := "deb [signed-by=" + keyring + "] " + strings.TrimPrefix(source, "deb ")

	// Re-enabling with a different key format must not leave the old file
	// behind: apt would keep trusting a keyring nothing points at any more.
	// Farklı bir anahtar biçimiyle yeniden açmak eski dosyayı bırakmamalı:
	// apt, artık hiçbir kaynağın göstermediği bir keyring'e güvenmeyi sürdürürdü.
	for _, stale := range repoKeyringCandidates(req.RepoID) {
		if stale != keyring {
			_ = os.Remove(stale)
		}
	}
	// apt drops to the unprivileged _apt user to run gpgv, so it must be able to
	// read the keyring. The agent runs with UMask=0027, which would turn a 0644
	// WriteFile into 0640 root:celikpanel — unreadable to _apt, and verification
	// then fails with "is not signed". chmod after writing defeats the umask so
	// both files are world-readable, as apt requires.
	// apt, gpgv'yi çalıştırmak için yetkisiz _apt kullanıcısına düşer; bu yüzden
	// keyring'i okuyabilmelidir. Agent UMask=0027 ile koşar; bu, 0644 WriteFile'ı
	// 0640 root:celikpanel'e çevirir — _apt okuyamaz ve doğrulama "imzasız"
	// hatasıyla başarısız olur. Yazdıktan sonra chmod umask'ı ezer; apt'ın
	// beklediği gibi her iki dosya da dünyaca-okunur olur.
	if err := os.WriteFile(keyring, key, 0o644); err != nil {
		resp.Error = fmt.Sprintf("write keyring: %v", err)
		return nil
	}
	if err := os.Chmod(keyring, 0o644); err != nil {
		resp.Error = fmt.Sprintf("chmod keyring: %v", err)
		return nil
	}
	if err := os.WriteFile(repoSourcePath(req.RepoID), []byte(signed+"\n"), 0o644); err != nil {
		resp.Error = fmt.Sprintf("write source: %v", err)
		return nil
	}
	if err := os.Chmod(repoSourcePath(req.RepoID), 0o644); err != nil {
		resp.Error = fmt.Sprintf("chmod source: %v", err)
		return nil
	}

	// Refresh only this source's lists (not the whole system), and do not prune
	// other sources' cached data.
	// Yalnız bu kaynağın listelerini tazele (tüm sistemi değil) ve diğer
	// kaynakların önbelleğini budama.
	cmd := exec.Command("apt-get", "update",
		"-o", "Dir::Etc::sourcelist=sources.list.d/celikpanel-"+req.RepoID+".list",
		"-o", "Dir::Etc::sourceparts=/dev/null",
		"-o", "APT::Get::List-Cleanup=0")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if out, err := cmd.CombinedOutput(); err != nil {
		// Roll the files back so a failed enable does not leave a half-configured
		// source that breaks later apt runs.
		// Başarısız açma sonrası ileriki apt çalışmalarını bozan yarı-yapılandırılmış
		// bir kaynak kalmasın diye dosyaları geri al.
		_ = os.Remove(repoSourcePath(req.RepoID))
		_ = os.Remove(keyring)
		resp.Error = fmt.Sprintf("apt update for new repo failed: %s", strings.TrimSpace(string(out)))
		return nil
	}

	resp.Enabled = true
	resp.Source = signed
	return nil
}

// DisableRepo removes the source and pinned key, then refreshes apt so the
// vendor's packages disappear from the candidate set (already-installed
// packages stay — removing them is a separate uninstall). The mirror of enable.
// DisableRepo, kaynağı ve sabitlenmiş anahtarı kaldırır, sonra apt'ı tazeler;
// böylece vendor paketleri aday kümesinden çıkar (zaten kurulu paketler kalır —
// onları kaldırmak ayrı bir uninstall'dır). Açmanın aynası.
func (a *Agent) DisableRepo(req *EnableRepoRequest, resp *RepoStatusResponse) error {
	if !validRepoID.MatchString(req.RepoID) {
		resp.Error = "invalid repo id"
		return nil
	}
	_ = os.Remove(repoSourcePath(req.RepoID))
	for _, k := range repoKeyringCandidates(req.RepoID) {
		_ = os.Remove(k)
	}
	cmd := exec.Command("apt-get", "update", "-o", "APT::Get::List-Cleanup=1")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	_, _ = cmd.CombinedOutput()
	resp.Enabled = false
	return nil
}

// RepoStatus reports whether our source file for this repo is present.
// RepoStatus, bu depo için kaynak dosyamızın var olup olmadığını bildirir.
func (a *Agent) RepoStatus(req *EnableRepoRequest, resp *RepoStatusResponse) error {
	if !validRepoID.MatchString(req.RepoID) {
		resp.Error = "invalid repo id"
		return nil
	}
	if data, err := os.ReadFile(repoSourcePath(req.RepoID)); err == nil {
		resp.Enabled = true
		resp.Source = strings.TrimSpace(string(data))
	}
	return nil
}

type RepoPackagesRequest struct {
	Pattern string `json:"pattern"`
}

type RepoPackagesResponse struct {
	Packages []string `json:"packages"` // newest major first, e.g. postgresql-17, 16, …
	Error    string   `json:"error,omitempty"`
}

// RepoPackages discovers which versioned packages are actually available now by
// matching the catalog's pattern against apt-cache — the repo, not our code, is
// the source of truth for which versions exist. Returned newest-major-first.
// RepoPackages, kataloğun desenini apt-cache ile eşleyerek şu an fiilen hangi
// sürümlü paketlerin mevcut olduğunu keşfeder — hangi sürümlerin var olduğunun
// kaynağı kodumuz değil depodur. En yeni major önce döner.
func (a *Agent) RepoPackages(req *RepoPackagesRequest, resp *RepoPackagesResponse) error {
	if detectPkgFamily() != "apt" {
		resp.Error = "not supported on this distro yet"
		return nil
	}
	re, err := regexp.Compile(req.Pattern)
	if err != nil {
		resp.Error = "invalid package pattern"
		return nil
	}
	out, err := exec.Command("apt-cache", "search", "--names-only", req.Pattern).Output()
	if err != nil {
		resp.Error = fmt.Sprintf("apt-cache search failed: %v", err)
		return nil
	}
	var pkgs []string
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if i := strings.IndexByte(name, ' '); i >= 0 {
			name = name[:i] // "postgresql-17 - The World's…" → "postgresql-17"
		}
		// apt-cache search treats the pattern loosely; re-filter exactly so only
		// true matches (postgresql-17, not postgresql-client-17) survive.
		// apt-cache search deseni gevşek ele alır; tam yeniden süz ki yalnız
		// gerçek eşleşmeler (postgresql-17, postgresql-client-17 değil) kalsın.
		if name != "" && re.MatchString(name) {
			pkgs = append(pkgs, name)
		}
	}
	sort.Slice(pkgs, func(i, j int) bool {
		ai, an := versionOf(pkgs[i])
		bi, bn := versionOf(pkgs[j])
		if ai != bi {
			return ai > bi
		}
		return an > bn
	})
	resp.Packages = pkgs
	return nil
}

// versionOf reads the first version number in a package name as (major, minor),
// so the newest offer sorts first: "postgresql-17" → (17,0), "php8.3-fpm" →
// (8,3), "php5.6-fpm" → (5,6).
//
// The previous version read the TRAILING integer, which works for
// postgresql-17 but returns 0 for every php8.x-fpm ("fpm" is not a number) —
// so with PHP in the catalogue all versions tied at 0 and the drawer listed
// them in whatever order apt-cache happened to emit.
//
// versionOf, bir paket adındaki ilk sürüm numarasını (major, minor) olarak
// okur; böylece en yeni teklif başa sıralanır: "postgresql-17" → (17,0),
// "php8.3-fpm" → (8,3), "php5.6-fpm" → (5,6).
//
// Önceki sürüm SONDAKİ tam sayıyı okuyordu; postgresql-17 için çalışır ama her
// php8.x-fpm için 0 döner ("fpm" sayı değil) — yani katalogda PHP varken tüm
// sürümler 0'da berabere kalıyor ve çekmece onları apt-cache ne sırayla
// verdiyse o sırayla listeliyordu.
var versionInName = regexp.MustCompile(`([0-9]+)(?:\.([0-9]+))?`)

func versionOf(pkg string) (major, minor int) {
	m := versionInName.FindStringSubmatch(pkg)
	if m == nil {
		return 0, 0
	}
	major, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		minor, _ = strconv.Atoi(m[2])
	}
	return major, minor
}

// osCodename reads VERSION_CODENAME from /etc/os-release (bookworm, noble, …),
// which the vendor source line needs to select the right suite.
// osCodename, /etc/os-release'ten VERSION_CODENAME okur (bookworm, noble, …);
// vendor kaynak satırı doğru paketi seçmek için buna ihtiyaç duyar.
func osCodename() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "VERSION_CODENAME="); ok {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

// fetchRepoKey downloads a signing key over https and sanity-checks it before
// it is ever written as a trusted keyring. It reports whether the key is
// ASCII-armoured so the caller can name the file for what it holds.
//
// Both forms are accepted because vendors publish both and apt reads both: the
// armoured form is used directly (apt ≥ 1.4) so no gpg dependency is pulled in,
// and the binary form IS a keyring already. What is NOT accepted is anything
// that is not an OpenPGP public key — an HTML error page saved as a trusted
// keyring would leave a repo that apt refuses to verify, with a confusing
// "not signed" error far from the real cause.
//
// fetchRepoKey, imza anahtarını https üzerinden indirir ve güvenilir keyring
// olarak yazılmadan önce doğruluğunu denetler. Anahtarın ASCII-zırhlı olup
// olmadığını da bildirir; böylece çağıran, dosyayı içeriğine göre adlandırır.
//
// İki biçim de kabul edilir çünkü vendor'lar ikisini de yayınlar ve apt ikisini
// de okur: zırhlı biçim doğrudan kullanılır (apt ≥ 1.4), böylece gpg bağımlılığı
// çekilmez; ikili biçim zaten bir keyring'dir. Kabul EDİLMEYEN, OpenPGP açık
// anahtarı olmayan her şeydir — güvenilir keyring diye kaydedilmiş bir HTML
// hata sayfası, apt'ın doğrulamayı reddettiği bir depo bırakır ve hata
// ("imzasız") gerçek sebepten çok uzakta görünür.
func fetchRepoKey(url string) (key []byte, armored bool, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		return nil, false, fmt.Errorf("fetch repo key: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("fetch repo key: HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20)) // 1 MiB is plenty for a key
	if err != nil {
		return nil, false, fmt.Errorf("read repo key: %v", err)
	}
	if strings.Contains(string(body), "BEGIN PGP PUBLIC KEY BLOCK") {
		return body, true, nil
	}
	if isBinaryPublicKey(body) {
		return body, false, nil
	}
	return nil, false, fmt.Errorf("downloaded repo key is neither an ASCII-armoured nor a binary OpenPGP public key")
}

// isBinaryPublicKey reports whether the bytes begin with an OpenPGP Public-Key
// packet (tag 6). Checking the tag — not merely "it is not text" — is what
// stops an HTML error page or a truncated download from being installed as a
// trusted keyring.
//
// The first byte is a packet header: bit 7 is always set. Bit 6 selects the
// format: new-format packets carry the tag in the low 6 bits, old-format ones
// in bits 2-5. RFC 4880 §4.2.
//
// isBinaryPublicKey, baytların bir OpenPGP Açık-Anahtar paketiyle (tag 6)
// başlayıp başlamadığını bildirir. Yalnız "metin değil" demek yerine tag'i
// denetlemek, bir HTML hata sayfasının ya da yarım inmiş bir dosyanın
// güvenilir keyring olarak kurulmasını engelleyen şeydir.
//
// İlk bayt paket başlığıdır: 7. bit her zaman settir. 6. bit biçimi seçer:
// yeni biçim paketler tag'i alttaki 6 bitte, eski biçim olanlar 2-5. bitlerde
// taşır. RFC 4880 §4.2.
func isBinaryPublicKey(b []byte) bool {
	if len(b) == 0 || b[0]&0x80 == 0 {
		return false
	}
	var tag byte
	if b[0]&0x40 != 0 {
		tag = b[0] & 0x3F // new format
	} else {
		tag = (b[0] >> 2) & 0x0F // old format
	}
	return tag == 6 // Public-Key Packet
}
