package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// R-054. `nft` is a userspace client for a kernel subsystem. When the kernel
// side cannot be reached the client fails with an exit status and a sentence
// on stderr, and every layer above it turned that into "nft table discovery
// failed: exit status 1" or an opaque 500. The most ordinary way to reach that
// state is an ordinary install: a prerequisite step that upgrades packages can
// replace the running kernel and delete the module tree the running kernel
// needs, after which the host can load no kernel module at all until it is
// restarted. This file exists to name that, exactly, so the operator is told
// to restart the server instead of being handed an exit code.
//
// R-054. `nft`, bir cekirdek altsisteminin kullanici alani istemcisidir.
// Cekirdek tarafina ulasilamadiginda istemci bir cikis koduyla basarisiz olur
// ve ustundeki her katman bunu kapali bir 500'e cevirirdi. Bu dosya nedeni tam
// olarak adlandirmak icin vardir: operatore bir cikis kodu degil, sunucuyu
// yeniden baslatmasi gerektigi soylenmelidir.

// Fixed host paths. They are variables only so tests can point the probe at a
// fixture tree; nothing outside this process can choose them.
// Sabit makine yollari. Yalnizca testler bir fikstur agacini gosterebilsin
// diye degiskendirler.
var (
	firewallKernelReleasePath = "/proc/sys/kernel/osrelease"
	firewallKernelModulesRoot = "/lib/modules"
)

// firewallEngineFault says why an nft invocation could not do its work.
// firewallEngineFault, bir nft cagrisinin isini neden yapamadigini soyler.
type firewallEngineFault int

const (
	// firewallEngineFaultNone: nft failed for a reason of its own - a rule it
	// would not accept, a table that is not there. Nothing here explains it.
	// firewallEngineFaultNone: nft kendi nedeniyle basarisiz oldu.
	firewallEngineFaultNone firewallEngineFault = iota
	// firewallEngineFaultKernelUnreachable: nft said it could not reach the
	// kernel's netfilter subsystem. Its own words are the only evidence, so
	// this names a reason but never on its own proves what was written.
	// firewallEngineFaultKernelUnreachable: nft cekirdege ulasamadigini
	// soyledi. Kanit yalnizca kendi sozudur.
	firewallEngineFaultKernelUnreachable
	// firewallEngineFaultModulesMissing: this host can load NO kernel module,
	// because the running kernel's module tree is not on disk while other
	// kernels' trees are. That is a structural fact about the machine, not a
	// reading of a message, and it holds until the machine is restarted.
	// firewallEngineFaultModulesMissing: bu makine HICBIR cekirdek modulu
	// yukleyemez; calisan cekirdegin modul agaci diskte degildir.
	firewallEngineFaultModulesMissing
)

// The two sentences an operator may receive. They name the machine's problem
// and the one action that fixes it, in the product's own plain voice.
// Operatorun alabilecegi iki cumle.
const (
	firewallEngineRebootSentence = "This server is running a kernel whose modules are no longer " +
		"on disk, so nftables cannot be loaded: a package upgrade replaced the kernel and this " +
		"server has not been restarted since. Restart this server, then turn the firewall on again."
	firewallEngineKernelSentence = "The nftables engine could not reach the kernel, so no firewall " +
		"rule was written. A kernel that was replaced without a restart is the usual cause; " +
		"restart this server and try again."
)

func firewallEngineFaultSentence(fault firewallEngineFault) string {
	switch fault {
	case firewallEngineFaultModulesMissing:
		return firewallEngineRebootSentence
	case firewallEngineFaultKernelUnreachable:
		return firewallEngineKernelSentence
	default:
		return ""
	}
}

// The wordings nft and libnftnl use when they cannot talk to the kernel. They
// are matched only to name a reason, never to decide that a host may be
// released: that decision rests on the structural module-tree proof below.
// nft'nin cekirdekle konusamadiginda kullandigi ifadeler. Yalnizca nedeni
// adlandirmak icin eslesirler.
var firewallKernelUnreachableSignatures = []string{
	"cache initialization failed",
	"could not process rule: no such file or directory",
	"could not process rule: operation not supported",
	"could not process rule: protocol not supported",
	"netlink: error: no such file or directory",
	"unable to initialize netlink socket",
	"protocol not supported",
	"module is not loaded",
}

