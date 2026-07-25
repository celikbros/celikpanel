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

// panelOwnedPrefixes are directories the panel itself creates and manages, so
// writing inside them is writing to our own work.
// panelOwnedPrefixes, panelin kendi oluşturup yönettiği dizinlerdir; oraya
// yazmak kendi işimize yazmaktır.
var panelOwnedPrefixes = []string{
	"/etc/nginx/sites-available/",
	"/etc/nginx/conf.d/",
	"/var/www/",
}

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
}

// configWriteAllowed reports whether the agent may write this path, and why not
// when it may not. The reason is returned so the panel can show something
// better than a generic refusal.
// configWriteAllowed, agent'ın bu yola yazıp yazamayacağını ve yazamıyorsa
// nedenini bildirir. Neden döndürülür ki panel genel bir retten iyisini
// gösterebilsin.
func configWriteAllowed(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	clean := filepath.Clean(path)

	// A symlink anywhere along the way can point outside every list below, so
	// the resolved path is what gets judged — and a link is refused outright
	// rather than silently followed.
	// Yol üzerindeki herhangi bir sembolik bağ aşağıdaki listelerin dışına
	// çıkabilir; bu yüzden yargılanan çözülmüş yoldur — ve bağ sessizce takip
	// edilmek yerine doğrudan reddedilir.
	if fi, err := os.Lstat(clean); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to write through a symbolic link: %s", clean)
	}

	for _, bad := range forbiddenPrefixes {
		if clean == strings.TrimSuffix(bad, "/") || strings.HasPrefix(clean, bad) {
			return "", fmt.Errorf("this path is protected and cannot be edited here: %s", clean)
		}
	}

	for _, pre := range panelOwnedPrefixes {
		if strings.HasPrefix(clean, pre) {
			return clean, nil
		}
	}

	// The scanner's discovered config files are the authoritative list of what
	// a component actually reads — the same list the UI offers for editing.
	// Tarayıcının keşfettiği yapılandırma dosyaları, bir bileşenin gerçekten
	// neyi okuduğunun yetkili listesidir — arayüzün düzenlemeye sunduğu liste.
	for _, p := range discoveredConfigPaths() {
		if filepath.Clean(p) == clean {
			return clean, nil
		}
	}

	return "", fmt.Errorf("not a managed configuration file: %s", clean)
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
