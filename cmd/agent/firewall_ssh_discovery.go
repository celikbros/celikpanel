package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The firewall keeps every proven sshd listener open so enabling it can never
// take away the operator's own way back in. That proof only means something
// when there is an SSH service to protect. Three different situations used to
// collapse into one sentence — "no verified listening sshd port was found" —
// and a host that simply has no SSH service could therefore never enable the
// firewall at all. They are told apart here.
//
// Güvenlik duvarı, kanıtlanmış her sshd dinleyicisini açık tutar; böylece onu
// açmak operatörün geri dönüş yolunu asla elinden alamaz. Bu kanıt yalnız
// korunacak bir SSH servisi varsa bir şey ifade eder. Eskiden üç ayrı durum
// tek bir cümleye — "doğrulanmış dinleyen sshd portu bulunamadı" — sıkışıyor
// ve hiç SSH servisi olmayan bir sunucu güvenlik duvarını hiç açamıyordu.
// Burada birbirinden ayrılırlar.

// errNoVerifiedSSHListener is returned by the listener probe when it ran
// cleanly and found no verified sshd listener. It says nothing about whether
// this host has an SSH service; sshServicePresence answers that.
// errNoVerifiedSSHListener, dinleyici yoklaması sorunsuz çalışıp doğrulanmış
// bir sshd dinleyicisi bulamadığında döner. Bu sunucuda SSH servisi olup
// olmadığını söylemez; onu sshServicePresence yanıtlar.
var errNoVerifiedSSHListener = errors.New("no verified listening sshd port was found")

// sshServicePresenceProber answers whether this host carries an SSH service at
// all, independently of whether it is listening right now.
// sshServicePresenceProber, bu sunucuda hiç SSH servisi olup olmadığını, şu an
// dinleyip dinlemediğinden bağımsız olarak yanıtlar.
type sshServicePresenceProber interface {
	SSHServicePresent() (bool, error)
}

// sshDiscoveryRefusal carries the exact reason a firewall change was refused,
// so the panel can name it instead of forwarding one opaque sentence.
// sshDiscoveryRefusal, bir güvenlik duvarı değişikliğinin tam olarak neden
// reddedildiğini taşır; böylece panel tek bir kapalı cümleyi iletmek yerine
// nedeni adıyla söyleyebilir.
type sshDiscoveryRefusal struct {
	reason  string
	message string
	cause   error
}

func (e *sshDiscoveryRefusal) Error() string { return e.message }

func (e *sshDiscoveryRefusal) Unwrap() error { return e.cause }

// classifySSHDiscovery turns a listener-probe failure into one of the three
// exact reasons. It never guesses: a presence probe that cannot run leaves the
// answer unknown, and unknown is a refusal, not a host without SSH.
// classifySSHDiscovery, bir dinleyici yoklaması hatasını üç tam nedenden
// birine çevirir. Asla tahmin etmez: çalışamayan bir varlık yoklaması yanıtı
// bilinmez bırakır ve bilinmez, SSH'ı olmayan bir sunucu değil, bir reddir.
func classifySSHDiscovery(runner firewallCommandRunner, discoveryErr error) *sshDiscoveryRefusal {
	if discoveryErr == nil {
		return nil
	}
	if !errors.Is(discoveryErr, errNoVerifiedSSHListener) {
		return &sshDiscoveryRefusal{
			reason: transport.SSHDiscoveryProbeFailed,
			message: fmt.Sprintf(
				"SSH listener discovery failed; firewall was not changed: %v",
				discoveryErr,
			),
			cause: discoveryErr,
		}
	}
	prober, ok := runner.(sshServicePresenceProber)
	if !ok {
		return &sshDiscoveryRefusal{
			reason: transport.SSHDiscoveryProbeFailed,
			message: "SSH listener discovery failed; firewall was not changed: " +
				"the SSH service presence prober is unavailable",
			cause: discoveryErr,
		}
	}
	present, err := prober.SSHServicePresent()
	if err != nil {
		return &sshDiscoveryRefusal{
			reason: transport.SSHDiscoveryProbeFailed,
			message: fmt.Sprintf(
				"SSH listener discovery failed; firewall was not changed: "+
					"whether this server has an SSH service could not be determined: %v",
				err,
			),
			cause: err,
		}
	}
	if present {
		return &sshDiscoveryRefusal{
			reason: transport.SSHDiscoveryNotListening,
			message: "this server has an SSH service but no verified listening sshd port; " +
				"firewall was not changed",
			cause: discoveryErr,
		}
	}
	return &sshDiscoveryRefusal{
		reason:  transport.SSHDiscoveryNoService,
		message: "this server has no SSH service, so no SSH port could be proven",
		cause:   discoveryErr,
	}
}

