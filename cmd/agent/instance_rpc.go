package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Agent.ListServiceInstances is the ONE per-version discovery contract (B3b):
// for a catalogue item, every installed copy on this host — see
// core.ServiceInstance for what a "copy" is. It replaces version-parsing from
// unit names on the panel side (extractVersion), which understood exactly one
// service on exactly one distro layout and answered "default" everywhere else.
//
// Agent.ListServiceInstances, sürüm-başına TEK keşif sözleşmesidir (B3b): bir
// katalog kalemi için bu makinedeki her kurulu kopya — "kopya"nın ne olduğu
// core.ServiceInstance'ta. Panel tarafında unit adından sürüm ayrıştırmanın
// (extractVersion) yerine geçer; o, tam olarak tek servisi tek dağıtım
// düzeninde anlıyor, geri kalan her yerde "default" cevaplıyordu.

type ServiceInstancesRequest = transport.ServiceInstancesRequest

type ServiceInstancesResponse = transport.ServiceInstancesResponse

func (a *Agent) ListServiceInstances(req *ServiceInstancesRequest, resp *ServiceInstancesResponse) error {
	resp.Instances = []core.ServiceInstance{}
	switch req.ID {
	case "php-fpm":
		resp.Instances = phpInstances()
	case "node":
		resp.Instances = nodeInstances()
	default:
		// Empty is the honest answer for a service with no per-instance
		// model: one unit is one copy and the row already tells that truth.
		// Growing a third implementation means adding a case here, nothing
		// else — the contract does not change.
		// Sürüm-başına modeli olmayan servise dürüst cevap boştur: tek unit
		// tek kopyadır ve satır o gerçeği zaten söyler. Üçüncü bir uygulama
		// eklemek buraya bir case eklemektir, başka bir şey değil — sözleşme
		// değişmez.
	}
	return nil
}

// phpInstances: on Debian/Sury every version is its own unit (php8.3-fpm) with
// its own config tree (/etc/php/8.3). On Arch there is exactly ONE unversioned
// php-fpm unit and /etc/php has no version directories — the version lives
// only in the binary, so we ask the binary. Both layouts come back as the same
// shape; the panel never learns which distro it is talking to.
//
// phpInstances: Debian/Sury'de her sürüm kendi unit'i (php8.3-fpm) ve kendi
// config ağacıyla (/etc/php/8.3) gelir. Arch'ta TEK sürümsüz php-fpm unit'i
// vardır ve /etc/php'de sürüm dizini yoktur — sürüm yalnız binary'dedir, biz
// de binary'ye sorarız. İki düzen de aynı biçimde döner; panel hangi dağıtımla
// konuştuğunu hiç öğrenmez.
func phpInstances() []core.ServiceInstance {
	out := []core.ServiceInstance{}
	for _, u := range unitsMatching(`^php[0-9]+\.[0-9]+-fpm$`) {
		v := strings.TrimSuffix(strings.TrimPrefix(u, "php"), "-fpm")
		inst := core.ServiceInstance{Version: v, Unit: u, Managed: true, Status: unitStatusLine(u)}
		if p := "/etc/php/" + v; dirExists(p) {
			inst.Path = p
		}
		out = append(out, inst)
	}
	if len(out) == 0 && unitExists("php-fpm") {
		inst := core.ServiceInstance{
			Version: phpBinaryVersion(),
			Unit:    "php-fpm",
			Managed: true,
			Status:  unitStatusLine("php-fpm"),
		}
		if dirExists("/etc/php") {
			inst.Path = "/etc/php"
		}
		out = append(out, inst)
	}
	sortInstancesNewestFirst(out)
	return out
}

// nodeInstances: a managed Node version is a verified tarball tree under
// runtimesBaseDir — no unit, no package; presence of bin/node IS installation.
// The system PATH node (if any) is reported last with Managed=false: shown for
// honesty, never operated — the panel only works with what it installed.
//
// nodeInstances: yönetilen bir Node sürümü, runtimesBaseDir altında doğrulanmış
// bir tarball ağacıdır — unit yok, paket yok; bin/node'un varlığı kurulumun
// kendisidir. PATH'teki sistem node'u (varsa) en sonda Managed=false ile
// bildirilir: dürüstlük için gösterilir, asla işletilmez — panel yalnız kendi
// kurduğuyla çalışır.
func nodeInstances() []core.ServiceInstance {
	out := []core.ServiceInstance{}
	entries, err := os.ReadDir(filepath.Join(runtimesBaseDir, "node"))
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() || !nodeVersionRe.MatchString(e.Name()) {
				continue
			}
			if _, err := os.Stat(nodeBinPath(e.Name())); err != nil {
				continue
			}
			dir := filepath.Join(runtimesBaseDir, "node", e.Name())
			out = append(out, core.ServiceInstance{
				Version:   e.Name(),
				Path:      dir,
				Managed:   true,
				SizeBytes: dirSizeBytes(dir),
			})
		}
	}
	sortInstancesNewestFirst(out)
	if path, lookErr := exec.LookPath("node"); lookErr == nil {
		if vout, verr := exec.Command("node", "--version").Output(); verr == nil {
			if v := strings.TrimPrefix(strings.TrimSpace(string(vout)), "v"); v != "" {
				out = append(out, core.ServiceInstance{Version: v, Path: path, Managed: false})
			}
		}
	}
	return out
}

