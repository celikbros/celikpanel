package main

import (
	"errors"
	"io/fs"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostcmd"
)

// R-055. WireGuard is a kernel module with a userspace client, so the VPN
// meets the same machine fault the firewall met in R-054: an install's
// prerequisite step can upgrade the running kernel and delete the module tree
// the running kernel needs, after which this host can load no kernel module at
// all until it is restarted. `modprobe wireguard` fails exactly as `nft` did,
// and every layer above it turned that into "could not inspect nftables
// policies" or an opaque 500. This file exists to name it, so the operator is
// told to restart the server instead of being handed a sentence about a
// policy inventory.
//
// R-055. WireGuard, kullanici alani istemcisi olan bir cekirdek modulduur; bu
// yuzden VPN, guvenlik duvarinin R-054'te karsilastigi makine hatasiyla
// karsilasir: kurulumun on gereksinim adimi calisan cekirdegi yukseltip
// calisan cekirdegin ihtiyac duydugu modul agacini silebilir. Bu dosya bunu
// adlandirmak icin vardir.

// The structural fact this file rests on - the running kernel's module tree is
// not on disk while another kernel's is - is a fact about the machine, not
// about nftables, and it is already written down once in
// firewall_engine_health.go where R-054 discovered it. It is read from here
// rather than copied: two paths asking the same machine the same question is
// not two proofs.
//
// Bu dosyanin dayandigi yapisal olgu makineye dairdir, nftables'a degil, ve
// R-054'un onu bulduğu yerde bir kez yazilmistir. Kopyalanmaz, oradan okunur.
func vpnHostCannotLoadKernelModules() bool {
	return runningKernelModulesMissing()
}

// The sentence an operator may receive. It names the machine's problem and the
// one action that fixes it, in the product's own plain voice.
// Operatorun alabilecegi cumle.
const vpnEngineRebootSentence = "This server is running a kernel whose modules are " +
	"no longer on disk, so WireGuard cannot be loaded: a package upgrade replaced the " +
	"kernel and this server has not been restarted since. Restart this server, then " +
	"set the VPN up again."

// Why nft's words may be repeated on the VPN path too, and why WireGuard's may
// not. The VPN asks nft about a policy inventory the agent composed itself, so
// nft's diagnostic holds nothing an operator may not read. `wg` is different,
// and the difference is not cosmetic: `wg syncconf` is handed the stripped
// interface configuration, which contains the interface's PrivateKey, and wg
// reports a configuration it will not accept by quoting the line it choked on.
// Repeating that output verbatim can hand the server's WireGuard private key
// to whoever is looking at the panel. So the wg paths read their output here,
// where it exists, and only what it meant travels.
//
// nft'nin sozlerinin VPN yolunda da neden tekrarlanabildigi ve WireGuard'in
// sozlerinin neden tekrarlanamadigi. `wg syncconf`, icinde arayuzun
// PrivateKey'i bulunan yapilandirmayi alir ve kabul etmedigi bir yapilandirmayi
// takildigi satiri alintilayarak bildirir. Bu ciktiyi oldugu gibi tekrarlamak,
// sunucunun WireGuard ozel anahtarini panele bakan kisiye verebilir.
const vpnNFTDiagnosticIsRepeatable = "nft's diagnostic describes a policy " +
	"inventory this agent composed itself; no key or credential is assembled " +
	"into it"

// wireGuardMeaning names what wg said, without saying it. Anything it does not
// recognise is reported as unrecognised rather than mislabelled, and is still
// never repeated - the same trade internal/services made for the mysql client.
//
// wireGuardMeaning, wg'nin ne soyledigini, soylemeden adlandirir. Tanimadigi
// her sey yanlis etiketlenmek yerine taninmamis olarak bildirilir.
func wireGuardMeaning(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "line unrecognized"),
		strings.Contains(lowered, "configuration parsing error"),
		strings.Contains(lowered, "invalid"):
		return "WireGuard would not accept the configuration this panel wrote"
	case strings.Contains(lowered, "no such device"),
		strings.Contains(lowered, "unable to access interface"),
		strings.Contains(lowered, "cannot find device"):
		return "the WireGuard interface this panel manages is not present on this server"
	case strings.Contains(lowered, "operation not permitted"),
		strings.Contains(lowered, "permission denied"):
		return "this server refused the change: WireGuard reported that the operation is not permitted"
	default:
		return ""
	}
}

