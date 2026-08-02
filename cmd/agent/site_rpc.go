package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

const siteMutationLockStripeCount = 256

var siteMutationLockStripes [siteMutationLockStripeCount]sync.Mutex
var siteUsernameLockStripes [siteMutationLockStripeCount]sync.Mutex

func siteMutationMutex(siteID int) *sync.Mutex {
	return &siteMutationLockStripes[uint(siteID)%siteMutationLockStripeCount]
}

func siteUsernameMutex(username string) *sync.Mutex {
	var hash uint32 = 2166136261
	for i := 0; i < len(username); i++ {
		hash ^= uint32(username[i])
		hash *= 16777619
	}
	return &siteUsernameLockStripes[hash%siteMutationLockStripeCount]
}

type siteLifecycleOps struct {
	prepareChallengeRoot func(*ApplyVhostRequest) error
	pathExists           func(string) (bool, error)
	mkdirAll             func(string, os.FileMode) error
	lookupUser           func(string) (*user.User, error)
	createUser           func(string, string, string) (bool, error)
	deleteUser           func(string) error
	killUser             func(string) error
	setOwnership         func(string, string) error
	createPool           func(int, string, string) (string, error)
	deletePool           func(int, string) error
	writeFileExclusive   func(string, []byte, os.FileMode) error
	applyLayout          func(string, string) error
	applyVhost           func(string, string) error
	removeVhost          func(string) error
	removeAppUnit        func(int) error
	removeAll            func(string) error
}

func (a *Agent) resolvedSiteLifecycleOps() siteLifecycleOps {
	if a.siteOps != nil {
		return *a.siteOps
	}
	return siteLifecycleOps{
		prepareChallengeRoot: prepareValidatedVhostChallengeRoot,
		pathExists: func(path string) (bool, error) {
			_, err := os.Lstat(path)
			if err == nil {
				return true, nil
			}
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		},
		mkdirAll:     os.MkdirAll,
		lookupUser:   user.Lookup,
		createUser:   a.userManager.CreateUser,
		deleteUser:   a.userManager.DeleteUser,
		setOwnership: a.userManager.SetOwnership,
		createPool:   a.phpManager.CreatePool,
		deletePool:   a.phpManager.DeletePool,
		writeFileExclusive: func(path string, content []byte, mode os.FileMode) error {
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			if _, err := file.Write(content); err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return err
			}
			if err := file.Sync(); err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return err
			}
			return file.Close()
		},
		applyLayout: applyHostingLayout,
		applyVhost:  a.nginxGen.ApplyVhost,
		removeVhost: a.nginxGen.RemoveVhost,
		removeAppUnit: func(siteID int) error {
			var response AppApplyResponse
			if err := a.RemoveAppUnit(&AppControlRequest{SiteID: siteID}, &response); err != nil {
				return err
			}
			if response.Error != "" {
				return errors.New(response.Error)
			}
			return nil
		},
		killUser: func(username string) error {
			err := exec.Command("pkill", "-u", username).Run()
			if err == nil {
				time.Sleep(300 * time.Millisecond)
				return nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
				return nil
			}
			return err
		},
		removeAll: os.RemoveAll,
	}
}

type siteLifecycleFailure struct {
	step string
	err  error
}

func logSiteLifecycleFailures(action, domain string, failures []siteLifecycleFailure) {
	for _, failure := range failures {
		log.Printf("%s %s: %s: %v", action, domain, failure.step, failure.err)
	}
}

func unknownSiteUser(err error) bool {
	var unknown user.UnknownUserError
	return errors.As(err, &unknown)
}

