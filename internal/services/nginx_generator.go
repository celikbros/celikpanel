package services

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostingpath"
)

//go:embed templates/nginx/vhost.conf.tmpl
var vhostTemplate string

type NginxGenerator struct {
	tmpl          *template.Template
	validateNginx func() error
	reloadNginx   func() error
	// writeVhostBatch is a test seam for failures that happen after a batch
	// item has partially touched the filesystem. Restores deliberately bypass
	// it so rollback can repair the injected forward-write failure.
	writeVhostBatch func(domain, config string) error
}

// nginxMutationMu serializes the complete nginx mutation transaction across
// every generator instance. nginx -t validates the whole configuration, so
// two otherwise unrelated vhost writes must never interleave between their
// snapshot, validation and reload steps.
var nginxMutationMu sync.Mutex

func NewNginxGenerator() (*NginxGenerator, error) {
	tmpl, err := template.New("vhost").Parse(vhostTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %v", err)
	}
	return &NginxGenerator{tmpl: tmpl}, nil
}

type VhostData struct {
	SiteID             int
	Domain             string
	TempDomain         string
	ServerNames        []string
	ACMEChallengeNames []string
	ACMEChallengeRoot  string
	DocumentRoot       string
	PHPSocket          string
	SSLType            string
	SSLCert            string
	SSLKey             string
	RedirectWWW        bool
	ForceHTTPS         bool
	HSTSEnabled        bool
	HSTSMaxAge         int
	// SSLAutoRedirect is kept for callers that still use the pre-settings
	// vhost API. New callers should use ForceHTTPS.
	SSLAutoRedirect bool
	// Project-type fields (roadmap 3A). Upstream is derived: node projects
	// proxy to 127.0.0.1:AppPort, proxy projects to ForwardTo.
	// Proje-tipi alanları (yol haritası 3A). Upstream türetilir: node
	// projeleri 127.0.0.1:AppPort'a, proxy projeleri ForwardTo'ya vekillenir.
	ProjectType string
	AppPort     int
	ForwardTo   string
	ForwardCode int
	Upstream    string
}

// Render executes the vhost template over prepared data, deriving the
// proxy upstream for node/proxy types.
// Render, hazırlanmış veriyle vhost şablonunu çalıştırır; node/proxy
// tiplerinde vekil upstream'ini türetir.
func (ng *NginxGenerator) Render(data VhostData) (string, error) {
	data.ServerNames = normalizedServerNames(data.Domain, data.TempDomain, data.ServerNames)
	data.ACMEChallengeNames = normalizedAdditionalServerNames(data.ACMEChallengeNames)
	if err := validateACMEChallengeRootForTemplate(
		data.ACMEChallengeRoot, data.DocumentRoot,
	); err != nil {
		return "", err
	}
	if !data.ForceHTTPS && data.SSLAutoRedirect {
		data.ForceHTTPS = true
	}
	if data.ProjectType == "" {
		data.ProjectType = "php"
	}
	switch data.ProjectType {
	case "php":
		// A php vhost without an FPM socket would render `unix:` with no
		// path — nginx rejects it. Refuse honestly instead of writing a
		// config that cannot work.
		// FPM soketi olmayan bir php vhost'u yolsuz `unix:` üretir — nginx
		// reddeder. Çalışamayacak bir yapılandırma yazmak yerine dürüstçe
		// reddet.
		if data.PHPSocket == "" {
			return "", fmt.Errorf("php project has no PHP-FPM socket configured for this site")
		}
	case "node":
		data.Upstream = fmt.Sprintf("http://127.0.0.1:%d", data.AppPort)
	case "proxy":
		data.Upstream = data.ForwardTo
	}
	if data.ForwardCode == 0 {
		data.ForwardCode = 301
	}

	var buf bytes.Buffer
	if err := ng.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}
	return buf.String(), nil
}

func validateACMEChallengeRootForTemplate(challengeRoot, documentRoot string) error {
	if challengeRoot == "" || !path.IsAbs(challengeRoot) ||
		path.Clean(challengeRoot) != challengeRoot ||
		strings.ContainsAny(challengeRoot, " \t\r\n;{}") {
		return fmt.Errorf("ACME challenge root must be an absolute canonical nginx path")
	}
	cleanDocumentRoot := path.Clean(documentRoot)
	if challengeRoot == cleanDocumentRoot ||
		strings.HasPrefix(challengeRoot, cleanDocumentRoot+"/") ||
		strings.HasPrefix(cleanDocumentRoot, challengeRoot+"/") {
		return fmt.Errorf("ACME challenge root must not overlap the tenant document root")
	}
	return nil
}

