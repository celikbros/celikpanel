package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/parser"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/systemd"
	"github.com/alicelik/celikpanel/internal/systemsqlite"
	"github.com/alicelik/celikpanel/internal/transport"
	"log"
	"net/rpc"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type Agent struct {
	parser       *parser.NginxParser
	systemdMgr   *systemd.Manager
	nginxGen     *services.NginxGenerator
	phpManager   *services.PHPFPMManager
	userManager  *services.UserManager
	sqliteAdmin  *systemsqlite.Manager
	siteOps      *siteLifecycleOps
	configMu     sync.Mutex
	configReload func(string) error
}

// RPC Methods Implementation

func (a *Agent) GetServices(args *transport.Empty, reply *[]core.Service) error {
	services, err := a.systemdMgr.ListServices()
	if err != nil {
		return err
	}
	*reply = services
	return nil
}

func (a *Agent) GetConfig(args *transport.GetConfigArgs, reply *transport.ConfigResponse) error {
	if args == nil || reply == nil {
		return errors.New(`config read requires a path and response`)
	}
	*reply = transport.ConfigResponse{}

	// The agent runs as root, so reads and writes must share the same
	// catalogue-derived path boundary. Otherwise this RPC becomes an arbitrary
	// root file reader for any caller that reaches the panel endpoint.
	path, err := configWriteAllowed(args.Path)
	if err != nil {
		log.Printf(`config read REFUSED %s: %v`, args.Path, err)
		if reply.Error = configRPCError(err); reply.Error != nil {
			return nil
		}
		return err
	}

	content, err := secureReadConfig(path)
	if err != nil {
		err = fmt.Errorf("failed to read file: %w", err)
		if reply.Error = configRPCError(err); reply.Error != nil {
			return nil
		}
		return err
	}

	reply.Content = string(content)

	// Try to parse if it's an Nginx file
	if strings.Contains(args.Path, "nginx") {
		parsed, _ := a.parser.Parse(string(content))
		reply.Parsed = fmt.Sprintf("%v", parsed)
	}

	return nil
}

