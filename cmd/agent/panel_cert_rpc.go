package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// lookupGroupID resolves a system group's numeric gid.
// lookupGroupID, bir sistem grubunun sayısal gid'ini çözer.
func lookupGroupID(name string) (int, bool) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	return gid, err == nil
}

// The panel's own certificate. Out of the box the panel serves HTTPS with a
// self-signed certificate — safe, but every browser warns. This RPC turns the
// operator's "get a real certificate" click into: ensure certbot exists,
// issue via Let's Encrypt (standalone :80), install the result where the
// panel loads its TLS material from, and drop a certbot deploy hook so future
// automatic renewals reach the panel too. The panel restart that activates
// the new certificate is triggered by the panel itself after replying.
//
// Panelin kendi sertifikası. Panel kutudan çıktığında HTTPS'i kendinden
// imzalı sertifikayla sunar — güvenli ama her tarayıcı uyarır. Bu RPC,
// operatörün "gerçek sertifika al" tıklamasını şuna çevirir: certbot'un
// varlığını sağla, Let's Encrypt ile al (standalone :80), sonucu panelin TLS
// malzemesini yüklediği yere kur ve gelecekteki otomatik yenilemeler panele
// de ulaşsın diye bir certbot deploy kancası bırak. Yeni sertifikayı
// etkinleştiren panel yeniden başlatması, cevap verdikten sonra panelin
// kendisi tarafından tetiklenir.

// validPanelCertDomain: a plain FQDN — becomes a certbot arg and part of a
// filesystem path, so nothing but hostname characters may pass.
// validPanelCertDomain: düz bir FQDN — certbot argümanı ve dosya yolu parçası
// olur; makine adı karakterlerinden başkası geçemez.
var validPanelCertDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
var validPanelCertLineage = regexp.MustCompile(`^celikpanel-panel-[a-f0-9]{24}$`)
var validStagedSiteLineage = regexp.MustCompile(`^cp-site-[1-9][0-9]*-[a-f0-9]{24}$`)

const (
	managedPanelTLSDir   = "/var/lib/celikpanel/tls"
	panelACMEVhostPrefix = "celikpanel-panel-acme-"
)

var (
	certificateCleanupIsolatedStorage = isolatedSiteCertbotStorage
	certificateCleanupLegacyConfigDir = func() string { return legacyCertbotConfigDir }
	certificateCleanupLookPath        = exec.LookPath
	certificateCleanupRunCertbot      = func(args ...string) ([]byte, error) {
		return runPanelCertCommand(panelCertCleanupTimeout, "certbot", args...)
	}
	panelCertLookPath             = exec.LookPath
	panelCertRunCommand           = runPanelCertCommand
	panelCertRunMutationCommand   = runPanelCertMutationCommand
	panelCertPrepareChallengeRoot = preparePanelACMEChallengeRoot
	panelCertApplyVhost           = func(
		ctx context.Context,
		a *Agent,
		name, config string,
	) error {
		if a == nil || a.nginxGen == nil {
			return fmt.Errorf("nginx configuration manager is unavailable")
		}
		return a.nginxGen.ApplyVhostWithCommandRunner(
			ctx,
			name,
			config,
			func(
				commandCtx context.Context,
				command string,
				args ...string,
			) ([]byte, error) {
				return panelCertRunMutationCommand(
					commandCtx, panelCertSystemdTimeout, command, args...,
				)
			},
		)
	}
	panelCertInstallFiles    = installPanelCertFiles
	panelCertWriteDeployHook = writePanelCertDeployHook
	panelCertEnsureRenewal   = ensurePanelCertRenewalScheduler
	panelCertActiveIdentity  = activePanelCertificateIdentity
	panelCertWithPublishLock = withPanelCertPublishLock
	panelCertStageIssue      = stagePanelCertificateIssueMaterial
	panelCertCommitIssue     = commitStandalonePanelCertificateIssueStep
	panelCertDetectPkgFamily = detectPkgFamily
	panelCertInstallPackages = installPackagesContext
)

type IssuePanelCertRequest = transport.IssuePanelCertificateRequest
type IssuePanelCertResponse = transport.IssuePanelCertificateResponse
type IssuePanelCertV2Request = transport.IssuePanelCertificateV2Request
type IssuePanelCertV2Response = transport.IssuePanelCertificateV2Response