func normalizedAdditionalServerNames(serverNames []string) []string {
	result := make([]string, 0, len(serverNames))
	seen := make(map[string]struct{}, len(serverNames))
	for _, candidate := range serverNames {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(candidate), "."))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func normalizedServerNames(domain, tempDomain string, serverNames []string) []string {
	candidates := make([]string, 0, len(serverNames)+2)
	candidates = append(candidates, domain, tempDomain)
	candidates = append(candidates, serverNames...)

	result := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		name := strings.ToLower(strings.TrimSpace(candidate))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

// GenerateVhost generates nginx vhost config from site data
func (ng *NginxGenerator) GenerateVhost(site *core.Site, domain *core.Domain, tempDomain string) (string, error) {
	acmeChallengeRoot, err := hostingpath.ACMEChallengeRoot(
		domain.SubscriptionID, domain.ID,
	)
	if err != nil {
		return "", fmt.Errorf("derive ACME challenge root: %w", err)
	}
	// HTTPS is written ONLY when a certificate actually exists on disk.
	//
	// Asking for SSL used to be enough to emit the HTTPS server block, and that
	// made the checkbox self-defeating: at domain-creation time no certificate
	// has been issued yet (it cannot be — the domain must resolve here first),
	// so the template rendered `ssl_certificate ;` with no argument, `nginx -t`
	// refused the whole config, and the operator got "internal server error"
	// while trying to add a domain (biovision.health on Boston, 25 Jul). Worse,
	// the same branch turned port 80 into a 301 to HTTPS — so even a config
	// nginx accepted would have redirected the ACME http-01 challenge away and
	// the certificate could never be obtained. A chicken that ate its own egg.
	//
	// Now: no certificate → a plain HTTP vhost, which is exactly what ACME
	// needs. The site is regenerated with HTTPS the moment the certificate is
	// issued (applySiteVhost is the single regeneration path).
	//
	// HTTPS YALNIZ diskte gerçekten sertifika varken yazılır.
	//
	// Eskiden SSL istemek HTTPS sunucu bloğunu yazdırmaya yetiyordu ve bu,
	// kutuyu kendi kendini baltalayan bir şeye çeviriyordu: alan adı
	// oluşturulurken henüz sertifika verilmemiştir (verilemez de — alan adının
	// önce buraya çözülmesi gerekir), dolayısıyla şablon argümansız
	// `ssl_certificate ;` üretiyor, `nginx -t` tüm yapılandırmayı reddediyor ve
	// operatör alan adı eklemeye çalışırken "sunucu hatası" alıyordu
	// (Boston'da biovision.health, 25 Tem). Dahası aynı dal 80 portunu HTTPS'e
	// 301 yapıyordu — yani nginx kabul etse bile ACME http-01 doğrulaması
	// başka yere yönlendirilir ve sertifika hiçbir zaman alınamazdı. Kendi
	// yumurtasını yiyen tavuk.
	//
	// Artık: sertifika yoksa düz HTTP vhost — ACME'nin ihtiyacı olan tam da bu.
	// Sertifika verilir verilmez site HTTPS ile yeniden üretilir (tek yeniden
	// üretim yolu applySiteVhost).
	hasCert := site.SSLCertPath != nil && *site.SSLCertPath != "" &&
		site.SSLKeyPath != nil && *site.SSLKeyPath != ""
	sslType := "none"
	if site.SSLEnabled && hasCert {
		sslType = "custom"
	}

	data := VhostData{
		SiteID:            site.ID,
		Domain:            domain.Name,
		TempDomain:        tempDomain,
		ACMEChallengeRoot: acmeChallengeRoot,
		DocumentRoot:      site.DocumentRoot,
		ProjectType:       site.ProjectType, // empty → Render defaults to php
		SSLType:           sslType,
		// Redirecting to HTTPS before a certificate exists takes the site
		// offline. Redirect only once HTTPS can actually answer.
		// Sertifika yokken HTTPS'e yönlendirmek siteyi tümüyle kapatır. Ancak
		// HTTPS gerçekten cevap verebildiğinde yönlendir.
		SSLAutoRedirect: site.SSLEnabled && hasCert,
	}
	// A missing FPM socket must not panic vhost generation (non-PHP types).
	// Eksik FPM soketi vhost üretimini panikletmemeli (PHP dışı tipler).
	if site.PHPFPMSocket != nil {
		data.PHPSocket = *site.PHPFPMSocket
	}

	if hasCert {
		data.SSLCert = *site.SSLCertPath
		data.SSLKey = *site.SSLKeyPath
	}

	return ng.Render(data)
}

// nginxDir is /etc/nginx in production; CELIKPANEL_NGINX_DIR redirects vhost
// output for a non-root development agent. In that dev mode validate/reload
// are skipped (the files are not part of the live nginx config).
// nginxDir üretimde /etc/nginx'tir; CELIKPANEL_NGINX_DIR, root olmayan
// geliştirme agent'ı için vhost çıktısını yönlendirir. O dev modunda
// doğrulama/yeniden yükleme atlanır (dosyalar canlı nginx config'inin
// parçası değildir).
var nginxDir = os.Getenv("CELIKPANEL_NGINX_DIR")

func nginxDevMode() bool { return nginxDir != "" }

func vhostPaths(domain string) (available, enabled string) {
	base := "/etc/nginx"
	if nginxDevMode() {
		base = nginxDir
	}
	return fmt.Sprintf("%s/sites-available/%s.conf", base, domain),
		fmt.Sprintf("%s/sites-enabled/%s.conf", base, domain)
}

type vhostSnapshot struct {
	config  string
	exists  bool
	enabled bool
}

// RenderedVhost is an already trust-checked nginx vhost ready for the
// filesystem transaction. Agent RPC validation owns all untrusted inputs;
// this type keeps the generator batch API small and deterministic.
type RenderedVhost struct {
	Domain string
	Config string
}

func (ng *NginxGenerator) snapshotVhost(domain string) (vhostSnapshot, error) {
	filename, symlinkPath := vhostPaths(domain)
	config, err := os.ReadFile(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			return vhostSnapshot{}, fmt.Errorf("failed to snapshot vhost file: %w", err)
		}
	} else {
		returnSnapshot := vhostSnapshot{config: string(config), exists: true}
		if _, statErr := os.Lstat(symlinkPath); statErr != nil {
			if !os.IsNotExist(statErr) {
				return vhostSnapshot{}, fmt.Errorf("failed to snapshot enabled vhost: %w", statErr)
			}
		} else {
			returnSnapshot.enabled = true
		}
		return returnSnapshot, nil
	}

	if _, statErr := os.Lstat(symlinkPath); statErr != nil && !os.IsNotExist(statErr) {
		return vhostSnapshot{}, fmt.Errorf("failed to snapshot enabled vhost: %w", statErr)
	}
	return vhostSnapshot{}, nil
}

