package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// DKIM signing — the missing half of DKIM. The panel has long generated keys
// and published the DNS records, but nothing ever SIGNED outgoing mail, so
// receivers saw a key in DNS and no signature on the message: a fail, not a
// pass. OpenDKIM runs as a milter on localhost; its key and signing tables
// are generated from the panel's key directory, so every domain that has a
// key signs automatically. milter_default_action=accept means a dead filter
// degrades to unsigned mail, never to lost mail.
//
// DKIM imzalama — DKIM'in eksik yarısı. Panel uzun süredir anahtar üretiyor
// ve DNS kayıtlarını yayımlıyordu ama giden postayı hiçbir şey İMZALAMIYORDU;
// alıcılar DNS'te anahtar görüp iletide imza bulamıyordu: geçer değil, kalır.
// OpenDKIM, localhost'ta milter olarak koşar; anahtar ve imzalama tabloları
// panelin anahtar dizininden üretilir; anahtarı olan her domain otomatik
// imzalar. milter_default_action=accept: ölü filtre imzasız postaya düşer,
// asla kayıp postaya değil.

const signingSelector = "celik"

const (
	opendkimConfPath  = "/etc/opendkim.conf"
	opendkimTablesDir = "/etc/celikpanel/dkim-tables"
	opendkimSocket    = "inet:8891@localhost"
	opendkimMilter    = "inet:localhost:8891"
)

type ConfigureDKIMSigningResponse = transport.ConfigureDKIMSigningResponse

// ConfigureDKIMSigning installs OpenDKIM if needed, regenerates the tables
// from the key directory and wires the milter into Postfix. Idempotent —
// also the resync path whenever a new domain key is created.
// ConfigureDKIMSigning, gerekirse OpenDKIM'i kurar, tabloları anahtar
// dizininden yeniden üretir ve milter'ı Postfix'e bağlar. Idempotent —
// yeni bir domain anahtarı üretildiğinde de aynı yol yeniden senkronlar.
func (a *Agent) ConfigureDKIMSigning(req *ServiceMutationRequest, resp *ConfigureDKIMSigningResponse) error {
	*resp = ConfigureDKIMSigningResponse{}
	if req == nil {
		return fmt.Errorf("DKIM signing configuration request is required")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepConfigureDKIMSigning, "opendkim", "", "configure"),
	)
	if err != nil {
		*resp = ConfigureDKIMSigningResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	// CELIKPANEL_MAIL_DIR marks a non-root dev agent (CELIKPANEL_DKIM_DIR
	// alone does not: installed units set it to the production default).
	// CELIKPANEL_MAIL_DIR root olmayan dev agent'ı işaretler (tek başına
	// CELIKPANEL_DKIM_DIR etmez: kurulu unit'ler onu üretim varsayılanına
	// ayarlar).
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		resp.Error = "DKIM signing is a production action; not available with CELIKPANEL_MAIL_DIR set"
		return nil
	}
	if _, err := exec.LookPath("postconf"); err != nil {
		resp.Error = "postfix is not installed"
		return nil
	}
	if _, err := exec.LookPath("opendkim"); err != nil {
		family := detectPkgFamily()
		if family != "apt" {
			resp.Error = "opendkim cannot be installed automatically on this distro yet"
			return nil
		}
		if _, err := installPackagesContext(ctx, family, []string{"opendkim", "opendkim-tools"}); err != nil {
			resp.Error = fmt.Sprintf("opendkim install: %v", err)
			return nil
		}
	}

	domains, err := dkimSignedDomains()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	conf := fmt.Sprintf(`# Managed by CelikPanel — DKIM signing for hosted domains. Do not edit by hand.
Syslog			yes
UMask			007
Mode			sv
Canonicalization	relaxed/simple
OversignHeaders		From
Socket			%s
PidFile			/run/opendkim/opendkim.pid
UserID			opendkim
KeyTable		%s/keytable
SigningTable		refile:%s/signingtable
InternalHosts		%s/trustedhosts
TrustAnchorFile		/usr/share/dns/root.key
`, opendkimSocket, opendkimTablesDir, opendkimTablesDir, opendkimTablesDir)
	if err := writeDKIMTables(ctx, domains, conf); err != nil {
		resp.Error = err.Error()
		return nil
	}

	if out, err := serviceMutationCommand(ctx, "systemctl", "enable", "opendkim").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("opendkim enable: %s", firstLine(string(out)))
		return nil
	}
	if out, err := serviceMutationCommand(ctx, "systemctl", "restart", "opendkim").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("opendkim restart: %s", firstLine(string(out)))
		return nil
	}

	// Wire the milters through the ONE composer: writing smtpd_milters here
	// directly used to erase any spam filter already wired (last writer wins,
	// silently). applyMilterChain composes every installed filter instead.
	// Milter'ları TEK besteciden bağla: burada doğrudan smtpd_milters yazmak,
	// önceden bağlanmış bir spam filtresini siliyordu (son yazan kazanır,
	// sessizce). applyMilterChain bunun yerine kurulu her filtreyi birleştirir.
	if err := applyMilterChain(ctx); err != nil {
		resp.Error = err.Error()
		return nil
	}

	resp.Configured = true
	resp.Domains = len(domains)
	resp.Detail = fmt.Sprintf("DKIM signing active for %d domain(s)", len(domains))
	return nil
}

