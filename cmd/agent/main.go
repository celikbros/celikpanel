package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/fs"
	"github.com/alicelik/celikpanel/internal/parser"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/systemd"
	"github.com/alicelik/celikpanel/internal/transport"
	"log"
	"net/rpc"
	"os"
	"os/exec"
	"strings"
)

type Agent struct {
	watcher     *fs.Watcher
	parser      *parser.NginxParser
	systemdMgr  *systemd.Manager
	nginxGen    *services.NginxGenerator
	phpManager  *services.PHPFPMManager
	userManager *services.UserManager
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
	content, err := os.ReadFile(args.Path)
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	reply.Content = string(content)

	// Try to parse if it's an Nginx file
	if strings.Contains(args.Path, "nginx") {
		parsed, _ := a.parser.Parse(string(content))
		reply.Parsed = fmt.Sprintf("%v", parsed)
	}

	return nil
}

func (a *Agent) UpdateConfig(args *transport.UpdateConfigArgs, reply *bool) error {
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
		return err
	}
	log.Printf("Updating config %s", path)

	// Keep the previous content so a failed validation can be UNDONE. The old
	// code wrote first, validated after, and on failure returned an error with
	// the comment "Revert? For now just return error" — leaving the file
	// broken on disk. Proven live (25 Jul): a single bad write emptied
	// /etc/nginx/nginx.conf; nginx kept serving from memory and would have
	// died at the next reload, with nothing on screen saying so. A config
	// editor that can destroy the file it edits is not an editor.
	//
	// Önceki içeriği sakla ki başarısız bir doğrulama GERİ ALINABİLSİN. Eski
	// kod önce yazıyor, sonra doğruluyor ve düşünce "Revert? For now just
	// return error" yorumuyla hata döndürüyordu — dosyayı diskte bozuk
	// bırakarak. Canlıda kanıtlandı (25 Tem): tek bir hatalı yazma
	// /etc/nginx/nginx.conf'u boşalttı; nginx bellekten sunmayı sürdürdü ve
	// bir sonraki yeniden yüklemede ölecekti, ekranda bunu söyleyen hiçbir şey
	// yokken. Düzenlediği dosyayı yok edebilen bir editör, editör değildir.
	previous, hadPrevious := []byte(nil), false
	if b, rerr := os.ReadFile(path); rerr == nil {
		previous, hadPrevious = b, true
	}
	restore := func() {
		if hadPrevious {
			if werr := os.WriteFile(path, previous, 0644); werr != nil {
				log.Printf("config rollback FAILED for %s: %v", path, werr)
			} else {
				log.Printf("config rolled back: %s", path)
			}
		} else {
			_ = os.Remove(path)
		}
	}

	if err := os.WriteFile(path, []byte(args.Content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}

	// Validate, then roll back on failure — never leave a broken config
	// behind. The validator is chosen by what the file belongs to; a file with
	// no validator is written as-is (we cannot check what we cannot parse).
	// Doğrula, düşerse geri al — arkanda asla bozuk bir yapılandırma bırakma.
	// Doğrulayıcı, dosyanın ait olduğu şeye göre seçilir; doğrulayıcısı olmayan
	// dosya olduğu gibi yazılır (ayrıştıramadığımızı denetleyemeyiz).
	if v := configValidator(path); v != nil {
		if out, verr := v.check(); verr != nil {
			restore()
			return fmt.Errorf("%s: %s", v.name, firstLine(out))
		}
		if v.reload != "" {
			a.systemdMgr.Reload(v.reload)
		}
	}

	*reply = true
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
		return &validatorSpec{name: "nginx config validation failed", reload: "nginx", check: run("nginx", "-t")}
	case strings.Contains(path, "/postfix/"):
		return &validatorSpec{name: "postfix config validation failed", reload: "postfix", check: run("postfix", "check")}
	case strings.Contains(path, "/dovecot"):
		return &validatorSpec{name: "dovecot config validation failed", reload: "dovecot", check: run("doveconf", "-n")}
	case strings.Contains(path, "/apache2/") || strings.Contains(path, "/httpd/"):
		return &validatorSpec{name: "apache config validation failed", reload: "", check: run("apachectl", "configtest")}
	}
	return nil
}

