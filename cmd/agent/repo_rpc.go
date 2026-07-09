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
// key cleanly. The armoured key is used directly, so no gpg is pulled in — the
// minimal-install promise holds.
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
// kaldırır. Zırhlı anahtar doğrudan kullanılır, böylece gpg çekilmez — minimal
// kurulum sözü korunur.

// validRepoID bounds the id to a filename-safe token, since it names the
// keyring and source files under /etc and /usr/share.
// validRepoID, id'yi dosya-adı güvenli bir dizgeye sınırlar; çünkü /etc ve
// /usr/share altındaki keyring ve kaynak dosyalarını adlandırır.
var validRepoID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

func repoKeyringPath(id string) string { return "/usr/share/keyrings/celikpanel-" + id + ".asc" }
func repoSourcePath(id string) string  { return "/etc/apt/sources.list.d/celikpanel-" + id + ".list" }

type EnableRepoRequest struct {
	RepoID         string `json:"repo_id"`
	KeyURL         string `json:"key_url"`
	SourceTemplate string `json:"source_template"`
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
	if !strings.HasPrefix(req.KeyURL, "https://") {
		resp.Error = "repo key URL must be https"
		return nil
	}

	codename := osCodename()
	if codename == "" {
		resp.Error = "could not determine the distribution codename (/etc/os-release)"
		return nil
	}
	source := strings.ReplaceAll(req.SourceTemplate, "{codename}", codename)
	// Only a plain "deb https://…" line is accepted, and signed-by= is injected
	// so the source trusts our pinned key and nothing else.
	// Yalnız düz bir "deb https://…" satırı kabul edilir ve signed-by= enjekte
	// edilir; böylece kaynak yalnız sabitlediğimiz anahtara güvenir.
	if !strings.HasPrefix(source, "deb https://") {
		resp.Error = "repo source must be a deb https:// line"
		return nil
	}
	signed := "deb [signed-by=" + repoKeyringPath(req.RepoID) + "] " + strings.TrimPrefix(source, "deb ")

	key, err := fetchArmoredKey(req.KeyURL)
	if err != nil {
		resp.Error = err.Error()
		return nil
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
	if err := os.WriteFile(repoKeyringPath(req.RepoID), key, 0o644); err != nil {
		resp.Error = fmt.Sprintf("write keyring: %v", err)
		return nil
	}
	if err := os.Chmod(repoKeyringPath(req.RepoID), 0o644); err != nil {
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
		_ = os.Remove(repoKeyringPath(req.RepoID))
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
	_ = os.Remove(repoKeyringPath(req.RepoID))
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
	sort.Slice(pkgs, func(i, j int) bool { return majorOf(pkgs[i]) > majorOf(pkgs[j]) })
	resp.Packages = pkgs
	return nil
}

// majorOf extracts the trailing integer of a versioned package name
// ("postgresql-17" → 17) for newest-first ordering; 0 when there is none.
// majorOf, sürümlü bir paket adının sondaki tam sayısını çıkarır
// ("postgresql-17" → 17); yoksa 0.
func majorOf(pkg string) int {
	if i := strings.LastIndexByte(pkg, '-'); i >= 0 {
		if n, err := strconv.Atoi(pkg[i+1:]); err == nil {
			return n
		}
	}
	return 0
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

// fetchArmoredKey downloads an ASCII-armoured signing key over https and
// sanity-checks it before it is ever written as a trusted keyring.
// fetchArmoredKey, ASCII-zırhlı imza anahtarını https üzerinden indirir ve
// güvenilir keyring olarak yazılmadan önce doğruluğunu denetler.
func fetchArmoredKey(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch repo key: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch repo key: HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20)) // 1 MiB is plenty for a key
	if err != nil {
		return nil, fmt.Errorf("read repo key: %v", err)
	}
	if !strings.Contains(string(body), "BEGIN PGP PUBLIC KEY BLOCK") {
		return nil, fmt.Errorf("downloaded repo key is not an ASCII-armoured PGP public key")
	}
	return body, nil
}