const issuePanelCertificateLegacyUnsupportedError = "Agent.IssuePanelCertificate is unsupported; use Agent.IssuePanelCertificateV2"

func validatePanelCertTLSDir(raw string) (string, error) {
	if raw != managedPanelTLSDir {
		return "", fmt.Errorf("invalid TLS directory")
	}
	return managedPanelTLSDir, nil
}

// IssuePanelCertificate is a zero-touch mixed-version compatibility endpoint.
func (a *Agent) IssuePanelCertificate(
	_ *IssuePanelCertRequest,
	resp *IssuePanelCertResponse,
) error {
	*resp = IssuePanelCertResponse{
		Error: issuePanelCertificateLegacyUnsupportedError,
	}
	return nil
}

func (a *Agent) IssuePanelCertificateV2(
	req *IssuePanelCertV2Request,
	resp *IssuePanelCertV2Response,
) error {
	*resp = IssuePanelCertV2Response{}
	if req == nil {
		resp.Error = "panel certificate request is required"
		return nil
	}
	commitment, err := mutationpayload.CanonicalPanelCertificateIssue(
		req.Domain,
		req.Email,
		req.TLSDir,
		req.ExpectedBuildCommit,
	)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := requireExpectedBuildCommit(
		commitment.ExpectedBuildCommit,
		"issue panel certificate",
	); err != nil {
		resp.Error = err.Error()
		return nil
	}
	authorizedReq := *req
	authorizedReq.Domain = commitment.Domain
	authorizedReq.Email = commitment.Email
	authorizedReq.TLSDir = commitment.TLSDir
	authorizedReq.ExpectedBuildCommit = commitment.ExpectedBuildCommit
	req = &authorizedReq
	domain := commitment.Domain
	stepCtx, finishStep, err := a.requiredServiceMutationStep(
		ServiceMutationBinding{
			MutationRequestID: req.MutationRequestID,
			MutationOwnerID:   req.MutationOwnerID,
		},
		newServiceMutationStepClaim(
			serviceMutationStepIssuePanelCertificate,
			commitment.Domain,
			commitment.Qualifier,
			"issue",
		),
	)
	if err != nil {
		*resp = IssuePanelCertV2Response{Error: err.Error()}
		return nil
	}
	defer finishStep()
	if !acquireSiteCertbot() {
		resp.Error = "another certificate operation is already in progress; retry shortly"
		return nil
	}
	defer releaseSiteCertbot()

	// certbot is installed on first use — a deliberate, user-initiated install
	// (the button says so), consistent with the minimal-install principle.
	// certbot ilk kullanımda kurulur — bilinçli, kullanıcı-tetikli bir kurulum
	// (düğme bunu söyler); minimal kurulum ilkesiyle tutarlı.
	family := panelCertDetectPkgFamily()
	certbotService := core.GetManagedServiceByID("certbot")
	if block, reason := core.ManagedServiceInstallBlock(
		certbotService, family,
	); block != core.ManagedServiceInstallBlockNone {
		resp.Error = reason
		return nil
	}
	certbotPackages := append([]string(nil), certbotService.Packages[family]...)
	if len(certbotPackages) == 0 {
		resp.Error = "certbot is not supported on this Linux distribution"
		return nil
	}
	installedCertbot := false
	if _, err := panelCertLookPath("certbot"); err != nil {
		if _, err := panelCertInstallPackages(
			stepCtx, family, certbotPackages,
		); err != nil {
			resp.Error = fmt.Sprintf("certbot install failed: %v", err)
			return nil
		}
		installedCertbot = true
	}

	// Standalone answers the HTTP-01 challenge on :80 itself — right for a
	// panel-only server. If a web server holds :80 the error says so honestly.
	// Standalone, HTTP-01 doğrulamasını :80'de kendisi cevaplar — yalnız-panel
	// sunucu için doğru olan. :80'i bir web sunucusu tutuyorsa hata bunu
	// dürüstçe söyler.
	challengeArgs, err := a.panelCertificateChallengeArgs(stepCtx, domain)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	args := append([]string{"certonly"}, challengeArgs...)
	args = append(args,
		"--preferred-challenges", "http",
		"--cert-name", panelCertLineageName(domain),
		"-d", domain,
		"--agree-tos", "--non-interactive", "--force-renewal",
	)
	args = append(args, "--email", req.Email)
	// Persist the source intent before Certbot starts, but do not hold the
	// publication lock while Certbot invokes deploy hooks. A previously
	// installed synchronous hook also takes this lock; holding it here would
	// deadlock Certbot against its own child hook. The durable mutation lease
	// remains owned for this entire RPC and every later privileged command.
	var intent panelCertificateActivationState
	if err := panelCertWithPublishLock(func() error {
		var err error
		intent, err = beginInteractivePanelCertificateIssuanceLocked(
			domain,
			req.MutationRequestID,
			commitment.Qualifier,
		)
		return err
	}); err != nil {
		if errors.Is(err, errPanelCertificateActivationPending) {
			resp.ErrorCode = transport.IssuePanelCertificateErrorActivationPending
		}
		resp.Error = fmt.Sprintf("begin panel certificate issuance: %v", err)
		return nil
	}

	out, issueErr := panelCertRunMutationCommand(
		stepCtx, panelCertIssueTimeout, "certbot", args...,
	)
	if issueErr != nil {
		cleanupErr := panelCertWithPublishLock(func() error {
			return clearPanelCertificateIssuanceIntentLocked(intent)
		})
		resp.Error = errors.Join(
			panelCertCommandError("certbot issue", out, issueErr),
			cleanupErr,
		).Error()
		return nil
	}

	var (
		issuedNotAfter time.Time
		hostPublished  bool
	)
	publishErr := panelCertWithPublishLock(func() (operationErr error) {
		if err := requirePanelCertificateIssuanceIntentLocked(intent); err != nil {
			return err
		}
		certificate, privateKey, leafDER, notAfter, err :=
			panelCertificateActivationReadSource(domain)
		if err != nil {
			return fmt.Errorf("read issued panel certificate source: %w", err)
		}
		state, err := bindPanelCertificateActivationMaterial(
			intent,
			leafDER,
			notAfter,
		)
		if err != nil {
			return err
		}
		if err := panelCertificateActivationWriteState(state); err != nil {
			return fmt.Errorf("bind issued panel certificate activation: %w", err)
		}
		cleanupState := true
		defer func() {
			if cleanupState || !hostPublished {
				operationErr = errors.Join(
					operationErr,
					clearInterruptedPanelCertificateActivation(
						req.MutationRequestID,
						commitment.Qualifier,
						domain,
					),
				)
			}
		}()
		if err := panelCertEnsureRenewal(stepCtx); err != nil {
			return err
		}
		if err := panelCertWriteDeployHook(domain, req.TLSDir); err != nil {
			return fmt.Errorf("install certbot deploy hook: %w", err)
		}
		receipt, err := newPanelCertificateIssueReceipt(
			req.MutationRequestID,
			commitment.Qualifier,
			domain,
			leafDER,
		)
		if err != nil {
			return err
		}
		stage, err := panelCertStageIssue(
			domain,
			req.TLSDir,
			certificate,
			privateKey,
			receipt,
		)
		if err != nil {
			return err
		}
		defer func() {
			operationErr = errors.Join(operationErr, stage.close())
		}()

		hostPublished, err = panelCertCommitIssue(stepCtx, stage.publish)
		if err != nil {
			if hostPublished {
				// Publication won the commit race. The manager either persisted
				// terminal success or retained the host lock for startup recovery.
				log.Printf(
					"Panel certificate host publication completed with receipt error: %v",
					err,
				)
				issuedNotAfter = notAfter
				cleanupState = false
				return nil
			}
			return err
		}
		if !hostPublished {
			return errors.New(
				"panel certificate commit completed without host publication",
			)
		}
		issuedNotAfter = notAfter
		cleanupState = false
		return nil
	})
	if publishErr != nil {
		var cleanupErr error
		if !hostPublished {
			cleanupErr = panelCertWithPublishLock(func() error {
				return clearInterruptedPanelCertificateActivation(
					req.MutationRequestID,
					commitment.Qualifier,
					domain,
				)
			})
		}
		resp.Error = fmt.Sprintf(
			"issue and publish panel certificate: %v",
			errors.Join(publishErr, cleanupErr),
		)
		return nil
	}
	wakePanelCertificateActivationReconciler()

	// Deploy hook: certbot's own timer renews the certificate; this hook
	// copies each renewal into the panel's TLS dir and restarts the panel, so
	// renewal needs no one to remember anything.
	// Deploy kancası: sertifikayı certbot'un kendi zamanlayıcısı yeniler; bu
	// kanca her yenilemeyi panelin TLS dizinine kopyalar ve paneli yeniden
	// başlatır — yenileme kimsenin bir şey hatırlamasını gerektirmez.
	// The hook and a working renewal scheduler were both verified before the
	// active certificate pair was published above.

	// "certbot's own timer" must actually exist and run: Debian's package
	// enables certbot.timer by itself, Arch ships certbot-renew.timer
	// DISABLED. Enable whichever this distro has — a certificate that
	// silently dies in 90 days is a trap, not a feature. Caught live on Arch.
	// "certbot'un kendi zamanlayıcısı" gerçekten var olmalı ve koşmalı:
	// Debian'ın paketi certbot.timer'ı kendisi etkinleştirir, Arch
	// certbot-renew.timer'ı KAPALI getirir. Bu dağıtımda hangisi varsa aç —
	// 90 günde sessizce ölen sertifika özellik değil tuzaktır. Arch'ta
	// canlıda yakalandı.
	resp.Issued = true
	resp.ExpiresAt = issuedNotAfter
	if installedCertbot {
		resp.Detail = "certbot installed"
	}
	return nil
}