// describeVPNHostFailure turns a failed VPN host command into the sentence the
// operator reads. The detail is decided by the caller, because only the caller
// knows what the command was holding, and it arrives already read. The order -
// instruction first, technical detail after - is the shared one, for the
// shared reason: this string is bounded before it is recorded.
//
// describeVPNHostFailure, basarisiz bir VPN makine komutunu operatorun okudugu
// cumleye cevirir. Ayrintiya cagiran karar verir; cunku komutun neyi tuttugunu
// yalnizca cagiran bilir.
func describeVPNHostFailure(prefix, detail string, err error) (
	restartRequired bool,
	message string,
) {
	instruction := ""
	if err != nil && vpnHostCannotLoadKernelModules() {
		instruction = vpnEngineRebootSentence
		restartRequired = true
	}
	return restartRequired, operatorFirstFailureSentence(instruction, prefix, detail)
}

// vpnHostError is a VPN host-command failure that carries whether this machine
// has to be restarted before the VPN can work at all, so a caller far from the
// command can still tell that apart from a configuration it would not accept.
// vpnHostError, bu makinenin yeniden baslatilmasi gerekip gerekmedigini tasiyan
// bir VPN makine-komutu hatasidir.
type vpnHostError struct {
	restartRequired bool
	message         string
}

func (e *vpnHostError) Error() string { return e.message }

// newVPNHostError is the nft path: the command's own words go out with the
// error, because they hold nothing an operator may not read.
// newVPNHostError, nft yoludur: komutun kendi sozleri hatayla birlikte gider.
func newVPNHostError(prefix string, out []byte, err error) *vpnHostError {
	restartRequired, message := describeVPNHostFailure(
		prefix, hostcmd.Verbatim(out, err, vpnNFTDiagnosticIsRepeatable), err,
	)
	return &vpnHostError{restartRequired: restartRequired, message: message}
}

// newVPNKeyBearingHostError is the wg path: the output is read where it
// exists, and only what it meant leaves this function, because the words can
// quote a line of a configuration that carries the interface's private key.
//
// newVPNKeyBearingHostError, wg yoludur: cikti var oldugu yerde okunur ve bu
// fonksiyondan yalnizca ne anlama geldigi cikar.
func newVPNKeyBearingHostError(prefix string, out []byte, err error) *vpnHostError {
	restartRequired, message := describeVPNHostFailure(
		prefix, hostcmd.Classified(out, err, wireGuardMeaning), err,
	)
	return &vpnHostError{restartRequired: restartRequired, message: message}
}

// vpnHostRestartRequired reports whether an error carries the machine's own
// reason. It is deliberately false for anything else, including an error that
// merely mentions a module: only the structural proof may make this true.
// vpnHostRestartRequired, bir hatanin makinenin kendi nedenini tasiyip
// tasimadigini bildirir; yalnizca yapisal kanit bunu dogru yapabilir.
func vpnHostRestartRequired(err error) bool {
	var hostErr *vpnHostError
	if errors.As(err, &hostErr) {
		return hostErr.restartRequired
	}
	return false
}

// R-058. There being no VPN server on this host is a fact about the filesystem
// - the configuration the sync would amend is not there - and it is read that
// way rather than from a message. A configuration that exists and cannot be
// read is a different fault: a directory whose permissions are wrong, a file
// that failed its security validation, a read that raced a writer. Those must
// not be reported as "not set up", because the answer to them is not "set the
// VPN up".
//
// R-058. Bu makinede VPN sunucusu olmamasi dosya sistemine dair bir olgudur -
// esitlemenin degistirecegi yapilandirma orada degildir - ve bir mesajdan degil
// bu yoldan okunur. Var olup okunamayan bir yapilandirma baska bir arizadir ve
// "kurulu degil" diye bildirilmemelidir; cunku cozumu "VPN'i kurun" degildir.
func vpnConfigurationAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