// dkimSignedDomains lists the domains that have a private key on disk.
// dkimSignedDomains, diskte özel anahtarı olan domain'leri listeler.
func dkimSignedDomains() ([]string, error) {
	base := dkimBaseDir
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var domains []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		key, err := dkimKeyPath(e.Name(), signingSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid DKIM key directory %q: %w", e.Name(), err)
		}
		exists, err := secureMailFileExists(key)
		if err != nil {
			return nil, fmt.Errorf("inspect DKIM private key for %s: %w", e.Name(), err)
		}
		if exists {
			domains = append(domains, e.Name())
		}
	}
	sort.Strings(domains)
	return domains, nil
}

// writeDKIMTables generates the OpenDKIM key/signing/trust tables and makes
// the private keys readable by the opendkim group (root keeps ownership).
// writeDKIMTables, OpenDKIM anahtar/imzalama/güven tablolarını üretir ve
// özel anahtarları opendkim grubunca okunur yapar (sahiplik root'ta kalır).
func writeDKIMTables(ctx context.Context, domains []string, conf string) error {
	if err := secureMkdirAll(opendkimTablesDir, 0o755); err != nil {
		return fmt.Errorf("create OpenDKIM table directory: %w", err)
	}
	if err := secureMkdirAll(dkimBaseDir, 0o750); err != nil {
		return fmt.Errorf("create DKIM key directory: %w", err)
	}
	// The whole directory chain must be traversable by opendkim, not just
	// the key file — a 0700 parent blocks the key silently. /etc/celikpanel
	// itself is root:celikpanel 0750, so opendkim joins that group instead
	// of us loosening the token directory.
	// Tüm dizin zinciri opendkim'ce geçilebilir olmalı, yalnız anahtar
	// dosyası değil — 0700 bir üst dizin anahtarı sessizce keser.
	// /etc/celikpanel'in kendisi root:celikpanel 0750; token dizinini
	// gevşetmek yerine opendkim o gruba katılır.
	if out, err := serviceMutationCommand(ctx, "usermod", "-aG", "celikpanel", "opendkim").CombinedOutput(); err != nil {
		return fmt.Errorf("add opendkim to celikpanel group: %w: %s", err, firstLine(string(out)))
	}
	opendkimGroup, err := user.LookupGroup("opendkim")
	if err != nil {
		return fmt.Errorf("look up opendkim group: %w", err)
	}
	opendkimGID, err := strconv.Atoi(opendkimGroup.Gid)
	if err != nil {
		return fmt.Errorf("parse opendkim group id %q: %w", opendkimGroup.Gid, err)
	}
	base := dkimBaseDir
	if err := secureSetMailDirectoryMetadata(base, 0o750, 0, opendkimGID); err != nil {
		return err
	}
	if err := secureSetMailDirectoryMetadata(opendkimTablesDir, 0o755, 0, 0); err != nil {
		return err
	}
	var kt, st strings.Builder
	for _, d := range domains {
		key, err := dkimKeyPath(d, signingSelector)
		if err != nil {
			return fmt.Errorf("invalid DKIM domain %q: %w", d, err)
		}
		fmt.Fprintf(&kt, "%s._domainkey.%s %s:%s:%s\n", signingSelector, d, d, signingSelector, key)
		fmt.Fprintf(&st, "*@%s %s._domainkey.%s\n", d, signingSelector, d)
		// OpenDKIM drops to its own user; give the group read access.
		// OpenDKIM kendi kullanıcısına düşer; gruba okuma izni ver.
		if err := secureSetMailFileMetadata(key, 0o640, 0, opendkimGID); err != nil {
			return fmt.Errorf("secure DKIM private key for %s: %w", d, err)
		}
		if err := secureSetMailDirectoryMetadata(filepath.Dir(key), 0o750, 0, opendkimGID); err != nil {
			return fmt.Errorf("secure DKIM key directory for %s: %w", d, err)
		}
	}
	trusted := "127.0.0.1\n::1\nlocalhost\n"
	return applyMailFileMutation(ctx, []mailFileWrite{
		{path: filepath.Join(opendkimTablesDir, "keytable"), content: []byte(kt.String()), mode: 0o644},
		{path: filepath.Join(opendkimTablesDir, "signingtable"), content: []byte(st.String()), mode: 0o644},
		{path: filepath.Join(opendkimTablesDir, "trustedhosts"), content: []byte(trusted), mode: 0o644},
		{path: opendkimConfPath, content: []byte(conf), mode: 0o644},
	}, nil, nil)
}
