package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

// The mail stack needs this server to have a fully qualified name: Postfix
// announces it, and the mail certificate is issued for it. Until now nothing
// in the product could give the server one, so a host whose operating-system
// hostname was a bare machine name could never install mail at all. Setting
// the hostname is an ordinary privileged host mutation, so it happens here,
// inside the same mutation lease as the rest of the install, with the same
// proofs: the exact name is validated on both sides, the persistent file is
// replaced atomically, and the result is read back from the kernel before the
// step reports success.
//
// Posta yığını, bu sunucunun tam nitelikli bir adı olmasını ister: Postfix onu
// duyurur ve posta sertifikası onun için verilir. Şimdiye dek üründe sunucuya
// böyle bir ad verebilecek hiçbir şey yoktu; bu yüzden işletim sistemi adı çıplak
// bir makine adı olan bir sunucu postayı hiç kuramıyordu. Ana bilgisayar adını
// koymak olağan, ayrıcalıklı bir sunucu değişikliğidir; bu yüzden burada, kurulumun
// geri kalanıyla aynı değişiklik kirası içinde ve aynı kanıtlarla yapılır: tam ad
// iki tarafta da doğrulanır, kalıcı dosya bölünmez biçimde değiştirilir ve adım
// başarı bildirmeden önce sonuç çekirdekten geri okunur.

type SetServerHostnameRequest = transport.SetServerHostnameRequest

type SetServerHostnameResponse = transport.SetServerHostnameResponse

var (
	hostnameFilePath  = "/etc/hostname"
	hostsFilePath     = "/etc/hosts"
	readAgentHostname = os.Hostname
)

// trustedHostnameCommandPaths keeps the privileged hostname change on the same
// footing as every other agent command: an exact allowlist, never a PATH
// lookup. hostnamectl is preferred because it also tells systemd; plain
// hostname is the fallback for a host without a running hostnamed.
// trustedHostnameCommandPaths, ayrıcalıklı ana bilgisayar adı değişikliğini
// diğer her agent komutuyla aynı temele oturtur: PATH araması değil, tam bir
// izin listesi. hostnamectl yeğlenir çünkü systemd'ye de haber verir; düz
// hostname ise hostnamed çalışmayan bir sunucu için yedektir.
var trustedHostnameCommandPaths = map[string][]string{
	"hostnamectl": {"/usr/bin/hostnamectl", "/bin/hostnamectl"},
	"hostname":    {"/usr/bin/hostname", "/bin/hostname"},
}

func trustedHostnameCommand(name string) (string, error) {
	candidates, ok := trustedHostnameCommandPaths[name]
	if !ok {
		return "", fmt.Errorf("hostname command %q is not allowlisted", name)
	}
	return firstTrustedExecutable(candidates, name)
}

// SetServerHostname gives this server an exact fully qualified name. It is a
// step of a mail profile install and is admitted by nothing else.
// SetServerHostname, bu sunucuya tam nitelikli bir ad verir. Bir posta profili
// kurulumunun adımıdır ve başka hiçbir şeyce kabul edilmez.
func (a *Agent) SetServerHostname(
	req *SetServerHostnameRequest,
	resp *SetServerHostnameResponse,
) error {
	if resp == nil {
		return errors.New("server hostname response is required")
	}
	*resp = SetServerHostnameResponse{}
	if req == nil {
		return errors.New("server hostname request is required")
	}
	canonical, err := hostname.CanonicalFQDN(req.Hostname)
	if err != nil || canonical != strings.TrimSpace(req.Hostname) {
		// The panel already canonicalizes. Anything else reaching the agent is
		// not a name this server may be given.
		// Panel zaten kanonikleştirir. Agent'a başka bir şey ulaşıyorsa, bu
		// sunucuya verilebilecek bir ad değildir.
		resp.Error = "server hostname must be a canonical fully qualified domain name"
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(
			serviceMutationStepSetServerHostname, "server-hostname", "", "set",
		),
	)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer finishStep()

	previous, err := readAgentHostname()
	if err != nil {
		resp.Error = fmt.Sprintf("read the current server hostname: %v", err)
		return nil
	}
	resp.Previous = previous
	resp.Hostname = canonical
	if previous == canonical {
		return nil
	}
	if err := applyServerHostname(ctx, canonical); err != nil {
		resp.Error = err.Error()
		return nil
	}
	// Read the result back from the kernel: a command that exited zero is not
	// proof that this server now answers to the name.
	// Sonucu çekirdekten geri oku: sıfırla çıkan bir komut, bu sunucunun artık
	// o ada yanıt verdiğinin kanıtı değildir.
	current, err := readAgentHostname()
	if err != nil {
		resp.Error = fmt.Sprintf("verify the new server hostname: %v", err)
		return nil
	}
	if current != canonical {
		resp.Error = "the server hostname did not change to the requested name"
		return nil
	}
	resp.Changed = true
	return nil
}

