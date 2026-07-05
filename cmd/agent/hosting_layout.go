package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// The on-disk permission model for hosted sites (the Plesk psaserv/psacln
// idea): every site runs and owns its files as its own Linux user, the web
// server reaches the files through group membership, and other site users
// are locked out. Explicit chmods — the agent runs under UMask=0027, so
// MkdirAll modes alone are not what lands on disk.
//
// Barındırılan sitelerin diskteki izin modeli (Plesk'in psaserv/psacln
// fikri): her site kendi Linux kullanıcısıyla çalışır ve dosyalarının sahibi
// odur, web sunucusu dosyalara grup üyeliğiyle ulaşır, diğer site
// kullanıcıları dışarıda kalır. Chmod'lar açıktır — agent UMask=0027 ile
// çalışır; MkdirAll kipleri diske olduğu gibi yansımaz.
//
//   …/subscriptions, base   0755 root      — sabit kökler, herkes geçer
//   subscriptions/N, sites  0751 root      — geçiş var, listeleme yok
//   site home               0750 user:web  — web sunucusu okur, başkası giremez
//   public_html             2750 user:web  — setgid: yeni dosya web grubunu alır
//   files                   0640 user:web

// webServerGroup reports the group the local web server runs as, or "" when
// none is installed yet (Debian family: www-data; RHEL family: nginx).
// webServerGroup, yerel web sunucusunun çalıştığı grubu bildirir; henüz
// kurulu değilse "" (Debian ailesi: www-data; RHEL ailesi: nginx).
func webServerGroup() string {
	for _, g := range []string{"www-data", "nginx"} {
		if _, err := user.LookupGroup(g); err == nil {
			return g
		}
	}
	return ""
}

// applyHostingLayout enforces the layout above for one freshly created site.
// Returns the first error for the caller to log; a dev (non-root) agent will
// fail here harmlessly and the caller must not abort site creation.
// applyHostingLayout, yukarıdaki düzeni yeni oluşturulan bir site için
// uygular. Günlüklemesi için ilk hatayı döndürür; dev (root olmayan) agent
// burada zararsızca başarısız olur ve çağıran site oluşturmayı iptal etmemeli.
func applyHostingLayout(documentRoot, username string) error {
	siteHome := filepath.Dir(documentRoot)
	sitesDir := filepath.Dir(siteHome)
	subscriptionDir := filepath.Dir(sitesDir)
	subscriptionsRoot := filepath.Dir(subscriptionDir)
	baseDir := filepath.Dir(subscriptionsRoot)

	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	keep(os.Chmod(baseDir, 0o755))
	keep(os.Chmod(subscriptionsRoot, 0o755))
	keep(os.Chmod(subscriptionDir, 0o751))
	keep(os.Chmod(sitesDir, 0o751))

	group := webServerGroup()
	if group == "" {
		// No web server yet — the site stays user-private; installing nginx
		// later and recreating/repairing the site applies the web group.
		// Henüz web sunucusu yok — site kullanıcıya özel kalır; nginx sonra
		// kurulunca siteyi yeniden oluşturmak/onarmak web grubunu uygular.
		return firstErr
	}

	owner := fmt.Sprintf("%s:%s", username, group)
	if out, err := exec.Command("chown", owner, siteHome).CombinedOutput(); err != nil {
		keep(fmt.Errorf("chown %s: %s", siteHome, string(out)))
	}
	if out, err := exec.Command("chown", "-R", owner, documentRoot).CombinedOutput(); err != nil {
		keep(fmt.Errorf("chown -R %s: %s", documentRoot, string(out)))
	}
	keep(os.Chmod(siteHome, 0o750))
	// Setgid is a separate FileMode flag in Go — a raw 0o2750 literal would
	// silently drop the bit.
	// Setgid, Go'da ayrı bir FileMode bayrağıdır — düz 0o2750 sabiti biti
	// sessizce düşürürdü.
	keep(os.Chmod(documentRoot, 0o750|os.ModeSetgid))

	// Only regular files at creation time (the placeholder index).
	// Oluşturma anında yalnız düz dosyalar var (yer tutucu index).
	entries, err := os.ReadDir(documentRoot)
	keep(err)
	for _, e := range entries {
		if e.Type().IsRegular() {
			keep(os.Chmod(filepath.Join(documentRoot, e.Name()), 0o640))
		}
	}
	return firstErr
}
