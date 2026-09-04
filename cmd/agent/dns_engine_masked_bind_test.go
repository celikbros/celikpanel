package main

import "testing"

// R-019, second cause. The product masks named.service and bind9.service so a
// package manager cannot start BIND behind its back, then its own
// PowerDNS-active proof refused to accept a masked BIND as stopped: a masked
// unit is loadState "masked", which is neither "not-found" nor
// "loaded"+"disabled". A PowerDNS-to-BIND switch on any host where BIND had
// been installed could not establish its own source.
//
// R-019'un ikinci sebebi. Ürün, bir paket yöneticisi arkasından BIND'ı
// başlatmasın diye named.service ve bind9.service birimlerini maskeler; sonra
// kendi PowerDNS-etkin kanıtı maskeli BIND'ı durdurulmuş saymayı reddediyordu:
// maskeli birim loadState "masked" bildirir, ki bu ne "not-found" ne de
// "loaded"+"disabled"dır. BIND'ın kurulmuş olduğu her sunucuda PowerDNS'ten
// BIND'a geçiş kendi kaynağını kuramıyordu.
func TestPDNSRuntimeProofAcceptsTheMaskedBINDItCreated(t *testing.T) {
	pdns := bindInstallUnitState{
		loadState: "loaded", activeState: "active", unitFileState: "enabled",
	}
	stopped := map[string]bindInstallUnitState{
		"absent": {loadState: "not-found", activeState: "inactive", unitFileState: ""},
		"loaded disabled": {
			loadState: "loaded", activeState: "inactive", unitFileState: "disabled",
		},
		"masked": {
			loadState: "masked", activeState: "inactive", unitFileState: "masked",
		},
		"masked at runtime": {
			loadState: "masked", activeState: "inactive", unitFileState: "masked-runtime",
		},
	}
	for name, state := range stopped {
		t.Run(name, func(t *testing.T) {
			if err := verifyExactPDNSRuntimeUnitStates(state, state, pdns); err != nil {
				t.Fatalf("a %s BIND must count as stopped: %v", name, err)
			}
		})
	}
}

// The relaxation is exactly "masked and inactive". A BIND that is running, or
// whose state is unknown, must still be refused — the proof exists to
// establish that PowerDNS alone is serving.
// Gevşetme tam olarak "maskeli ve etkin değil"dir. Çalışan ya da durumu
// bilinmeyen bir BIND yine reddedilmelidir — kanıt, yalnız PowerDNS'in hizmet
// verdiğini saptamak için vardır.
func TestPDNSRuntimeProofStillRefusesAnyLiveOrUnknownBIND(t *testing.T) {
	pdns := bindInstallUnitState{
		loadState: "loaded", activeState: "active", unitFileState: "enabled",
	}
	refused := map[string]bindInstallUnitState{
		"masked but active": {
			loadState: "masked", activeState: "active", unitFileState: "masked",
		},
		"masked but activating": {
			loadState: "masked", activeState: "activating", unitFileState: "masked",
		},
		"loaded and active": {
			loadState: "loaded", activeState: "active", unitFileState: "enabled",
		},
		"loaded enabled inactive": {
			loadState: "loaded", activeState: "inactive", unitFileState: "enabled",
		},
		"absent but active": {
			loadState: "not-found", activeState: "active", unitFileState: "",
		},
		"empty": {},
	}
	for name, state := range refused {
		t.Run(name, func(t *testing.T) {
			if err := verifyExactPDNSRuntimeUnitStates(state, state, pdns); err == nil {
				t.Fatalf("a %s BIND must be refused", name)
			}
		})
	}
}

// PowerDNS itself is unchanged: the proof still demands it be exactly loaded,
// active and enabled.
// PowerDNS tarafı değişmedi: kanıt onun tam olarak yüklü, etkin ve
// etkinleştirilmiş olmasını istemeye devam ediyor.
func TestPDNSRuntimeProofStillDemandsAnExactlyActivePowerDNS(t *testing.T) {
	maskedBIND := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	for name, pdns := range map[string]bindInstallUnitState{
		"inactive": {loadState: "loaded", activeState: "inactive", unitFileState: "enabled"},
		"disabled": {loadState: "loaded", activeState: "active", unitFileState: "disabled"},
		"masked":   {loadState: "masked", activeState: "inactive", unitFileState: "masked"},
		"absent":   {loadState: "not-found", activeState: "inactive", unitFileState: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifyExactPDNSRuntimeUnitStates(maskedBIND, maskedBIND, pdns); err == nil {
				t.Fatalf("a %s PowerDNS must be refused", name)
			}
		})
	}
}
