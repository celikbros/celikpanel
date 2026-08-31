package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/alicelik/celikpanel/internal/transport"
)

// DKIM key management. Keys are generated with pure Go crypto (no opendkim
// tooling required — the single-binary constitution), stored as PEM under
// dkimBaseDir, and the public key is handed back for the DNS TXT record.
// Whether a signing filter (opendkim/rspamd) is installed is reported
// honestly and separately: a key + DNS record is the necessary start, signing
// integration is its own step.
//
// DKIM anahtar yönetimi. Anahtarlar saf Go kriptosuyla üretilir (opendkim
// aracı gerekmez — tek-binary anayasası), dkimBaseDir altında PEM olarak
// saklanır ve genel anahtar DNS TXT kaydı için geri verilir. Bir imzalama
// filtresinin (opendkim/rspamd) kurulu olup olmadığı dürüstçe ve ayrıca
// raporlanır: anahtar + DNS kaydı gerekli başlangıçtır, imzalama entegrasyonu
// kendi adımıdır.

// dkimBaseDir: production default /var/lib/celikpanel-dkim/keys (root agent);
// CELIKPANEL_DKIM_DIR overrides for non-root development. It is deliberately
// NOT under /etc/celikpanel — see dkim_storage_migration.go for why that move
// matters and how an existing store is carried over.
// dkimBaseDir: üretim varsayılanı /var/lib/celikpanel-dkim/keys (root agent);
// CELIKPANEL_DKIM_DIR root olmayan geliştirme için geçersiz kılar. Bilerek
// /etc/celikpanel altında DEĞİLDİR — bu taşımanın neden önemli olduğu ve mevcut
// bir deponun nasıl aktarıldığı için bkz. dkim_storage_migration.go.
var dkimBaseDir = func() string {
	if d := os.Getenv("CELIKPANEL_DKIM_DIR"); d != "" {
		return d
	}
	return productionDKIMKeyDir
}()

var (
	dkimDomainRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	dkimSelectorRe = regexp.MustCompile(`^[a-z0-9]{1,32}$`)
	dkimKeyMu      sync.Mutex
)

type DKIMStatusRequest = transport.DKIMStatusRequest

type DKIMStatusResponse = transport.DKIMStatusResponse

type DKIMEnsureRequest = transport.DKIMEnsureRequest

type DKIMEnsureResponse = transport.DKIMEnsureResponse

// dkimKeyPath validates inputs and returns the private-key path. Validation
// lives on the agent because it is the privileged side: it must not trust the
// panel to keep names path-safe.
// dkimKeyPath, girdileri doğrular ve özel anahtar yolunu döndürür. Doğrulama
// agent'tadır çünkü ayrıcalıklı taraf odur: adların yol-güvenli olduğuna
// panele güvenmemelidir.
func dkimKeyPath(domain, selector string) (string, error) {
	if !dkimDomainRe.MatchString(domain) || len(domain) > 253 {
		return "", fmt.Errorf("invalid domain")
	}
	if !dkimSelectorRe.MatchString(selector) {
		return "", fmt.Errorf("invalid selector")
	}
	return filepath.Join(dkimBaseDir, domain, selector+".private"), nil
}

// dkimPublicB64 derives the base64 SubjectPublicKeyInfo (the p= value of the
// DKIM TXT record) from a private key.
// dkimPublicB64, bir özel anahtardan base64 SubjectPublicKeyInfo'yu (DKIM TXT
// kaydının p= değeri) türetir.
func dkimPublicB64(key *rsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

func readDKIMKey(path string) (*rsa.PrivateKey, error) {
	data, err := secureReadConfig(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", path)
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func signingFilterInstalled() bool {
	for _, bin := range []string{"opendkim", "rspamd"} {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
	}
	return false
}

// GetDKIMStatus reports whether a key exists (never creates one).
// GetDKIMStatus, bir anahtarın var olup olmadığını bildirir (asla oluşturmaz).
func (a *Agent) GetDKIMStatus(req *DKIMStatusRequest, resp *DKIMStatusResponse) error {
	resp.SigningInstalled = signingFilterInstalled()
	if req == nil {
		resp.Error = "DKIM status request is required"
		return nil
	}
	// Read the store only after it is where this binary believes it is.
	// Skipping this would answer "no key for this domain" while the key sits
	// in the old directory — and the panel would offer to mint a second one.
	// Depo, ancak bu ikilinin sandığı yerde olduktan sonra okunur. Bunu
	// atlamak, anahtar eski dizinde dururken "bu alan adı için anahtar yok"
	// yanıtını verirdi — ve panel ikinci bir anahtar üretmeyi önerirdi.
	if err := ensureDKIMStorageMigrated(); err != nil {
		resp.Error = err.Error()
		return nil
	}

	path, err := dkimKeyPath(req.Domain, req.Selector)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	key, err := readDKIMKey(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No key yet is a normal state, not an error.
			// Henüz anahtar olmaması normal bir durumdur, hata değildir.
			return nil
		}
		resp.Error = fmt.Sprintf("cannot read DKIM key: %v", err)
		return nil
	}
	pub, err := dkimPublicB64(key)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.HasKey = true
	resp.PublicKeyB64 = pub
	return nil
}

// EnsureDKIMKey creates a 2048-bit RSA key for the domain/selector if none
// exists, and returns the public part either way. Idempotent.
// EnsureDKIMKey, yoksa domain/selector için 2048-bit RSA anahtarı oluşturur
// ve her durumda genel kısmı döndürür. Bağımsızdır.
func (a *Agent) EnsureDKIMKey(req *DKIMEnsureRequest, resp *DKIMEnsureResponse) error {
	if req == nil {
		resp.Error = "DKIM key request is required"
		return nil
	}
	path, err := dkimKeyPath(req.Domain, req.Selector)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}

	dkimKeyMu.Lock()
	defer dkimKeyMu.Unlock()

	// Same reason as GetDKIMStatus, with a sharper consequence: generating a
	// key here while the real one is still in the old directory would replace
	// a published DNS record's key and break signing for that domain.
	// GetDKIMStatus ile aynı sebep, sonucu daha keskin: gerçek anahtar hâlâ
	// eski dizindeyken burada anahtar üretmek, yayımlanmış bir DNS kaydının
	// anahtarını değiştirir ve o alan adı için imzalamayı bozardı.
	if err := ensureDKIMStorageMigrated(); err != nil {
		resp.Error = err.Error()
		return nil
	}

	if key, err := readDKIMKey(path); err == nil {
		if err := secureChmodMailFile(path, 0o600); err != nil {
			resp.Error = fmt.Sprintf("cannot secure existing DKIM key: %v", err)
			return nil
		}
		pub, perr := dkimPublicB64(key)
		if perr != nil {
			resp.Error = perr.Error()
			return nil
		}
		resp.PublicKeyB64 = pub
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		resp.Error = fmt.Sprintf("cannot read existing DKIM key: %v", err)
		return nil
	}

	if err := secureMkdirAll(filepath.Dir(path), 0o700); err != nil {
		resp.Error = fmt.Sprintf("cannot create key directory: %v", err)
		return nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		resp.Error = fmt.Sprintf("key generation failed: %v", err)
		return nil
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := secureWriteConfig(path, pemBytes, 0o600); err != nil {
		resp.Error = fmt.Sprintf("cannot write key: %v", err)
		return nil
	}
	if err := secureChmodMailFile(path, 0o600); err != nil {
		resp.Error = fmt.Sprintf("cannot secure new DKIM key: %v", err)
		return nil
	}

	pub, err := dkimPublicB64(key)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Created = true
	resp.PublicKeyB64 = pub
	return nil
}
