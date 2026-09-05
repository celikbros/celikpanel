package main

import "errors"

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

// describeVPNHostFailure turns a failed VPN host command into the sentence the
// operator reads. It always carries what the command actually said, and adds
// the machine's own reason when there is one to add. The order - instruction
// first, technical detail after - is the shared one, for the shared reason:
// this string is bounded before it is recorded.
//
// describeVPNHostFailure, basarisiz bir VPN makine komutunu operatorun okudugu
// cumleye cevirir. Sira - once talimat, sonra teknik ayrinti - paylasilan
// siradir ve nedeni de paylasilir: bu dize kaydedilmeden once sinirlanir.
func describeVPNHostFailure(prefix string, out []byte, err error) (
	restartRequired bool,
	message string,
) {
	instruction := ""
	if err != nil && vpnHostCannotLoadKernelModules() {
		instruction = vpnEngineRebootSentence
		restartRequired = true
	}
	// firewallCommandDiagnostic recovers what a failed command wrote to
	// stderr, which is where both nft and wg write the sentence that explains
	// them; it is about a failed command, not about the firewall, and is read
	// from here for the same reason the module-tree probe is.
	// firewallCommandDiagnostic, basarisiz bir komutun stderr'e yazdigini geri
	// kazanir; guvenlik duvarina degil, basarisiz bir komuta dairdir.
	return restartRequired, operatorFirstFailureSentence(
		instruction, prefix, firewallCommandDiagnostic(out, err),
	)
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

func newVPNHostError(prefix string, out []byte, err error) *vpnHostError {
	restartRequired, message := describeVPNHostFailure(prefix, out, err)
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
