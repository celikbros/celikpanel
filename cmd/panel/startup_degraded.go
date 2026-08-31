package main

import (
	"net/http"
	"sort"
	"time"
)

// Startup recovery repairs derived state. When one of those repairs fails, the
// subsystem it belongs to is recorded as degraded and the panel keeps starting:
// it binds, serves, and can say what is wrong. Exiting instead converts one
// subsystem's unrecognised state into a total outage, because systemd restarts
// the unit and the same state fails the same way on every boot.
//
// The rule this encodes: "I cannot reconcile this" and "the world is broken"
// are not the same branch, and neither of them is os.Exit(1).
//
// Açılış kurtarması türetilmiş durumu onarır. Bu onarımlardan biri başarısız
// olursa ait olduğu alt sistem "kısıtlı" olarak kaydedilir ve panel açılmaya
// devam eder: portu dinler, hizmet verir ve neyin bozuk olduğunu söyleyebilir.
// Bunun yerine çıkmak, tek bir alt sistemin tanınmayan durumunu tam kesintiye
// çevirir; çünkü systemd birimi yeniden başlatır ve aynı durum her açılışta
// aynı şekilde başarısız olur.
//
// Kodladığı kural: "bunu uzlaştıramıyorum" ile "dünya bozuldu" aynı dal
// değildir ve ikisi de os.Exit(1) değildir.

const (
	degradedSubsystemServiceOperations = "service-operations"
	degradedSubsystemAppInstalls       = "app-installs"
	degradedSubsystemCertificates      = "certificates"
	degradedSubsystemVPN               = "vpn"
	degradedSubsystemMailFilters       = "mail-filters"
	degradedSubsystemSealedSecrets     = "sealed-secrets"
)

// degradedSubsystem is what the operator is owed when a repair fails: which
// subsystem, a stable machine code, the underlying cause, and when it happened.
// degradedSubsystem, bir onarım başarısız olduğunda operatörün hakkı olan
// bilgidir: hangi alt sistem, kararlı bir makine kodu, altta yatan sebep ve ne
// zaman olduğu.
type degradedSubsystem struct {
	Subsystem string    `json:"subsystem"`
	Code      string    `json:"code"`
	Detail    string    `json:"detail"`
	At        time.Time `json:"at"`
}

// markSubsystemDegraded records a failed startup repair. It never blocks the
// caller and never terminates the process.
// markSubsystemDegraded başarısız bir açılış onarımını kaydeder. Çağıranı asla
// engellemez ve süreci asla sonlandırmaz.
func (p *Panel) markSubsystemDegraded(subsystem, code string, cause error) {
	if p == nil || subsystem == "" {
		return
	}
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	p.degradedMu.Lock()
	defer p.degradedMu.Unlock()
	if p.degraded == nil {
		p.degraded = make(map[string]degradedSubsystem)
	}
	// The first failure is the diagnosis; a later one is usually its echo.
	// İlk hata teşhistir; sonrakiler genelde onun yankısıdır.
	if _, seen := p.degraded[subsystem]; seen {
		return
	}
	p.degraded[subsystem] = degradedSubsystem{
		Subsystem: subsystem,
		Code:      code,
		Detail:    detail,
		At:        time.Now().UTC(),
	}
}

// clearSubsystemDegraded is how a subsystem recovers without a restart: a later
// successful reconcile retires the entry.
// clearSubsystemDegraded, bir alt sistemin yeniden başlatmadan toparlanma
// yoludur: sonraki başarılı uzlaştırma kaydı emekliye ayırır.
func (p *Panel) clearSubsystemDegraded(subsystem string) {
	if p == nil {
		return
	}
	p.degradedMu.Lock()
	defer p.degradedMu.Unlock()
	delete(p.degraded, subsystem)
}

func (p *Panel) subsystemDegraded(subsystem string) (degradedSubsystem, bool) {
	if p == nil {
		return degradedSubsystem{}, false
	}
	p.degradedMu.RLock()
	defer p.degradedMu.RUnlock()
	entry, ok := p.degraded[subsystem]
	return entry, ok
}

// degradedSubsystems returns every open entry, oldest first, for diagnostics.
// degradedSubsystems, teşhis için açık her kaydı en eskiden başlayarak döner.
func (p *Panel) degradedSubsystems() []degradedSubsystem {
	if p == nil {
		return nil
	}
	p.degradedMu.RLock()
	defer p.degradedMu.RUnlock()
	if len(p.degraded) == 0 {
		return nil
	}
	entries := make([]degradedSubsystem, 0, len(p.degraded))
	for _, entry := range p.degraded {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].At.Equal(entries[j].At) {
			return entries[i].Subsystem < entries[j].Subsystem
		}
		return entries[i].At.Before(entries[j].At)
	})
	return entries
}

// requireSubsystemOperational refuses a mutation whose subsystem failed its
// startup repair, with the stored code and cause so the screen can say exactly
// what is wrong instead of spinning. Reads are deliberately left alone: a
// degraded subsystem still shows the operator what it believes.
//
// requireSubsystemOperational, açılış onarımı başarısız olmuş bir alt sisteme
// ait değişikliği reddeder; saklanan kod ve sebeple birlikte, böylece ekran
// dönüp durmak yerine tam olarak neyin bozuk olduğunu söyleyebilir. Okumalara
// bilerek dokunulmaz: kısıtlı bir alt sistem de operatöre ne bildiğini gösterir.
func (p *Panel) requireSubsystemOperational(
	w http.ResponseWriter,
	subsystem string,
) bool {
	entry, degraded := p.subsystemDegraded(subsystem)
	if !degraded {
		return true
	}
	// The cause stays server-side. httperr.go's contract is explicit — handlers
	// never hand internal error text to the client — and a startup recovery
	// failure quotes agent state, filesystem paths and request identities. The
	// client gets the subsystem and a stable code; the operator reads the cause
	// from the DEGRADED journal line the same failure already wrote.
	// Sebep sunucu tarafında kalır. httperr.go'nun sözleşmesi açık: işleyiciler
	// istemciye asla iç hata metni vermez — ve bir açılış kurtarma hatası agent
	// durumunu, dosya yollarını ve istek kimliklerini alıntılar. İstemci alt
	// sistemi ve kararlı bir kod alır; operatör sebebi, aynı hatanın zaten
	// yazdığı DEGRADED günlük satırından okur.
	writeCodedErrorDetails(
		w,
		http.StatusConflict,
		errCodeSubsystemDegraded,
		"this subsystem did not finish its startup recovery, so changes are held",
		"/settings",
		[]string{entry.Subsystem, entry.Code},
	)
	return false
}