func (a *Agent) UpdateConfig(args *transport.UpdateConfigArgs, reply *transport.UpdateConfigResponse) error {
	if args == nil || reply == nil {
		return errors.New("config update requires a path, content and response")
	}
	*reply = transport.UpdateConfigResponse{}

	// The path is judged by configWriteAllowed, which cleans it, refuses
	// symlinks, protects the machine's own keys/units/cron, and otherwise
	// accepts only files the scanner discovered for a catalogue component.
	// The previous check was a bare prefix test that "/etc/../root/.ssh/
	// authorized_keys" passed.
	// Yolu configWriteAllowed yargılar: temizler, sembolik bağları reddeder,
	// makinenin kendi anahtar/unit/cron dosyalarını korur ve bunun dışında
	// yalnız tarayıcının bir katalog bileşeni için bulduğu dosyaları kabul
	// eder. Önceki denetim, "/etc/../root/.ssh/authorized_keys"in geçtiği
	// çıplak bir önek sınavıydı.
	path, err := configWriteAllowed(args.Path)
	if err != nil {
		log.Printf("config write REFUSED %s: %v", args.Path, err)
		if reply.Error = configRPCError(err); reply.Error != nil {
			return nil
		}
		return err
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	log.Printf("Updating config %s", path)

	reload := a.configReload
	if reload == nil {
		if a.systemdMgr == nil {
			reload = func(string) error { return errors.New("systemd manager unavailable") }
		} else {
			reload = a.systemdMgr.Reload
		}
	}
	if err := applyConfigUpdate(path, []byte(args.Content), configValidator(path), reload); err != nil {
		if reply.Error = configRPCError(err); reply.Error != nil {
			return nil
		}
		return err
	}

	reply.Success = true
	return nil
}

// applyConfigUpdate atomically publishes one file, validates it and reloads
// the owning service. A failed validation or reload restores the previous
// bytes atomically; reload failure also reloads the restored configuration so
// disk and the running daemon cannot silently diverge.
func applyConfigUpdate(path string, content []byte, validator *validatorSpec, reload func(string) error) error {
	previous, hadPrevious := []byte(nil), false
	if existing, err := secureReadConfig(path); err == nil {
		previous, hadPrevious = existing, true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read existing file safely: %w", err)
	}
	restore := func() error {
		if hadPrevious {
			return secureWriteConfig(path, previous, 0o644)
		}
		if err := secureRemoveConfig(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}

	if err := secureWriteConfig(path, content, 0o644); err != nil {
		if rollbackErr := restore(); rollbackErr != nil {
			return fmt.Errorf("failed to write file: %v; rollback also failed: %v", err, rollbackErr)
		}
		return fmt.Errorf("failed to write file: %w", err)
	}

	if validator == nil {
		return nil
	}
	if out, err := validator.check(); err != nil {
		if rollbackErr := restore(); rollbackErr != nil {
			return fmt.Errorf("%s: %s; rollback failed: %v", validator.name, firstLine(out), rollbackErr)
		}
		return fmt.Errorf("%w (%s): %s", errConfigValidationFail, validator.name, firstLine(out))
	}
	if validator.reload == "" {
		return nil
	}
	if reload == nil {
		if rollbackErr := restore(); rollbackErr != nil {
			return fmt.Errorf("reload %s unavailable; rollback failed: %v", validator.reload, rollbackErr)
		}
		return fmt.Errorf("reload %s unavailable; previous configuration restored", validator.reload)
	}
	if err := reload(validator.reload); err != nil {
		rollbackErr := restore()
		if rollbackErr != nil {
			return fmt.Errorf("reload %s failed: %v; rollback failed: %v", validator.reload, err, rollbackErr)
		}
		if out, checkErr := validator.check(); checkErr != nil {
			return fmt.Errorf("reload %s failed: %v; previous configuration was restored but no longer validates: %s", validator.reload, err, firstLine(out))
		}
		if oldReloadErr := reload(validator.reload); oldReloadErr != nil {
			return fmt.Errorf("reload %s failed: %v; previous configuration was restored but its reload failed: %w", validator.reload, err, oldReloadErr)
		}
		return fmt.Errorf("reload %s failed: %v; previous configuration restored and reloaded", validator.reload, err)
	}
	return nil
}

// configValidator picks the syntax check for a config file, plus the unit to
// reload once it passes. Nil means "no validator known for this file".
// configValidator, bir yapılandırma dosyasının sözdizim denetimini ve geçince
// yeniden yüklenecek unit'i seçer. Nil, "bu dosya için bilinen doğrulayıcı
// yok" demektir.
type validatorSpec struct {
	name   string
	reload string
	check  func() (string, error)
}

func configValidator(path string) *validatorSpec {
	run := func(name string, arg ...string) func() (string, error) {
		return func() (string, error) {
			out, err := exec.Command(name, arg...).CombinedOutput()
			return string(out), err
		}
	}
	switch {
	case strings.Contains(path, "/nginx/"):
		return &validatorSpec{name: "nginx", reload: "nginx", check: run("nginx", "-t")}
	case strings.Contains(path, "/postfix/"):
		return &validatorSpec{name: "postfix", reload: "postfix", check: run("postfix", "check")}
	case strings.Contains(path, "/dovecot"):
		return &validatorSpec{name: "dovecot", reload: "dovecot", check: run("doveconf", "-n")}
	case strings.Contains(path, "/apache2/") || strings.Contains(path, "/httpd/"):
		return &validatorSpec{name: "apache", reload: "", check: run("apachectl", "configtest")}
	}
	return nil
}

// ServiceMutationActionRequest carries the durable lease for a managed
// service lifecycle action performed as part of a larger mutation.
// ServiceMutationActionRequest, daha büyük bir değişikliğin parçası olarak
// yapılan yönetilen servis yaşam döngüsü eyleminin kalıcı kirasını taşır.
type ServiceMutationActionRequest = transport.ServiceMutationActionRequest

func (a *Agent) ServiceMutationAction(req *ServiceMutationActionRequest, reply *transport.ServiceActionResult) error {
	*reply = transport.ServiceActionResult{}
	if req == nil {
		return fmt.Errorf("service mutation action request is required")
	}
	name := strings.TrimSuffix(strings.TrimSpace(req.ServiceName), ".service")
	if name == "" || core.ServiceForUnit(name) == nil {
		reply.Error = "unknown managed service"
		return nil
	}
	action := req.Action
	switch action {
	case "start", "stop", "restart", "reload":
	default:
		reply.Error = "invalid service action"
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepServiceAction, name, "", action),
	)
	if err != nil {
		*reply = transport.ServiceActionResult{Error: err.Error()}
		return nil
	}
	defer finishStep()
	return a.serviceActionContext(ctx, name, action, reply)
}

func (a *Agent) serviceActionContext(ctx context.Context, serviceName, action string, reply *transport.ServiceActionResult) error {
	name := strings.TrimSuffix(strings.TrimSpace(serviceName), ".service")
	if name == "" || core.ServiceForUnit(name) == nil {
		reply.Error = "unknown managed service"
		return nil
	}
	switch action {
	case "start", "stop", "restart", "reload":
	default:
		reply.Error = "invalid service action"
		return nil
	}
	systemctl, err := serviceMutationSystemctlResolver()
	if err != nil {
		reply.Error = "systemd client failed security validation"
		return nil
	}
	out, err := runServiceMutationCombinedOutput(ctx, systemctl, action, name)
	if err != nil {
		log.Printf("ERROR service %s %s: %v: %s", action, name, err, strings.TrimSpace(string(out)))
		reply.Error = firstLine(fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out))))
		return nil
	}
	reply.Success = true
	return nil
}