func (ng *NginxGenerator) restoreVhost(domain string, snapshot vhostSnapshot) error {
	if err := ng.deleteVhostFiles(domain); err != nil {
		return fmt.Errorf("remove mutated vhost: %w", err)
	}
	if !snapshot.exists {
		return nil
	}
	if err := ng.writeVhostFile(domain, snapshot.config); err != nil {
		return fmt.Errorf("restore vhost file: %w", err)
	}
	if !snapshot.enabled {
		_, symlinkPath := vhostPaths(domain)
		if err := os.Remove(symlinkPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("restore disabled vhost state: %w", err)
		}
	}
	return nil
}

func (ng *NginxGenerator) rollbackVhostMutation(domain string, snapshot vhostSnapshot, cause error) error {
	return ng.rollbackVhostMutationWithRuntime(
		domain, snapshot, cause, ng.ValidateNginx, ng.ReloadNginx,
	)
}

func (ng *NginxGenerator) rollbackVhostMutationWithRuntime(
	domain string,
	snapshot vhostSnapshot,
	cause error,
	validateNginx func() error,
	reloadNginx func() error,
) error {
	if err := ng.restoreVhost(domain, snapshot); err != nil {
		return fmt.Errorf("%w; rollback restore failed: %v", cause, err)
	}
	if err := validateNginx(); err != nil {
		return fmt.Errorf("%w; rollback validation failed: %v", cause, err)
	}
	if err := reloadNginx(); err != nil {
		return fmt.Errorf("%w; rollback reload failed: %v", cause, err)
	}
	return fmt.Errorf("%w; rollback restored and reloaded the previous vhost", cause)
}

