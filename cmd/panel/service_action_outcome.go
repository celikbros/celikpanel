package main

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The answer to a Start, Stop, Restart or Reload on the Services page that the
// Agent could not report as done (10 Oct 2026; D-024, D-025 invariant 2).
//
// The Agent no longer reports `systemctl`'s exit status alone: on some
// platforms the unit named is a wrapper and that status says nothing about the
// daemon (Ubuntu's `postfix.service`, Debian's and Ubuntu's
// `postgresql.service`). It reports what it observed, and so there are two
// answers that are not success:
//
//   - SERVICE_ACTION_FAILED: a verified failure. `reason` is the stage
//     ("check", "reload", "start", "stop", "verify", "command", and since
//     11 Oct 2026 "not_running", "reload_reread", "reload_not_reread").
//   - SERVICE_ACTION_UNKNOWN: the action was sent and what came of it could
//     not be established. Never shown as done, never as a verified failure.
//
// Both carry `vars`: `unit`, `action`, `command` (what the server owner runs
// to see the service's own answer), `detail` (one bounded line: the service's
// own words or what the check observed) and, when another unit owns the
// daemon, `owner_unit`. A failed action used to answer 500 "internal server
// error" with the reason only in the server log.
//
// Hizmetler sayfasındaki bir işlemin, Agent'ın "yapıldı" diye bildiremediği
// yanıtı. İki yanıt başarı değildir: doğrulanmış hata ve bilinmeyen sonuç.
const (
	errCodeServiceActionFailed  = "SERVICE_ACTION_FAILED"
	errCodeServiceActionUnknown = "SERVICE_ACTION_UNKNOWN"
)

const serviceActionResume = " The server owner runs the command shown to read the service's own answer, corrects what it names, and then repeats this action here; nothing repeats it automatically."

// A failed reload says only what was verified (11 Oct 2026). The sentence used
// to end "and keeps running with the settings it had" for every failed reload.
// Measured on Debian 13 and Ubuntu 24.04: PostgreSQL had re-read its files
// although its unit reported the reload as failed, and a Postfix and a Dovecot
// that were not running at all got the same sentence. So:
//
//   - "reload": the reload was reported as failed, and which settings the
//     service runs with was not read. Neither is claimed.
//   - "reload_reread" / "reload_not_reread": PostgreSQL only, whose running
//     server was asked before and after (pg_conf_load_time()).
//   - "not_running": an unmet prerequisite, answered 409: there was nothing to
//     reload, nothing was changed, and Start is the action that applies.
//
// Başarısız bir yeniden yükleme yalnızca doğrulananı söyler. Cümle eskiden her
// başarısız yeniden yükleme için "önceki ayarlarıyla çalışmayı sürdürüyor" diye
// bitiyordu; bu ölçümde iki durumda doğru değildi.
var serviceActionFailedMessages = map[string]string{
	"check": "Nothing was changed: the service's own check refuses its configuration, so the action was not carried out." + serviceActionResume,
	"reload": "The service reported that the reload failed. CelikPanel cannot read from this service which settings it is running with now, so it says neither that it kept the settings it had nor that it took the files on disk." +
		serviceActionResume,
	transport.ServiceActionStageReloadReread: "The unit reported the reload as failed, but PostgreSQL itself re-read its configuration files after it: the settings in the files on disk are in effect now, except those that need a restart. " +
		"A step of the unit's own reload command failed after the server had been signalled. " +
		"The server owner runs the command shown to see which step, and corrects it so that the next reload is reported as it went; the reload does not need to be repeated for these settings.",
	transport.ServiceActionStageReloadNotReread: "The reload failed and PostgreSQL did not re-read its configuration files: it is running with the settings it had before." + serviceActionResume,
	transport.ServiceActionStageNotRunning: "The service is not running, so there was nothing to reload and nothing was changed. " +
		"If it should run, the server owner starts it with Start on this page; it reads its configuration files when it starts.",
	"start":   "The service did not start, or did not stay running." + serviceActionResume,
	"stop":    "The service did not stop: its daemon is still running." + serviceActionResume,
	"verify":  "The action was sent, but afterwards the service's daemon is not in the state that was asked for." + serviceActionResume,
	"command": "The server's service manager did not carry out the action." + serviceActionResume,
}

const serviceActionFailedMessage = "The action did not take effect." + serviceActionResume

const serviceActionUnknownMessage = "The action was sent, but what came of it could not be verified, so it is not reported as done. " +
	"This is not a verified failure: the service may already be in the state that was asked for. " +
	"The server owner runs the command shown to see the service's state, and repeats this action here only if it is still needed; nothing repeats it automatically."

var serviceActionUnitName = regexp.MustCompile(`^[A-Za-z0-9@._:\\-]{1,128}$`)

// serviceActionCommand is the one command that shows the service's own answer.
// Postfix answers for itself on every platform; `systemctl status postfix` on
// Ubuntu answers for the wrapper unit.
func serviceActionCommand(unit, action string, reply *transport.ServiceActionResult) string {
	if unit == "postfix" {
		switch {
		case reply.Stage == "check":
			return "sudo postfix check"
		case action == "reload" && reply.Stage != transport.ServiceActionStageNotRunning:
			// Prints what Postfix objects to, and reloads when it objects to nothing.
			return "sudo postfix reload"
		}
		return "sudo postfix status"
	}
	target := unit
	if reply.Unit != "" && serviceActionUnitName.MatchString(reply.Unit) {
		target = reply.Unit
	}
	return "sudo systemctl status " + target
}

// writeServiceActionOutcome answers a service action the Agent classified as a
// verified failure or as unknown. It reports whether it wrote the answer; an
// answer the Agent did not classify is left to the caller.
func writeServiceActionOutcome(w http.ResponseWriter, unit, action string, reply *transport.ServiceActionResult) bool {
	if reply == nil || reply.Success || reply.Error == "" {
		return false
	}
	code, message, reason := "", "", ""
	switch reply.Outcome {
	case transport.ServiceActionFailed:
		code, message = errCodeServiceActionFailed, serviceActionFailedMessage
		if sentence, known := serviceActionFailedMessages[reply.Stage]; known {
			message, reason = sentence, reply.Stage
		}
	case transport.ServiceActionUnknown:
		code, message = errCodeServiceActionUnknown, serviceActionUnknownMessage
	default:
		return false
	}
	vars := map[string]string{"unit": unit, "action": action, "command": serviceActionCommand(unit, action, reply)}
	if detail := boundedAgentDiagnostic(reply.Detail); detail != "" {
		vars["detail"] = detail
	}
	if reply.Unit != "" && serviceActionUnitName.MatchString(reply.Unit) {
		vars["owner_unit"] = reply.Unit
	}
	// A stopped service that was asked to reload is an unmet prerequisite, not
	// a failure of something that was attempted.
	status := http.StatusBadGateway
	if reason == transport.ServiceActionStageNotRunning {
		status = http.StatusConflict
	}
	log.Printf("[%d][service action] %s %s: %s %s: %s", status, action, unit, code, reason, boundedAgentDiagnostic(reply.Error))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: message, Code: code, Reason: reason, Vars: vars})
	return true
}