func (a *Agent) panelCertificateChallengeArgs(
	ctx context.Context,
	domain string,
) ([]string, error) {
	if _, err := panelCertLookPath("nginx"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return []string{"--standalone"}, nil
		}
		return nil, fmt.Errorf("inspect nginx availability: %w", err)
	}

	output, err := panelCertRunMutationCommand(
		ctx, panelCertSystemdTimeout,
		"systemctl", "is-active", "--quiet", "nginx",
	)
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			return nil, fmt.Errorf(
				"nginx is installed but not active (%s); start nginx and retry: %w",
				detail, err,
			)
		}
		return nil, fmt.Errorf(
			"nginx is installed but not active; start nginx and retry: %w", err,
		)
	}

	challengeRoot, err := panelCertPrepareChallengeRoot()
	if err != nil {
		return nil, fmt.Errorf("prepare panel ACME challenge root: %w", err)
	}
	config := renderPanelACMEChallengeVhost(domain, challengeRoot)
	if err := panelCertApplyVhost(
		ctx, a, panelACMEVhostName(domain), config,
	); err != nil {
		return nil, fmt.Errorf("publish panel ACME nginx vhost: %w", err)
	}
	return []string{"--webroot", "--webroot-path", challengeRoot}, nil
}

// panelACMEVhostName gives every candidate panel hostname an independent
// challenge vhost. A failed A -> B certificate attempt must not overwrite the
// still-active A renewal route before B has been issued and published.
func panelACMEVhostName(domain string) string {
	sum := sha256.Sum256([]byte(domain))
	return fmt.Sprintf("%s%x", panelACMEVhostPrefix, sum[:12])
}

