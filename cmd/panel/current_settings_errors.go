package main

import (
	"encoding/json"
	"log"
	"net/http"
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

const currentSettingsUnreadableMessage = "CelikPanel could not read what is currently set on this server, so nothing was changed. " +
	"Reload the page to read it again. If it keeps failing, the server owner checks that the service behind this page is running, then reloads."

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

func writeSettingsVersionRequired(w http.ResponseWriter, resource string) {
	writeSettingsRefusal(w, http.StatusConflict, errCodeSettingsVersionRequired, settingsVersionRequiredMessage, resource)
}

func writeSettingsChanged(w http.ResponseWriter, resource string) {
	writeSettingsRefusal(w, http.StatusConflict, errCodeSettingsChanged, settingsChangedMessage, resource)
}
