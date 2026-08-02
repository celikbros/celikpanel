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
	"github.com/alicelik/celikpanel/internal/transport"
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

// Values substituted into a source line come from /etc/os-release, but are
// still treated as data rather than trusted apt syntax. Keeping them to one
// conservative token prevents a malformed host file from injecting another
// source line.
// Kaynak satirina yerlestirilen degerler /etc/os-release'ten gelse de guvenilir
// apt sozdizimi degil, veri sayilir. Tek ve tutucu bir token ile sinirlamak,
// bozuk bir makine dosyasinin ikinci bir kaynak satiri enjekte etmesini
// engeller.
var validRepoOSReleaseToken = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}$`)

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
type EnableRepoRequest = transport.EnableRepoRequest

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

// Stable repository error codes cross the privileged RPC boundary. Detailed
// command output remains available to server logs, while the panel translates
// these constants instead of exposing raw agent English to the browser.
//
// Sabit depo hata kodlari yetkili RPC sinirini gecer. Ayrintili komut ciktisi
// sunucu logunda kalir; panel tarayiciya ham agent Ingilizcesi vermek yerine bu
// sabitleri cevirir.
const (
	repoErrInvalidRequest          = "REPO_INVALID_REQUEST"
	repoErrUnsupportedSystem       = "REPO_UNSUPPORTED_SYSTEM"
	repoErrUnsupportedDistribution = "REPO_UNSUPPORTED_DISTRIBUTION"
	repoErrKeyUntrusted            = "REPO_KEY_UNTRUSTED"
	repoErrEnableFailed            = "REPO_ENABLE_FAILED"
	repoErrDisableFailed           = "REPO_DISABLE_FAILED"
	repoErrConfigurationInvalid    = "REPO_CONFIGURATION_INVALID"
	repoErrStatusFailed            = "REPO_STATUS_FAILED"
	repoErrPackagesFailed          = "REPO_PACKAGES_FAILED"
)

type RepoStatusResponse = transport.RepoStatusResponse

// signedRepoSource builds the one exact source line CelikPanel owns. Keeping
// enable and status on the same helper prevents a stale or hand-edited source
// from being reported as healthy merely because the file exists.
// signedRepoSource, CelikPanel'in yönettiği tek ve tam kaynak satırını üretir.
// Enable ve status'un aynı yardımcıyı kullanması, eski ya da elle değiştirilmiş
// bir kaynağın sırf dosya var diye sağlıklı görünmesini engeller.
func signedRepoSource(source, keyring string) string {
	return "deb [signed-by=" + keyring + "] " + strings.TrimPrefix(source, "deb ")
}

// runRepoPackageMutation serializes repository publication/removal and apt
// refresh with package install/remove operations.
//
// runRepoPackageMutation, depo yayinlama/kaldirma ve apt yenilemeyi paket
// kurulum/kaldirma islemleriyle siraya koyar.
func runRepoPackageMutation(operation func() error) error {
	packageOperationMu.Lock()
	defer packageOperationMu.Unlock()
	return operation()
}

// EnableRepo pins the vendor's signing key and writes an apt source that trusts
// only that key, then refreshes just this source's package list. It is
// idempotent: re-enabling simply rewrites the key and source.
// EnableRepo, vendor'ın imza anahtarını sabitler ve yalnız o anahtara güvenen
// bir apt kaynağı yazar, sonra yalnız bu kaynağın paket listesini tazeler.
// İdempotenttir: yeniden açmak anahtarı ve kaynağı yeniden yazar.
func (a *Agent) EnableRepo(req *EnableRepoRequest, resp *RepoStatusResponse) error {
	if req == nil {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = "missing request"
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		resp.ErrorCode = repoErrEnableFailed
		resp.Error = err.Error()
		return nil
	}
	defer finishStep()
	defer ensureRepoStatusErrorCode(resp, repoErrEnableFailed)
	if detectPkgFamily() != "apt" {
		resp.ErrorCode = repoErrUnsupportedSystem
		resp.Error = "managed repositories are only supported on apt (Debian/Ubuntu) systems yet"
		return nil
	}
	if !validRepoID.MatchString(req.RepoID) {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = "invalid repo id"
		return nil
	}
	repo := repoFromCatalogue(req.RepoID)
	if repo == nil {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = "unknown repository"
		return nil
	}
	if !strings.HasPrefix(repo.KeyURL, "https://") {
		resp.Error = "repo key URL must be https"
		resp.ErrorCode = repoErrInvalidRequest
		return nil
	}

	source, err := repoSourceForHost(repo)
	if err != nil {
		resp.ErrorCode = repoErrUnsupportedDistribution
		resp.Error = err.Error()
		return nil
	}
	// Only a plain "deb https://…" line is accepted, and signed-by= is injected
	// so the source trusts our pinned key and nothing else.
	// Yalnız düz bir "deb https://…" satırı kabul edilir ve signed-by= enjekte
	// edilir; böylece kaynak yalnız sabitlediğimiz anahtara güvenir.
	// The key is fetched BEFORE the source line is built: its format decides
	// the keyring filename, and signed-by= must point at the name we actually
	// write. / Anahtar, kaynak satırından ÖNCE indirilir: biçimi keyring dosya
	// adını belirler ve signed-by= gerçekten yazdığımız adı göstermelidir.
	key, armored, err := fetchRepoKey(repo.KeyURL, repo.KeyFingerprint)
	if err != nil {
		resp.ErrorCode = repoErrKeyUntrusted
		resp.Error = err.Error()
		return nil
	}
	keyring := repoKeyringPath(req.RepoID, armored)
	signed := signedRepoSource(source, keyring)

	// Re-enabling with a different key format must not leave the old file
	// behind: apt would keep trusting a keyring nothing points at any more.
	// Farklı bir anahtar biçimiyle yeniden açmak eski dosyayı bırakmamalı:
	// apt, artık hiçbir kaynağın göstermediği bir keyring'e güvenmeyi sürdürürdü.
	paths := repoRecipePaths{
		Keyring: keyring,
		Source:  repoSourcePath(req.RepoID),
	}
	for _, candidate := range repoKeyringCandidates(req.RepoID) {
		if candidate != keyring {
			paths.StaleKeyrings = append(paths.StaleKeyrings, candidate)
		}
	}
	// Publishing the key/source pair and refreshing apt share the package
	// mutation lock with install/remove. This prevents an install from
	// observing the key-first/source-last commit in the middle.
	//
	// Anahtar/kaynak ciftini yayinlama ve apt yenileme, kurulum/kaldirma ile
	// ayni paket mutasyon kilidini kullanir. Boylece bir kurulum anahtar-once,
	// kaynak-sonra commit'ini yarida goremez.
	err = runRepoPackageMutation(func() error {
		return prepareAndPublishRepoRecipe(paths, key, source, func(stagedSource string) ([]byte, error) {
			cmd := serviceMutationCommand(ctx, "apt-get", "update",
				"-o", "Dir::Etc::sourcelist="+stagedSource,
				"-o", "Dir::Etc::sourceparts=/dev/null",
				"-o", "APT::Get::List-Cleanup=0")
			cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
			return cmd.CombinedOutput()
		})
	})
	if err != nil {
		resp.MutationApplied = repoMutationApplied(err)
		resp.PartialSuccess = resp.MutationApplied
		resp.Repairable = resp.MutationApplied
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

	// Refresh only this source's lists (not the whole system), and do not prune
	// other sources' cached data.
	// Yalnız bu kaynağın listelerini tazele (tüm sistemi değil) ve diğer
	// kaynakların önbelleğini budama.
	// Roll the files back so a failed enable does not leave a half-configured
	// source that breaks later apt runs.
	// Başarısız açma sonrası ileriki apt çalışmalarını bozan yarı-yapılandırılmış
	// bir kaynak kalmasın diye dosyaları geri al.

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
	if req == nil {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = "missing request"
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		resp.ErrorCode = repoErrDisableFailed
		resp.Error = err.Error()
		return nil
	}
	defer finishStep()
	defer ensureRepoStatusErrorCode(resp, repoErrDisableFailed)
	if !validRepoID.MatchString(req.RepoID) {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = "invalid repo id"
		return nil
	}
	// Removal and apt refresh must be one package-manager critical section.
	// install/remove operations therefore cannot use stale source/key state.
	//
	// Kaldirma ve apt yenileme tek paket-yoneticisi kritik bolgesi olmalidir.
	// Boylece kurulum/kaldirma islemleri eski kaynak/anahtar durumunu kullanamaz.
	err = runRepoPackageMutation(func() error {
		return disableRepoRecipe(
			repoSourcePath(req.RepoID),
			repoKeyringCandidates(req.RepoID),
			func() ([]byte, error) {
				cmd := serviceMutationCommand(ctx, "apt-get", "update", "-o", "APT::Get::List-Cleanup=1")
				cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
				return cmd.CombinedOutput()
			},
		)
	})
	if err != nil {
		resp.MutationApplied = repoMutationApplied(err)
		resp.PartialSuccess = resp.MutationApplied
		resp.Repairable = resp.MutationApplied
		resp.Error = err.Error()
		return nil
	}
	resp.Enabled = false
	return nil
}

// repoRecipeStatus compares the managed source and referenced keyring with the
// exact recipe compiled into the catalogue. It performs no network access and
// is separated from RepoStatus so stale-hostname and damaged-key cases can be
// tested without writing under /etc.
//
// repoRecipeStatus, yönetilen kaynağı ve onun işaret ettiği keyring'i kataloğa
// derlenmiş tam tarifle karşılaştırır. Ağ erişimi yapmaz; eski-hostname ve bozuk
// anahtar durumları /etc altına yazmadan sınanabilsin diye RepoStatus'tan
// ayrılmıştır.
func repoRecipeStatus(repo *core.ManagedRepo, expectedSource, actualSource string, readFile func(string) ([]byte, error)) (bool, string) {
	actualSource = strings.TrimSpace(actualSource)
	for _, armored := range []bool{true, false} {
		keyring := repoKeyringPath(repo.ID, armored)
		if actualSource != signedRepoSource(expectedSource, keyring) {
			continue
		}
		key, err := readFile(keyring)
		if err != nil {
			return false, fmt.Sprintf("referenced keyring cannot be read: %v", err)
		}
		actualArmored, err := validateRepoPublicKey(key, repo.KeyFingerprint)
		if err != nil {
			return false, fmt.Sprintf("referenced keyring is not the pinned OpenPGP public key: %v", err)
		}
		if actualArmored != armored {
			return false, "referenced keyring extension does not match its OpenPGP encoding"
		}
		return true, ""
	}
	return false, "source line does not match the current catalogue recipe"
}

// RepoStatus reports healthy only when both the source contents and referenced
// keyring match the current recipe. Existing drift is marked repairable because
// EnableRepo is idempotent and rewrites both files from the trusted catalogue.
// RepoStatus, yalnız kaynak içeriği ve işaret edilen keyring güncel tarifle
// eşleştiğinde sağlıklı bildirir. Mevcut drift onarılabilir olarak işaretlenir;
// çünkü EnableRepo idempotenttir ve iki dosyayı da güvenilir katalogdan yeniden
// yazar.
func (a *Agent) RepoStatus(req *EnableRepoRequest, resp *RepoStatusResponse) error {
	if req == nil {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = "missing request"
		return nil
	}
	defer ensureRepoStatusErrorCode(resp, repoErrStatusFailed)
	if !validRepoID.MatchString(req.RepoID) {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = "invalid repo id"
		return nil
	}
	repo := repoFromCatalogue(req.RepoID)
	if repo == nil {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = "unknown repository"
		return nil
	}
	expectedSource, err := repoSourceForHost(repo)
	if err != nil {
		resp.ErrorCode = repoErrUnsupportedDistribution
		resp.Error = err.Error()
		return nil
	}
	sourcePath := repoSourcePath(req.RepoID)
	managedPaths := append([]string{sourcePath}, repoKeyringCandidates(req.RepoID)...)
	// Recover an interrupted key/source transaction before reporting health, so
	// callers never inspect a crash-created half state.
	// Sağlık bildirmeden önce yarım kalmış anahtar/kaynak işlemini kurtar; böylece
	// çağıranlar çökme sonucu oluşmuş yarım durumu hiçbir zaman incelemez.
	if err := runRepoPackageMutation(func() error {
		return recoverRepoTransaction(sourcePath, managedPaths)
	}); err != nil {
		resp.Repairable = true
		resp.ErrorCode = repoErrConfigurationInvalid
		resp.Error = fmt.Sprintf("recover repository transaction: %v", err)
		return nil
	}
	data, err := readSecureRepoFile(sourcePath, repoManagedFileMode)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		resp.Repairable = true
		resp.ErrorCode = repoErrConfigurationInvalid
		resp.Error = fmt.Sprintf("read repository source: %v", err)
		return nil
	}
	resp.Source = strings.TrimSpace(string(data))
	healthy, reason := repoRecipeStatus(repo, expectedSource, resp.Source, func(path string) ([]byte, error) {
		return readSecureRepoFile(path, repoManagedFileMode)
	})
	if !healthy {
		resp.Repairable = true
		resp.ErrorCode = repoErrConfigurationInvalid
		resp.Error = fmt.Sprintf("managed repository configuration has drifted: %s; enable it again to repair", reason)
		return nil
	}
	resp.Enabled = true
	return nil
}

type RepoPackagesRequest = transport.RepoPackagesRequest
type RepoPackagesResponse = transport.RepoPackagesResponse

// RepoPackages discovers which versioned packages are actually available now by
// matching the catalog's pattern against apt-cache — the repo, not our code, is
// the source of truth for which versions exist. Returned newest-major-first.
// RepoPackages, kataloğun desenini apt-cache ile eşleyerek şu an fiilen hangi
// sürümlü paketlerin mevcut olduğunu keşfeder — hangi sürümlerin var olduğunun
// kaynağı kodumuz değil depodur. En yeni major önce döner.
// catalogueRepoPackagePattern resolves the only trusted search pattern from a
// repository id. An empty catalogue pattern means “no version menu”, not
// “enumerate every apt package”.
// catalogueRepoPackagePattern, güvenilen tek arama desenini depo kimliğinden
// çözer. Boş katalog deseni “sürüm menüsü yok” demektir; “apt'teki her paketi
// listele” demek değildir.
func catalogueRepoPackagePattern(repoID string) (string, *regexp.Regexp, error) {
	if !validRepoID.MatchString(repoID) {
		return "", nil, fmt.Errorf("invalid repo id")
	}
	repo := repoFromCatalogue(repoID)
	if repo == nil {
		return "", nil, fmt.Errorf("unknown repository")
	}
	pattern := strings.TrimSpace(repo.PackagePattern)
	if pattern == "" {
		return "", nil, fmt.Errorf("repository does not offer versioned package selection")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", nil, fmt.Errorf("invalid package pattern in catalogue")
	}
	return pattern, re, nil
}

func (a *Agent) RepoPackages(req *RepoPackagesRequest, resp *RepoPackagesResponse) error {
	resp.Packages = []string{}
	defer ensureRepoPackagesErrorCode(resp, repoErrPackagesFailed)
	if req == nil {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = "missing request"
		return nil
	}

	// Resolve the search expression from the privileged agent's catalogue
	// before probing apt. The caller names a repo; it never supplies executable
	// search syntax, and a repo without a version pattern (Netdata) is rejected
	// without running `apt-cache search ""`.
	// apt sorgusundan önce arama ifadesini yetkili agent'ın kataloğundan çöz.
	// Çağıran yalnız depo adını verir, çalıştırılabilir arama sözdizimi vermez;
	// sürüm deseni olmayan depo (Netdata) `apt-cache search ""` çalıştırılmadan
	// reddedilir.
	pattern, re, err := catalogueRepoPackagePattern(req.RepoID)
	if err != nil {
		resp.ErrorCode = repoErrInvalidRequest
		resp.Error = err.Error()
		return nil
	}
	if detectPkgFamily() != "apt" {
		resp.ErrorCode = repoErrUnsupportedSystem
		resp.Error = "not supported on this distro yet"
		return nil
	}
	out, err := exec.Command("apt-cache", "search", "--names-only", pattern).Output()
	if err != nil {
		resp.ErrorCode = repoErrPackagesFailed
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
		if name != "" && re.FindString(name) == name {
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
	return osReleaseValue("VERSION_CODENAME")
}

func osDistributionID() string {
	return strings.ToLower(osReleaseValue("ID"))
}

func osReleaseValue(key string) string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	return parseOSReleaseValue(data, key)
}

func parseOSReleaseValue(data []byte, key string) string {
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), key+"="); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// repoSourceForHost selects an exact distro template where the catalogue
// requires one, then substitutes only a validated codename. A non-empty
// SourceTemplates map is deliberately a strict allowlist: Ubuntu must use the
// Ubuntu Netdata tree, Debian the Debian tree, and an unknown apt derivative
// is reported as unsupported instead of being fed Debian packages by accident.
// repoSourceForHost, katalog gerektiriyorsa tam dagitim sablonunu secer ve
// yalniz dogrulanmis kod adini yerlestirir. Bos olmayan SourceTemplates haritasi
// kati bir izin listesidir: Ubuntu kendi, Debian kendi Netdata agacini kullanir;
// bilinmeyen bir apt turevine yanlislikla Debian paketi verilmez, desteklenmiyor
// olarak bildirilir.
func repoSourceForHost(repo *core.ManagedRepo) (string, error) {
	return renderRepoSource(repo, osDistributionID(), osCodename())
}

func renderRepoSource(repo *core.ManagedRepo, distroID, codename string) (string, error) {
	if repo == nil {
		return "", fmt.Errorf("unknown repository")
	}
	if !validRepoOSReleaseToken.MatchString(codename) {
		return "", fmt.Errorf("could not determine a safe distribution codename (/etc/os-release)")
	}
	template := repo.SourceTemplate
	if len(repo.SourceTemplates) > 0 {
		if !validRepoOSReleaseToken.MatchString(distroID) {
			return "", fmt.Errorf("could not determine a safe distribution ID (/etc/os-release)")
		}
		var ok bool
		template, ok = repo.SourceTemplates[distroID]
		if !ok {
			return "", fmt.Errorf("%s repository is not offered on distribution %s", repo.Name, distroID)
		}
	}
	source := strings.ReplaceAll(template, "{codename}", codename)
	if source == "" || strings.Contains(source, "{codename}") || strings.ContainsAny(source, "\r\n") {
		return "", fmt.Errorf("invalid repository source template")
	}
	if !strings.HasPrefix(source, "deb https://") {
		return "", fmt.Errorf("repo source must be a deb https:// line")
	}
	return source, nil
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
func fetchRepoKey(url, expectedFingerprint string) (key []byte, armored bool, err error) {
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
	armored, err = validateRepoPublicKey(body, expectedFingerprint)
	if err != nil {
		return nil, false, fmt.Errorf("validate repo key: %v", err)
	}
	return body, armored, nil
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
	_, err := primaryPublicKeyFingerprint(b)
	return err == nil
}
