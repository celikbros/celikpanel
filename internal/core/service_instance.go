package core

// ServiceInstance is ONE installed copy of a catalogue item, as discovered on
// the host. It is the single per-version contract between agent and panel
// (B3b): php8.3-fpm on Debian, the lone php-fpm on Arch, and each Node tree
// under /opt/celikpanel/runtimes are all "an instance". The old model forced
// every discovery into a bare version string parsed out of a unit name — which
// worked for exactly one service on exactly one distro and produced the
// "default" sentinel everywhere else.
//
// ServiceInstance, bir katalog kaleminin makinede keşfedilen TEK kurulu
// kopyasıdır. Agent ile panel arasındaki sürüm-başına tek sözleşmedir (B3b):
// Debian'daki php8.3-fpm, Arch'taki tek php-fpm ve /opt/celikpanel/runtimes
// altındaki her Node ağacı birer "instance"tır. Eski model her keşfi unit
// adından ayrıştırılan çıplak bir sürüm dizisine zorluyordu — bu, tam olarak
// tek serviste ve tek dağıtımda çalışıyor, geri kalan her yerde "default"
// sentinel'ini üretiyordu.
type ServiceInstance struct {
	// Version is what the operator picks and plans grant (D-014): "8.3" for
	// PHP (major.minor — the packaging unit), "24.18.0" for Node (full
	// semver — the tarball unit). The two shapes coexist by design.
	// Version, operatörün seçtiği ve planların verdiği şeydir (D-014): PHP'de
	// "8.3" (major.minor — paketleme birimi), Node'da "24.18.0" (tam semver —
	// tarball birimi). İki biçim bilerek yan yanadır.
	Version string `json:"version"`
	// Unit is the systemd unit operating THIS instance ("php8.3-fpm").
	// Empty means this instance has no daemon of its own — a Node tree is
	// only ever executed by per-site app units, so start/stop of the
	// instance itself is meaningless and the UI must not offer it.
	// Unit, BU kopyayı işleten systemd unit'idir ("php8.3-fpm"). Boşsa bu
	// kopyanın kendine ait daemon'ı yoktur — bir Node ağacını yalnız site
	// başına app unit'leri çalıştırır; kopyanın kendisini başlat/durdur
	// anlamsızdır ve arayüz bunu sunmamalıdır.
	Unit string `json:"unit,omitempty"`
	// Path is where the instance lives: /etc/php/8.3 on Debian, /etc/php on
	// Arch, /opt/celikpanel/runtimes/node/24.18.0 for a managed Node.
	// Path, kopyanın yaşadığı yerdir.
	Path string `json:"path,omitempty"`
	// Managed: the panel installed it and may operate/remove it. False marks
	// something found on the host (the system PATH node) — shown for honesty,
	// never operated: the panel only works with what it installed.
	// Managed: panel kurdu, işletebilir/kaldırabilir. False, makinede bulunanı
	// işaretler (sistem PATH'indeki node) — dürüstlük için gösterilir, asla
	// işletilmez: panel yalnız kendi kurduğuyla çalışır.
	Managed bool `json:"managed"`
	// Status uses the scan's existing dialect ("active (running)",
	// "inactive (dead)"); empty when Unit is empty — no daemon, no state.
	// Status taramanın mevcut sözlüğünü kullanır; Unit boşken boştur —
	// daemon yoksa durum da yoktur.
	Status string `json:"status,omitempty"`
	// SizeBytes is the instance's disk footprint where one directory honestly
	// owns it (a Node tree). Zero for PHP: its bytes are spread across shared
	// package paths and any single number would be an invention.
	// SizeBytes, tek bir dizinin dürüstçe sahiplendiği yerde kopyanın disk
	// ayak izidir (Node ağacı). PHP'de sıfırdır: baytları paylaşılan paket
	// yollarına dağılmıştır, tek bir sayı uydurma olurdu.
	SizeBytes int64 `json:"size_bytes,omitempty"`
}
