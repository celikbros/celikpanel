package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
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

type NodeVersionsResponse struct {
	Installed []string `json:"installed"`
	// SystemVersion is the `node` found on PATH, if any ("" otherwise).
	// SystemVersion, PATH'te bulunan `node` sürümüdür (yoksa "").
	SystemVersion string `json:"system_version"`
}

// ListNodeVersions reports what is actually on disk — never a wish list.
// ListNodeVersions, gerçekten diskte olanı bildirir — asla bir dilek listesi değil.
func (a *Agent) ListNodeVersions(_ *struct{}, resp *NodeVersionsResponse) error {
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

type NodeInstallRequest struct {
	Version string `json:"version"` // e.g. "24.18.0"
}

type NodeInstallResponse struct {
	Installed bool   `json:"installed"`
	Error     string `json:"error,omitempty"`
}

// InstallNodeVersion downloads, verifies and unpacks an official Node build.
// Idempotent: an already-installed version returns success immediately.
// InstallNodeVersion, resmi bir Node derlemesini indirir, doğrular ve açar.
// Bağımsızdır: zaten kurulu bir sürüm hemen başarı döndürür.
func (a *Agent) InstallNodeVersion(req *NodeInstallRequest, resp *NodeInstallResponse) error {
	if !nodeVersionRe.MatchString(req.Version) {
		resp.Error = "invalid version (expected e.g. 24.18.0)"
		return nil
	}
	if _, err := os.Stat(nodeBinPath(req.Version)); err == nil {
		resp.Installed = true
		return nil
	}

	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" {
		resp.Error = "unsupported architecture: " + runtime.GOARCH
		return nil
	}

	base := fmt.Sprintf("https://nodejs.org/dist/v%s", req.Version)
	tarName := fmt.Sprintf("node-v%s-linux-%s.tar.xz", req.Version, arch)

	client := &http.Client{Timeout: 5 * time.Minute}

	// 1. Official checksums first — they decide whether anything is trusted.
	// 1. Önce resmi sağlama toplamları — neye güvenileceğine onlar karar verir.
	wantSum, err := fetchNodeChecksum(client, base+"/SHASUMS256.txt", tarName)
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

	dl, err := client.Get(base + "/" + tarName)
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

	if out, err := exec.Command("tar", "-xJf", tmp.Name(), "-C", stage, "--strip-components=1").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("extract failed: %v: %s", err, string(out))
		return nil
	}

	dest := filepath.Join(runtimesBaseDir, "node", req.Version)
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
func fetchNodeChecksum(client *http.Client, url, fileName string) (string, error) {
	res, err := client.Get(url)
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
