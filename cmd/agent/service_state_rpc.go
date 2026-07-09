package main

import "github.com/alicelik/celikpanel/internal/core"

// InstalledServiceIDs reports which catalogue services have their package
// present, independent of whether the unit is currently running. This closes
// the gap where a service is installed but not started — most visibly
// WireGuard, whose wg-quick@wg0 template unit only starts once a config
// exists, so a "running units" scan alone would keep calling it "not
// installed". firstPresentUnit already handles template units via
// list-unit-files, so we reuse it here.
//
// InstalledServiceIDs, hangi katalog servislerinin paketinin var olduğunu
// bildirir — unit'in şu an çalışıp çalışmadığından bağımsız. Bu, bir servis
// kurulu ama başlatılmamışken oluşan boşluğu kapatır — en görünür örnek
// WireGuard: wg-quick@wg0 şablon unit'i ancak bir config varken başlar, bu
// yüzden yalnız "çalışan unit" taraması onu sürekli "kurulu değil" sanardı.
func (a *Agent) InstalledServiceIDs(_ *struct{}, reply *[]string) error {
	var ids []string
	for i := range core.ManagedServices {
		svc := &core.ManagedServices[i]
		// serviceInstalled also covers daemonless tools (no unit, package
		// presence decides) — phpMyAdmin and friends.
		// serviceInstalled, daemon'suz araçları da kapsar (unit yok, paket
		// varlığı belirler) — phpMyAdmin ve benzerleri.
		if a.serviceInstalled(svc) {
			ids = append(ids, svc.ID)
		}
	}
	*reply = ids
	return nil
}
