package services

import (
	"fmt"

	"github.com/alicelik/celikpanel/internal/transport"
)

// HostingRootNotTraversableError is the Agent's typed refusal to create a
// site because a directory above the hosting base, which CelikPanel does not
// own, keeps the web server or the site users from reaching site files
// (native finding P3). Nothing was changed on the server; the Panel answers
// with the directory, its mode, who acts and the owner's command.
// HostingRootNotTraversableError, barındırma kökünün üstündeki, CelikPanel'e
// ait olmayan bir dizin web sunucusunu ya da site kullanıcılarını site
// dosyalarından uzak tuttuğu için Agent'ın tipli site oluşturma reddidir.
// Sunucuda hiçbir şey değişmedi.
type HostingRootNotTraversableError struct {
	Block transport.HostingRootBlock
}

func (e *HostingRootNotTraversableError) Error() string {
	return fmt.Sprintf("%s: %s (mode %s, owner %s)",
		transport.HostingRootNotTraversable, e.Block.Directory, e.Block.Mode, e.Block.Owner)
}

// WebServerRefusedConfigError is the Agent's typed answer to a site whose
// virtual host nginx refused (transport.WebServerRefusedConfig; 12 Oct 2026).
// Detail is the line of nginx's own output that names what it refused; it can
// name paths of this server.
// WebServerRefusedConfigError, sanal konağını nginx'in reddettiği bir site için
// Agent'ın tipli yanıtıdır. Detail, nginx'in neyi reddettiğini adlandıran kendi
// satırıdır.
type WebServerRefusedConfigError struct {
	Detail string
}

func (e *WebServerRefusedConfigError) Error() string {
	return transport.WebServerRefusedConfig + ": " + e.Detail
}
