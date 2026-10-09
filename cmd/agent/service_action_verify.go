package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostcmd"
	"github.com/alicelik/celikpanel/internal/transport"
)

// What a Start, Stop, Restart or Reload on the Services page came to
// (10 Oct 2026; D-024, D-025 invariant 2).
//
// The action used to be `systemctl <action> <unit>` and its exit status. That
// status is the job result of the unit that was named, and some packaged units
// run nothing themselves:
//
//   - Ubuntu 24.04 `postfix.service`: `Type=oneshot`, `ExecStart=/bin/true`,
//     `ExecReload=/bin/true`. The daemon is `postfix@-.service` (measured, set1
//     2026-10-08). "Reload" answered success while Postfix had not reloaded.
//   - Debian and Ubuntu `postgresql.service`: the same shape; the server is
//     `postgresql@<version>-<cluster>.service`.
//
// A stop or restart of such a unit is propagated to the units that are
// `PartOf=` it, a start pulls in the units it `Wants=`, a reload goes to the
// units with `ReloadPropagatedFrom=` it; none of their results is the named
// unit's result. And a unit that is real can still report a start that did not
// last: with `Type=simple` the job succeeds when the program was executed.
//
// So:
//
//   - Postfix and Dovecot are asked themselves, with the helpers the mail
//     settings use (mail_service_verify.go): Postfix's own check, its own
//     reload command, `postfix status` and the master's process ID; Dovecot's
//     `doveconf -n` and a main process that stays.
//   - Any other unit is first asked what it is. A unit that runs `/bin/true`
//     and has instance units behind it (`<name>@....service`) is judged by
//     those units' state after the action, read from systemd.
//   - A unit that runs its own daemon keeps its job result, as before.
//
// Three answers, never two: verified, a verified failure with the service's
// own line, or unknown. Unknown is never success.
//
// Hizmetler sayfasındaki Başlat, Durdur, Yeniden başlat ya da Yeniden yükle
// işleminin sonucu. Eskiden `systemctl` çıkış durumuydu; o durum adı verilen
// unit'in iş sonucudur ve bazı paketlenmiş unit'ler kendileri hiçbir şey
// çalıştırmaz (Ubuntu'da `postfix.service`, Debian ve Ubuntu'da
// `postgresql.service`). Postfix ve Dovecot'a kendileri sorulur; başka bir
// sarmalayıcı unit, arkasındaki örnek unit'lerin işlemden sonraki durumuyla
// değerlendirilir. Üç yanıt vardır: doğrulandı, doğrulanmış hata, bilinmiyor.

// serviceActionRunner runs the fixed commands of one service action. The
// action itself keeps the step's lease as its only limit, as it always had; a
// check gets the mail launcher's deadline so that a program that does not
// answer cannot hold the operation.
func serviceActionRunner(ctx context.Context, systemctl string) mailServiceRunner {
	return func(name string, args ...string) ([]byte, error) {
		if name != "systemctl" {
			return runMailTLSMutationCommand(ctx, name, args...)
		}
		if len(args) > 0 && args[0] == "show" {
			return runMailTLSMutationCommand(ctx, systemctl, args...)
		}
		return runServiceMutationCombinedOutput(ctx, systemctl, args...)
	}
}

// verifiedServiceAction performs one action on one managed unit and answers
// only with what was observed.
func verifiedServiceAction(run mailServiceRunner, serviceID, unit, action string) transport.ServiceActionResult {
	var result transport.ServiceActionResult
	switch {
	case serviceID == "postfix" && unit == "postfix":
		result = postfixServiceAction(run, action)
	case serviceID == "dovecot" && unit == "dovecot":
		result = dovecotServiceAction(run, action)
	default:
		result = unitServiceAction(run, unit, action)
	}
	if !result.Success {
		log.Printf("ERROR service %s %s: %s %s: %s", action, unit, result.Outcome, result.Stage, result.Error)
	}
	return result
}

