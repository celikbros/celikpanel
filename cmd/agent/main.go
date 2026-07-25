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

	// Write to file
	// 0644 is standard for config files
	if err := os.WriteFile(path, []byte(args.Content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}

	// Reload service if needed
	if strings.Contains(path, "nginx") {
		// Validate first
		if err := exec.Command("nginx", "-t").Run(); err != nil {
			// Revert? For now just return error
			return fmt.Errorf("nginx config validation failed: %v", err)
		}
		a.systemdMgr.Reload("nginx")
	}

	*reply = true
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
