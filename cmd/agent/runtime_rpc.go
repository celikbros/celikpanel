package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Node.js runtime management — roadmap 3A. Official tarballs are installed
// side by side under runtimesBaseDir so every project picks its own version
// (no nvm, no external tooling — single-binary constitution). Downloads are
// verified against the official SHASUMS256.txt before extraction; an
// unverifiable download is discarded, never installed.
//
// Node.js runtime yönetimi — yol haritası 3A. Resmi tarball'lar
// runtimesBaseDir altında yan yana kurulur; her proje kendi sürümünü seçer
// (nvm yok, dış araç yok — tek-binary anayasası). İndirmeler açılmadan önce
// resmi SHASUMS256.txt ile doğrulanır; doğrulanamayan indirme kurulmaz,
// atılır.

var runtimesBaseDir = func() string {
	if d := os.Getenv("CELIKPANEL_RUNTIMES_DIR"); d != "" {
		return d
	}
	return "/opt/celikpanel/runtimes"
}()

var nodeVersionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type NodeVersionsResponse = transport.NodeVersionsResponse

// ListNodeVersions reports what is actually on disk — never a wish list.
// ListNodeVersions, gerçekten diskte olanı bildirir — asla bir dilek listesi değil.
func (a *Agent) ListNodeVersions(_ *transport.Empty, resp *NodeVersionsResponse) error {
	resp.Installed = []string{}

	entries, err := os.ReadDir(filepath.Join(runtimesBaseDir, "node"))
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && nodeVersionRe.MatchString(e.Name()) {
				// Only count versions whose binary is really there.
				// Yalnızca ikili dosyası gerçekten var olan sürümleri say.
				if _, err := os.Stat(nodeBinPath(e.Name())); err == nil {
					resp.Installed = append(resp.Installed, e.Name())
				}
			}
		}
	}
	// Numeric, newest first. sort.Strings was a real bug here: lexically
	// "24.18.0" < "9.9.9", so the version pickers listed a two-digit major
	// below a single-digit one.
	// Sayısal, en yeni önce. sort.Strings burada gerçek bir hataydı: sözlükte
	// "24.18.0" < "9.9.9" olduğundan sürüm seçiciler iki basamaklı major'ı
	// tek basamaklının altında listeliyordu.
	sort.SliceStable(resp.Installed, func(i, j int) bool {
		return versionLess(resp.Installed[j], resp.Installed[i])
	})

	if out, err := exec.Command("node", "--version").Output(); err == nil {
		resp.SystemVersion = strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	}
	return nil
}

type NodeInstallRequest = transport.NodeInstallRequest

type NodeInstallResponse = transport.NodeInstallResponse

// InstallNodeVersion downloads, verifies and unpacks an official Node build.
// Idempotent: an already-installed version returns success immediately.
// InstallNodeVersion, resmi bir Node derlemesini indirir, doğrular ve açar.
// Bağımsızdır: zaten kurulu bir sürüm hemen başarı döndürür.
func (a *Agent) InstallNodeVersion(req *NodeInstallRequest, resp *NodeInstallResponse) error {
	*resp = NodeInstallResponse{}
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	version := req.Version
	if !nodeVersionRe.MatchString(version) {
		resp.Error = "invalid version (expected e.g. 24.18.0)"
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepInstallNodeVersion, "node", version, "install"),
	)
	if err != nil {
		*resp = NodeInstallResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	if _, err := os.Stat(nodeBinPath(version)); err == nil {
		resp.Installed = true
		return nil
	}

	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" {
		resp.Error = "unsupported architecture: " + runtime.GOARCH
		return nil
	}

	base := fmt.Sprintf("https://nodejs.org/dist/v%s", version)
	tarName := fmt.Sprintf("node-v%s-linux-%s.tar.xz", version, arch)

	client := &http.Client{Timeout: 5 * time.Minute}

	// 1. Official checksums first — they decide whether anything is trusted.
	// 1. Önce resmi sağlama toplamları — neye güvenileceğine onlar karar verir.
	wantSum, err := fetchNodeChecksum(ctx, client, base+"/SHASUMS256.txt", tarName)
	if err != nil {
		resp.Error = fmt.Sprintf("cannot fetch checksums: %v", err)
		return nil
	}

	// 2. Download the tarball to a temp file, hashing as we go.
	// 2. Tarball'ı geçici dosyaya indir, indirirken özetle.
	if err := os.MkdirAll(runtimesBaseDir, 0o755); err != nil {
		resp.Error = err.Error()
		return nil
	}
	tmp, err := os.CreateTemp(runtimesBaseDir, "node-dl-*")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	dlRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+tarName, nil)
	if err != nil {
		resp.Error = fmt.Sprintf("download request failed: %v", err)
		return nil
	}
	dl, err := client.Do(dlRequest)
	if err != nil {
		resp.Error = fmt.Sprintf("download failed: %v", err)
		return nil
	}
	defer dl.Body.Close()
	if dl.StatusCode != http.StatusOK {
		resp.Error = fmt.Sprintf("download failed: HTTP %d (does the version exist?)", dl.StatusCode)
		return nil
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), dl.Body); err != nil {
		resp.Error = fmt.Sprintf("download failed: %v", err)
		return nil
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSum {
		resp.Error = "checksum mismatch — download discarded"
		return nil
	}

	// 3. Extract into a staging dir, then move into place atomically-ish so a
	// half-extracted tree never looks installed.
	// 3. Hazırlık dizinine aç, sonra yerine taşı; yarım açılmış bir ağaç asla
	// kurulu görünmesin.
	stage, err := os.MkdirTemp(runtimesBaseDir, "node-stage-*")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer os.RemoveAll(stage)

	if out, err := runServiceMutationCombinedOutput(ctx, "tar", "-xJf", tmp.Name(), "-C", stage, "--strip-components=1"); err != nil {
		resp.Error = fmt.Sprintf("extract failed: %v: %s", err, string(out))
		return nil
	}

	dest := filepath.Join(runtimesBaseDir, "node", version)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := os.Rename(stage, dest); err != nil {
		resp.Error = err.Error()
		return nil
	}

	resp.Installed = true
	return nil
}

