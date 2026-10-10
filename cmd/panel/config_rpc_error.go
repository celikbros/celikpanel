package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// settingsResourceConfigFile names a managed configuration file in the three
// shared settings answers (current_settings_errors.go).
const settingsResourceConfigFile = "config_file"

// The sentence for each reason a configuration write was refused before
// anything was written (D-024): what is wrong, that nothing changed, the
// action. The line the service's own program said travels beside it in
// `vars.detail`, bounded and with the validation copy's name replaced; it is
// what the screen shows next to the field.
//
// Bir yapılandırma yazısının hiçbir şey yazılmadan reddedildiği her gerekçenin
// cümlesi: ne yanlış, hiçbir şeyin değişmediği, eylem. Hizmetin kendi
// programının söylediği satır `vars.detail` içinde yanında taşınır.
var configInvalidMessages = map[string]string{
	transport.ConfigInvalidEmpty: "Nothing was saved: the new content is empty, and CelikPanel does not replace a configuration file with nothing. " +
		"Reload the page to read the file again, then make the change.",
	transport.ConfigInvalidShape: "Nothing was saved: the new content is not a configuration file CelikPanel writes (it is larger than 1 MB or holds a NUL byte).",
	transport.ConfigInvalidSyntax: "Nothing was saved: a line that was changed or added is not one the service accepts. " +
		"Correct the line named below and save again.",
	transport.ConfigInvalidDaemon: "Nothing is changed: the service's own program read the new file and refused it. " +
		"Correct what it names below and save again.",
	transport.ConfigInvalidLockout: "Nothing was saved: this change would take away the local administrator access to PostgreSQL " +
		"(the rule that lets the server's own postgres account connect over the local socket). " +
		"CelikPanel and the owner's console both use it. Keep a `local all postgres peer` rule above any rule that would refuse it, then save again.",
	transport.ConfigInvalidNoValidator: "Nothing was saved: the program that checks this file before it replaces the current one could not be run on this server, " +
		"and CelikPanel does not install a database configuration it could not check. " +
		"The server owner can edit the file on the server and reload the service.",
}

const configInvalidGenericMessage = "Nothing is changed: the new configuration was refused. Correct it and save again."

// A reload failed after the file was written. Each sentence says only what the
// Agent verified (10 Oct 2026). Measured on three platforms: the previous file
// was back in place and the answer said it could not be put back. <unit> is
// replaced by the unit the Agent names.
// Dosya yazıldıktan sonra yeniden yükleme başarısız oldu. Her cümle yalnız
// Agent'ın doğruladığını söyler.
var configReloadFailedMessages = map[string]string{
	transport.ConfigReloadRestored: "The change was not kept: the service could not reload with the new file, so CelikPanel put the previous file back and the service is running with it. " +
		"What the service said is below. Correct the setting and save again.",
	transport.ConfigReloadRestoredUnitFailed: "The change was not kept, and the previous file is back in place. The service's systemd unit could not reload, with the new file and again with the previous one, " +
		"so CelikPanel asked the server directly: it read the previous file again and is running with the settings it had before your change. " +
		"What the unit's reload said is below. It failed with the previous file too, so the cause is not only this change. " +
		"The server owner runs sudo systemctl reload <unit> on the server, corrects what it reports, and then saves the change here again.",
	transport.ConfigReloadRestoredUnknown: "The change was not kept, and the previous file is back in place. The service's systemd unit could not reload, with the new file and again with the previous one, " +
		"and CelikPanel could not establish which settings the service is running with now: a reload that fails part-way may already have made it read the new file. " +
		"What the unit's reload said is below. The server owner runs sudo systemctl reload <unit> on the server, corrects what it reports, and reloads this page.",
	transport.ConfigReloadNotRestored: "The service could not reload with the new file, and CelikPanel could not put the previous file back with certainty. " +
		"The server owner checks the file on the server; the copy named below holds the previous file. " +
		"Then reload the service (sudo systemctl reload <unit>) and reload this page.",
}

// configUnitName is a unit name the Agent may put into a sentence.
var configUnitName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@_.:-]{0,127}$`)

func writeConfigRPCError(w http.ResponseWriter, rpcErr *transport.ConfigRPCError) {
	if rpcErr == nil {
		writeServerError(w, fmt.Errorf("agent returned an empty configuration error"))
		return
	}

	message := strings.TrimSpace(rpcErr.Message)
	switch rpcErr.Code {
	case transport.ConfigErrorPathRefused:
		if message == "" {
			message = "configuration path refused"
		}
		writeCodedError(w, http.StatusForbidden, errCodeConfigPathRefused, message, "")
	case transport.ConfigErrorUnreadable:
		writeCurrentSettingsUnreadable(w, settingsResourceConfigFile)
	case transport.ConfigErrorVersionRequired:
		writeSettingsVersionRequired(w, settingsResourceConfigFile)
	case transport.ConfigErrorChanged:
		writeSettingsChanged(w, settingsResourceConfigFile)
	case transport.ConfigErrorValidationFail:
		sentence, known := configInvalidMessages[rpcErr.Reason]
		reason := rpcErr.Reason
		if !known {
			sentence, reason = configInvalidGenericMessage, ""
		}
		writeConfigRefusal(w, http.StatusUnprocessableEntity, errCodeConfigInvalid, sentence, reason, rpcErr)
	case transport.ConfigErrorReloadFailed:
		sentence, known := configReloadFailedMessages[rpcErr.Reason]
		reason := rpcErr.Reason
		if !known {
			sentence, reason = configReloadFailedMessages[transport.ConfigReloadNotRestored], transport.ConfigReloadNotRestored
		}
		unit := "<service>"
		if configUnitName.MatchString(rpcErr.Unit) {
			unit = rpcErr.Unit
		}
		sentence = strings.ReplaceAll(sentence, "<unit>", unit)
		writeConfigRefusal(w, http.StatusBadGateway, errCodeConfigReloadFailed, sentence, reason, rpcErr)
	default:
		// Unknown codes are protocol failures. Never downgrade them because
		// their human-readable text happens to contain a familiar phrase.
		writeServerError(w, fmt.Errorf("agent returned unknown configuration error code %q", rpcErr.Code))
	}
}

// writeConfigRefusal answers a refusal that carries evidence: one bounded line
// from the service (`detail`), the line of the file it is about (`line`) and
// the setting it names (`name`). They are values for the screen's own
// sentence, never a sentence themselves.
// writeConfigRefusal, kanıt taşıyan bir reddi yanıtlar.
func writeConfigRefusal(w http.ResponseWriter, status int, code, message, reason string, rpcErr *transport.ConfigRPCError) {
	vars := map[string]string{}
	if detail := boundedAgentDiagnostic(rpcErr.Detail); detail != "" {
		vars["detail"] = detail
	}
	if rpcErr.Line > 0 {
		vars["line"] = strconv.Itoa(rpcErr.Line)
	}
	if name := strings.TrimSpace(rpcErr.Name); name != "" && len(name) <= 200 {
		vars["name"] = name
	}
	if configUnitName.MatchString(rpcErr.Unit) {
		vars["unit"] = rpcErr.Unit
	}
	if len(vars) == 0 {
		vars = nil
	}
	log.Printf("[%d][config] refused: %s %s", status, code, reason)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: message, Code: code, Reason: reason, Vars: vars})
}
