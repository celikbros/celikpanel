package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/alicelik/celikpanel/internal/services"
)

// A site the Agent refused because an owner's directory above the hosting
// base blocks the web server or the site users (native finding P3). Nothing
// was changed on the server. The answer names the directory, its mode and
// owner, who acts (the server owner), the command that allows traversal and
// how work resumes (create the site again). CelikPanel never runs that
// command itself: it did not create the directory (D-022, D-024).
//
// Barındırma kökünün üstündeki bir sahip dizini web sunucusunu ya da site
// kullanıcılarını engellediği için Agent'ın reddettiği site. Sunucuda hiçbir
// şey değişmedi. Yanıt dizini, kipini ve sahibini, kimin işlem yapacağını
// (sunucu sahibi), geçişe izin veren komutu ve işin nasıl süreceğini (siteyi
// yeniden oluşturmak) söyler. CelikPanel o komutu kendisi çalıştırmaz.

const hostingRootNotTraversableMessage = "The site was not created and nothing was changed: %s (mode %s, owner %s) " +
	"keeps the web server or the site users from reaching /var/www/celikpanel, so a site there would answer 404 " +
	"and its scheduled tasks would not run. CelikPanel does not change directories it did not create. " +
	"The server owner decides: to allow it, run %s on the server. Then create the site again; nothing retries by itself."

// hostingRootNotTraversable finds the Agent's typed refusal in a site
// creation error. When the Panel's own cleanup of the refused site also
// failed, the answer stays the ordinary error: "create the site again" would
// not be true then.
// hostingRootNotTraversable, site oluşturma hatasında Agent'ın tipli reddini
// bulur. Panel'in reddedilen siteyi temizlemesi de başarısızsa yanıt olağan
// hata olarak kalır.
func hostingRootNotTraversable(err error) (*services.HostingRootNotTraversableError, bool) {
	if joined, ok := err.(interface{ Unwrap() []error }); ok && len(joined.Unwrap()) != 1 {
		return nil, false
	}
	var refusal *services.HostingRootNotTraversableError
	if !errors.As(err, &refusal) || refusal == nil || refusal.Block.Directory == "" {
		return nil, false
	}
	return refusal, true
}

func writeHostingRootNotTraversable(w http.ResponseWriter, refusal *services.HostingRootNotTraversableError) {
	block := refusal.Block
	command := "sudo chmod 755 " + block.Directory
	log.Printf("[409][domain] site creation refused: %s", refusal.Error())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error:   fmt.Sprintf(hostingRootNotTraversableMessage, block.Directory, block.Mode, block.Owner, command),
		Code:    errCodeHostingRootNotTraversable,
		Details: []string{block.Directory, block.Mode, block.Owner},
		Vars: map[string]string{
			"directory": block.Directory,
			"mode":      block.Mode,
			"owner":     block.Owner,
			"command":   command,
		},
	})
}