// unitStatusLine reads one unit's state via `systemctl show` — stable
// machine-readable output, unlike list-units' human formatting — and renders
// it in the scan's existing dialect ("active (running)") so per-instance
// status and row status speak one language.
// unitStatusLine, tek unit'in durumunu `systemctl show` ile okur —
// list-units'in insan biçiminin aksine kararlı makine çıktısı — ve taramanın
// mevcut sözlüğünde ("active (running)") döndürür; kopya durumu ile satır
// durumu tek dil konuşur.
func unitStatusLine(unit string) string {
	out, err := exec.Command("systemctl", "show", unit+".service", "--property=ActiveState,SubState").Output()
	if err != nil {
		return ""
	}
	var active, sub string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if v := strings.TrimPrefix(line, "ActiveState="); v != line {
			active = v
		}
		if v := strings.TrimPrefix(line, "SubState="); v != line {
			sub = v
		}
	}
	return composeUnitStatus(active, sub)
}

func composeUnitStatus(active, sub string) string {
	if active == "" {
		return ""
	}
	if sub == "" {
		return active
	}
	return active + " (" + sub + ")"
}

var phpVersionInBanner = regexp.MustCompile(`PHP ([0-9]+\.[0-9]+)`)

// phpBinaryVersion asks the interpreter itself ("PHP 8.4.11 (fpm-fcgi)…" →
// "8.4"). Only reached on the single-unit layout, where no file or unit name
// carries the version. Empty when nothing answers — the caller shows an
// instance with an unknown version rather than inventing one.
// phpBinaryVersion, yorumlayıcının kendisine sorar. Yalnız tek-unit düzeninde
// çağrılır; orada sürümü hiçbir dosya ya da unit adı taşımaz. Kimse cevap
// vermezse boştur — çağıran, sürüm uydurmak yerine sürümü bilinmeyen bir
// kopya gösterir.
func phpBinaryVersion() string {
	for _, bin := range []string{"php-fpm", "php"} {
		if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		out, err := exec.Command(bin, "-v").Output()
		if err != nil {
			continue
		}
		if m := phpVersionInBanner.FindSubmatch(out); m != nil {
			return string(m[1])
		}
	}
	return ""
}

// dirExists: true only for an existing directory.
func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// dirSizeBytes: `du -sb` (the established tool here — usage_rpc.go uses it for
// site homes). Zero on failure: size is decoration, never worth an error.
// dirSizeBytes: `du -sb` (buradaki yerleşik araç — usage_rpc.go site home'ları
// için kullanıyor). Hatada sıfır: boyut süstür, asla hataya değmez.
func dirSizeBytes(path string) int64 {
	out, err := exec.Command("du", "-sb", path).Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// versionSegments parses every dotted numeric run ("24.18.0" → [24 18 0]).
// Non-numeric tails are ignored; comparison is segment-wise numeric, so
// 24.18.0 > 9.9.9 and 8.10 > 8.9 — the two orderings lexical sorts get wrong.
// versionSegments her noktalı sayısal parçayı ayrıştırır. Sayısal olmayan
// kuyruklar yok sayılır; karşılaştırma parça parça sayısaldır — 24.18.0 >
// 9.9.9 ve 8.10 > 8.9, yani sözlük sıralamasının yanlış yaptığı iki sıra.
func versionSegments(v string) []int {
	segs := []int{}
	for _, part := range strings.Split(v, ".") {
		digits := part
		for i, r := range part {
			if r < '0' || r > '9' {
				digits = part[:i]
				break
			}
		}
		if digits == "" {
			break
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			break
		}
		segs = append(segs, n)
	}
	return segs
}

func versionLess(a, b string) bool {
	as, bs := versionSegments(a), versionSegments(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

func sortInstancesNewestFirst(in []core.ServiceInstance) {
	sort.SliceStable(in, func(i, j int) bool {
		return versionLess(in[j].Version, in[i].Version)
	})
}