func panelCertLineageName(domain string) string {
	sum := sha256.Sum256([]byte(domain))
	return fmt.Sprintf("celikpanel-panel-%x", sum[:12])
}

func renderPanelACMEChallengeVhost(domain, challengeRoot string) string {
	return fmt.Sprintf(
		"# Managed by CelikPanel. Kept active for certbot renewals.\n"+
			"server {\n"+
			"    listen 80;\n"+
			"    listen [::]:80;\n"+
			"    server_name %s;\n\n"+
			"    location ^~ /.well-known/acme-challenge/ {\n"+
			"        root %s;\n"+
			"        default_type text/plain;\n"+
			"        try_files $uri =404;\n"+
			"    }\n\n"+
			"    location / {\n"+
			"        return 404;\n"+
			"    }\n"+
			"}\n",
		domain, challengeRoot,
	)
}

// installPanelCertFiles copies the live LE material over the panel's
// cert/key pair: root-owned, group-readable so the low-privilege panel
// (celikpanel group) can load it but nobody else can read the key.
// installPanelCertFiles, canlı LE malzemesini panelin cert/key çiftinin
// üzerine kopyalar: root sahipli, grup-okunur — düşük yetkili panel
// (celikpanel grubu) yükleyebilsin ama anahtarı başkası okuyamasın.
func ensurePanelCertRenewalScheduler(ctx context.Context) error {
	var failures []string
	for _, timer := range []string{"certbot.timer", "certbot-renew.timer"} {
		output, err := panelCertRunMutationCommand(
			ctx, panelCertSystemdTimeout,
			"systemctl", "enable", "--now", timer,
		)
		if err == nil {
			return nil
		}
		failures = append(
			failures,
			panelCertCommandError("enable "+timer, output, err).Error(),
		)
	}
	return fmt.Errorf(
		"certificate was issued but automatic renewal could not be enabled: %s",
		strings.Join(failures, "; "),
	)
}