func (ng *NginxGenerator) rollbackVhostMutations(
	touched []RenderedVhost,
	snapshots map[string]vhostSnapshot,
	cause error,
) error {
	var rollbackErrors []error
	for index := len(touched) - 1; index >= 0; index-- {
		item := touched[index]
		snapshot, exists := snapshots[item.Domain]
		if !exists {
			continue
		}
		if err := ng.restoreVhost(item.Domain, snapshot); err != nil {
			rollbackErrors = append(
				rollbackErrors,
				fmt.Errorf("restore %s: %w", item.Domain, err),
			)
		}
	}
	if len(rollbackErrors) > 0 {
		return fmt.Errorf(
			"%w; batch rollback incomplete: %v",
			cause,
			errors.Join(rollbackErrors...),
		)
	}
	if err := ng.ValidateNginx(); err != nil {
		return fmt.Errorf(
			"%w; batch rollback incomplete: rollback validation: %v",
			cause,
			err,
		)
	}
	if err := ng.ReloadNginx(); err != nil {
		return fmt.Errorf(
			"%w; batch rollback incomplete: rollback reload: %v",
			cause,
			err,
		)
	}
	return fmt.Errorf(
		"%w; rollback restored and reloaded all touched vhosts",
		cause,
	)
}

// ApplyVhosts atomically activates a complete vhost set from nginx's point of
// view. Every old file is snapshotted before the first write. All writes,
// the single nginx validation and the single reload are serialized with every
// other nginx mutation. Any failure restores only the attempted write prefix;
// trailing vhosts that were never touched remain byte-for-byte unchanged. The
// restored set is reloaded only after every restore and nginx validation pass.
func (ng *NginxGenerator) ApplyVhosts(vhosts []RenderedVhost) error {
	if len(vhosts) == 0 {
		return nil
	}

	nginxMutationMu.Lock()
	defer nginxMutationMu.Unlock()

	snapshots := make(map[string]vhostSnapshot, len(vhosts))
	for _, item := range vhosts {
		if _, exists := snapshots[item.Domain]; exists {
			return fmt.Errorf("duplicate vhost domain %q", item.Domain)
		}
		snapshot, err := ng.snapshotVhost(item.Domain)
		if err != nil {
			return fmt.Errorf("snapshot vhost %s: %w", item.Domain, err)
		}
		snapshots[item.Domain] = snapshot
	}
	touched := make([]RenderedVhost, 0, len(vhosts))
	for _, item := range vhosts {
		// Include the attempted item before calling the writer: a failure can
		// happen after its available file or enabled link was already changed.
		touched = append(touched, item)
		writeVhost := ng.writeVhostFile
		if ng.writeVhostBatch != nil {
			writeVhost = ng.writeVhostBatch
		}
		if err := writeVhost(item.Domain, item.Config); err != nil {
			return ng.rollbackVhostMutations(
				touched,
				snapshots,
				fmt.Errorf("write vhost %s: %w", item.Domain, err),
			)
		}
	}
	if err := ng.ValidateNginx(); err != nil {
		return ng.rollbackVhostMutations(
			touched,
			snapshots,
			fmt.Errorf("nginx batch validation failed: %w", err),
		)
	}
	if err := ng.ReloadNginx(); err != nil {
		return ng.rollbackVhostMutations(
			touched,
			snapshots,
			fmt.Errorf("nginx batch reload failed: %w", err),
		)
	}
	return nil
}

// ApplyVhost atomically applies one vhost from nginx's point of view:
// snapshot, write, validate and reload are serialized with all other vhost
// mutations. Any failure restores, validates and reloads the previous state.
func (ng *NginxGenerator) ApplyVhost(domain, config string) error {
	return ng.applyVhostWithRuntime(
		domain, config, ng.ValidateNginx, ng.ReloadNginx,
	)
}

