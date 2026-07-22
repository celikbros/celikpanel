package main

import (
	"encoding/json"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

// A cached scan must never be able to answer a catalogue question. The bug
// this guards against shipped to both live servers: the cache held whole API
// responses, so after the Kind field was added every service came back with
// kind:"" — the old rows simply had no such key, and nothing re-derived it.
// The same staleness hit every catalogue field: a renamed service kept its old
// name on screen until someone happened to press Scan.
//
// Önbellekteki tarama, bir katalog sorusuna asla yanıt verememelidir. Bu testin
// koruduğu hata iki canlı sunucuya da gitti: önbellek tüm API yanıtlarını
// tutuyordu, bu yüzden Kind alanı eklendikten sonra her servis kind:"" ile
// döndü — eski satırlarda böyle bir anahtar yoktu ve hiçbir şey onu yeniden
// türetmiyordu. Aynı bayatlama her katalog alanını vuruyordu: adı değişen bir
// servis, biri Tara'ya basana dek ekranda eski adıyla kalıyordu.
func TestCatalogViewIgnoresStaleCatalogueFields(t *testing.T) {
	// A cache row written by an older panel: full responses, a stale name, the
	// deleted `daemonless` flag, and no `kind` at all.
	legacy := `[{"id":"nginx","name":"ESKI AD","description":"bayat aciklama",
	             "icon":"?","category":"yanlis","versions":["1.0"],
	             "status":"active (running)","is_installed":true,
	             "daemonless":true,"config_files":[]},
	            {"id":"phpmyadmin","name":"pma","status":"not_installed",
	             "is_installed":false,"config_files":[]}]`

	view := catalogView(decodeScanCache(legacy), "apt")
	if len(view) != len(core.ManagedServices) {
		t.Fatalf("view has %d services, catalogue has %d", len(view), len(core.ManagedServices))
	}

	byID := map[string]ManagedServiceResponse{}
	for _, s := range view {
		byID[s.ID] = s
	}

	nginx := byID["nginx"]
	catalogNginx := core.GetManagedServiceByID("nginx")
	if nginx.Name != catalogNginx.Name {
		t.Errorf("name came from the cache (%q), not the catalogue (%q)", nginx.Name, catalogNginx.Name)
	}
	if nginx.Kind != catalogNginx.Kind {
		t.Errorf("kind = %q, want %q — a field absent from the cache must still be served", nginx.Kind, catalogNginx.Kind)
	}
	if nginx.Category != catalogNginx.Category {
		t.Errorf("category = %q, want %q", nginx.Category, catalogNginx.Category)
	}

	// Observed state, by contrast, MUST survive the round trip: it is the one
	// thing only the scan knows. / Gözlenen durum ise round trip'ten sağ
	// çıkmalıdır: yalnız taramanın bildiği tek şey odur.
	if !nginx.IsInstalled || nginx.Status != "active (running)" {
		t.Errorf("observed state lost: installed=%v status=%q", nginx.IsInstalled, nginx.Status)
	}
	// Pre-B3b versions (minus the sentinel) also survive until the next scan.
	// B3b öncesi sürümler de (sentinel hariç) bir sonraki taramaya dek yaşar.
	if len(nginx.Versions) != 1 || nginx.Versions[0] != "1.0" {
		t.Errorf("legacy versions lost: %v", nginx.Versions)
	}
	if pma := byID["phpmyadmin"]; pma.IsInstalled {
		t.Error("phpmyadmin should still read as not installed")
	}
}

// The "default" sentinel is dead (B3b): an installed service with no known
// versions answers versions: [] — the UI's honest "—" — and never invents a
// word that used to leak into router state, ?version= queries and RPC calls.
// Versions now derive from the instance list; the legacy Versions field is
// honored only when no instances exist (pre-upgrade cache rows).
//
// "default" sentinel'i öldü (B3b): sürümü bilinmeyen kurulu servis
// versions: [] der — arayüzün dürüst "—"si — ve eskiden yönlendirici durumuna,
// ?version= sorgularına ve RPC çağrılarına sızan o kelimeyi asla uydurmaz.
// Versions artık instance listesinden türetilir; eski Versions alanına yalnız
// hiç instance yokken uyulur (yükseltme öncesi önbellek satırları).
func TestSentinelIsDeadAndInstancesDriveVersions(t *testing.T) {
	obs := []serviceObservation{
		// Installed, zero versions known — the case that used to say "default".
		{ID: "nginx", IsInstalled: true, Status: "active (running)"},
		// A legacy row that literally carries the sentinel: it must be dropped.
		{ID: "mariadb", IsInstalled: true, Status: "inactive (dead)", Versions: []string{"default"}},
		// A runtime with real instances: versions come from them, in order.
		{ID: "php-fpm", IsInstalled: true, Status: "active (running)", Instances: []core.ServiceInstance{
			{Version: "8.4", Unit: "php8.4-fpm", Managed: true, Status: "active (running)"},
			{Version: "8.3", Unit: "php8.3-fpm", Managed: true, Status: "inactive (dead)"},
		}},
		// A runtime whose only instance is unmanaged (system PATH node):
		// shown in the drawer, but it grants no versions and no installedness.
		{ID: "node", IsInstalled: false, Status: "not_installed", Instances: []core.ServiceInstance{
			{Version: "20.11.1", Path: "/usr/bin/node", Managed: false},
		}},
	}

	byID := map[string]ManagedServiceResponse{}
	for _, s := range catalogView(obs, "apt") {
		byID[s.ID] = s
	}

	for _, id := range []string{"nginx", "mariadb"} {
		if got := byID[id].Versions; len(got) != 0 {
			t.Errorf("%s: versions = %v, want [] — the sentinel must stay dead", id, got)
		}
	}
	php := byID["php-fpm"]
	if len(php.Versions) != 2 || php.Versions[0] != "8.4" || php.Versions[1] != "8.3" {
		t.Errorf("php versions = %v, want [8.4 8.3] from instances", php.Versions)
	}
	if len(php.Instances) != 2 || php.Instances[0].Unit != "php8.4-fpm" {
		t.Errorf("php instances lost detail: %+v", php.Instances)
	}
	node := byID["node"]
	if len(node.Versions) != 0 {
		t.Errorf("node versions = %v — an unmanaged instance must not grant versions", node.Versions)
	}
	if node.IsInstalled {
		t.Error("a system-PATH-only node must not read as installed")
	}
	if len(node.Instances) != 1 || node.Instances[0].Managed {
		t.Errorf("the unmanaged instance must still be visible in the drawer: %+v", node.Instances)
	}
	// Instances is part of the response contract: always [], never null.
	// Instances yanıt sözleşmesinin parçası: her zaman [], asla null.
	if byID["nginx"].Instances == nil {
		t.Error("instances must be non-nil for every row")
	}
}

// Every catalogue entry appears in the view even when the cache has never
// heard of it — that is how a newly added service shows up right after deploy
// instead of waiting for a rescan.
// Önbellek onu hiç duymamış olsa bile her katalog kalemi görünümde yer alır —
// yeni eklenen bir servis, taramayı beklemek yerine dağıtımdan hemen sonra
// böyle ortaya çıkar.
func TestCatalogViewIncludesUnscannedServices(t *testing.T) {
	view := catalogView(nil, "apt")
	if len(view) != len(core.ManagedServices) {
		t.Fatalf("empty cache produced %d rows, want %d", len(view), len(core.ManagedServices))
	}
	for _, s := range view {
		if s.Kind == "" {
			t.Errorf("%s: kind is empty", s.ID)
		}
		if s.IsInstalled || s.Status != "not_installed" {
			t.Errorf("%s: unscanned service must read as not installed, got %q", s.ID, s.Status)
		}
	}
}

// The current format round-trips exactly, and pkgFamily still selects the
// right package names — the one host fact that reaches the view from outside
// the catalogue.
// Güncel biçim birebir round-trip yapar ve pkgFamily doğru paket adlarını
// seçmeyi sürdürür — görünüme katalog dışından ulaşan tek makine gerçeği.
func TestScanCacheRoundTripAndPkgFamily(t *testing.T) {
	obs := []serviceObservation{{ID: "nginx", IsInstalled: true, Status: "active (running)"}}
	data, err := json.Marshal(scanCacheDoc{Observations: obs})
	if err != nil {
		t.Fatal(err)
	}
	back := decodeScanCache(string(data))
	if len(back) != 1 || back[0].ID != "nginx" || !back[0].IsInstalled {
		t.Fatalf("round trip lost the observation: %+v", back)
	}

	catalogNginx := core.GetManagedServiceByID("nginx")
	for _, family := range []string{"apt", "pacman"} {
		want := catalogNginx.Packages[family]
		if len(want) == 0 {
			continue
		}
		for _, s := range catalogView(back, family) {
			if s.ID == "nginx" && len(s.Packages) != len(want) {
				t.Errorf("%s: packages = %v, want %v", family, s.Packages, want)
			}
		}
	}
}