type DeleteCertLineageRequest = transport.DeleteCertLineageRequest
type DeleteCertLineageResponse = transport.DeleteCertLineageResponse

// DeleteCertLineage removes only explicitly authorized CelikPanel
// customer-site lineages and one verified, exact immutable snapshot version.
// It never removes a domain snapshot root or deletes from the global
// /etc/letsencrypt store, where panel and operator-owned lineages may live.
// DeleteCertLineage yalnız açıkça yetkilendirilmiş CelikPanel müşteri-site
// lineage'larını ve doğrulanmış tek bir değişmez snapshot sürümünü siler.
// Domain snapshot kökünü veya panel/operatör lineage'larının bulunabileceği
// global /etc/letsencrypt deposunu asla silmez.
func (a *Agent) DeleteCertLineage(req *DeleteCertLineageRequest, resp *DeleteCertLineageResponse) error {
	if req == nil {
		resp.Error = "certificate lineage deletion request is required"
		return nil
	}
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "delete certificate lineage"); err != nil {
		resp.Error = err.Error()
		return nil
	}
	domain := strings.TrimSpace(req.Domain)
	if domain != "" {
		var err error
		domain, err = canonicalCertificateDomain(domain)
		if err != nil {
			resp.Error = "invalid domain name"
			return nil
		}
	}
	snapshotPath := strings.TrimSpace(req.SnapshotPath)
	if snapshotPath != req.SnapshotPath {
		resp.Error = "managed certificate snapshot path must be canonical"
		return nil
	}
	if req.DeleteCanonical && domain == "" {
		resp.Error = "canonical lineage deletion requires a domain"
		return nil
	}
	if snapshotPath != "" && domain == "" {
		resp.Error = "snapshot deletion requires a domain"
		return nil
	}
	if !req.DeleteCanonical && len(req.LineageNames) == 0 &&
		snapshotPath == "" {
		resp.Error = "a canonical lineage, staged lineage, or exact snapshot is required"
		return nil
	}
	if len(req.LineageNames) > 100 {
		resp.Error = "too many staged lineages"
		return nil
	}
	seen := make(map[string]struct{}, len(req.LineageNames))
	normalizedLineages := make([]string, 0, len(req.LineageNames))
	for _, raw := range req.LineageNames {
		lineage := strings.ToLower(strings.TrimSpace(raw))
		if !validStagedSiteLineage.MatchString(lineage) {
			resp.Error = "invalid staged lineage name"
			return nil
		}
		if _, ok := seen[lineage]; ok {
			continue
		}
		seen[lineage] = struct{}{}
		normalizedLineages = append(normalizedLineages, lineage)
	}
	var snapshotVersionDir string
	if snapshotPath != "" {
		var err error
		snapshotVersionDir, err = verifyManagedCertificateVersionPath(
			domain, snapshotPath,
		)
		if err != nil {
			resp.Error = fmt.Sprintf(
				"refuse unsafe immutable certificate snapshot cleanup: %v", err,
			)
			return nil
		}
	}
	if !acquireSiteCertbot() {
		resp.Error = "another site certificate operation is already running; retry shortly"
		return nil
	}
	defer releaseSiteCertbot()

	deleteLineage := func(storage certbotStorage, lineage string) error {
		if !certbotLineageExists(storage.ConfigDir, lineage) {
			return nil
		}
		if _, err := certificateCleanupLookPath("certbot"); err != nil {
			return fmt.Errorf("certbot lineage exists but certbot is missing")
		}
		args := []string{"delete"}
		args = append(args, storage.commandArgs()...)
		args = append(args, "--cert-name", lineage, "--non-interactive")
		out, err := certificateCleanupRunCertbot(args...)
		if err != nil {
			return panelCertCommandError("certbot delete", out, err)
		}
		resp.Deleted = true
		return nil
	}

	var cleanupErrors []string
	isolated := certificateCleanupIsolatedStorage()
	if req.DeleteCanonical {
		// The canonical domain lineage is removed only from CelikPanel's
		// isolated store. A same-named global lineage may be the panel
		// certificate or operator-owned legacy material.
		if err := deleteLineage(isolated, domain); err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		}
	}

	legacy := isolated
	legacy.ConfigDir = certificateCleanupLegacyConfigDir()
	for _, lineage := range normalizedLineages {
		if err := deleteLineage(isolated, lineage); err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		}
		// A staged reissue of an upgraded legacy certificate deliberately
		// reuses that legacy account store. The random, agent-generated prefix
		// makes this exact deletion safe without exposing arbitrary names.
		if err := deleteLineage(legacy, lineage); err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		}
	}

	if snapshotVersionDir != "" {
		versionRoot, err := customCertificateDirectory(domain)
		if err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		} else {
			managedRoot := filepath.Dir(versionRoot)
			relativeVersion, err := filepath.Rel(managedRoot, snapshotVersionDir)
			if err != nil || relativeVersion == "." || relativeVersion == ".." ||
				strings.HasPrefix(relativeVersion, ".."+string(filepath.Separator)) {
				cleanupErrors = append(
					cleanupErrors,
					"refuse snapshot cleanup outside the managed certificate root",
				)
			} else if err := secureDeleteManagedCertificateSnapshot(
				managedRoot, filepath.ToSlash(relativeVersion),
			); err != nil {
				cleanupErrors = append(
					cleanupErrors,
					fmt.Sprintf(
						"remove exact immutable certificate snapshot: %v", err,
					),
				)
			} else {
				resp.Deleted = true
			}
		}
	}
	if len(cleanupErrors) != 0 {
		resp.Error = strings.Join(cleanupErrors, "; ")
	}
	return nil
}