func postfixServiceAction(run mailServiceRunner, action string) transport.ServiceActionResult {
	var state string
	var err error
	switch action {
	case "reload":
		state, err = applyPostfixVerified(run, mailServiceReload)
	case "restart":
		state, err = applyPostfixVerified(run, mailServiceRestart)
	case "start":
		state, err = applyPostfixVerified(run, mailServiceStart)
	case "stop":
		state, err = stopPostfixVerified(run)
	}
	return mailServiceActionResult("Postfix", action, state, err)
}

func dovecotServiceAction(run mailServiceRunner, action string) transport.ServiceActionResult {
	var state string
	var err error
	switch action {
	case "reload":
		state, err = applyDovecotVerified(run, mailServiceReload)
	case "restart":
		state, err = applyDovecotVerified(run, mailServiceRestart)
	case "start":
		state, err = applyDovecotVerified(run, mailServiceStart)
	case "stop":
		state, err = stopDovecotVerified(run)
	}
	return mailServiceActionResult("Dovecot", action, state, err)
}

func mailServiceActionResult(service, action, state string, err error) transport.ServiceActionResult {
	if err != nil {
		var failure *mailServiceError
		if !errors.As(err, &failure) {
			return transport.ServiceActionResult{Error: err.Error(), Outcome: transport.ServiceActionUnknown, Stage: mailServiceStageVerify}
		}
		result := transport.ServiceActionResult{
			Error: failure.Error(), Outcome: transport.ServiceActionFailed, Stage: failure.stage, Detail: failure.detail,
		}
		if failure.unknown {
			result.Outcome = transport.ServiceActionUnknown
		}
		return result
	}
	if state == mailServiceNotRunning {
		// Only a reload ends here: a stopped daemon was left stopped.
		detail := service + " is not running; nothing was reloaded"
		return transport.ServiceActionResult{
			Error: detail, Outcome: transport.ServiceActionFailed, Stage: mailServiceStageReload, Detail: detail,
		}
	}
	if state == "" {
		return transport.ServiceActionResult{Error: "invalid service action", Outcome: transport.ServiceActionUnknown}
	}
	return transport.ServiceActionResult{Success: true, Outcome: transport.ServiceActionVerified, Applied: state}
}

// systemdProperties parses `systemctl show` output. A property printed more
// than once (two ExecReload commands) keeps every value, in order.
func systemdProperties(out []byte) map[string][]string {
	values := map[string][]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok && name != "" {
			values[name] = append(values[name], value)
		}
	}
	return values
}

// wrapperUnit is a unit that runs nothing itself, with the instance units
// systemd says stand behind it.
type wrapperUnit struct {
	// wants: started with it. partOf: stopped and restarted with it. reloads:
	// reloaded with it. Each lists `<name>@....service` units only.
	wants, partOf, reloads []string
}