// firewallCommandDiagnostic collects everything a failed command said. A
// non-combined Output() call keeps stderr inside the exit error, which is
// exactly where nft writes the sentence that explains itself.
// firewallCommandDiagnostic, basarisiz bir komutun soyledigi her seyi toplar.
func firewallCommandDiagnostic(out []byte, err error) string {
	var parts []string
	if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
		parts = append(parts, trimmed)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if trimmed := strings.TrimSpace(string(exitErr.Stderr)); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if err != nil {
		if trimmed := strings.TrimSpace(err.Error()); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	// nft points at the offending line with its own newlines and carets. That
	// is a diagnostic, and it ends up inside a JSON error the browser shows, so
	// it is flattened to one line here rather than pasted into a dialog.
	// nft, hatali satiri kendi satir sonlari ve isaretleriyle gosterir; bu bir
	// teshis metnidir ve tarayicinin gosterdigi bir JSON hatasina girer.
	return strings.Join(strings.Fields(strings.Join(parts, "; ")), " ")
}

func runningKernelRelease() string {
	raw, err := os.ReadFile(firewallKernelReleasePath)
	if err != nil {
		return ""
	}
	release := strings.TrimSpace(string(raw))
	// A release string is a path component here; anything that could escape
	// the module root is treated as unreadable rather than followed.
	// Bir surum dizesi burada yol bilesenidir; kacabilecek her sey okunamaz
	// sayilir.
	if release == "" || release == "." || release == ".." ||
		strings.ContainsAny(release, "/\\\x00") {
		return ""
	}
	return release
}

func kernelModuleTreeUsable(tree string) bool {
	info, err := os.Lstat(tree)
	if err != nil || !info.IsDir() {
		return false
	}
	// A directory that outlived its dependency index cannot load a module
	// either, so an empty shell counts as gone rather than as present.
	// Bagimlilik dizinini yitirmis bir dizin de modul yukleyemez.
	for _, index := range []string{"modules.dep", "modules.dep.bin", "modules.builtin"} {
		entry, err := os.Lstat(filepath.Join(tree, index))
		if err == nil && entry.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// runningKernelModulesMissing is true only when this host demonstrably keeps
// kernel module trees, keeps at least one, and does not keep the one the
// running kernel needs. A container or a modules-less kernel never had a tree
// and is never accused of needing a restart.
// runningKernelModulesMissing, yalnizca bu makinenin kanitli bicimde modul
// agaclari tuttugu, en az birini tuttugu ve calisan cekirdegin ihtiyac
// duydugunu tutmadigi durumda dogrudur.
func runningKernelModulesMissing() bool {
	release := runningKernelRelease()
	if release == "" {
		return false
	}
	if kernelModuleTreeUsable(filepath.Join(firewallKernelModulesRoot, release)) {
		return false
	}
	entries, err := os.ReadDir(firewallKernelModulesRoot)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == release {
			continue
		}
		if kernelModuleTreeUsable(filepath.Join(firewallKernelModulesRoot, entry.Name())) {
			return true
		}
	}
	return false
}

// classifyFirewallEngineFault judges a failed nft invocation. The structural
// proof is asked first, because it is the only one that may be trusted with a
// decision rather than only with a sentence.
// classifyFirewallEngineFault, basarisiz bir nft cagrisini degerlendirir.
func classifyFirewallEngineFault(out []byte, err error) firewallEngineFault {
	if err == nil {
		return firewallEngineFaultNone
	}
	if runningKernelModulesMissing() {
		return firewallEngineFaultModulesMissing
	}
	diagnostic := strings.ToLower(firewallCommandDiagnostic(out, err))
	for _, signature := range firewallKernelUnreachableSignatures {
		if strings.Contains(diagnostic, signature) {
			return firewallEngineFaultKernelUnreachable
		}
	}
	return firewallEngineFaultNone
}

// describeFirewallEngineFailure turns a failed nft invocation into the
// sentence the operator reads. It always carries what the command actually
// said, and adds the machine's own reason when there is one to add.
// describeFirewallEngineFailure, basarisiz bir nft cagrisini operatorun
// okudugu cumleye cevirir.
func describeFirewallEngineFailure(
	prefix string,
	out []byte,
	err error,
) (firewallEngineFault, string) {
	fault := classifyFirewallEngineFault(out, err)
	detail := firewallCommandDiagnostic(out, err)
	if detail == "" {
		detail = "unknown"
	}
	// The operator's sentence goes first and the command's own words follow it
	// in brackets. Order matters: this string is carried as a failure reason
	// that is bounded before it is recorded, and nft's diagnostic is long
	// enough to push everything after it past the limit. What may be lost is
	// the diagnostic, never the one instruction the operator has to act on.
	// Operatorun cumlesi once, komutun kendi sozleri parantez icinde sonra
	// gelir. Sira onemlidir: bu dize kaydedilmeden once sinirlanir ve nft'nin
	// teshis metni, ardindaki her seyi sinirin disina itecek kadar uzundur.
	if sentence := firewallEngineFaultSentence(fault); sentence != "" {
		return fault, sentence + " (" + prefix + ": " + detail + ")"
	}
	return fault, prefix + ": " + detail
}

// firewallEngineError is an nft failure that carries its classification, so a
// caller far from the command can still tell a kernel that cannot be reached
// from a rule that was refused.
// firewallEngineError, siniflandirmasini tasiyan bir nft hatasidir.
type firewallEngineError struct {
	fault   firewallEngineFault
	message string
}

func (e *firewallEngineError) Error() string { return e.message }

func newFirewallEngineError(prefix string, out []byte, err error) *firewallEngineError {
	fault, message := describeFirewallEngineFailure(prefix, out, err)
	return &firewallEngineError{fault: fault, message: message}
}

// firewallEngineFaultOf reports the classification an error carries, if any.
// firewallEngineFaultOf, bir hatanin tasidigi siniflandirmayi bildirir.
func firewallEngineFaultOf(err error) firewallEngineFault {
	var engineErr *firewallEngineError
	if errors.As(err, &engineErr) {
		return engineErr.fault
	}
	return firewallEngineFaultNone
}

// discoverFirewallTables is the one place a commit reads the nft table list.
// It is deliberately the first host contact a plan makes, so a machine whose
// kernel cannot answer is discovered before anything is changed.
// discoverFirewallTables, bir taahhudun nft tablo listesini okudugu tek yerdir
// ve bilerek bir planin ilk makine temasidir.
func discoverFirewallTables(runner firewallCommandRunner) ([]byte, error) {
	out, err := runner.Output("nft", "list", "tables")
	if err != nil {
		return nil, newFirewallEngineError("nft table discovery failed", out, err)
	}
	return out, nil
}
