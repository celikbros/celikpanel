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
//     ("check", "reload", "start", "stop", "verify", "command").
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

var serviceActionFailedMessages = map[string]string{
	"check":   "Nothing was changed: the service's own check refuses its configuration, so the action was not carried out." + serviceActionResume,
	"reload":  "The service was not reloaded and keeps running with the settings it had." + serviceActionResume,
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
		case action == "reload":
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
	log.Printf("[502][service action] %s %s: %s %s: %s", action, unit, code, reason, boundedAgentDiagnostic(reply.Error))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: message, Code: code, Reason: reason, Vars: vars})
	return true
}
