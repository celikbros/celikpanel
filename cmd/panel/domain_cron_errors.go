package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/rpc"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Scheduled tasks are native user crontabs, so they need the server's cron
// service. When the Agent proves it is absent, the answer names the condition,
// who acts, the action and how work resumes (D-024) instead of the masked
// INTERNAL answer upd1 recorded on a fresh Debian 13 server (1 Oct 2026). The
// Agent's raw line stays in the Panel log only.
//
// Zamanlanmış görevler yerel kullanıcı crontab'larıdır; sunucunun cron
// hizmetini gerektirir. Agent onun olmadığını kanıtladığında yanıt; koşulu,
// kimin işlem yapacağını, eylemi ve işin nasıl süreceğini söyler (D-024). Agent
// satırı yalnız Panel günlüğünde kalır.

const (
	// cronReasonRead refines CRON_NOT_INSTALLED for listing: nothing could be
	// read. cronReasonWrite: a create, change or delete was not saved.
	cronReasonRead  = "read"
	cronReasonWrite = "write"
)

const cronNotInstalledWriteMessage = "Scheduled tasks need this server's cron service, and it is not installed, so nothing was changed. " +
	"The server owner installs it once: in CelikPanel open Components and install \"Scheduled tasks (cron)\", " +
	"or on the server run sudo apt-get install cron (Debian/Ubuntu), or sudo pacman -S cronie and then sudo systemctl enable --now cronie (Arch). " +
	"Then create or change the task again; nothing retries by itself."

const cronNotInstalledReadMessage = "Scheduled tasks cannot be shown because this server's cron service is not installed, and no scheduled task runs until it is. " +
	"The server owner installs it once: in CelikPanel open Components and install \"Scheduled tasks (cron)\", " +
	"or on the server run sudo apt-get install cron (Debian/Ubuntu), or sudo pacman -S cronie and then sudo systemctl enable --now cronie (Arch). " +
	"Then open this page again."

const cronJobDuplicateMessage = "A scheduled task with the same schedule and command already exists, so nothing was added. " +
	"Change the existing task instead, or enable it if it is disabled."

// The same task stands on two lines of the crontab (written there by hand: the
// Panel refuses to add a copy). A change or a delete cannot say which of the
// two it means, so nothing is changed (9 Oct 2026).
// Aynı görev crontab'da iki satırda duruyor. Bir değişiklik hangisini
// kastettiğini söyleyemez; hiçbir şey değiştirilmez.
const cronJobAmbiguousMessage = "This task stands twice in the crontab, so CelikPanel cannot tell which line to change and changed nothing. " +
	"The server owner removes one of the two lines on the server (sudo crontab -u <site user> -e), then reloads this list."

// agentReportedCronNotInstalled matches the Agent's exact answer. Older Agents
// returned the same text from AddCronJob, so they are classified too.
// agentReportedCronNotInstalled, Agent'ın tam yanıtını eşler.
func agentReportedCronNotInstalled(err error) bool {
	return agentAnsweredExactly(err, transport.CronNotInstalled)
}

// agentAnsweredExactly reports whether err is the Agent's own RPC answer with
// exactly this text. A text that did not come from the Agent, or that carries
// any extra wording, is not the known condition.
// agentAnsweredExactly, err'in tam bu metinle Agent'ın kendi RPC yanıtı olup
// olmadığını bildirir.
func agentAnsweredExactly(err error, text string) bool {
	var serverErr rpc.ServerError
	if !errors.As(err, &serverErr) {
		return false
	}
	return strings.TrimSpace(string(serverErr)) == text
}

// agentUnreadableEvidence reports whether err is the Agent's own "could not be
// read" answer for this sentence, alone or with the evidence lines the contract
// defines (transport.UnreadableEvidence), and returns that evidence.
// agentUnreadableEvidence, err'in Agent'ın bu cümleyle verdiği "okunamadı"
// yanıtı olup olmadığını bildirir ve varsa kanıtı döndürür.
func agentUnreadableEvidence(err error, sentence string) (known bool, cause, detail string) {
	var serverErr rpc.ServerError
	if !errors.As(err, &serverErr) {
		return false, "", ""
	}
	return transport.UnreadableEvidence(string(serverErr), sentence)
}

// agentMutationBusy is the classified answer for an Agent lock held by another
// CelikPanel change: 409 HOST_MUTATION_BUSY, "wait for it to finish, then try
// again", in both languages already.
// agentMutationBusy, başka bir CelikPanel değişikliğinin tuttuğu Agent kilidi
// için sınıflandırılmış yanıttır.
func agentMutationBusy() error {
	return &hostMutationBusyError{reason: transport.HostMutationReasonAgentMutation}
}

// writeCronAgentError answers a failed cron RPC. The known "cron not
// installed" condition becomes 409 CRON_NOT_INSTALLED with the reason for the
// kind of request; everything else keeps the ordinary classified/INTERNAL
// path.
// writeCronAgentError, başarısız bir cron RPC'sini yanıtlar.
func writeCronAgentError(w http.ResponseWriter, err error, reason string) {
	// The Agent's refusals that protect the owner's crontab (8 Oct 2026): an
	// unreadable crontab is unknown, not empty; a change must say which
	// crontab it was built from and that must still be the one on the server;
	// the same task is not added twice.
	// Sahibin crontab'ını koruyan Agent retleri.
	if known, cause, said := agentUnreadableEvidence(err, transport.CronStateUnreadable); known {
		writeScheduledTasksUnreadable(w, cause, said)
		return
	}
	switch {
	case agentAnsweredExactly(err, transport.CronVersionRequired):
		writeSettingsVersionRequired(w, settingsResourceScheduledTasks)
		return
	case agentAnsweredExactly(err, transport.CronStateChanged):
		writeSettingsChanged(w, settingsResourceScheduledTasks)
		return
	case agentAnsweredExactly(err, transport.CronJobDuplicate):
		log.Printf("[409][cron] duplicate task refused")
		writeCodedError(w, http.StatusConflict, errCodeCronJobDuplicate, cronJobDuplicateMessage, "")
		return
	case agentAnsweredExactly(err, transport.CronJobAmbiguous):
		log.Printf("[409][cron] a task that stands twice in the crontab was not changed")
		writeCodedError(w, http.StatusConflict, errCodeCronJobAmbiguous, cronJobAmbiguousMessage, "")
		return
	}
	if !agentReportedCronNotInstalled(err) {
		writeServerError(w, err)
		return
	}
	message := cronNotInstalledWriteMessage
	if reason == cronReasonRead {
		message = cronNotInstalledReadMessage
	}
	log.Printf("[409][cron] %s refused: %s", reason, boundedAgentDiagnostic(err.Error()))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: message, Code: errCodeCronNotInstalled, Reason: reason})
}

// writeNativeCronRemovalRefused answers a request to uninstall native cron.
// Nothing is changed; the owner decides with the package manager (D-022).
// writeNativeCronRemovalRefused, yerel cron'u kaldırma isteğini yanıtlar.
func writeNativeCronRemovalRefused(w http.ResponseWriter) {
	log.Printf("[409][services] native cron removal refused")
	writeCodedError(w, http.StatusConflict, errCodeNativeCronRemovalRefused, core.NativeCronRemovalRefusal, "")
}
