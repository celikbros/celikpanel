package main

import (
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

func (a *Agent) ReloadService(args *transport.ServiceArgs, reply *bool) error {
	log.Printf("Reloading service %s", args.ServiceName)
	if err := a.systemdMgr.Reload(args.ServiceName); err != nil {
		return err
	}
	*reply = true
	return nil
}

func (a *Agent) StartService(args *transport.ServiceArgs, reply *bool) error {
	log.Printf("Starting service %s", args.ServiceName)
	if err := a.systemdMgr.Start(args.ServiceName); err != nil {
		log.Printf("ERROR starting service %s: %v", args.ServiceName, err)
		return err
	}
	log.Printf("Successfully started service %s", args.ServiceName)
	*reply = true
	return nil
}

func (a *Agent) StopService(args *transport.ServiceArgs, reply *bool) error {
	log.Printf("Stopping service %s", args.ServiceName)
	if err := a.systemdMgr.Stop(args.ServiceName); err != nil {
		log.Printf("ERROR stopping service %s: %v", args.ServiceName, err)
		return err
	}
	log.Printf("Successfully stopped service %s", args.ServiceName)
	*reply = true
	return nil
}

func (a *Agent) RestartService(args *transport.ServiceArgs, reply *bool) error {
	log.Printf("Restarting service %s", args.ServiceName)
	if err := a.systemdMgr.Restart(args.ServiceName); err != nil {
		log.Printf("ERROR restarting service %s: %v", args.ServiceName, err)
		return err
	}
	log.Printf("Successfully restarted service %s", args.ServiceName)
	*reply = true
	return nil
}

// ResetFailedUnit clears a unit's "failed" state so it can be started again.
// A unit that failed once (e.g. Dovecot before its TLS cert existed) stays
// failed until reset, so a later, correct start would be refused and a fixed
// install would still look broken. Best-effort: a unit that was never failed
// resets harmlessly.
// ResetFailedUnit, bir unit'in "failed" durumunu temizler; böylece yeniden
// başlatılabilir. Bir kez başarısız olan unit (örn. TLS sertifikası yokken
// Dovecot) sıfırlanana dek failed kalır; sonraki doğru başlatma reddedilir ve
// düzeltilmiş bir kurulum yine bozuk görünürdü. En-iyi-çaba: hiç başarısız
// olmamış unit zararsızca sıfırlanır.
func (a *Agent) ResetFailedUnit(args *transport.ServiceArgs, reply *bool) error {
	_ = exec.Command("systemctl", "reset-failed", args.ServiceName).Run()
	*reply = true
	return nil
}

func main() {
	log.Println("Starting CelikPanel Agent...")

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