type RestartPanelSoonRequest = transport.RestartPanelSoonRequest

// RestartPanelSoon wakes the durable activation reconciler. The reconciler
// owns the mutation lease, publication lock, restart and exact listener proof.
// RestartPanelSoon dayanıklı etkinleştirme uzlaştırıcısını uyandırır.
func (a *Agent) RestartPanelSoon(req *RestartPanelSoonRequest, resp *bool) error {
	if req == nil {
		return fmt.Errorf("panel restart request is required")
	}
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "restart panel"); err != nil {
		return err
	}
	wakePanelCertificateActivationReconciler()
	*resp = true
	return nil
}

func schedulePanelRestart() error {
	wakePanelCertificateActivationReconciler()
	return nil
}

// schedulePanelCertificateRestart is retained for compatibility. It wakes the
// durable reconciler and never launches an untracked detached process.
func schedulePanelCertificateRestart(ctx context.Context) error {
	if ctx == nil {
		return errors.New("panel certificate activation context is required")
	}
	wakePanelCertificateActivationReconciler()
	return nil
}

func restartPanelAfterCertificatePublish() error {
	manager, err := agentServiceMutationManager()
	if err != nil {
		if errors.Is(err, errServiceMutationHostBusy) {
			return nil
		}
		return err
	}
	err = reconcilePanelCertificateActivationOnce(context.Background(), manager)
	if errors.Is(err, errPanelCertificateActivationBusy) {
		return nil
	}
	return err
}

// panelCertExpiry parses NotAfter from a PEM certificate, zero on any failure.
// panelCertExpiry, PEM sertifikadan NotAfter okur; hata durumunda sıfır.
func panelCertExpiry(path string) time.Time {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}
	}
	return cert.NotAfter
}

// certbotFirstError extracts certbot's actual complaint from its output.
// certbotFirstError, certbot'un asıl şikâyetini çıktısından ayıklar.
func certbotFirstError(out string) string {
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "Problem") || strings.Contains(l, "Error") ||
			strings.Contains(l, "error") || strings.Contains(l, "failed") ||
			strings.Contains(l, "Invalid") || strings.Contains(l, "Timeout") {
			return l
		}
	}
	return firstLine(out)
}
