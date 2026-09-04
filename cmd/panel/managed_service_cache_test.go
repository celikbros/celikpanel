package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func mustDecodeScanCache(t *testing.T, data string) []serviceObservation {
	t.Helper()
	observations, err := decodeScanCache(data)
	if err != nil {
		t.Fatal(err)
	}
	return observations
}

func newManagedServiceCachePanel(t *testing.T) *Panel {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	return &Panel{db: database, pkgFamilyVal: "apt"}
}

// A host this panel has never scanned still gets its whole installable
// catalogue — but on the wire every row says "not observed", never "absent".
//
// This test replaces one that pinned the opposite. The old version asserted
// the catalogue length and a null scanned_at and never looked at
// is_installed, which is precisely how R-040 survived: with no scan row the
// handler shipped `is_installed: false, status: "not_installed"` for every
// service, byte-identical to a host that had been inspected and found empty.
// On 3 September 2026 the same host answered `installed: true` for BIND on
// /api/v1/dns/engine (it probes dpkg live) and `is_installed: false` here.
//
// The assertion is made against the RAW JSON on purpose. `is_installed: null`
// is the contract a client sees; a typed decode into *bool would pass even if
// the field vanished from the payload entirely.
//
// Bu panelin hiç taramadığı bir makine yine de kurulabilir kataloğunun tamamını
// alır — ama telde her satır "gözlenmedi" der, asla "yok" demez. Bu test,
// tersini çivileyen bir testin yerine geçer: eski sürüm katalog uzunluğuna ve
// null scanned_at'e bakıyor, is_installed'e hiç bakmıyordu. R-040 tam da böyle
// hayatta kaldı. İddia bilerek HAM JSON üzerinde kurulur: istemcinin gördüğü
// sözleşme `is_installed: null`'dır.
func TestManagedServicesUnscannedHostReportsUnknownNotAbsent(t *testing.T) {
	panel := newManagedServiceCachePanel(t)
	recorder := httptest.NewRecorder()
	panel.handleManagedServices(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/managed-services", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload managedServicesPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ScannedAt != nil {
		t.Fatalf("scanned_at = %v, want null on a fresh database", payload.ScannedAt)
	}
	if len(payload.Services) != len(core.ManagedServices) {
		t.Fatalf("fresh database returned %d services, want catalogue size %d", len(payload.Services), len(core.ManagedServices))
	}

	var wire struct {
		Services []struct {
			ID          string          `json:"id"`
			Status      string          `json:"status"`
			IsInstalled json.RawMessage `json:"is_installed"`
			Evidence    string          `json:"installed_evidence"`
		} `json:"services"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Services) == 0 {
		t.Fatal("no services on the wire")
	}
	for _, service := range wire.Services {
		if string(service.IsInstalled) != "null" {
			t.Errorf("%s: is_installed = %s, want null — this host was never looked at",
				service.ID, service.IsInstalled)
		}
		if service.Status != "unknown" {
			t.Errorf("%s: status = %q, want %q", service.ID, service.Status, "unknown")
		}
	}
	// The catalogue is still complete and still installable-shaped: an
	// unobserved host must not become an empty Components page.
	// Katalog yine de eksiksizdir: gözlenmemiş makine boş bir Bileşenler
	// sayfasına dönüşmemelidir.
	for _, service := range payload.Services {
		if service.Observed() {
			t.Fatalf("%s: reported as observed on a fresh database", service.ID)
		}
		if service.Installed() {
			t.Fatalf("%s: reported as installed on a fresh database", service.ID)
		}
	}
}

func TestManagedServicesConditionalScanReusesFreshCache(t *testing.T) {
	panel := newManagedServiceCachePanel(t)
	scannedAt := time.Now().UTC().Truncate(time.Second)
	raw, err := json.Marshal(scanCacheDoc{Observations: []serviceObservation{{
		ID: "nftables", IsInstalled: true, Status: "installed",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panel.db.GetDB().Exec(
		`INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, ?)`,
		string(raw), scannedAt.Format(time.RFC3339),
	); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	panel.handleManagedServicesScan(recorder, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/managed-services/scan?max_age_seconds=300",
		nil,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload managedServicesPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ScannedAt == nil || !payload.ScannedAt.Equal(scannedAt) {
		t.Fatalf("conditional scan timestamp = %v, want cached %v", payload.ScannedAt, scannedAt)
	}
}

func TestManagedServicesConditionalCacheRejectsFutureTimestamp(t *testing.T) {
	panel := newManagedServiceCachePanel(t)
	raw, err := json.Marshal(scanCacheDoc{Observations: []serviceObservation{{
		ID: "nftables", IsInstalled: true, Status: "installed",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panel.db.GetDB().Exec(
		`INSERT INTO service_scan_cache (id, data, scanned_at) VALUES (1, ?, ?)`,
		string(raw), time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	); err != nil {
		t.Fatal(err)
	}

	if _, fresh, err := panel.managedServicesCacheWithin(
		context.Background(), 5*time.Minute,
	); err != nil {
		t.Fatal(err)
	} else if fresh {
		t.Fatal("future-dated service cache was accepted as fresh")
	}
}

func TestManagedServiceScanMaxAgeRejectsInvalidValue(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/managed-services/scan?max_age_seconds=0", nil)
	if _, _, err := managedServiceScanMaxAge(request); err == nil {
		t.Fatal("zero max_age_seconds must be rejected")
	}
}

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

	view := catalogView(mustDecodeScanCache(t, legacy), "apt")
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
	if !nginx.Installed() || nginx.Status != "active (running)" {
		t.Errorf("observed state lost: installed=%v status=%q", nginx.Installed(), nginx.Status)
	}
	// Pre-B3b versions (minus the sentinel) also survive until the next scan.
	// B3b öncesi sürümler de (sentinel hariç) bir sonraki taramaya dek yaşar.
	if len(nginx.Versions) != 1 || nginx.Versions[0] != "1.0" {
		t.Errorf("legacy versions lost: %v", nginx.Versions)
	}
	if pma := byID["phpmyadmin"]; pma.Installed() {
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
	if node.Installed() {
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

func TestCatalogViewCarriesStoppedPostgreSQLRepairPackage(t *testing.T) {
	obs := []serviceObservation{{
		ID:                    "postgresql",
		IsInstalled:           true,
		Status:                "inactive (dead)",
		Unit:                  "postgresql",
		InstalledRepoPackages: []string{"postgresql-17"},
	}}
	for _, service := range catalogView(obs, "apt") {
		if service.ID == "postgresql" {
			if service.RepairPackage != "postgresql-17" {
				t.Fatalf("repair_package = %q, want the observed installed package", service.RepairPackage)
			}
			if !service.RepairAvailable {
				t.Fatal("repair must be available for one exact installed PostgreSQL major")
			}
			return
		}
	}
	t.Fatal("postgresql missing from catalog view")
}

func TestSafeRepairPackageRejectsMultipleForeignOrUnmanaged(t *testing.T) {
	postgres := core.GetManagedServiceByID("postgresql")
	if postgres == nil {
		t.Fatal("postgresql missing from catalogue")
	}
	tests := []struct {
		name        string
		family      string
		observation serviceObservation
	}{
		{
			name:   "multiple installed majors",
			family: "apt",
			observation: serviceObservation{InstalledRepoPackages: []string{
				"postgresql-17", "postgresql-16",
			}},
		},
		{
			name:   "unmanaged unit without package proof",
			family: "apt",
			observation: serviceObservation{Instances: []core.ServiceInstance{
				{Unit: "postgresql@17-main", Managed: false},
			}},
		},
		{
			name:        "foreign package response",
			family:      "apt",
			observation: serviceObservation{InstalledRepoPackages: []string{"postgresql-client-17"}},
		},
		{
			name:        "non apt host",
			family:      "pacman",
			observation: serviceObservation{InstalledRepoPackages: []string{"postgresql-17"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := safeRepairPackage(postgres, test.observation, test.family); got != "" {
				t.Fatalf("safeRepairPackage = %q, want empty", got)
			}
			if _, available := repairSelection(postgres, test.observation, test.family); test.family == "apt" && available {
				t.Fatal("versioned apt repair must fail closed without one exact package")
			}
		})
	}
}

func TestOldCacheWithoutInstalledPackageProofFailsClosed(t *testing.T) {
	old := `{"observations":[{"id":"postgresql","is_installed":true,"status":"inactive (dead)","unit":"postgresql@17-main"}]}`
	view := catalogView(mustDecodeScanCache(t, old), "apt")
	for _, service := range view {
		if service.ID == "postgresql" {
			if service.RepairAvailable || service.RepairPackage != "" {
				t.Fatalf("old cache repair = (%q, %v), want unavailable without package proof", service.RepairPackage, service.RepairAvailable)
			}
			return
		}
	}
	t.Fatal("postgresql missing from catalog view")
}

// Every catalogue entry appears in the view even when the cache has never
// heard of it — that is how a newly added service shows up right after deploy
// instead of waiting for a rescan. What it must NOT do is claim the service is
// absent: nobody looked (R-040).
// Önbellek onu hiç duymamış olsa bile her katalog kalemi görünümde yer alır —
// yeni eklenen bir servis, taramayı beklemek yerine dağıtımdan hemen sonra
// böyle ortaya çıkar. Yapmaması gereken şey, servisin yok olduğunu iddia
// etmektir: kimse bakmadı (R-040).
func TestCatalogViewIncludesUnscannedServicesWithoutClaimingAbsence(t *testing.T) {
	view := catalogView(nil, "apt")
	if len(view) != len(core.ManagedServices) {
		t.Fatalf("empty cache produced %d rows, want %d", len(view), len(core.ManagedServices))
	}
	for _, s := range view {
		if s.Kind == "" {
			t.Errorf("%s: kind is empty", s.ID)
		}
		if s.IsInstalled != nil {
			t.Errorf("%s: unobserved service answered is_installed=%v, want null", s.ID, *s.IsInstalled)
		}
		if s.Status != "unknown" {
			t.Errorf("%s: unobserved status = %q, want %q", s.ID, s.Status, "unknown")
		}
		// Conflicts and unmet requirements are read off the installed set.
		// With nothing observed that set is unknown, not empty, so neither
		// may be asserted — "install PHP first" is a claim about a host we
		// have not looked at.
		// Çakışma ve karşılanmamış gereksinim kurulu kümeden okunur; hiçbir
		// şey gözlenmemişken o küme boş değil bilinmeyendir.
		if s.ConflictWith != "" || len(s.RequiresMissing) > 0 {
			t.Errorf("%s: unobserved row asserted conflict=%q requires=%v",
				s.ID, s.ConflictWith, s.RequiresMissing)
		}
	}
}

// A service added to the catalogue after the last scan was taken is in exactly
// the same position as a never-scanned host: the observation for it does not
// exist. It must read as unknown, not as absent, until a scan actually asks
// the host about it.
// Son taramadan sonra kataloğa eklenen bir servis, hiç taranmamış bir makineyle
// tam olarak aynı durumdadır: ona ait gözlem yoktur. Bir tarama makineye onu
// gerçekten sorana dek "yok" değil "bilinmiyor" okunmalıdır.
func TestCatalogViewLeavesServicesOutsideTheScanUnknown(t *testing.T) {
	view := catalogView([]serviceObservation{
		{ID: "nginx", IsInstalled: true, Status: "active (running)"},
		{ID: "mariadb", IsInstalled: false, Status: "not_installed"},
	}, "apt")

	byID := map[string]ManagedServiceResponse{}
	for _, s := range view {
		byID[s.ID] = s
	}
	if got := byID["nginx"]; !got.Installed() || got.Status != "active (running)" {
		t.Errorf("observed installed nginx = (%v, %q)", got.IsInstalled, got.Status)
	}
	// Observed and genuinely absent: false, not null. The scan asked.
	// Gözlendi ve gerçekten yok: null değil false. Tarama sordu.
	mariadb := byID["mariadb"]
	if mariadb.IsInstalled == nil || *mariadb.IsInstalled {
		t.Errorf("observed-absent mariadb = %v, want a non-null false", mariadb.IsInstalled)
	}
	if mariadb.Status != "not_installed" {
		t.Errorf("observed-absent mariadb status = %q, want %q", mariadb.Status, "not_installed")
	}
	for _, s := range view {
		if s.ID == "nginx" || s.ID == "mariadb" {
			continue
		}
		if s.IsInstalled != nil || s.Status != "unknown" {
			t.Errorf("%s: outside the scan it answered (%v, %q), want (null, unknown)",
				s.ID, s.IsInstalled, s.Status)
		}
	}
}

// The component surface and the DNS engine surface ask different questions of
// the host, and they are allowed to answer differently for the same service at
// the same moment: the component scan reads the systemd unit table, the DNS
// engine surface reads the package database. That divergence is deliberate —
// merging them would widen the answer firewall policy consumes — so every row
// must SAY which question produced it rather than leave the operator to guess
// why two screens disagree about a masked BIND.
// Bileşen yüzeyi ile DNS motoru yüzeyi makineye farklı sorular sorar ve aynı
// servis için aynı anda farklı yanıt verebilirler. Bu ayrım bilerektir; bu
// yüzden her satır, yanıtını hangi sorunun ürettiğini SÖYLEMELİDİR.
func TestCatalogViewNamesWhichInstallQuestionItAnswered(t *testing.T) {
	byID := map[string]ManagedServiceResponse{}
	for _, s := range catalogView(nil, "apt") {
		byID[s.ID] = s
	}
	for id, want := range map[string]core.ServiceInstallEvidence{
		// BIND is decided from systemd units here; /api/v1/dns/engine decides
		// the same engine from dpkg. A sealed engine can read differently.
		"bind": core.EvidenceSystemdUnit,
		"pdns": core.EvidenceSystemdUnit,
		// A tool with no unit of ours is decided from its packages.
		"phpmyadmin": core.EvidencePackage,
		// Roundcube installs as a web application: neither unit nor package.
		"roundcube": core.EvidenceApplicationFiles,
		// A portable runtime with no package mapping cannot be asked at all.
		"node": core.EvidenceNone,
	} {
		if got := byID[id].InstalledEvidence; got != want {
			t.Errorf("%s: installed_evidence = %q, want %q", id, got, want)
		}
	}
	// The label is the agent's own branch selector, not a second opinion:
	// cmd/agent/service_state_rpc.go switches on this exact call.
	// Etiket, agent'ın kendi dal seçicisidir, ikinci bir görüş değil.
	for i := range core.ManagedServices {
		managed := &core.ManagedServices[i]
		if got, want := byID[managed.ID].InstalledEvidence,
			core.InstalledEvidenceFor(managed, "apt"); got != want {
			t.Errorf("%s: payload evidence %q disagrees with discovery %q", managed.ID, got, want)
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
	back := mustDecodeScanCache(t, string(data))
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

func TestDecodeScanCacheRejectsCorruptOrUnverifiedDocuments(t *testing.T) {
	for _, data := range []string{
		`{"observations":[`,
		`{"unexpected":[]}`,
		`{"observations":null}`,
		`[{"id":"nginx"`,
	} {
		if observations, err := decodeScanCache(data); err == nil {
			t.Fatalf("decodeScanCache(%q) = %+v without an error", data, observations)
		}
	}
}
