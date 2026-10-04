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