// sshUnitNames are every unit that would make an SSH service present on a
// systemd host even while no sshd executable sits on a trusted path.
// sshUnitNames, güvenilen bir yolda sshd çalıştırılabiliri olmasa bile systemd
// kullanan bir sunucuda SSH servisini var kılacak birimlerin tümüdür.
var sshUnitNames = []string{"ssh.socket", "sshd.socket", "ssh.service", "sshd.service"}

// SSHServicePresent reports whether this host carries an SSH service. A
// trusted sshd executable is proof on its own. Otherwise systemd is asked
// about every SSH unit; a host with no unit manager has no SSH units to hold,
// so it is an honest absence rather than an unknown.
// SSHServicePresent, bu sunucuda bir SSH servisi olup olmadığını bildirir.
// Güvenilen bir sshd çalıştırılabiliri tek başına kanıttır. Aksi hâlde systemd'ye
// her SSH birimi sorulur; birim yöneticisi olmayan bir sunucuda tutulacak SSH
// birimi de yoktur, bu yüzden bu bilinmezlik değil dürüst bir yokluktur.
func (r hostFirewallCommandRunner) SSHServicePresent() (bool, error) {
	if _, err := trustedSSHDExecutablePath(); err == nil {
		return true, nil
	}
	systemctlPath, err := trustedCommandExecutablePath("systemctl")
	if err != nil {
		return false, nil
	}
	for _, unit := range sshUnitNames {
		out, outErr := serviceMutationCommand(r.commandContext(),
			systemctlPath, "show", "--no-pager", "--property=LoadState", "--value", unit,
		).CombinedOutput()
		state := strings.TrimSpace(string(out))
		if state == "not-found" {
			continue
		}
		if outErr != nil {
			return false, errors.New(commandFailureDetail(
				"systemctl "+unit+" LoadState failed", out, outErr))
		}
		if state == "" {
			return false, fmt.Errorf("systemctl %s returned an empty LoadState", unit)
		}
		return true, nil
	}
	return false, nil
}

// readFirewallSSHDiscovery fills the SSH half of a status response. A host
// that simply has no SSH service is a state, not a fault, so it names itself
// in SSHDiscoveryReason and leaves Error alone; the two refusals still show up
// as errors on a firewall that is already live.
// readFirewallSSHDiscovery, bir durum yanıtının SSH yarısını doldurur. Hiç SSH
// servisi olmayan bir sunucu bir arıza değil bir durumdur; bu yüzden kendini
// SSHDiscoveryReason'da adlandırır ve Error'a dokunmaz. İki ret ise zaten
// canlı olan bir güvenlik duvarında hata olarak görünmeyi sürdürür.
func readFirewallSSHDiscovery(runner firewallCommandRunner, resp *FirewallStatusResponse) {
	ports, err := detectSSHPortsWithRunner(runner)
	if err == nil {
		resp.SSHPorts = ports
		resp.SSHDiscoveryReason = ""
		return
	}
	refusal := classifySSHDiscovery(runner, err)
	resp.SSHDiscoveryReason = refusal.reason
	if refusal.reason == transport.SSHDiscoveryNoService || !resp.Enabled {
		return
	}
	resp.Error = appendFirewallError(resp.Error, refusal.message)
}