// inspectWrapperUnit asks systemd what unit is. It answers a wrapper only on
// positive evidence: a oneshot whose single start command is `true`. Anything
// else, and anything that could not be read, is not a wrapper as far as is
// known, and the unit keeps its own job result.
func inspectWrapperUnit(run mailServiceRunner, unit string) *wrapperUnit {
	if strings.Contains(unit, "@") {
		// An instance of a template is the unit that runs the daemon.
		return nil
	}
	out, err := run("systemctl", "show", unit+".service",
		"--property=Type", "--property=ExecStart", "--property=Wants",
		"--property=ConsistsOf", "--property=PropagatesReloadTo")
	if err != nil {
		return nil
	}
	values := systemdProperties(out)
	if len(values["Type"]) != 1 || values["Type"][0] != "oneshot" || len(values["ExecStart"]) != 1 {
		return nil
	}
	program := ""
	for _, field := range strings.Split(strings.Trim(values["ExecStart"][0], "{} "), ";") {
		if name, value, ok := strings.Cut(strings.TrimSpace(field), "="); ok && name == "path" {
			program = value
		}
	}
	if program != "/bin/true" && program != "/usr/bin/true" {
		return nil
	}
	instances := func(property string) []string {
		seen := map[string]bool{}
		var out []string
		for _, value := range values[property] {
			for _, name := range strings.Fields(value) {
				if strings.HasPrefix(name, unit+"@") && strings.HasSuffix(name, ".service") && !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
		}
		sort.Strings(out)
		return out
	}
	return &wrapperUnit{wants: instances("Wants"), partOf: instances("ConsistsOf"), reloads: instances("PropagatesReloadTo")}
}

type memberUnitState struct {
	active, sub, result string
	// reloadResult is systemd's verdict on the unit's last reload; execReload
	// is the reload commands with their last run (start time, process ID).
	reloadResult, execReload string
	pid                      int
}

func (s memberUnitState) running() bool { return s.active == "active" }
func (s memberUnitState) stopped() bool { return s.active == "inactive" || s.active == "failed" }
func (s memberUnitState) settling() bool {
	return s.active == "activating" || s.active == "deactivating" || s.active == "reloading"
}

func (s memberUnitState) describe() string {
	text := s.active
	if s.sub != "" {
		text += " (" + s.sub + ")"
	}
	if s.result != "" && s.result != "success" {
		text += ", result " + s.result
	}
	return text
}

func readMemberUnit(run mailServiceRunner, member string) (memberUnitState, error) {
	out, err := run("systemctl", "show", member,
		"--property=ActiveState", "--property=SubState", "--property=MainPID",
		"--property=Result", "--property=ReloadResult", "--property=ExecReload")
	if err != nil {
		return memberUnitState{}, fmt.Errorf("%s could not be read: %s", member, hostcmd.Bounded(mailServiceLine(out), 300))
	}
	values := systemdProperties(out)
	first := func(name string) string {
		if len(values[name]) == 0 {
			return ""
		}
		return values[name][0]
	}
	state := memberUnitState{
		active: first("ActiveState"), sub: first("SubState"), result: first("Result"),
		reloadResult: first("ReloadResult"), execReload: strings.Join(values["ExecReload"], "\n"),
	}
	state.pid, _ = strconv.Atoi(first("MainPID"))
	if state.active == "" {
		return memberUnitState{}, fmt.Errorf("%s did not report its state", member)
	}
	return state, nil
}

func unionOfUnits(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, name := range list {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// unitServiceAction acts on a unit that is neither Postfix nor Dovecot.
func unitServiceAction(run mailServiceRunner, unit, action string) transport.ServiceActionResult {
	wrapper := inspectWrapperUnit(run, unit)
	before := map[string]memberUnitState{}
	var members []string
	if wrapper != nil {
		switch action {
		case "start":
			members = wrapper.wants
		case "reload":
			members = wrapper.reloads
		default:
			members = unionOfUnits(wrapper.wants, wrapper.partOf)
		}
		for _, member := range members {
			state, err := readMemberUnit(run, member)
			if err != nil {
				// Nothing was sent yet; what stands behind the unit is unknown,
				// so its result could not be told afterwards either.
				return transport.ServiceActionResult{
					Error:   "nothing was changed: " + unit + ".service only groups other units, and " + err.Error(),
					Outcome: transport.ServiceActionUnknown, Stage: mailServiceStageCheck, Unit: member, Detail: err.Error(),
				}
			}
			before[member] = state
		}
	}

	out, err := run("systemctl", action, unit)
	if err != nil {
		result := transport.ServiceActionResult{
			Error:   firstLine(fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))),
			Outcome: transport.ServiceActionFailed, Stage: transport.ServiceActionStageCommand, Detail: mailServiceLine(out),
		}
		if !mailServiceExited(err) {
			// Not the command's own exit status: it may or may not have acted.
			result.Outcome = transport.ServiceActionUnknown
		}
		return result
	}
	if wrapper == nil {
		// The unit runs its own daemon: this is that unit's job result.
		return transport.ServiceActionResult{Success: true}
	}
	if action == "restart" {
		// A restart starts what the unit wants and restarts what was running;
		// a unit its owner had stopped and that is not wanted stays stopped.
		wanted := map[string]bool{}
		for _, member := range wrapper.wants {
			wanted[member] = true
		}
		var expected []string
		for _, member := range members {
			if wanted[member] || before[member].running() {
				expected = append(expected, member)
			}
		}
		members = expected
	}
	return verifyWrapperAction(run, unit, action, members, before)
}

// verifyWrapperAction judges an action on a wrapper unit by the units behind
// it. systemctl has already reported success for the wrapper.
func verifyWrapperAction(run mailServiceRunner, unit, action string, members []string, before map[string]memberUnitState) transport.ServiceActionResult {
	wrapperName := unit + ".service"
	unknown := func(member, detail string) transport.ServiceActionResult {
		return transport.ServiceActionResult{
			Error:   "systemctl " + action + " " + unit + " reported success, but " + wrapperName + " only groups other units and the result could not be verified: " + detail,
			Outcome: transport.ServiceActionUnknown, Stage: mailServiceStageVerify, Unit: member, Detail: detail,
		}
	}
	failed := func(stage, member, detail string) transport.ServiceActionResult {
		return transport.ServiceActionResult{
			Error:   "systemctl " + action + " " + unit + " reported success for " + wrapperName + ", which only groups other units, but " + detail,
			Outcome: transport.ServiceActionFailed, Stage: stage, Unit: member, Detail: detail,
		}
	}
	if action == "reload" {
		// Only what was running can be reloaded.
		var running []string
		for _, member := range members {
			if before[member].running() {
				running = append(running, member)
			}
		}
		if len(members) > 0 && len(running) == 0 {
			return failed(mailServiceStageReload, members[0], "none of the units behind it is running; nothing was reloaded")
		}
		members = running
	}
	if len(members) == 0 {
		return unknown("", "systemd names no unit behind it for this action")
	}

	applied := map[string]string{"start": mailServiceStarted, "restart": mailServiceRestarted, "stop": mailServiceStopped, "reload": mailServiceReloaded}[action]
	for _, member := range members {
		var now memberUnitState
		for attempt := 0; attempt < mailServiceStartPolls; attempt++ {
			if attempt > 0 {
				mailServiceSleep(mailServicePollInterval)
			}
			var err error
			if now, err = readMemberUnit(run, member); err != nil {
				return unknown(member, err.Error())
			}
			if !now.settling() {
				break
			}
		}
		was := before[member]
		switch action {
		case "start", "restart":
			if !now.running() {
				return failed(mailServiceStageStart, member, member+" is "+now.describe())
			}
			if action == "restart" && was.running() && was.pid > 0 && now.pid == was.pid {
				return failed(mailServiceStageVerify, member, fmt.Sprintf("the same main process (%d) of %s is still running: the restart did not reach it", now.pid, member))
			}
		case "stop":
			if !now.stopped() {
				return failed(mailServiceStageStop, member, member+" is "+now.describe())
			}
		case "reload":
			if !now.running() {
				return failed(mailServiceStageVerify, member, member+" is "+now.describe()+" after the reload")
			}
			if now.execReload == "" || now.reloadResult == "" {
				return unknown(member, member+" does not report a reload command or its result")
			}
			if now.execReload == was.execReload {
				return failed(mailServiceStageReload, member, "the reload did not reach "+member+": its reload command was not run")
			}
			if now.reloadResult != "success" {
				return failed(mailServiceStageReload, member, "the reload of "+member+" failed ("+now.reloadResult+"); it keeps running with its previous settings")
			}
		}
	}
	result := transport.ServiceActionResult{Success: true, Outcome: transport.ServiceActionVerified, Applied: applied}
	if len(members) == 1 {
		result.Unit = members[0]
	}
	return result
}
