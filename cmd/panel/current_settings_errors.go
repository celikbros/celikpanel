package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The Panel must not replace what the server owner has set with what a page
// happened to show (D-022; constitution: detect owner changes, never silently
// overwrite; unknown is not absent). Three screens could do exactly that after
// one failed read on a healthy server — the server mail policy, a domain's
// automatic backup schedule and a domain's scheduled tasks (8 Oct 2026). Their
// reads and writes now share three answers:
//
//   - CURRENT_SETTINGS_UNREADABLE: the current state could not be read. It is
//     unknown, so nothing is offered as a setting and nothing is written.
//   - SETTINGS_VERSION_REQUIRED: a write that does not say which state it was
//     built from. A page that never loaded, or an older cached page, cannot
//     write.
//   - SETTINGS_CHANGED: the state on the server is no longer the one the write
//     was built from. Nothing is written; the owner reloads and decides again.
//
// Reason names the resource. These are refusals before any change.
//
// Panel, sunucu sahibinin ayarladığını bir sayfanın o an gösterdiğiyle
// değiştirmemelidir. Üç ekran, sağlıklı bir sunucuda tek bir başarısız okumadan
// sonra tam bunu yapabiliyordu. Okuma ve yazmaları artık üç yanıtı paylaşır:
// geçerli durum okunamadı (bilinmiyor; hiçbir şey ayar diye sunulmaz ve
// yazılmaz), yazı hangi durumdan kurulduğunu söylemiyor, sunucudaki durum artık
// yazının kurulduğu durum değil. Hepsi herhangi bir değişiklikten önceki rettir.

const (
	settingsResourceMailPolicy     = "mail_policy"
	settingsResourceScheduledTasks = "scheduled_tasks"
	settingsResourceBackupSchedule = "backup_schedule"
)

// The sentence asserts no cause (10 Oct 2026). It used to end "the server owner
// checks that the service behind this page is running"; in every measured case
// the service was running and the cause was something else (/etc/cron.allow
// without the site user, a relocated cron spool, a main.cf Postfix refuses).
// When the server's own program printed a line, it travels in `vars.detail`.
// Cümle hiçbir neden ileri sürmez. Sunucunun kendi programı bir satır yazdıysa
// o satır `vars.detail` içinde taşınır.
const currentSettingsUnreadableMessage = "CelikPanel could not read what is currently set on this server, so nothing is shown as a setting and nothing was changed. " +
	"CelikPanel has not established why the read failed; when the server's own program printed a reason, it is shown with this message. " +
	"Reload the page to read it again."

// The scheduled tasks of a site user could not be read. One sentence per cause
// the Agent verified itself, and a neutral one for everything else (D-024:
// the current reason, who acts, the action, how work resumes).
// Bir site kullanıcısının zamanlanmış görevleri okunamadı. Agent'ın kendisinin
// doğruladığı her neden için bir cümle, geri kalan her şey için yansız bir cümle.
const scheduledTasksUnreadableMessage = "CelikPanel could not read this site user's scheduled tasks from the server, so the list is not shown and nothing was changed. " +
	"This does not mean the user has no tasks. CelikPanel has not established why; what the server's crontab program said is shown with this message when it said anything. " +
	"Reload the page to read the tasks again. If it keeps failing, the server owner runs sudo crontab -u <site user> -l on the server, which prints the same reason."

var scheduledTasksUnreadableCauses = map[string]string{
	transport.UnreadableCronAllow: "CelikPanel cannot read or change this site user's scheduled tasks: this server restricts crontab with /etc/cron.allow, and the user is not listed in it. " +
		"Nothing was changed, and the tasks already on the server are untouched. While the server restricts crontab this way, CelikPanel cannot manage this user's tasks. " +
		"The server owner adds the site user's name on its own line in /etc/cron.allow, then reloads this page.",
	transport.UnreadableCronDeny: "CelikPanel cannot read or change this site user's scheduled tasks: /etc/cron.deny on this server lists the user, so crontab refuses it. " +
		"Nothing was changed, and the tasks already on the server are untouched. While the user is listed there, CelikPanel cannot manage this user's tasks. " +
		"The server owner removes the site user's line from /etc/cron.deny, then reloads this page.",
}

const settingsVersionRequiredMessage = "This request did not say which settings it was built from, so nothing was changed. " +
	"Reload the page so it reads the current settings, then make the change again."

const settingsChangedMessage = "The settings changed on the server since this page loaded, so nothing was changed. " +
	"Reload the page to see the current settings, then make the change again."

func writeSettingsRefusal(w http.ResponseWriter, status int, code, message, resource string) {
	log.Printf("[%d][settings] %s refused: %s", status, resource, code)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: message, Code: code, Reason: resource})
}

// writeCurrentSettingsUnreadable answers a read, or the read before a write,
// that could not establish the current state.
// writeCurrentSettingsUnreadable, geçerli durumu belirleyemeyen okumayı yanıtlar.
func writeCurrentSettingsUnreadable(w http.ResponseWriter, resource string) {
	writeSettingsRefusal(w, http.StatusBadGateway, errCodeCurrentSettingsUnreadable, currentSettingsUnreadableMessage, resource)
}

// writeScheduledTasksUnreadable answers a crontab that could not be read, with
// the cause when the Agent verified one (`detail`, a machine token) and the
// line crontab printed (`vars.detail`).
// writeScheduledTasksUnreadable, okunamayan bir crontab'ı yanıtlar.
func writeScheduledTasksUnreadable(w http.ResponseWriter, cause, said string) {
	message, known := scheduledTasksUnreadableCauses[cause]
	if !known {
		message, cause = scheduledTasksUnreadableMessage, ""
	}
	var vars map[string]string
	if said = boundedAgentDiagnostic(said); said != "" {
		vars = map[string]string{"detail": said}
	}
	log.Printf("[502][settings] %s refused: %s %s", settingsResourceScheduledTasks, errCodeCurrentSettingsUnreadable, cause)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error: message, Code: errCodeCurrentSettingsUnreadable, Reason: settingsResourceScheduledTasks,
		Detail: cause, Vars: vars,
	})
}

func writeSettingsVersionRequired(w http.ResponseWriter, resource string) {
	writeSettingsRefusal(w, http.StatusConflict, errCodeSettingsVersionRequired, settingsVersionRequiredMessage, resource)
}

func writeSettingsChanged(w http.ResponseWriter, resource string) {
	writeSettingsRefusal(w, http.StatusConflict, errCodeSettingsChanged, settingsChangedMessage, resource)
}