type ServiceMutationServiceRequest = transport.ServiceMutationServiceRequest

func (a *Agent) StartServiceMutation(req *ServiceMutationServiceRequest, reply *bool) error {
	*reply = false
	if req == nil {
		return fmt.Errorf("start service mutation request is required")
	}
	name := strings.TrimSuffix(strings.TrimSpace(req.ServiceName), ".service")
	if name == "" || core.ServiceForUnit(name) == nil {
		return errors.New("unknown managed service")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepStartService, name, "", "start"),
	)
	if err != nil {
		*reply = false
		return err
	}
	defer finishStep()
	var result transport.ServiceActionResult
	if err := a.serviceActionContext(ctx, name, "start", &result); err != nil {
		return err
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	*reply = result.Success
	return nil
}

func (a *Agent) ResetFailedUnitMutation(req *ServiceMutationServiceRequest, reply *bool) error {
	*reply = false
	if req == nil {
		return fmt.Errorf("reset failed service mutation request is required")
	}
	name := strings.TrimSuffix(strings.TrimSpace(req.ServiceName), ".service")
	if name == "" || core.ServiceForUnit(name) == nil {
		return errors.New("unknown managed service")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepResetFailedUnit, name, "", "reset-failed"),
	)
	if err != nil {
		*reply = false
		return err
	}
	defer finishStep()
	systemctl, err := serviceMutationSystemctlResolver()
	if err != nil {
		return errors.New("systemd client failed security validation")
	}
	if out, err := runServiceMutationCombinedOutput(ctx, systemctl, "reset-failed", name); err != nil {
		return fmt.Errorf("reset failed unit: %v: %s", err, strings.TrimSpace(string(out)))
	}
	*reply = true
	return nil
}

