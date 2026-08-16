package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/services"
)

// Who may the config editor write to, as root?
//
// The old answer was "anything whose path starts with /etc/ or /var/www/",
// checked with a bare strings.HasPrefix and no cleaning. That is not a
// boundary, it is a suggestion:
//
//   - "/etc/../root/.ssh/authorized_keys" passes the prefix test and lands in
//     /root/.ssh — the server is handed over.
//   - "/etc/cron.d/anything" and "/etc/systemd/system/x.service" are inside the
//     prefix by construction, and both mean "run my command as root".
//   - "/etc/celikpanel/agent.token" is the credential guarding this very RPC.
//
// The rule now: a path is written only if it is one of the config files the
// SCANNER actually discovered for a catalogue component, or lives under a
// directory the panel itself owns. Everything is resolved with filepath.Clean
// and symlinks are refused, so no ".." and no link can escape. This mirrors the
// invariant already true of installs: the agent re-derives what it is allowed
// to touch from its own compiled catalogue and the real filesystem — never from
// the request.
//
// Yapılandırma editörü root olarak nereye yazabilir?
//
// Eski cevap "yolu /etc/ ya da /var/www/ ile başlayan her şey"di; çıplak bir
// strings.HasPrefix ile, temizleme yapılmadan. Bu bir sınır değil, bir
// temennidir:
//
//   - "/etc/../root/.ssh/authorized_keys" önek sınavını geçer ve /root/.ssh'a
//     düşer — sunucu devredilmiş olur.
//   - "/etc/cron.d/herhangi" ve "/etc/systemd/system/x.service" zaten öneğin
//     içindedir ve ikisi de "komutumu root olarak çalıştır" demektir.
//   - "/etc/celikpanel/agent.token" tam da bu RPC'yi koruyan kimlik bilgisidir.
//
// Yeni kural: bir yola ancak TARAYICININ bir katalog bileşeni için gerçekten
// keşfettiği yapılandırma dosyalarından biriyse ya da panelin kendi sahibi
// olduğu bir dizinin altındaysa yazılır. Her şey filepath.Clean ile çözülür ve
// sembolik bağlar reddedilir; böylece ne ".." ne de bir bağ dışarı kaçabilir.
// Bu, kurulumlarda zaten geçerli olan değişmez kuralın aynısıdır: agent, neye
// dokunabileceğini kendi derlenmiş kataloğundan ve gerçek dosya sisteminden
// yeniden türetir — istekten asla.

// forbiddenExact names paths that are inside an allowed area but must never be
// rewritten through the config editor, because writing them is not editing a
// config — it is taking over the machine or the panel.
// forbiddenExact, izinli bir alanın içinde olan ama yapılandırma editöründen
// asla yeniden yazılmaması gereken yolları adlandırır; çünkü onları yazmak
// yapılandırma düzenlemek değil, makineyi ya da paneli devralmaktır.
var forbiddenPrefixes = []string{
	"/etc/celikpanel/",
	"/etc/systemd/",
	"/etc/cron.d/",
	"/etc/cron.hourly/",
	"/etc/cron.daily/",
	"/etc/sudoers",
	"/etc/pam.d/",
	"/etc/ssh/",
	"/etc/shadow",
	"/etc/passwd",
	"/etc/group",
	"/etc/ld.so.preload",
	"/etc/apt/sources.list.d/",
	"/etc/apt/trusted.gpg.d/",
	// DNS engine configuration, zone generations, and runtime databases are
	// published only through the epoch-bound DNS engine workflows. The generic
	// root config editor must never bypass their journal/rollback invariants.
	"/etc/powerdns/",
	"/etc/bind/",
	"/etc/named.conf",
	"/etc/named/",
	"/var/lib/powerdns/",
	"/var/cache/bind/celikpanel/",
	"/var/named/celikpanel/",
}

// configWriteAllowed reports whether the agent may write this path, and why not
// when it may not. The reason is returned so the panel can show something
// better than a generic refusal.
// configWriteAllowed, agent'ın bu yola yazıp yazamayacağını ve yazamıyorsa
// nedenini bildirir. Neden döndürülür ki panel genel bir retten iyisini
// gösterebilsin.
func configWriteAllowed(path string) (string, error) {
	return configWriteAllowedFrom(path, discoveredConfigPaths, rejectConfigPathSymlinks)
}

