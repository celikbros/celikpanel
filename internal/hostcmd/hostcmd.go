// Package hostcmd is where a privileged command's failure is read.
//
// The same defect was found on three real machines in one week. A privileged
// command fails, the call that ran it throws away what the command said, and
// every layer above sees "exit status 1". R-046 could say only that a group of
// files did not match, not which. R-054 logged "nft table discovery failed:
// exit status 1" while nft's stderr - the one sentence that named the fault -
// sat unread inside *exec.ExitError. R-053 discarded the mysql client's
// "Access denied for user 'root'@'localhost' (using password: NO)" and the
// panel answered a bare 500. In every case the reason existed and nobody read
// it where it existed.
//
// What is shared between those three is NOT the runner. This tree launches
// privileged commands through several launchers and each exists for a reason
// that is not shared: cmd/agent's serviceMutationCommand registers the child
// against the durable service mutation ledger so a lost lease kills it;
// internal/services' runNginxCommand and cmd/agent's runMailTLSCommand give a
// dispatched net/rpc handler an agent-owned deadline; MariaDBDriver's
// mysqlCommand writes a 0600 defaults-extra-file so a password never appears
// in a process argument. Merging those into one runner would either drag the
// mutation ledger into internal/, which cannot import package main, or strip
// it out of the agent. So the shared piece is smaller than a runner: it is the
// failure value - reading the output where it exists, keeping it with the
// error, and deciding what may be repeated.
//
// Paket hostcmd, ayricalikli bir komutun basarisizliginin okundugu yerdir.
//
// Ayni kusur bir hafta icinde uc gercek makinede bulundu: komut basarisiz
// olur, onu calistiran cagri komutun soyledigini atar ve ustundeki her katman
// "exit status 1" gorur. Uc olayda da neden vardi ve var oldugu yerde kimse
// okumadi. Paylasilan sey calistirici DEGILDIR - bu agacta her calistirici
// paylasilmayan bir nedenle vardir - paylasilan sey basarisizlik degeridir:
// ciktiyi var oldugu yerde okumak, hatayla birlikte tasimak ve neyin
// tekrarlanabilecegine karar vermek.
package hostcmd

import (
	"errors"
	"os/exec"
	"strings"
)

// Stderr recovers what a failed command wrote to its standard error when the
// caller used Output() rather than CombinedOutput(). os/exec keeps it inside
// *exec.ExitError and nothing above ever looks, which is precisely where R-054
// lost nft's only sentence.
//
// This is the one place in the tree that reads that field. The guard in this
// package proves it, so a second reader cannot appear somewhere else and then
// quietly stop being maintained.
//
// Stderr, cagiran taraf CombinedOutput() yerine Output() kullandiginda
// basarisiz bir komutun standart hataya yazdigini geri kazanir. os/exec bunu
// *exec.ExitError icinde tutar ve ustteki hicbir sey bakmaz.
func Stderr(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return strings.TrimSpace(string(exitErr.Stderr))
	}
	return ""
}