func removeSiteUser(
	ops siteLifecycleOps,
	username, expectedHome string,
) error {
	account, err := ops.lookupUser(username)
	if err != nil {
		if unknownSiteUser(err) {
			return nil
		}
		return fmt.Errorf("lookup user: %w", err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil || uid < 1000 {
		return fmt.Errorf("refusing non-tenant uid %q", account.Uid)
	}
	if filepath.Clean(account.HomeDir) != expectedHome {
		return fmt.Errorf("refusing user whose home does not match the immutable site home")
	}
	if err := ops.killUser(username); err != nil {
		return fmt.Errorf("stop site processes: %w", err)
	}
	if err := ops.deleteUser(username); err != nil {
		return fmt.Errorf("delete site user: %w", err)
	}
	return nil
}

func (a *Agent) validatedCreateSiteRequest(
	req transport.CreateSiteRequest,
) (
	transport.CreateSiteRequest,
	*ApplyVhostRequest,
	services.RenderedVhost,
	error,
) {
	if req.SiteID <= 0 {
		return transport.CreateSiteRequest{}, nil, services.RenderedVhost{},
			fmt.Errorf("a positive site identity is required")
	}
	if err := hostingpath.ValidateDocumentRoot(
		req.DocumentRoot,
		req.SubscriptionID,
		req.DomainID,
	); err != nil {
		return transport.CreateSiteRequest{}, nil, services.RenderedVhost{},
			fmt.Errorf("refusing a document root outside the site's immutable home")
	}

	domain, err := hostname.CanonicalFQDN(req.Domain)
	if err != nil {
		return transport.CreateSiteRequest{}, nil, services.RenderedVhost{},
			fmt.Errorf("a valid canonical domain is required")
	}
	req.Domain = domain
	if req.Username != services.SiteUsername(domain) {
		return transport.CreateSiteRequest{}, nil, services.RenderedVhost{},
			fmt.Errorf("the system user does not match the immutable site identity")
	}
	if strings.TrimSpace(req.Password) == "" {
		return transport.CreateSiteRequest{}, nil, services.RenderedVhost{},
			fmt.Errorf("a site password is required")
	}
	if strings.TrimSpace(req.TempDomain) != "" {
		tempDomain, canonicalErr := hostname.CanonicalFQDN(req.TempDomain)
		if canonicalErr != nil {
			return transport.CreateSiteRequest{}, nil, services.RenderedVhost{},
				fmt.Errorf("temporary domain is invalid")
		}
		req.TempDomain = tempDomain
	}

	req.ProjectType = strings.ToLower(strings.TrimSpace(req.ProjectType))
	if req.ProjectType == "" {
		req.ProjectType = "php"
	}
	if req.ProjectType != "php" && req.ProjectType != "static" {
		return transport.CreateSiteRequest{}, nil, services.RenderedVhost{},
			fmt.Errorf("unsupported site creation project type")
	}

	phpSocket := ""
	if req.ProjectType == "php" {
		if err := services.ValidatePHPVersion(req.PHPVersion); err != nil {
			return transport.CreateSiteRequest{}, nil, services.RenderedVhost{}, err
		}
		phpSocket = fmt.Sprintf(
			"/var/run/php/php%s-fpm-site%d.sock",
			req.PHPVersion,
			req.SiteID,
		)
	}
	vhostReq := &ApplyVhostRequest{
		ExpectedBuildCommit: req.ExpectedBuildCommit,
		SiteID:              req.SiteID,
		SubscriptionID:      req.SubscriptionID,
		DomainID:            req.DomainID,
		Domain:              req.Domain,
		TempDomain:          req.TempDomain,
		DocumentRoot:        req.DocumentRoot,
		PHPSocket:           phpSocket,
		SSLType:             "none",
		ProjectType:         req.ProjectType,
	}
	rendered, err := a.renderValidatedVhost(vhostReq)
	if err != nil {
		return transport.CreateSiteRequest{}, nil, services.RenderedVhost{},
			fmt.Errorf("invalid site vhost request: %w", err)
	}
	return req, vhostReq, rendered, nil
}

func rollbackCreateSite(
	ops siteLifecycleOps,
	req transport.CreateSiteRequest,
	siteHome string,
	poolMayExist, userMayExist, homeMayExist bool,
) []siteLifecycleFailure {
	var failures []siteLifecycleFailure
	if poolMayExist {
		if err := ops.deletePool(req.SiteID, req.PHPVersion); err != nil {
			failures = append(failures, siteLifecycleFailure{"PHP-FPM pool rollback", err})
		}
	}
	if userMayExist {
		if err := removeSiteUser(ops, req.Username, siteHome); err != nil {
			failures = append(failures, siteLifecycleFailure{"system user rollback", err})
		}
	}
	if homeMayExist {
		if err := ops.removeAll(siteHome); err != nil {
			failures = append(failures, siteLifecycleFailure{"site files rollback", err})
		}
	}
	return failures
}

func failCreateSite(
	reply *transport.CreateSiteResponse,
	domain, stage string,
	cause error,
	rollbackFailures []siteLifecycleFailure,
) {
	reply.Success = false
	reply.ErrorMessage = "site provisioning failed during " + stage
	log.Printf("CreateSite %s: %s: %v", domain, stage, cause)
	if len(rollbackFailures) > 0 {
		reply.ErrorMessage += "; automatic rollback is incomplete"
		logSiteLifecycleFailures("CreateSite rollback", domain, rollbackFailures)
	}
}

// CreateSite handles site creation with all privileged operations
func (a *Agent) CreateSite(req transport.CreateSiteRequest, reply *transport.CreateSiteResponse) error {
	mutationMu := siteMutationMutex(req.SiteID)
	mutationMu.Lock()
	defer mutationMu.Unlock()

	if err := requireExpectedBuildCommit(
		req.ExpectedBuildCommit,
		"creating a site",
	); err != nil {
		reply.ErrorMessage = err.Error()
		return nil
	}
	req, vhostReq, rendered, err := a.validatedCreateSiteRequest(req)
	if err != nil {
		reply.ErrorMessage = err.Error()
		return nil
	}
	identityMu := siteUsernameMutex(req.Username)
	identityMu.Lock()
	defer identityMu.Unlock()

	ops := a.resolvedSiteLifecycleOps()
	siteHome, err := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	if err != nil {
		reply.ErrorMessage = "the immutable site identity is invalid"
		return nil
	}
	if exists, statErr := ops.pathExists(siteHome); statErr != nil {
		failCreateSite(reply, req.Domain, "site identity preflight", statErr, nil)
		return nil
	} else if exists {
		reply.ErrorMessage = "site provisioning refused because its immutable home already exists"
		return nil
	}
	if account, lookupErr := ops.lookupUser(req.Username); lookupErr == nil {
		log.Printf(
			"CreateSite %s: refusing existing user %q (uid %s, home %q)",
			req.Domain,
			req.Username,
			account.Uid,
			account.HomeDir,
		)
		reply.ErrorMessage = "site provisioning refused because its system user already exists"
		return nil
	} else if !unknownSiteUser(lookupErr) {
		failCreateSite(reply, req.Domain, "site identity preflight", lookupErr, nil)
		return nil
	}
	if err := ops.prepareChallengeRoot(vhostReq); err != nil {
		failCreateSite(reply, req.Domain, "ACME challenge preparation", err, nil)
		return nil
	}

	homeMayExist := false
	userMayExist := false
	poolMayExist := false
	fail := func(stage string, cause error) {
		failCreateSite(
			reply,
			req.Domain,
			stage,
			cause,
			rollbackCreateSite(
				ops,
				req,
				siteHome,
				poolMayExist,
				userMayExist,
				homeMayExist,
			),
		)
	}

	// 1. Create directory structure.
	homeMayExist = true
	err = ops.mkdirAll(req.DocumentRoot, 0o750)
	if err != nil {
		fail("document root creation", err)
		return nil
	}

	// 2. Create Linux user
	userMayExist, err = ops.createUser(req.Username, siteHome, req.Password)
	if err != nil {
		fail("system user creation", err)
		return nil
	}

	// 3. Set ownership
	if err := ops.setOwnership(siteHome, req.Username); err != nil {
		fail("site ownership", err)
		return nil
	}

	// 4. Create PHP-FPM pool — php sites only. The error is actionable: on a
	// minimal server PHP may simply not be installed, and the operator must
	// hear "install it or pick another type", not a bare path error.
	// 4. PHP-FPM havuzu — yalnız php siteleri. Hata eyleme dönüktür: minimal
	// bir sunucuda PHP hiç kurulu olmayabilir; operatör çıplak bir yol hatası
	// değil "kur ya da başka tip seç" duymalı.
	socket := ""
	if req.ProjectType == "php" {
		poolMayExist = true
		socket, err = ops.createPool(req.SiteID, req.Username, req.PHPVersion)
		if err != nil {
			fail("PHP-FPM pool creation", err)
			return nil
		}
	}
	if socket != vhostReq.PHPSocket {
		fail(
			"PHP-FPM socket verification",
			errors.New("generated socket did not match the immutable site identity"),
		)
		return nil
	}

	// 5. Create the placeholder page. Deliberately NOT phpinfo(): that leaks
	// paths, modules and settings to anyone who finds the fresh site. For php
	// sites the tiny PHP expression still proves PHP execution end-to-end;
	// static sites get plain HTML (no PHP anywhere in their path).
	// 5. Yer tutucu sayfayı oluştur. Bilerek phpinfo() DEĞİL: o, taze siteyi
	// bulan herkese yolları, modülleri ve ayarları sızdırır. php sitelerinde
	// küçük PHP ifadesi PHP'nin uçtan uca çalıştığını yine de kanıtlar; statik
	// siteler düz HTML alır (yollarında hiç PHP yok).
	placeholderName, indexContent := celikPanelSitePlaceholder(req.Domain, req.ProjectType)
	placeholderPath := filepath.Join(req.DocumentRoot, placeholderName)
	if err := ops.writeFileExclusive(placeholderPath, indexContent, 0o640); err != nil {
		fail("placeholder creation", err)
		return nil
	}
	if err := ops.setOwnership(placeholderPath, req.Username); err != nil {
		fail("placeholder ownership", err)
		return nil
	}

	// 5b. Hosting permission layout: web-server group access, setgid
	// docroot, traverse-only parents — after the placeholder exists so file
	// modes are covered too. Best-effort in dev where the agent is not
	// root; in production a failure surfaces as the site not serving and
	// the log line makes it diagnosable.
	// 5b. Barındırma izin düzeni: web sunucusu grubuna erişim, setgid
	// docroot, yalnız-geçişli üst dizinler — dosya kipleri de kapsansın
	// diye yer tutucu oluştuktan sonra. Agent'ın root olmadığı dev'de
	// en-iyi-çaba; üretimde hata sitenin yayınlanmaması olarak görünür ve
	// günlük satırı teşhis ettirir.
	if err := ops.applyLayout(req.DocumentRoot, req.Username); err != nil {
		fail("hosting permission layout", err)
		return nil
	}

	reply.NginxConfig = rendered.Config

	// 7. Apply, validate and reload as one serialized transaction. The safe
	// API restores and reactivates the previous vhost on every failure.
	err = ops.applyVhost(rendered.Domain, rendered.Config)
	if err != nil {
		fail("nginx vhost activation", err)
		return nil
	}

	reply.PHPSocket = socket
	reply.Success = true
	return nil
}

type DeleteSiteRequest = transport.DeleteSiteRequest
type DeleteSiteResponse = transport.DeleteSiteResponse

// DeleteSite tears down everything CreateSite built: app unit, vhost, PHP
// pool, system user and files. Idempotent — already-gone pieces are fine, so
// a half-failed delete can be retried. Because this runs as root, the paths
// and the user are strictly validated first: only site homes under the
// hosting base and only non-system users can be removed.
// DeleteSite, CreateSite'ın kurduğu her şeyi söker: uygulama unit'i, vhost,
// PHP havuzu, sistem kullanıcısı ve dosyalar. Idempotenttir — zaten olmayan
// parçalar sorun değildir; yarım kalmış silme yeniden denenebilir. Root
// olarak çalıştığı için önce yol ve kullanıcı sıkıca doğrulanır: yalnız
// barındırma kökü altındaki site dizinleri ve yalnız sistem-dışı
// kullanıcılar silinebilir.
func (a *Agent) DeleteSite(req *DeleteSiteRequest, resp *DeleteSiteResponse) error {
	if req == nil {
		resp.Error = "delete site request is required"
		return nil
	}
	mutationMu := siteMutationMutex(req.SiteID)
	mutationMu.Lock()
	defer mutationMu.Unlock()

	if err := requireExpectedBuildCommit(
		req.ExpectedBuildCommit,
		"deleting a site",
	); err != nil {
		resp.Error = err.Error()
		return nil
	}

	if req.SiteID <= 0 {
		resp.Error = "refusing to delete a site without a positive site identity"
		return nil
	}
	cleanHome, err := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	if err != nil {
		resp.Error = "refusing to delete a site without a valid immutable hosting identity"
		return nil
	}
	if req.SiteHome != "" && filepath.Clean(req.SiteHome) != cleanHome {
		resp.Error = "refusing a site home that does not match the immutable hosting identity"
		return nil
	}
	canonicalDomain, err := hostname.CanonicalFQDN(req.Domain)
	if err != nil || canonicalDomain != req.Domain {
		resp.Error = "refusing to delete a site without a canonical domain"
		return nil
	}
	expectedUsername := services.SiteUsername(canonicalDomain)
	if req.Username != expectedUsername {
		resp.Error = "refusing a system user that does not match the immutable site identity"
		return nil
	}
	if req.PHPVersion != "" {
		if err := services.ValidatePHPVersion(req.PHPVersion); err != nil {
			resp.Error = "refusing an invalid PHP version during site deletion"
			return nil
		}
	}
	if err := hostingpath.ValidateDocumentRoot(
		filepath.Join(cleanHome, "public_html"),
		req.SubscriptionID,
		req.DomainID,
	); err != nil {
		resp.Error = "refusing an inconsistent immutable hosting identity"
		return nil
	}
	identityMu := siteUsernameMutex(req.Username)
	identityMu.Lock()
	defer identityMu.Unlock()

	ops := a.resolvedSiteLifecycleOps()
	var failures []siteLifecycleFailure
	keepFailure := func(step string, err error) {
		if err != nil {
			failures = append(failures, siteLifecycleFailure{step, err})
		}
	}

	// 1. Remove, validate and reload the vhost as one rollback-capable nginx
	// transaction. If nginx cannot confirm the site is no longer served, do not
	// destroy any tenant resource behind that still-live route.
	if err := ops.removeVhost(req.Domain); err != nil {
		failures = append(failures, siteLifecycleFailure{"nginx vhost", err})
		logSiteLifecycleFailures("DeleteSite", req.Domain, failures)
		resp.Success = false
		resp.Error = "site cleanup incomplete: nginx vhost"
		return nil
	}

	// 2. Supervised app unit (node projects) — harmless when absent.
	keepFailure("application unit", ops.removeAppUnit(req.SiteID))

	// 3. PHP-FPM pool for this site.
	if req.PHPVersion != "" {
		keepFailure("PHP-FPM pool", ops.deletePool(req.SiteID, req.PHPVersion))
	}

	// 4. System user — never a system account, even if asked.
	// 4. Sistem kullanıcısı — istense bile asla bir sistem hesabı değil.
	if req.Username != "" {
		keepFailure("system user", removeSiteUser(ops, req.Username, cleanHome))
	}

	// 5. Files — userdel -r usually removed the home already.
	// 5. Dosyalar — userdel -r genelde home'u zaten kaldırdı.
	keepFailure("site files", ops.removeAll(cleanHome))

	if len(failures) > 0 {
		logSiteLifecycleFailures("DeleteSite", req.Domain, failures)
		steps := make([]string, 0, len(failures))
		for _, failure := range failures {
			steps = append(steps, failure.step)
		}
		resp.Success = false
		resp.Error = "site cleanup incomplete: " + strings.Join(steps, ", ")
		return nil
	}

	resp.Success = true
	return nil
}