// configWriteAllowedFrom keeps the authorization decision independent from
// filesystem accessibility. A caller-supplied path that is not in the
// catalogue-derived allow-list is refused before we inspect any of its path
// components. Besides producing the typed refusal promised by the RPC, this
// prevents an unprivileged test runner (or a future non-root agent) from
// turning an expected refusal into an incidental EACCES transport error.
// Filesystem inspection remains mandatory after a path is authorized, so a
// managed path containing a symlink is still rejected fail-closed.
func configWriteAllowedFrom(path string, discover func() []string, inspect func(string) error) (string, error) {
	if path == "" {
		return "", configPathRefusal("path is required")
	}
	if !strings.HasPrefix(path, "/") {
		return "", configPathRefusal("path must be absolute")
	}
	clean := filepath.Clean(path)
	policyPath := filepath.ToSlash(clean)

	for _, bad := range forbiddenPrefixes {
		if policyPath == strings.TrimSuffix(bad, "/") || strings.HasPrefix(policyPath, bad) {
			return "", configPathRefusal("this path is protected and cannot be edited here: %s", clean)
		}
	}
	if discover == nil {
		return "", fmt.Errorf("managed configuration discovery is unavailable")
	}

	// The scanner's discovered config files are the authoritative list of what
	// a component actually reads — the same list the UI offers for editing.
	// Tarayıcının keşfettiği yapılandırma dosyaları, bir bileşenin gerçekten
	// neyi okuduğunun yetkili listesidir — arayüzün düzenlemeye sunduğu liste.
	managed := false
	for _, p := range discover() {
		if filepath.Clean(p) == clean {
			managed = true
			break
		}
	}
	if !managed {
		return "", configPathRefusal("not a managed configuration file: %s", clean)
	}
	if inspect == nil {
		return "", fmt.Errorf("managed configuration path inspection is unavailable")
	}

	// Check every existing component only after the catalogue authorizes the
	// path. The actual file operation repeats this rule atomically in the Linux
	// kernel with openat2.
	// Mevcut her yol bileşenini ancak katalog yolu yetkilendirdikten sonra
	// denetle. Asıl dosya işlemi bu kuralı Linux çekirdeğinde openat2 ile atomik
	// olarak tekrarlar.
	if err := inspect(clean); err != nil {
		return "", err
	}
	return clean, nil
}

// rejectConfigPathSymlinks checks every existing path component. Missing
// suffixes are inspected until the first missing component. Missing paths are
// still rejected by the discovered-config allow-list below; this preflight
// only improves error messages and secureConfig* remains authoritative.
func rejectConfigPathSymlinks(clean string) error {
	current := string(os.PathSeparator)
	for _, part := range strings.Split(strings.TrimPrefix(clean, current), current) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("cannot inspect managed configuration path %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return configPathRefusal("refusing configuration path containing a symbolic link: %s", current)
		}
	}
	return nil
}

// discoveredConfigPaths returns every config file the scanner finds for the
// catalogue's components on THIS host. Deriving it (instead of hardcoding a
// second list) means a component added to the catalogue tomorrow is editable
// without touching this file — and a path no component owns is never writable.
// discoveredConfigPaths, tarayıcının BU makinede katalog bileşenleri için
// bulduğu her yapılandırma dosyasını döndürür. Bunu türetmek (ikinci bir liste
// yazmak yerine), yarın kataloğa eklenen bir bileşenin bu dosyaya dokunmadan
// düzenlenebilir olması demektir — ve hiçbir bileşenin sahiplenmediği bir yol
// asla yazılabilir değildir.
func discoveredConfigPaths() []string {
	scanner := services.NewServiceScanner()
	var out []string
	for i := range core.ManagedServices {
		svc := &core.ManagedServices[i]
		// Scan by every unit name the component can have, plus its id — the
		// scanner keys on the unit ("named", "apache2"), not the catalogue id.
		// Bileşenin sahip olabileceği her unit adıyla ve id'siyle tara —
		// tarayıcı katalog id'sine değil unit'e ("named", "apache2") bakar.
		for _, name := range append(append([]string{}, svc.SystemNames...), svc.ID) {
			cfgs, err := scanner.ScanService(name)
			if err != nil {
				continue
			}
			for _, cf := range cfgs {
				out = append(out, cf.Path)
			}
		}
	}
	return out
}