func main() {
	// Hidden system-update modes exit before managers, RPC, sockets, or
	// background tasks. The worker accepts only one canonical request ID.
	if len(os.Args) == 2 && os.Args[1] == "--inspect-build-identity" {
		fmt.Printf("version=%s\ncommit=%s\n", buildVersion, buildCommit)
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--self-update-worker" {
		if err := runSystemUpdateWorker(os.Args[2]); err != nil {
			log.Fatalf("System update worker failed: %v", err)
		}
		return
	}
	// The hidden owner worker exits before any root-only manager, socket, or background task starts.
	// Gizli sahip çalışanı, root'a özel yönetici, soket veya arka plan görevi başlamadan çıkar.
	if handled, err := handleSystemSQLiteOwnerWorker(); handled {
		if err != nil {
			log.Printf("Isolated SQLite worker failed: %v", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--restart-panel-after-certificate-publish" {
		if err := restartPanelAfterCertificatePublish(); err != nil {
			log.Fatalf("Restart panel after certificate publish: %v", err)
		}
		log.Println("Panel certificate activation reconciliation completed")
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--deploy-panel-certificate" {
		lineageName := strings.ToLower(strings.TrimSpace(os.Args[2]))
		if !validPanelCertLineage.MatchString(lineageName) {
			log.Fatal("Invalid panel certificate lineage")
		}
		deployed, err := deployRenewedPanelCertFiles(lineageName, managedPanelTLSDir)
		if err != nil {
			log.Fatalf("Deploy panel certificate: %v", err)
		}
		if !deployed {
			log.Printf("Renewed lineage %s is not the active panel identity; skipped", lineageName)
			return
		}
		log.Println("Panel certificate activation queued for durable reconciliation")
		return
	}
	log.Println("Starting CelikPanel Agent...")
	// The unit preflight validates the exact restore batch with nft --check and
	// exits before the privileged RPC socket or background workers are opened.
	// Unit ön kontrolü tam geri yükleme paketini nft --check ile doğrular ve
	// ayrıcalıklı RPC soketi ya da arka plan işleri açılmadan çıkar.
	if len(os.Args) == 2 && os.Args[1] == "--check-firewall-restore" {
		if err := checkFirewallRestore(); err != nil {
			log.Fatalf("Firewall restore preflight failed: %v", err)
		}
		log.Println("Firewall restore preflight complete")
		return
	}
	// The early-boot oneshot calls only this fixed mode. It restores the
	// root-owned firewall snapshot before network-pre.target, then exits without
	// opening the privileged RPC socket or starting background workers.
	// Erken-açılış oneshot yalnız bu sabit modu çağırır. Root-sahipli firewall
	// snapshot'ını network-pre.target'tan önce geri yükler; ayrıcalıklı RPC
	// soketini açmadan veya arka plan işlerini başlatmadan çıkar.
	if len(os.Args) == 2 && os.Args[1] == "--restore-firewall" {
		if err := restoreFirewallSnapshot(); err != nil {
			log.Fatalf("Firewall restore failed: %v", err)
		}
		log.Println("Firewall restore complete")
		return
	}
	// Ledger creation is an explicit one-time transition and never occurs during normal agent startup.
	// Ledger oluşturma açık bir kerelik geçiştir ve normal agent başlangıcında asla gerçekleşmez.
	if len(os.Args) == 2 && os.Args[1] == "--initialize-service-mutation-ledger" {
		if err := initializeServiceMutationLedger("", ""); err != nil {
			log.Fatalf("Failed to initialize service mutation ledger: %v", err)
		}
		log.Println("Service mutation ledger initialized")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--check-initial-service-mutation-ledger" {
		if err := checkInitialServiceMutationLedger("", ""); err != nil {
			log.Fatalf("Initial service mutation ledger check failed: %v", err)
		}
		log.Println("Initial service mutation ledger is exact and idle")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--check-initial-service-mutation-ledger-under-external-lock" {
		if err := checkInitialServiceMutationLedgerUnderExternalLock("", ""); err != nil {
			log.Fatalf("Initial service mutation ledger external-lock check failed: %v", err)
		}
		log.Println("Initial service mutation ledger is exact under the external lock")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--check-pre-ledger-service-mutation-idle" {
		if err := checkPreLedgerServiceMutationIdle("", ""); err != nil {
			log.Fatalf("Pre-ledger service mutation idle check failed: %v", err)
		}
		log.Println("Pre-ledger service mutation state is idle")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--check-pre-ledger-service-mutation-idle-under-external-lock" {
		if err := checkPreLedgerServiceMutationIdleUnderExternalLock("", ""); err != nil {
			log.Fatalf("Pre-ledger service mutation external-lock check failed: %v", err)
		}
		log.Println("Pre-ledger service mutation state is exact under the external lock")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--check-service-mutation-idle" {
		if err := checkServiceMutationIdle("", ""); err != nil {
			log.Fatalf("Service mutation idle check failed: %v", err)
		}
		log.Println("Service mutation state is idle")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--check-service-mutation-idle-under-external-lock" {
		if err := checkServiceMutationIdleUnderExternalLock("", ""); err != nil {
			log.Fatalf("Service mutation external-lock check failed: %v", err)
		}
		log.Println("Service mutation state is idle under the external lock")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--prepare-bind-generation-root-under-external-lock" {
		ctx, cancel := context.WithTimeout(
			context.Background(), bindSignedUpdatePreparationTimeout,
		)
		defer cancel()
		if err := prepareBINDGenerationRootForSignedUpdateUnderExternalLock(
			ctx, "", "",
		); err != nil {
			log.Fatalf("Prepare BIND generation root under external lock: %v", err)
		}
		log.Println("BIND generation root signed-update preparation complete")
		return
	}

	mutationManager, err := agentServiceMutationManager()
	if err != nil {
		log.Fatalf("Failed to initialize service mutation ledger: %v", err)
	}
	if err := reconcileSystemUpdatesAtStartup(); err != nil {
		log.Fatalf("Failed to reconcile system update state: %v", err)
	}

	// Initialize Systemd Manager
	sysMgr := systemd.NewManager()

	// Initialize Nginx Generator
	nginxGen, err := services.NewNginxGenerator()
	if err != nil {
		log.Fatalf("Failed to create nginx generator: %v", err)
	}

	// Initialize PHP-FPM Manager
	phpMgr, err := services.NewPHPFPMManager()
	if err != nil {
		log.Fatalf("Failed to create PHP-FPM manager: %v", err)
	}

	// Initialize User Manager
	userMgr := services.NewUserManager()

	// Initialize the fixed system SQLite inventory and private snapshot registry.
	// Sabit sistem SQLite envanterini ve özel anlık görüntü kaydını başlat.
	sqliteAdmin, err := newSystemSQLiteManager()
	if err != nil {
		// Keep the core agent available when only the optional SQLite maintenance feature cannot start.
		// Yalnızca isteğe bağlı SQLite bakım özelliği başlatılamadığında ana ajanı kullanılabilir tut.
		log.Printf("Warning: system SQLite administration is unavailable")
	} else {
		defer sqliteAdmin.Close()
	}

	// Initialize Agent
	agent := &Agent{
		parser:      parser.NewNginxParser(),
		systemdMgr:  sysMgr,
		nginxGen:    nginxGen,
		phpManager:  phpMgr,
		userManager: userMgr,
		sqliteAdmin: sqliteAdmin,
	}

	// Register RPC
	if err := rpc.Register(agent); err != nil {
		log.Fatalf(`failed to register agent RPC service: %v`, err)
	}

	token, err := transport.LoadOrCreateToken(transport.AgentTokenPath())
	if err != nil {
		log.Fatalf("Failed to load agent token: %v", err)
	}

	socketPath := transport.AgentSocketPath()
	listener, err := transport.ListenAgent(socketPath)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()
	log.Printf("Agent listening on unix socket %s", socketPath)
	startPanelCertificateActivationReconciler(mutationManager)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatalf("Accept failed: %v", err)
		}
		go transport.ServeAgentConn(rpc.DefaultServer, conn, token)
	}
}