// NginxCommandRunner lets an owning mutation supervisor execute nginx
// validation and reload without escaping its process tracker.
type NginxCommandRunner func(
	context.Context,
	string,
	...string,
) ([]byte, error)

// ApplyVhostWithCommandRunner preserves the same snapshot/rollback
// transaction as ApplyVhost while routing every subprocess through the
// caller-owned durable mutation runner.
func (ng *NginxGenerator) ApplyVhostWithCommandRunner(
	ctx context.Context,
	domain, config string,
	run NginxCommandRunner,
) error {
	if ctx == nil || run == nil {
		return errors.New("nginx mutation command context and runner are required")
	}
	return ng.applyVhostWithRuntime(
		domain,
		config,
		func() error { return ng.validateNginxWithCommandRunner(ctx, run) },
		func() error { return ng.reloadNginxWithCommandRunner(ctx, run) },
	)
}

func (ng *NginxGenerator) applyVhostWithRuntime(
	domain, config string,
	validateNginx func() error,
	reloadNginx func() error,
) error {
	nginxMutationMu.Lock()
	defer nginxMutationMu.Unlock()

	snapshot, err := ng.snapshotVhost(domain)
	if err != nil {
		return err
	}
	if err := ng.writeVhostFile(domain, config); err != nil {
		return ng.rollbackVhostMutationWithRuntime(
			domain, snapshot, fmt.Errorf("vhost write failed: %w", err),
			validateNginx, reloadNginx,
		)
	}
	if err := validateNginx(); err != nil {
		return ng.rollbackVhostMutationWithRuntime(
			domain, snapshot, fmt.Errorf("nginx validation failed: %w", err),
			validateNginx, reloadNginx,
		)
	}
	if err := reloadNginx(); err != nil {
		return ng.rollbackVhostMutationWithRuntime(
			domain, snapshot, fmt.Errorf("nginx reload failed: %w", err),
			validateNginx, reloadNginx,
		)
	}
	return nil
}

// RemoveVhost safely removes one vhost and reloads nginx. If remove,
// validation or reload fails, the previous vhost is restored and activated.
func (ng *NginxGenerator) RemoveVhost(domain string) error {
	nginxMutationMu.Lock()
	defer nginxMutationMu.Unlock()

	snapshot, err := ng.snapshotVhost(domain)
	if err != nil {
		return err
	}
	if err := ng.deleteVhostFiles(domain); err != nil {
		return ng.rollbackVhostMutation(
			domain, snapshot, fmt.Errorf("vhost removal failed: %w", err),
		)
	}
	if err := ng.ValidateNginx(); err != nil {
		return ng.rollbackVhostMutation(
			domain, snapshot, fmt.Errorf("nginx validation after vhost removal failed: %w", err),
		)
	}
	if err := ng.ReloadNginx(); err != nil {
		return ng.rollbackVhostMutation(
			domain, snapshot, fmt.Errorf("nginx reload after vhost removal failed: %w", err),
		)
	}
	return nil
}

// WriteAndValidateVhost remains as a compatibility name for older callers.
// The safe operation also reloads nginx so no caller can accidentally leave a
// validated-but-inactive vhost behind.
func (ng *NginxGenerator) WriteAndValidateVhost(domain, config string) error {
	return ng.ApplyVhost(domain, config)
}

// WriteVhostFile is the compatibility entry point for raw file writes. New
// production callers should use ApplyVhost so validation, reload and rollback
// stay in the same transaction. The global lock still prevents this legacy
// operation from interleaving with another nginx mutation.
func (ng *NginxGenerator) WriteVhostFile(domain string, config string) error {
	nginxMutationMu.Lock()
	defer nginxMutationMu.Unlock()
	return ng.writeVhostFile(domain, config)
}

func (ng *NginxGenerator) writeVhostFile(domain string, config string) error {
	filename, symlinkPath := vhostPaths(domain)
	availableDir := filepath.Dir(filename)
	if err := os.MkdirAll(availableDir, 0o755); err != nil {
		return err
	}
	if err := atomicWriteRegularFile(filename, []byte(config), 0o644); err != nil {
		return fmt.Errorf("failed to write vhost file: %w", err)
	}

	// Publish the enabled link with one rename. Remove+Symlink left a window
	// in which nginx could observe no enabled vhost at all.
	enabledDir := filepath.Dir(symlinkPath)
	if err := os.MkdirAll(enabledDir, 0o755); err != nil {
		return err
	}
	if err := atomicReplaceSymlink(symlinkPath, filename); err != nil {
		return fmt.Errorf("failed to replace enabled vhost link: %w", err)
	}

	return nil
}