func nodeBinPath(version string) string {
	return filepath.Join(runtimesBaseDir, "node", version, "bin", "node")
}

// fetchNodeChecksum pulls the expected SHA-256 for one file out of the
// official SHASUMS256.txt.
// fetchNodeChecksum, resmi SHASUMS256.txt'ten tek dosyanın beklenen
// SHA-256'sını çeker.
func fetchNodeChecksum(ctx context.Context, client *http.Client, url, fileName string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}

	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == fileName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s", fileName)
}

// --- B3d: per-version removal + upstream LTS list ---
// --- B3d: sürüm başına kaldırma + kaynak LTS listesi ---

type NodeRemoveRequest = transport.NodeRemoveRequest

type NodeRemoveResponse = transport.NodeRemoveResponse

// RemoveNodeVersion deletes ONE managed runtime tree. The semver gate plus
// the fixed base directory mean the path cannot name anything but a tree we
// installed; usage checks (who runs on it) are the panel's job — the agent
// stays tenant-blind. Idempotent: removing an absent version reports removed.
// RemoveNodeVersion, yönetilen TEK runtime ağacını siler. Semver kapısı +
// sabit taban dizini, yolun bizim kurduğumuz bir ağaçtan başkasını
// adlandıramamasını sağlar; kullanım denetimi (üstünde kim koşuyor) panelin
// işidir — agent kiracı-kördür. Idempotent: olmayan sürümü kaldırmak
// kaldırıldı bildirir.
func (a *Agent) RemoveNodeVersion(req *NodeRemoveRequest, resp *NodeRemoveResponse) error {
	*resp = NodeRemoveResponse{}
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	version := req.Version
	if !nodeVersionRe.MatchString(version) {
		resp.Error = "not a valid node version"
		return nil
	}
	_, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepRemoveNodeVersion, "node", version, "remove"),
	)
	if err != nil {
		*resp = NodeRemoveResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	authorizedReq := *req
	authorizedReq.Version = version
	return a.removeNodeVersion(&authorizedReq, resp)
}

// removeNodeVersion performs the already-authorized filesystem mutation.
// removeNodeVersion, önceden yetkilendirilmiş dosya sistemi değişikliğini yapar.
func (a *Agent) removeNodeVersion(req *NodeRemoveRequest, resp *NodeRemoveResponse) error {
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	if !nodeVersionRe.MatchString(req.Version) {
		resp.Error = "not a valid node version"
		return nil
	}
	dir := filepath.Join(runtimesBaseDir, "node", req.Version)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		resp.Removed = true
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		resp.Error = fmt.Sprintf("could not remove %s: %v", req.Version, err)
		return nil
	}
	resp.Removed = true
	return nil
}

type NodeLTSRelease = transport.NodeLTSRelease

type NodeLTSResponse = transport.NodeLTSResponse

// ListNodeLTS fetches the official release index and returns the newest
// build of each LTS line, newest line first — the drawer's named install
// options. The free-text version box died with B3d: an operator should pick
// "Node 24 (LTS)" the way they pick "PHP 8.3", not transcribe semvers.
// ListNodeLTS, resmi sürüm dizinini çeker ve her LTS hattının en yeni
// yapımını, en yeni hat önce döndürür — çekmecenin adlandırılmış kurulum
// seçenekleri. Serbest sürüm kutusu B3d ile öldü: operatör "PHP 8.3" seçer
// gibi "Node 24 (LTS)" seçmeli, semver kopyalamamalı.
func (a *Agent) ListNodeLTS(_ *transport.Empty, resp *NodeLTSResponse) error {
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Get("https://nodejs.org/dist/index.json")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		resp.Error = fmt.Sprintf("HTTP %d", res.StatusCode)
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	releases, err := parseNodeLTS(body)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Releases = releases
	return nil
}

// parseNodeLTS: index entries carry lts as EITHER false OR a codename string
// — a deliberately mixed-type JSON field, hence json.RawMessage. The index
// is newest-first per line, so the first entry seen for a major is its
// newest build. Capped at 4 lines: the drawer offers choices, not an archive.
// parseNodeLTS: dizin kayıtlarında lts alanı YA false YA kod adı dizesidir —
// bilerek karışık tipli bir JSON alanı, bu yüzden json.RawMessage. Dizin hat
// başına en-yeni-önce sıralıdır; bir major için görülen ilk kayıt en yeni
// yapımıdır. 4 hatla sınırlı: çekmece arşiv değil seçenek sunar.
func parseNodeLTS(body []byte) ([]NodeLTSRelease, error) {
	var raw []struct {
		Version string          `json:"version"` // "v24.18.0"
		LTS     json.RawMessage `json:"lts"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("release index unreadable: %v", err)
	}
	seen := map[string]bool{}
	out := []NodeLTSRelease{}
	for _, r := range raw {
		var name string
		if json.Unmarshal(r.LTS, &name) != nil || name == "" {
			continue // lts:false ya da beklenmedik tip
		}
		v := strings.TrimPrefix(r.Version, "v")
		if !nodeVersionRe.MatchString(v) {
			continue
		}
		major := v[:strings.Index(v, ".")]
		if seen[major] {
			continue
		}
		seen[major] = true
		out = append(out, NodeLTSRelease{Version: v, Name: name})
		if len(out) == 4 {
			break
		}
	}
	return out, nil
}
