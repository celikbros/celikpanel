package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Serving the database web tools. phpMyAdmin/phpPgAdmin are PHP files, not
// daemons: something must serve them. We give them a loopback-only nginx
// server (127.0.0.1:8306) and the panel reverse-proxies /dbtool/... to it —
// so the tools are reachable only through an authenticated admin panel
// session, never directly from the network, and no firewall port opens.
//
// Veritabanı web araçlarının sunumu. phpMyAdmin/phpPgAdmin daemon değil PHP
// dosyalarıdır: bir şey onları sunmalı. Onlara yalnız-loopback bir nginx
// sunucusu (127.0.0.1:8306) veririz ve panel /dbtool/...'u ona ters-vekiller —
// böylece araçlara yalnız kimlik doğrulamalı yönetici panel oturumu üzerinden
// ulaşılır, ağdan asla doğrudan ulaşılmaz ve güvenlik duvarında port açılmaz.

const (
	dbToolsConfPath = "/etc/nginx/conf.d/celikpanel-dbtools.conf"
	dbToolsAddr     = "127.0.0.1:8306"
)

// dbToolRoots: catalogue tool id → the docroot its distro package installs.
// dbToolRoots: katalog araç id'si → dağıtım paketinin kurduğu kök dizin.
var dbToolRoots = map[string][]string{
	"phpmyadmin": {"/usr/share/phpmyadmin", "/usr/share/webapps/phpMyAdmin"},
	"phppgadmin": {"/usr/share/phppgadmin"},
}

func firstInstalledDBToolRoot(roots []string) string {
	for _, root := range roots {
		if fileExistsAgent(root) {
			return root
		}
	}
	return ""
}

type ConfigureDBToolsResponse struct {
	Configured bool     `json:"configured"`
	Tools      []string `json:"tools"`
	Error      string   `json:"error,omitempty"`
}

// ConfigureDBTools regenerates the loopback nginx server for whichever tools
// are installed right now; with none installed the config is removed. Called
// by the panel after a tool is installed or uninstalled — idempotent.
// ConfigureDBTools, şu an kurulu araçlar için yalnız-loopback nginx
// sunucusunu yeniden üretir; hiçbiri kurulu değilse yapılandırma kaldırılır.
// Panel bir araç kurulunca/kaldırılınca çağırır — idempotenttir.
func (a *Agent) ConfigureDBTools(req *ServiceMutationRequest, resp *ConfigureDBToolsResponse) error {
	if req == nil {
		return fmt.Errorf("database tools configuration request is required")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(req.ServiceMutationBinding)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer finishStep()
	if _, err := exec.LookPath("nginx"); err != nil {
		resp.Error = "nginx is not installed"
		return nil
	}

	var tools []string
	installedRoots := make(map[string]string)
	for id, roots := range dbToolRoots {
		if root := firstInstalledDBToolRoot(roots); root != "" {
			tools = append(tools, id)
			installedRoots[id] = root
		}
	}

	if len(tools) == 0 {
		_ = os.Remove(dbToolsConfPath)
		if out, err := runServiceMutationCombinedOutput(ctx, "systemctl", "reload", "nginx"); err != nil {
			resp.Error = commandFailureDetail("nginx reload", out, err)
			return nil
		}
		resp.Configured = true
		return nil
	}

	// The tools are PHP: hand .php to the default FPM pool of the installed
	// PHP version. The path is version-dependent, so detect — never assume.
	// Araçlar PHP'dir: .php'yi kurulu PHP sürümünün varsayılan FPM havuzuna
	// ver. Yol sürüme bağlıdır; tespit et — asla varsayma.
	socket := detectFPMSocket()
	if socket == "" {
		resp.Error = "PHP-FPM is not installed"
		return nil
	}

	var b strings.Builder
	b.WriteString("# Managed by CelikPanel — database web tools, loopback only.\n")
	b.WriteString("# The panel reverse-proxies /dbtool/ here behind an admin session.\n")
	b.WriteString("server {\n")
	fmt.Fprintf(&b, "    listen %s;\n", dbToolsAddr)
	b.WriteString("    server_name _;\n")
	for _, id := range tools {
		root := installedRoots[id]
		// Path-preserving locations: the browser path and this server's path
		// are identical, so the tools' absolute URLs survive the panel proxy
		// without any rewriting.
		// Yol-koruyan location'lar: tarayıcı yolu ile bu sunucunun yolu aynı;
		// araçların mutlak URL'leri panel vekilinden yeniden yazım olmadan
		// sağ çıkar.
		fmt.Fprintf(&b, `
    location /dbtool/%s/ {
        alias %s/;
        index index.php;
        location ~ ^/dbtool/%s/(.+\.php)$ {
            alias %s/$1;
            include fastcgi_params;
            fastcgi_param SCRIPT_FILENAME $request_filename;
            fastcgi_pass unix:%s;
        }
    }
`, id, root, id, root, socket)
	}
	b.WriteString("}\n")

	if err := os.WriteFile(dbToolsConfPath, []byte(b.String()), 0o644); err != nil {
		resp.Error = fmt.Sprintf("write nginx config: %v", err)
		return nil
	}
	// Validate before reload — a broken tools config must not take the web
	// server (and every customer site) down with it.
	// Yeniden yüklemeden önce doğrula — bozuk bir araç yapılandırması web
	// sunucusunu (ve tüm müşteri sitelerini) beraberinde düşürmemeli.
	if out, err := serviceMutationCommand(ctx, "nginx", "-t").CombinedOutput(); err != nil {
		_ = os.Remove(dbToolsConfPath)
		resp.Error = fmt.Sprintf("nginx rejected the tools config: %s", firstLine(string(out)))
		return nil
	}
	if out, err := runServiceMutationCombinedOutput(ctx, "systemctl", "reload", "nginx"); err != nil {
		resp.Error = commandFailureDetail("nginx reload", out, err)
		return nil
	}

	resp.Configured = true
	resp.Tools = tools
	return nil
}
