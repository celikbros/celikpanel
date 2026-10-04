package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alicelik/celikpanel/internal/hostcmd"
)

// A component install that stops must say which component, which part of the
// install and the host's own reason, where the owner reads it (D-024). Before
// this, a setup wizard on Arch stopped at its mail step with only "The service
// could not be installed and verified." (upd1 finding P2); the reason existed
// only in the Panel journal.
//
// The guidance is derived from the failed operation's last durable phase and
// its in-memory cause. It is stored in the failed row's existing result_json
// under "failure" (no schema migration) and read back only for failed rows. The
// detail is one bounded line with URL credentials, hash-shaped tokens and
// key=value secrets removed; raw command output never leaves the Panel log.
//
// Duran bir bileşen kurulumu hangi bileşen, kurulumun hangi kısmı ve makinenin
// kendi nedenini sahibin okuduğu yerde söylemelidir (D-024). Bilgi, başarısız
// işlemin son kalıcı aşamasından ve bellek içi nedeninden türetilir; mevcut
// result_json alanında "failure" altında saklanır (şema geçişi yok). Ayrıntı,
// URL kimlik bilgileri, hash biçimli ve anahtar=değer gizli bilgiler çıkarılmış,
// sınırlı tek satırdır.

const (
	serviceFailureStepPreflight      = "preflight"
	serviceFailureStepPackageInstall = "package_install"
	serviceFailureStepConfigure      = "configure"
	serviceFailureStepUnitStart      = "unit_start"
	serviceFailureStepVerify         = "verify"

	serviceFailureDetailLimit = 180
	serviceFailureResultKey   = "failure"
)

// serviceFailureGuidanceCodes are the install failures whose cause is a host
// fact the owner can act on. Lease, persistence and platform failures keep
// their own specific texts.
var serviceFailureGuidanceCodes = map[string]bool{
	"service_install_failed":      true,
	"mail_profile_install_failed": true,
	"node_runtime_install_failed": true,
}

// A setup service child keeps its initial "queued" phase until the package
// transaction has returned (runServiceInstall advances only afterwards), so a
// failure there is the package install.
var serviceFailurePhaseSteps = map[string]string{
	"queued":      serviceFailureStepPackageInstall,
	"preflight":   serviceFailureStepPreflight,
	"installing":  serviceFailureStepPackageInstall,
	"downloading": serviceFailureStepPackageInstall,
	"configuring": serviceFailureStepConfigure,
	"mail-stack":  serviceFailureStepConfigure,
	"submission":  serviceFailureStepConfigure,
	"mail-tls":    serviceFailureStepConfigure,
	"syncing":     serviceFailureStepConfigure,
	"syncing_dns": serviceFailureStepConfigure,
	"starting":    serviceFailureStepUnitStart,
	"scanning":    serviceFailureStepVerify,
	"verifying":   serviceFailureStepVerify,
}

var (
	serviceFailureComponentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	serviceFailureHashPattern      = regexp.MustCompile(`\{[A-Za-z0-9._-]+\}\S*|\$[0-9a-z]{1,8}\$\S*`)
	serviceFailureKeyValuePattern  = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key|key|auth)\s*=\s*\S+`)
	serviceFailureProfilePrefix    = regexp.MustCompile(`^install profile service [a-z0-9-]+: `)
)

type serviceOperationFailureGuidance struct {
	Component string `json:"component"`
	Step      string `json:"step"`
	Detail    string `json:"detail,omitempty"`
}

// serviceFailureComponentAndStep names the component and the install step
// from the last phase the runner recorded. Mail profiles record
// profile/<profile>/<service>/<phase> for each service and
// profile/<profile>/<phase> for their own final steps.
func serviceFailureComponentAndStep(serviceID, phase string) (string, string) {
	component := serviceID
	last := phase
	if strings.HasPrefix(phase, "profile/") {
		parts := strings.Split(phase, "/")
		switch len(parts) {
		case 4:
			component, last = parts[2], parts[3]
		case 3:
			last = parts[2]
		}
	}
	if !serviceFailureComponentPattern.MatchString(component) {
		component = ""
	}
	return component, serviceFailurePhaseSteps[last]
}

// serviceFailureDetail is the one line of the cause an owner can act on. A
// package transaction contributes the package manager's own "error:"/"E:"
// line; anything else contributes its first line.
func serviceFailureDetail(step string, cause error) string {
	if cause == nil {
		return ""
	}
	text := strings.TrimSpace(cause.Error())
	line := ""
	if step == serviceFailureStepPackageInstall {
		line = hostcmd.PackageManagerReason([]byte(text), 0)
	}
	if line == "" {
		line, _, _ = strings.Cut(text, "\n")
	}
	return boundedServiceFailureDetail(serviceFailureProfilePrefix.ReplaceAllString(strings.TrimSpace(line), ""))
}

func boundedServiceFailureDetail(text string) string {
	if index := strings.IndexAny(text, "\r\n"); index >= 0 {
		text = text[:index]
	}
	// On one line this only flattens it and keeps scheme and host of any URL,
	// dropping user part, path and query that may carry tokens.
	text = hostcmd.PackageManagerReason([]byte(text), 0)
	text = serviceFailureHashPattern.ReplaceAllString(text, "[redacted]")
	text = serviceFailureKeyValuePattern.ReplaceAllString(text, "$1=[redacted]")
	text = strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > serviceFailureDetailLimit {
		text = strings.TrimSpace(string(runes[:serviceFailureDetailLimit-1])) + "…"
	}
	return text
}

// annotateServiceOperationFailure fills the guidance once, at the only place
// that holds both the final phase and the cause.
func annotateServiceOperationFailure(serviceID, phase string, failure *serviceOperationFailure) {
	if failure == nil || !serviceFailureGuidanceCodes[failure.Code] {
		return
	}
	component, step := serviceFailureComponentAndStep(serviceID, phase)
	if failure.Component == "" {
		failure.Component = component
	}
	if failure.Step == "" {
		failure.Step = step
	}
	if failure.Detail == "" {
		failure.Detail = serviceFailureDetail(failure.Step, failure.Cause)
	}
}

// withServiceFailureGuidance records the guidance in the failed row's result.
func withServiceFailureGuidance(result serviceOperationResult, failure *serviceOperationFailure) serviceOperationResult {
	if failure == nil || failure.Component == "" || failure.Step == "" {
		return result
	}
	if result == nil {
		result = serviceOperationResult{"success": false}
	}
	result[serviceFailureResultKey] = serviceOperationFailureGuidance{
		Component: failure.Component,
		Step:      failure.Step,
		Detail:    failure.Detail,
	}
	return result
}

// serviceFailureGuidanceFromResult reads the guidance back from a failed row.
// Anything malformed is ignored: the code and message stay authoritative.
func serviceFailureGuidanceFromResult(raw string) (serviceOperationFailureGuidance, bool) {
	var envelope struct {
		Failure *serviceOperationFailureGuidance `json:"failure"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil || envelope.Failure == nil {
		return serviceOperationFailureGuidance{}, false
	}
	guidance := *envelope.Failure
	if !serviceFailureComponentPattern.MatchString(guidance.Component) {
		return serviceOperationFailureGuidance{}, false
	}
	switch guidance.Step {
	case serviceFailureStepPreflight, serviceFailureStepPackageInstall, serviceFailureStepConfigure,
		serviceFailureStepUnitStart, serviceFailureStepVerify:
	default:
		return serviceOperationFailureGuidance{}, false
	}
	guidance.Detail = boundedServiceFailureDetail(guidance.Detail)
	return guidance, true
}
