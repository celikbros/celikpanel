package main

import "strings"

// One rule, in one place, next to the ledger it protects.
//
// A privileged host mutation - the DNS engine switch, mail TLS, the firewall,
// the VPN peer set - runs under the service mutation ledger, and the ledger
// hands the whole host to one plan at a time. When such a plan cannot be
// completed, exactly one question decides whether the host is handed back:
// what state did the plan leave the machine in? A plan that is provably where
// it found the host may be finished as failed and the lease released; a plan
// that changed something it cannot prove it put back must keep the lease, so
// startup recovery gets the next word rather than a half-changed machine being
// declared free.
//
// That question was answered three times in three commit paths - R-046 for
// mail, R-054 for the firewall, R-055 for the VPN - and each answer was
// written out again next to the path that asked it. R-055 asked for the answer
// to be lifted out, so the rule exists once and a fifth path inherits it
// instead of rediscovering it. The paths still answer "what state is the host
// in": that evidence is theirs, and it is different in each of them. What
// lives here is only what the answer means for the ledger.
//
// Tek kural, tek yerde, korudugu defterin yaninda.
//
// Ayricalikli bir makine mutasyonu - DNS motoru degisimi, posta TLS'i, guvenlik
// duvari, VPN peer kumesi - servis mutasyon defteri altinda calisir ve defter
// makinenin tamamini bir seferde tek bir plana verir. Tamamlanamayan bir plan
// icin tek bir soru, makinenin geri verilip verilmeyecegine karar verir: plan
// makineyi hangi durumda birakti? Kanitli bicimde buldugu yerde birakan bir
// plan basarisiz olarak bitirilebilir ve kira serbest birakilabilir; geri
// koydugunu kanitlayamadigi bir sey degistiren bir plan kirayi tutmak
// zorundadir.
//
// Bu soru uc ayri taahhut yolunda uc kez yanitlandi - posta icin R-046,
// guvenlik duvari icin R-054, VPN icin R-055 - ve her yanit soruyu soran yolun
// yanina yeniden yazildi. R-055 yanitin disari cikarilmasini istedi; boylece
// kural bir kez var olur. Yollar hala "makine hangi durumda" sorusunu
// yanitlar: o kanit onlarindir. Burada yasayan yalnizca yanitin defter icin ne
// anlama geldigidir.

// hostMutationOutcome is what a reconciliation left behind on the host.
// hostMutationOutcome, bir uzlastirmanin makinede biraktigi durumdur.
type hostMutationOutcome int

const (
	// hostMutationUntouched: the failure happened before any host change.
	// hostMutationUntouched: basarisizlik, makinede hicbir sey degismeden
	// once oldu.
	hostMutationUntouched hostMutationOutcome = iota
	// hostMutationRestored: the host was changed and everything this attempt
	// changed was put back, and the restoration was proved - not assumed.
	// What counts as proof belongs to the path: the firewall reads the running
	// kernel's module tree, mail reads back its restored files and reloads
	// both daemons, the VPN reads back the durable configuration it renamed
	// and re-synchronises the live interface it touched.
	// hostMutationRestored: makine degistirildi ve bu denemenin degistirdigi
	// her sey geri konuldu; geri koyma varsayilmadi, kanitlandi.
	hostMutationRestored
	// hostMutationConverged: the committed plan is applied and verified. This
	// is a success, and it is in this list because a classifier that cannot
	// name success cannot refuse it a clean failure either.
	// hostMutationConverged: taahhut edilmis plan uygulandi ve dogrulandi.
	hostMutationConverged
	// hostMutationAmbiguous: the host was changed and could not be proved put
	// back, or a change may be half applied. Everything that is not one of the
	// three above is this one, including anything that could not be classified
	// at all.
	// hostMutationAmbiguous: makine degistirildi ve geri konuldugu
	// kanitlanamadi. Yukaridaki uc durumdan biri olmayan her sey budur.
	hostMutationAmbiguous
)

// mayEndPlan is the rule. Only a plan that is provably where it found the host
// may be finished as failed and release the ledger; anything else - including
// an outcome nobody could classify - poisons and holds the lock. It is
// deliberately written as an allow-list of two, so an outcome added above is
// refused by default rather than accidentally granted the host.
//
// mayEndPlan kuraldir. Yalnizca kanitli bicimde makineyi buldugu yerde birakan
// bir plan basarisiz olarak bitirilip defteri serbest birakabilir; digerleri -
// hic siniflandirilamayan bir sonuc dahil - defteri zehirler ve kilidi tutar.
// Bilerek iki elemanli bir izin listesi olarak yazilmistir.
func (outcome hostMutationOutcome) mayEndPlan() bool {
	return outcome == hostMutationUntouched || outcome == hostMutationRestored
}

