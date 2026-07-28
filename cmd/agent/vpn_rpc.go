package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// Built-in VPN server on WireGuard. The panel owns the peer ledger (SQLite)
// and pushes the full desired peer set here — the same full-state-push split
// as DNS zones. The agent does the privileged work: writes wg0.conf, brings
// the interface up, applies peer changes live. Keys are generated in pure Go
// (curve25519, already a dependency via argon2) so nothing is shelled out for
// secrets; client private keys are returned once and never stored anywhere.
//
// WireGuard üzerinde yerleşik VPN sunucusu. Peer defterini panel tutar
// (SQLite) ve istenen peer setinin tamamını buraya iter — DNS zone'larıyla
// aynı tam-durum-itme bölünmesi. Ayrıcalıklı işi agent yapar: wg0.conf'u
// yazar, arayüzü kaldırır, peer değişikliklerini canlı uygular. Anahtarlar
// saf Go'da üretilir (curve25519, argon2 üzerinden zaten bağımlılık);
// istemci özel anahtarları bir kez döndürülür ve hiçbir yerde saklanmaz.

const (
	wgConfDir   = "/etc/wireguard"
	wgIface     = "wg0"
	wgSubnet    = "10.8.0.0/24"
	wgServerIP  = "10.8.0.1"
	wgDefaultPt = 51820
)

func wgConfPath() string { return filepath.Join(wgConfDir, wgIface+".conf") }

// wgKeyPair generates a WireGuard (Curve25519) key pair, base64-encoded like
// the wg tool produces.
// wgKeyPair, wg aracının ürettiği gibi base64 kodlu bir WireGuard
// (Curve25519) anahtar çifti üretir.
func wgKeyPair() (privB64, pubB64 string, err error) {
	var priv [32]byte
	if _, err = rand.Read(priv[:]); err != nil {
		return "", "", err
	}
	// Standard Curve25519 clamping — without it the key is invalid.
	// Standart Curve25519 kırpması — onsuz anahtar geçersizdir.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv[:]), base64.StdEncoding.EncodeToString(pub), nil
}

type VPNKeysResponse struct {
	PrivateKey   string `json:"private_key"`
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key"`
	Error        string `json:"error,omitempty"`
}

// GenerateVPNKeys returns a fresh client key pair plus a preshared key. The
// private key goes to the client config only; the panel stores just the
// public and preshared keys.
// GenerateVPNKeys, taze bir istemci anahtar çifti ve bir ön-paylaşımlı
// anahtar döndürür. Özel anahtar yalnız istemci config'ine gider.
func (a *Agent) GenerateVPNKeys(_ *struct{}, resp *VPNKeysResponse) error {
	priv, pub, err := wgKeyPair()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	psk := make([]byte, 32)
	if _, err := rand.Read(psk); err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.PrivateKey, resp.PublicKey = priv, pub
	resp.PresharedKey = base64.StdEncoding.EncodeToString(psk)
	return nil
}

type SetupVPNRequest struct {
	ServiceMutationBinding
	Port int `json:"port"` // 0 → default 51820
}

type SetupVPNResponse struct {
	Created bool   `json:"created"` // false when config already existed
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
}