// flatten strips control characters by rebuilding the string from its fields.
// A tool points at an offending line with its own newlines and carets; that is
// a diagnostic, and it ends up inside a JSON error a browser renders.
//
// flatten, dizeyi alanlarindan yeniden kurarak kontrol karakterlerini atar.
func flatten(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// Diagnostic collects EVERYTHING a failed command said, on one line: its
// output, the stderr Output() hid, and the exit error itself. Nothing is
// dropped, because at the point of failure nobody yet knows which of the three
// carries the sentence that matters - R-054 found it in the second.
//
// Diagnostic, basarisiz bir komutun soyledigi HER SEYI tek satirda toplar.
func Diagnostic(out []byte, err error) string {
	var parts []string
	if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := Stderr(err); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if err != nil {
		if trimmed := strings.TrimSpace(err.Error()); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return flatten(strings.Join(parts, "; "))
}

// Reason is the shorter reading: the best single answer to "why", and nothing
// after it. It differs from Diagnostic deliberately. Diagnostic serves a
// message an operator reads once, where a redundant "exit status 1" costs
// nothing and a missed sentence costs everything; Reason serves the many
// places that already print a prefix of their own and would otherwise repeat
// the exit status on every line. Both obey the same rule - the exit error is
// the LAST resort, never the first - and that rule is what the three fixes
// were about.
//
// Reason daha kisa okumadir: "neden" sorusunun en iyi tek yaniti. Diagnostic
// ile farki bilerektir; ikisi de ayni kurala uyar - cikis hatasi ilk degil SON
// caredir.
func Reason(out []byte, err error) string {
	if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
		return trimmed
	}
	if trimmed := Stderr(err); trimmed != "" {
		return trimmed
	}
	if err != nil {
		if trimmed := strings.TrimSpace(err.Error()); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Verbatim is the raw-forwarding path for a message an operator will read: it
// returns the command's own words unchanged, and it exists so the decision to
// repeat them is written where it is made rather than inferred later from the
// absence of a decision. because is not a comment - it is an argument, it is
// greppable, and the guard in this package checks that every call site passes
// a real one.
//
// An empty reason fails closed and returns nothing. A caller that forgot to
// justify itself loses the detail; it never leaks it.
//
// Note the asymmetry, which is the whole point: Fail is safe and says nothing
// about secrets, while THIS is the function whose name and argument list make
// a reviewer stop. Safety is not opt-in.
//
// Verbatim, operatorun okuyacagi bir mesaj icin ham iletim yoludur: komutun
// kendi sozlerini degistirmeden dondurur ve sozleri tekrarlama karari
// verildigi yerde yazilsin diye vardir. because bir yorum degil, bir
// argumandir. Bos bir gerekce kapali duser ve hicbir sey dondurmez.
func Verbatim(out []byte, err error, because string) string {
	if strings.TrimSpace(because) == "" {
		return ""
	}
	return Diagnostic(out, err)
}

// OperatorFirst puts the instruction the operator has to act on in front, and
// the command's own words behind it in brackets. The order is the lesson, not
// the punctuation: this string is carried as a failure reason that is bounded
// before it is recorded, and a tool's diagnostic is long enough to push
// everything after it past the limit. The first live R-054 run truncated
// exactly the sentence that mattered, which is how the rule got written down;
// R-055 needed it a second time.
//
// OperatorFirst, operatorun uygulamasi gereken talimati one, komutun kendi
// sozlerini parantez icinde arkaya koyar. Ders noktalama degil siradir.
func OperatorFirst(instruction, prefix, detail string) string {
	if detail == "" {
		detail = "unknown"
	}
	if instruction == "" {
		return prefix + ": " + detail
	}
	return instruction + " (" + prefix + ": " + detail + ")"
}

// Bounded keeps a reason short enough to record durably and to show, and
// refuses to trail off into nothing. What may be lost is the tail of a
// command's diagnostic, never the instruction - which is why OperatorFirst
// exists and why it is applied before this is.
//
// Bounded, bir nedeni kalici yazilacak ve gosterilecek kadar kisa tutar ve
// hicbir seye donusmeyi reddeder.
func Bounded(reason string, limit int) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "unknown"
	}
	if limit > 0 && len(reason) > limit {
		return reason[:limit] + "..."
	}
	return reason
}

// Classifier reads a command's own words once, at the only place that has
// them, and returns a short developer-authored label for what they meant. It
// returns the empty string when it recognises nothing, which is an honest
// answer and never a licence to repeat the text.
//
// Classifier, bir komutun kendi sozlerini yalnizca onlara sahip olan yerde bir
// kez okur ve ne anlama geldikleri icin gelistiricinin yazdigi kisa bir etiket
// dondurur.
type Classifier func(text string) string

// withheldSuffix is what an operator is told when the output was read and
// could not be named. It is deliberately not silence: a reason that was
// weighed and withheld is a different thing from a reason that was thrown
// away, and the difference is the whole subject of this package.
//
// withheldSuffix, cikti okunup adlandirilamadiginda operatore soylenendir.
// Bilerek sessizlik degildir.
const withheldSuffix = " (the reason was read here and is not repeated: " +
	"this command's output can carry a secret)"

// Classified is the safe reading for a message an operator will read, and it
// is what a caller gets by saying nothing special. The output is read once,
// here, where it exists - and only what the classifier made of it travels.
//
// This is the discipline R-053 established and it must not be lost: a client
// diagnostic can echo the statement it failed on, and a statement can carry a
// password.
//
// Classified, operatorun okuyacagi bir mesaj icin guvenli okumadir ve ozel bir
// sey soylemeyen cagiranin aldigi seydir. Cikti burada, var oldugu yerde bir
// kez okunur ve yalnizca siniflandiricinin ondan cikardigi sey tasinir.
func Classified(out []byte, err error, classify Classifier) string {
	if classify != nil {
		if class := strings.TrimSpace(classify(Diagnostic(out, err))); class != "" {
			return class
		}
	}
	return causeText(err) + withheldSuffix
}

func causeText(cause error) string {
	if cause == nil {
		return "unknown"
	}
	if trimmed := flatten(strings.TrimSpace(cause.Error())); trimmed != "" {
		return trimmed
	}
	return "unknown"
}

// Failure is a privileged command's failure that did not discard its reason.
// Failure, nedenini atmamis ayricalikli bir komut basarisizligidir.
type Failure struct {
	prefix string
	// class is the classifier's developer-authored label, empty when nothing
	// was recognised. It is always safe to show and always safe to compare.
	// class, siniflandiricinin gelistirici tarafindan yazilmis etiketidir.
	class string
	// detail is what this failure is allowed to say.
	// detail, bu basarisizligin soyleyebilecegi seydir.
	detail string
	// because is the reason a call site gave for repeating the command's own
	// words, kept next to the value it justifies rather than in a review
	// comment nobody will find again. It is empty on the classified path.
	// because, cagri yerinin ham sozleri tekrarlamak icin verdigi gerekcedir.
	because  string
	verbatim bool
	cause    error
}

func (f *Failure) Error() string { return f.prefix + ": " + f.detail }

func (f *Failure) Unwrap() error { return f.cause }

// Class reports the label a classifier gave this failure, if any.
// Class, bir siniflandiricinin bu basarisizliga verdigi etiketi bildirir.
func (f *Failure) Class() string { return f.class }

// Disclosed reports whether this failure carries the command's own words, and
// the reason the call site gave for that.
// Disclosed, bu basarisizligin komutun kendi sozlerini tasiyip tasimadigini ve
// cagri yerinin bunun icin verdigi gerekceyi bildirir.
func (f *Failure) Disclosed() (bool, string) { return f.verbatim, f.because }

// Fail is how a privileged command's failure becomes an error, and it is the
// safe form. Note which way round the defaults are: a caller that says nothing
// gets the classified path, and a caller that wants the raw text has to ask
// for it by name and write down why, in FailVerbatim.
//
// Fail, ayricalikli bir komut basarisizliginin nasil hataya donustugudur ve
// guvenli bicimdir.
func Fail(prefix string, out []byte, err error, classify Classifier) error {
	if err == nil {
		return nil
	}
	class := ""
	if classify != nil {
		class = strings.TrimSpace(classify(Diagnostic(out, err)))
	}
	return &Failure{
		prefix: prefix,
		class:  class,
		detail: Classified(out, err, classify),
		cause:  err,
	}
}

// FailVerbatim carries the command's own words out to the operator. It is the
// path that must be asked for, and an empty reason fails closed onto the
// classified path with no classifier - the words are dropped. A call site that
// forgot to justify itself is quiet, never leaky.
//
// FailVerbatim, komutun kendi sozlerini operatore tasir. Istenmesi gereken yol
// budur; bos bir gerekce kapali duser ve sozler atilir.
func FailVerbatim(prefix string, out []byte, err error, because string) error {
	if err == nil {
		return nil
	}
	detail := Verbatim(out, err, because)
	if detail == "" {
		return Fail(prefix, out, err, nil)
	}
	return &Failure{
		prefix:   prefix,
		detail:   detail,
		because:  strings.TrimSpace(because),
		verbatim: true,
		cause:    err,
	}
}

// ClassOf reports the classification an error carries, through any number of
// wrappings a caller added on the way out.
// ClassOf, bir hatanin tasidigi siniflandirmayi bildirir.
func ClassOf(err error) string {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.class
	}
	return ""
}