// ServiceMutationActionRequest carries the durable lease for a managed
// service lifecycle action performed as part of a larger mutation.
// ServiceMutationActionRequest, daha büyük bir değişikliğin parçası olarak
// yapılan yönetilen servis yaşam döngüsü eyleminin kalıcı kirasını taşır.
type ServiceMutationActionRequest struct {
	ServiceMutationBinding
	ServiceName string `json:"service_name"`
	Action      string `json:"action"`
}

func (a *Agent) ServiceMutationAction(req *ServiceMutationActionRequest, reply *transport.ServiceActionResult) error {
	if req == nil {
		return fmt.Errorf("service mutation action request is required")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	defer finishStep()
	return a.serviceActionContext(ctx, req.ServiceName, req.Action, reply)
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
	out, err := runServiceMutationCombinedOutput(ctx, "systemctl", action, name)
	if err != nil {
		log.Printf("ERROR service %s %s: %v: %s", action, name, err, strings.TrimSpace(string(out)))
		reply.Error = firstLine(fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out))))
		return nil
	}
	reply.Success = true
	return nil
}

type ServiceMutationServiceRequest struct {
	ServiceMutationBinding
	ServiceName string `json:"service_name"`
}

func (a *Agent) StartServiceMutation(req *ServiceMutationServiceRequest, reply *bool) error {
	if req == nil {
		return fmt.Errorf("start service mutation request is required")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		return err
	}
	defer finishStep()
	var result transport.ServiceActionResult
	if err := a.serviceActionContext(ctx, req.ServiceName, "start", &result); err != nil {
		return err
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	*reply = result.Success
	return nil
}

func (a *Agent) ResetFailedUnitMutation(req *ServiceMutationServiceRequest, reply *bool) error {
	if req == nil {
		return fmt.Errorf("reset failed service mutation request is required")
	}
	name := strings.TrimSuffix(strings.TrimSpace(req.ServiceName), ".service")
	if name == "" || core.ServiceForUnit(name) == nil {
		return errors.New("unknown managed service")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		return err
	}
	defer finishStep()
	if out, err := runServiceMutationCombinedOutput(ctx, "systemctl", "reset-failed", name); err != nil {
		return fmt.Errorf("reset failed unit: %v: %s", err, strings.TrimSpace(string(out)))
	}
	*reply = true
	return nil
}

func main() {
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
	// opening the privileged RPC socket or starting any watcher.
	// Erken-açılış oneshot yalnız bu sabit modu çağırır. Root-sahipli firewall
	// snapshot'ını network-pre.target'tan önce geri yükler; ayrıcalıklı RPC
	// soketini açmadan veya izleyici başlatmadan çıkar.
	if len(os.Args) == 2 && os.Args[1] == "--restore-firewall" {
		if err := restoreFirewallSnapshot(); err != nil {
			log.Fatalf("Firewall restore failed: %v", err)
		}
		log.Println("Firewall restore complete")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--check-service-mutation-idle" {
		if err := checkServiceMutationIdle("", ""); err != nil {
			log.Fatalf("Service mutation idle check failed: %v", err)
		}
		log.Println("Service mutation state is idle")
		return
	}

	if _, err := agentServiceMutationManager(); err != nil {
		log.Fatalf("Failed to initialize service mutation ledger: %v", err)
	}

	// Initialize Watcher
	w, err := fs.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
	}
	go w.Start()
	defer w.Close()

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

	// Initialize Agent
	agent := &Agent{
		watcher:     w,
		parser:      parser.NewNginxParser(),
		systemdMgr:  sysMgr,
		nginxGen:    nginxGen,
		phpManager:  phpMgr,
		userManager: userMgr,
	}

	// Register RPC
	rpc.Register(agent)

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

	// Watcher Event Loop (for debugging)
	go func() {
		for event := range w.Events {
			log.Printf("FS Event: %s", event)
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatalf("Accept failed: %v", err)
		}
		go transport.ServeAgentConn(rpc.DefaultServer, conn, token)
	}
}