func atomicWriteRegularFile(filename string, content []byte, mode os.FileMode) (returnErr error) {
	directory := filepath.Dir(filename)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(filename)+".tmp-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		if temporary != nil {
			_ = temporary.Close()
		}
		if returnErr != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	temporary = nil
	if err := os.Rename(temporaryName, filename); err != nil {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}

	return nil
}

func atomicReplaceSymlink(linkPath, target string) (returnErr error) {
	directory := filepath.Dir(linkPath)
	placeholder, err := os.CreateTemp(directory, "."+filepath.Base(linkPath)+".tmp-")
	if err != nil {
		return err
	}
	temporaryName := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		_ = os.Remove(temporaryName)
		return err
	}
	if err := os.Remove(temporaryName); err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := os.Symlink(target, temporaryName); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, linkPath); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

// ValidateNginx runs nginx -t to validate configuration. Skipped in dev
// mode: the redirected vhost files are not part of the live nginx config,
// so validating it would prove nothing about them.
// ValidateNginx, yapılandırmayı doğrulamak için nginx -t çalıştırır. Dev
// modunda atlanır: yönlendirilmiş vhost dosyaları canlı nginx config'inin
// parçası değildir; onu doğrulamak bunlar hakkında bir şey kanıtlamaz.
func (ng *NginxGenerator) ValidateNginx() error {
	if ng.validateNginx != nil {
		return ng.validateNginx()
	}
	if nginxDevMode() {
		return nil
	}
	output, err := runNginxCommand("nginx", "-t")
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return fmt.Errorf("nginx validation failed: %w", err)
		}
		return fmt.Errorf("nginx validation failed: %s: %w", detail, err)
	}
	return nil
}

func (ng *NginxGenerator) validateNginxWithCommandRunner(
	ctx context.Context,
	run NginxCommandRunner,
) error {
	if ng.validateNginx != nil {
		return ng.validateNginx()
	}
	output, err := run(ctx, "nginx", "-t")
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return fmt.Errorf("nginx validation failed: %w", err)
		}
		return fmt.Errorf("nginx validation failed: %s: %w", detail, err)
	}
	return nil
}

// ReloadNginx reloads nginx service
func (ng *NginxGenerator) ReloadNginx() error {
	if ng.reloadNginx != nil {
		return ng.reloadNginx()
	}
	if nginxDevMode() {
		return nil
	}
	output, err := runNginxCommand("systemctl", "reload", "nginx")
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return fmt.Errorf("nginx reload failed: %w", err)
		}
		return fmt.Errorf("nginx reload failed: %s: %w", detail, err)
	}
	return nil
}

func (ng *NginxGenerator) reloadNginxWithCommandRunner(
	ctx context.Context,
	run NginxCommandRunner,
) error {
	if ng.reloadNginx != nil {
		return ng.reloadNginx()
	}
	output, err := run(ctx, "systemctl", "reload", "nginx")
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return fmt.Errorf("nginx reload failed: %w", err)
		}
		return fmt.Errorf("nginx reload failed: %s: %w", detail, err)
	}
	return nil
}

// DeleteVhost is the compatibility entry point for raw file removal. New
// production callers should use RemoveVhost. Errors are never swallowed.
func (ng *NginxGenerator) DeleteVhost(domain string) error {
	nginxMutationMu.Lock()
	defer nginxMutationMu.Unlock()
	return ng.deleteVhostFiles(domain)
}

func (ng *NginxGenerator) deleteVhostFiles(domain string) error {
	filename, symlinkPath := vhostPaths(domain)
	var removeErrors []error
	if err := os.Remove(symlinkPath); err != nil && !os.IsNotExist(err) {
		removeErrors = append(removeErrors, fmt.Errorf("remove enabled vhost link: %w", err))
	}
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		removeErrors = append(removeErrors, fmt.Errorf("remove vhost file: %w", err))
	}
	return errors.Join(removeErrors...)
}