// A failure reason is recorded durably in the ledger and shown to an operator,
// so it is bounded. What may be lost is the tail of a command's diagnostic,
// never the sentence the operator has to act on - which is why the reason is
// the last thing every message below carries, and why a tool's own words go
// behind the instruction in operatorFirstFailureSentence.
//
// Bir basarisizlik nedeni deftere kalici yazilir ve operatore gosterilir, bu
// yuzden sinirlidir. Kaybolabilecek olan bir komutun teshis metninin
// kuyrugudur; operatorun uygulamasi gereken cumle degil.
const hostMutationFailureReasonLimit = 400

func boundedHostMutationReason(cause error) string {
	reason := ""
	if cause != nil {
		reason = strings.TrimSpace(cause.Error())
	}
	if reason == "" {
		return "unknown"
	}
	if len(reason) > hostMutationFailureReasonLimit {
		return reason[:hostMutationFailureReasonLimit] + "..."
	}
	return reason
}

// hostMutationFailureVoice is one path's words for the two failures it is
// allowed to end on. The words are the path's - only the path knows what it
// changed, what it did not undo, and what an interrupted predecessor may have
// left behind - but the shape is shared, and so is the order: what happened
// first, what was not undone next, and the technical reason last.
//
// hostMutationFailureVoice, bir yolun bitebilecegi iki basarisizlik icin kendi
// sozleridir. Sozler yolun kendisinindir; bicim ve sira paylasilir: once ne
// oldugu, sonra nelerin geri alinmadigi, en son teknik neden.
type hostMutationFailureVoice struct {
	// The ledger codes for the two endings.
	untouchedCode string
	restoredCode  string
	// The first sentence of each ending, in the product's plain voice. Each is
	// a complete sentence; the shared tail follows after a single space.
	untouchedLead string
	restoredLead  string
	// What a clean failure does NOT undo, said out loud rather than left for
	// the operator to guess.
	residue string
	// And the one thing a recovery cannot know: an earlier attempt that was
	// killed never got to put its own work back. Only startup recovery says
	// this, because only startup recovery may be speaking for a predecessor.
	interrupted string
}

// cleanFailureText names a terminal failure, or refuses to. afterRestart says
// whether startup recovery is speaking, which is the only case that has to
// warn about an interrupted predecessor it could not undo.
//
// cleanFailureText kalici bir basarisizligi adlandirir ya da adlandirmayi
// reddeder. afterRestart, baslangic kurtarmasinin konustugu durumu ayirir.
func (voice hostMutationFailureVoice) cleanFailureText(
	outcome hostMutationOutcome,
	cause error,
	afterRestart bool,
) (code string, message string, clean bool) {
	if !outcome.mayEndPlan() {
		return "", "", false
	}
	tail := ""
	if afterRestart {
		tail = voice.interrupted + " "
	}
	tail += voice.residue + " Reason: " + boundedHostMutationReason(cause)
	if outcome == hostMutationUntouched {
		return voice.untouchedCode, voice.untouchedLead + " " + tail, true
	}
	return voice.restoredCode, voice.restoredLead + " " + tail, true
}

// operatorFirstFailureSentence puts the instruction the operator has to act on
// in front, and the command's own words behind it in brackets. The order is
// the lesson, not the punctuation: this string is carried as a failure reason
// that is bounded before it is recorded, and a tool's diagnostic is long
// enough to push everything after it past the limit. The first live R-054 run
// truncated exactly the sentence that mattered, which is how the rule got
// written down; R-055 needed it a second time, which is why it is here.
//
// operatorFirstFailureSentence, operatorun uygulamasi gereken talimati one,
// komutun kendi sozlerini parantez icinde arkaya koyar. Ders noktalama degil
// siradir: bu dize kaydedilmeden once sinirlanir ve bir aracin teshis metni
// ardindaki her seyi sinirin disina itecek kadar uzundur.
func operatorFirstFailureSentence(instruction, prefix, detail string) string {
	if detail == "" {
		detail = "unknown"
	}
	if instruction == "" {
		return prefix + ": " + detail
	}
	return instruction + " (" + prefix + ": " + detail + ")"
}