func applyServerHostname(ctx context.Context, canonical string) error {
	if err := writeHostnameFile(canonical); err != nil {
		return fmt.Errorf("write %s: %w", hostnameFilePath, err)
	}
	if err := setLiveHostname(ctx, canonical); err != nil {
		return err
	}
	if err := ensureHostsEntry(canonical); err != nil {
		return fmt.Errorf("write %s: %w", hostsFilePath, err)
	}
	return nil
}

var setLiveHostname = func(ctx context.Context, canonical string) error {
	var failures []string
	for _, attempt := range [][]string{
		{"hostnamectl", "set-hostname"},
		{"hostname"},
	} {
		path, err := trustedHostnameCommand(attempt[0])
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", attempt[0], err))
			continue
		}
		args := append(append([]string(nil), attempt[1:]...), canonical)
		out, err := serviceMutationCommand(ctx, path, args...).CombinedOutput()
		if err == nil {
			return nil
		}
		failures = append(failures, commandFailureDetail(attempt[0]+" failed", out, err))
	}
	return fmt.Errorf("set the live server hostname: %s", strings.Join(failures, "; "))
}

func writeHostnameFile(canonical string) error {
	return replaceHostFileAtomically(hostnameFilePath, []byte(canonical+"\n"))
}

// ensureHostsEntry makes the new name resolve locally. Postfix and the mail
// certificate both look the server's own name up, so a name that resolves
// nowhere is a name the mail stack cannot use. Only the 127.0.1.1 line is
// rewritten; every other line of the file is preserved byte for byte.
// ensureHostsEntry, yeni adın yerelde çözülmesini sağlar. Postfix de posta
// sertifikası da sunucunun kendi adını arar; hiçbir yerde çözülmeyen bir ad,
// posta yığınının kullanamayacağı bir addır. Yalnız 127.0.1.1 satırı yeniden
// yazılır; dosyanın diğer her satırı bayt bayt korunur.
func ensureHostsEntry(canonical string) error {
	existing, err := os.ReadFile(hostsFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			existing = nil
		} else {
			return err
		}
	}
	short, _, _ := strings.Cut(canonical, ".")
	desired := "127.0.1.1\t" + canonical
	if short != "" && short != canonical {
		desired += "\t" + short
	}
	var kept []string
	replaced := false
	for _, line := range strings.Split(string(existing), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "127.0.1.1" {
			if !replaced {
				kept = append(kept, desired)
				replaced = true
			}
			continue
		}
		kept = append(kept, line)
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	if !replaced {
		kept = append(kept, desired)
	}
	updated := []byte(strings.Join(kept, "\n") + "\n")
	if bytes.Equal(updated, existing) {
		return nil
	}
	return replaceHostFileAtomically(hostsFilePath, updated)
}

// replaceHostFileAtomically publishes content through a staged rename so a
// crash can never leave this server with half a name.
// replaceHostFileAtomically, içeriği evreli bir yeniden adlandırmayla
// yayımlar; böylece bir çökme bu sunucuyu yarım bir adla asla bırakamaz.
func replaceHostFileAtomically(path string, content []byte) error {
	directory := filepath.Dir(path)
	stage, err := os.CreateTemp(directory, ".celikpanel-hostname-*")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	published := false
	defer func() {
		_ = stage.Close()
		if !published {
			_ = os.Remove(stagePath)
		}
	}()
	if err := stage.Chmod(0o644); err != nil {
		return err
	}
	if _, err := stage.Write(content); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := os.Rename(stagePath, path); err != nil {
		return err
	}
	published = true
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}