// SetupVPN writes the server-side wg0.conf (once), enables IP forwarding and
// starts wg-quick@wg0. Re-running against an existing config only ensures the
// service is up — it never regenerates the server key, which would strand
// every issued client config.
// SetupVPN, sunucu tarafı wg0.conf'u (bir kez) yazar, IP yönlendirmeyi açar
// ve wg-quick@wg0'ı başlatır. Mevcut config'e karşı yeniden koşmak yalnız
// servisin ayakta olmasını sağlar — sunucu anahtarını asla yeniden üretmez;
// bu, verilmiş her istemci config'ini geçersiz bırakırdı.
func (a *Agent) SetupVPN(req *SetupVPNRequest, resp *SetupVPNResponse) error {
	if req == nil {
		return fmt.Errorf("VPN setup request is required")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer finishStep()
	if _, err := exec.LookPath("wg"); err != nil {
		resp.Error = "wireguard is not installed"
		return nil
	}
	port := req.Port
	if port <= 0 || port > 65535 {
		port = wgDefaultPt
	}

	if _, err := os.Stat(wgConfPath()); os.IsNotExist(err) {
		priv, _, err := wgKeyPair()
		if err != nil {
			resp.Error = err.Error()
			return nil
		}
		wan := defaultRouteIface()
		// NAT so clients can reach the internet through the tunnel. This box
		// has nftables (no iptables); an own table keeps teardown clean.
		// İstemciler tünelden internete çıkabilsin diye NAT. Bu kutuda
		// nftables var (iptables yok); kendi tablomuz kapanışı temiz tutar.
		postUp := fmt.Sprintf(`nft add table inet celikpanel_vpn; nft add chain inet celikpanel_vpn postrouting '{ type nat hook postrouting priority 100 ; }'; nft add rule inet celikpanel_vpn postrouting ip saddr %s oifname "%s" masquerade`, wgSubnet, wan)
		postDown := `nft delete table inet celikpanel_vpn`
		conf := fmt.Sprintf(`[Interface]
# CelikPanel VPN server — managed by the panel; peers are synced below.
Address = %s/24
ListenPort = %d
PrivateKey = %s
PostUp = %s
PostDown = %s
`, wgServerIP, port, priv, postUp, postDown)
		if err := os.MkdirAll(wgConfDir, 0o700); err != nil {
			resp.Error = err.Error()
			return nil
		}
		if err := os.WriteFile(wgConfPath(), []byte(conf), 0o600); err != nil {
			resp.Error = err.Error()
			return nil
		}
		resp.Created = true
	}

	// Clients cannot go anywhere without kernel forwarding; persist + apply.
	// Çekirdek yönlendirmesi olmadan istemciler hiçbir yere gidemez.
	_ = os.WriteFile("/etc/sysctl.d/99-celikpanel-vpn.conf", []byte("net.ipv4.ip_forward = 1\n"), 0o644)
	_ = serviceMutationCommand(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1").Run()

	if err := enableServiceForMutation(ctx, "wg-quick@"+wgIface, true); err != nil {
		resp.Error = fmt.Sprintf("wg-quick start failed: %v", err)
		return nil
	}
	resp.Detail = "VPN server is up on udp/" + strconv.Itoa(port)
	return nil
}

type VPNPeerSpec struct {
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key"`
	IP           string `json:"ip"` // e.g. "10.8.0.2"
}

type SyncVPNPeersRequest struct {
	ServiceMutationBinding
	Peers []VPNPeerSpec `json:"peers"`
}

type SyncVPNPeersResponse struct {
	Applied bool   `json:"applied"` // true when the live interface was updated
	Error   string `json:"error,omitempty"`
}

// SyncVPNPeers rewrites the peer section of wg0.conf from the panel's full
// desired list and applies it to the live interface without dropping existing
// tunnels (wg syncconf diffs instead of resetting).
// SyncVPNPeers, wg0.conf'un peer bölümünü panelin istediği tam listeden
// yeniden yazar ve mevcut tünelleri düşürmeden canlı arayüze uygular
// (wg syncconf sıfırlamak yerine fark uygular).
func (a *Agent) SyncVPNPeers(req *SyncVPNPeersRequest, resp *SyncVPNPeersResponse) error {
	if req == nil {
		return fmt.Errorf("VPN peer sync request is required")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer finishStep()
	raw, err := os.ReadFile(wgConfPath())
	if err != nil {
		resp.Error = "VPN server is not set up"
		return nil
	}
	// Keep the [Interface] section (everything before the first [Peer]).
	// [Interface] bölümünü koru (ilk [Peer]'dan önceki her şey).
	conf := string(raw)
	if i := strings.Index(conf, "[Peer]"); i >= 0 {
		conf = conf[:i]
	}
	conf = strings.TrimRight(conf, "\n") + "\n"
	var b strings.Builder
	b.WriteString(conf)
	for _, p := range req.Peers {
		if !validWGKey(p.PublicKey) || !validWGKey(p.PresharedKey) || !validPeerIP(p.IP) {
			resp.Error = "invalid peer spec"
			return nil
		}
		fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s/32\n", p.PublicKey, p.PresharedKey, p.IP)
	}
	if err := os.WriteFile(wgConfPath(), []byte(b.String()), 0o600); err != nil {
		resp.Error = err.Error()
		return nil
	}

	// Apply live only if the interface is up; otherwise the next start of
	// wg-quick@wg0 picks the file up.
	// Yalnız arayüz ayaktaysa canlı uygula; değilse dosyayı bir sonraki
	// wg-quick@wg0 başlangıcı alır.
	if serviceMutationCommand(ctx, "wg", "show", wgIface).Run() != nil {
		return nil
	}
	stripped, err := serviceMutationCommand(ctx, "wg-quick", "strip", wgIface).Output()
	if err != nil {
		resp.Error = "wg-quick strip failed"
		return nil
	}
	tmp, err := os.CreateTemp("", "celikpanel-wg-*.conf")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(stripped); err != nil {
		tmp.Close()
		resp.Error = err.Error()
		return nil
	}
	tmp.Close()
	if out, err := serviceMutationCommand(ctx, "wg", "syncconf", wgIface, tmp.Name()).CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("wg syncconf failed: %s", firstLine(string(out)))
		return nil
	}
	resp.Applied = true
	return nil
}

type VPNPeerStat struct {
	PublicKey     string `json:"public_key"`
	LastHandshake int64  `json:"last_handshake"` // unix seconds, 0 = never
	RxBytes       int64  `json:"rx_bytes"`
	TxBytes       int64  `json:"tx_bytes"`
}

type VPNStatusResponse struct {
	Installed       bool          `json:"installed"`
	Configured      bool          `json:"configured"`
	Running         bool          `json:"running"`
	ServerPublicKey string        `json:"server_public_key,omitempty"`
	Port            int           `json:"port,omitempty"`
	Endpoint        string        `json:"endpoint,omitempty"` // server public IP
	Peers           []VPNPeerStat `json:"peers,omitempty"`
	Error           string        `json:"error,omitempty"`
}

// VPNStatus reports the real state of the WireGuard server: whether the tools
// are installed, the config exists, the interface is up, and per-peer live
// traffic/handshake counters from the kernel.
// VPNStatus, WireGuard sunucusunun gerçek durumunu bildirir: araçlar kurulu
// mu, config var mı, arayüz ayakta mı ve çekirdekten peer başına canlı
// trafik/el-sıkışma sayaçları.
func (a *Agent) VPNStatus(_ *struct{}, resp *VPNStatusResponse) error {
	if _, err := exec.LookPath("wg"); err != nil {
		return nil
	}
	resp.Installed = true
	if _, err := os.Stat(wgConfPath()); err != nil {
		return nil
	}
	resp.Configured = true
	resp.Endpoint = detectPublicIP()

	dump, err := exec.Command("wg", "show", wgIface, "dump").Output()
	if err != nil {
		// Interface down: still report key/port from the config so the panel
		// can show what a client would connect to.
		// Arayüz kapalı: panel yine de bağlanılacak bilgiyi gösterebilsin.
		resp.ServerPublicKey, resp.Port = wgConfIdentity()
		return nil
	}
	resp.Running = true
	for i, line := range strings.Split(strings.TrimSpace(string(dump)), "\n") {
		f := strings.Split(line, "\t")
		if i == 0 {
			// interface line: private-key, public-key, listen-port, fwmark
			if len(f) >= 3 {
				resp.ServerPublicKey = f[1]
				resp.Port, _ = strconv.Atoi(f[2])
			}
			continue
		}
		// peer line: public-key, psk, endpoint, allowed-ips, handshake, rx, tx, keepalive
		if len(f) >= 7 {
			hs, _ := strconv.ParseInt(f[4], 10, 64)
			rx, _ := strconv.ParseInt(f[5], 10, 64)
			tx, _ := strconv.ParseInt(f[6], 10, 64)
			resp.Peers = append(resp.Peers, VPNPeerStat{PublicKey: f[0], LastHandshake: hs, RxBytes: rx, TxBytes: tx})
		}
	}
	return nil
}

// wgConfIdentity derives the server public key and port from wg0.conf when
// the interface is down.
// wgConfIdentity, arayüz kapalıyken sunucu genel anahtarını ve portu
// wg0.conf'tan türetir.
func wgConfIdentity() (pub string, port int) {
	raw, err := os.ReadFile(wgConfPath())
	if err != nil {
		return "", 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "PrivateKey":
			if raw, err := base64.StdEncoding.DecodeString(v); err == nil && len(raw) == 32 {
				if p, err := curve25519.X25519(raw, curve25519.Basepoint); err == nil {
					pub = base64.StdEncoding.EncodeToString(p)
				}
			}
		case "ListenPort":
			port, _ = strconv.Atoi(v)
		}
	}
	return pub, port
}

// defaultRouteIface returns the interface of the default route ("eth0"-like),
// used for the NAT masquerade rule.
// defaultRouteIface, NAT masquerade kuralı için varsayılan rotanın arayüzünü
// döndürür.
func defaultRouteIface() string {
	out, err := exec.Command("ip", "-o", "route", "show", "default").Output()
	if err != nil {
		return "eth0"
	}
	f := strings.Fields(string(out))
	for i := range f {
		if f[i] == "dev" && i+1 < len(f) {
			return f[i+1]
		}
	}
	return "eth0"
}

// detectPublicIP returns the source address the kernel would use towards the
// internet — the address clients dial.
// detectPublicIP, çekirdeğin internete doğru kullanacağı kaynak adresi
// döndürür — istemcilerin aradığı adres.
func detectPublicIP() string {
	out, err := exec.Command("ip", "-o", "route", "get", "1.1.1.1").Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	for i := range f {
		if f[i] == "src" && i+1 < len(f) {
			return f[i+1]
		}
	}
	return ""
}

func validWGKey(s string) bool {
	raw, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(raw) == 32
}

func validPeerIP(s string) bool {
	if !strings.HasPrefix(s, "10.8.0.") {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "10.8.0."))
	return err == nil && n >= 2 && n <= 254
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
