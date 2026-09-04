package main

import "testing"

// S-7 Boston negative, register R-028: the mirror image of R-019's second
// cause. A BIND-to-PowerDNS switch installs PowerDNS under a persistent mask
// so the package manager cannot start it early; the proof of the active BIND
// source then ran after that install and refused the masked PowerDNS as "not
// exactly absent or loaded, inactive, and disabled". Every switch that had to
// install PowerDNS therefore failed at epoch 2 with the product's own mask as
// the reason.
//
// S-7 Boston negatifi, defter R-028: R-019'un ikinci sebebinin ayna görüntüsü.
// BIND'dan PowerDNS'e geçiş, paket yöneticisi erken başlatmasın diye PowerDNS'i
// kalıcı bir maske altında kurar; etkin BIND kaynağının kanıtı o kurulumdan
// sonra koştu ve maskeli PowerDNS'i "tam olarak yok ya da yüklü, etkin değil ve
// devre dışı değil" diye reddetti. PowerDNS kurması gereken her geçiş böylece
// epoch 2'de ürünün kendi maskesi yüzünden düştü.
func TestActiveBINDProofAcceptsTheMaskedPowerDNSItsInstallGuardCreated(t *testing.T) {
	active := bindInstallUnitState{
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
	for name, pdns := range stopped {
		t.Run("apt "+name, func(t *testing.T) {
			err := verifyExactActiveBINDUnitStates(testDebian13BINDProfile(), active, active, pdns)
			if err != nil {
				t.Fatalf("a %s PowerDNS must count as stopped beside an active BIND: %v", name, err)
			}
		})
		t.Run("pacman "+name, func(t *testing.T) {
			absentAlias := bindInstallUnitState{
				loadState: "not-found", activeState: "inactive", unitFileState: "",
			}
			err := verifyExactActiveBINDUnitStates(testPacmanBINDProfile(), active, absentAlias, pdns)
			if err != nil {
				t.Fatalf("a %s PowerDNS must count as stopped beside an active BIND: %v", name, err)
			}
		})
	}
}

// The relaxation is exactly "masked and inactive". A PowerDNS that is running,
// activating, merely disabled-but-enabled, or of unknown state must still be
// refused: the proof exists to establish that BIND alone is serving.
// Gevşetme tam olarak "maskeli ve etkin değil"dir. Çalışan, başlatılmakta olan,
// yalnız etkinleştirilmiş ya da durumu bilinmeyen bir PowerDNS yine
// reddedilmelidir: kanıt, yalnız BIND'ın hizmet verdiğini saptamak için vardır.
func TestActiveBINDProofStillRefusesAnyLiveOrUnknownPowerDNS(t *testing.T) {
	active := bindInstallUnitState{
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
	for name, pdns := range refused {
		t.Run(name, func(t *testing.T) {
			err := verifyExactActiveBINDUnitStates(testDebian13BINDProfile(), active, active, pdns)
			if err == nil {
				t.Fatalf("a %s PowerDNS must be refused beside an active BIND", name)
			}
		})
	}
}

// BIND itself is unchanged: the proof still demands named.service be exactly
// loaded, active and enabled, with the APT alias active or the pacman alias
// absent.
// BIND tarafı değişmedi: kanıt named.service'in tam olarak yüklü, etkin ve
// etkinleştirilmiş olmasını, APT takma adının etkin ya da pacman takma adının
// yok olmasını istemeye devam ediyor.
func TestActiveBINDProofStillDemandsAnExactlyActiveBIND(t *testing.T) {
	maskedPDNS := bindInstallUnitState{
		loadState: "masked", activeState: "inactive", unitFileState: "masked",
	}
	active := bindInstallUnitState{
		loadState: "loaded", activeState: "active", unitFileState: "enabled",
	}
	for name, named := range map[string]bindInstallUnitState{
		"inactive": {loadState: "loaded", activeState: "inactive", unitFileState: "enabled"},
		"disabled": {loadState: "loaded", activeState: "active", unitFileState: "disabled"},
		"masked":   {loadState: "masked", activeState: "inactive", unitFileState: "masked"},
		"absent":   {loadState: "not-found", activeState: "inactive", unitFileState: ""},
	} {
		t.Run("named "+name, func(t *testing.T) {
			err := verifyExactActiveBINDUnitStates(testDebian13BINDProfile(), named, active, maskedPDNS)
			if err == nil {
				t.Fatalf("a %s named.service must be refused", name)
			}
		})
	}
	t.Run("apt alias inactive", func(t *testing.T) {
		alias := bindInstallUnitState{loadState: "loaded", activeState: "inactive", unitFileState: "enabled"}
		if err := verifyExactActiveBINDUnitStates(testDebian13BINDProfile(), active, alias, maskedPDNS); err == nil {
			t.Fatal("an inactive APT alias must be refused")
		}
	})
	t.Run("pacman alias present", func(t *testing.T) {
		if err := verifyExactActiveBINDUnitStates(testPacmanBINDProfile(), active, active, maskedPDNS); err == nil {
			t.Fatal("a present bind9.service on pacman must be refused")
		}
	})
}
