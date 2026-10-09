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
	// Read before a stop, so that a unit systemd shows as failed afterwards
	// can be told apart from one that already was.
	var watched []string
	notFailedBefore := map[string]bool{}
	if action == "stop" {
		watched = stopWatchedUnits(serviceID, unit)
		for _, name := range watched {
			if state, known := readUnitFailure(run, name); known && state.active != "failed" {
				notFailedBefore[name] = true
			}
		}
	}
	switch {
	case serviceID == "postfix" && unit == "postfix":
		result = postfixServiceAction(run, action)
	case serviceID == "dovecot" && unit == "dovecot":
		result = dovecotServiceAction(run, action)
	default:
		result = unitServiceAction(run, serviceID, unit, action)
	}
	if !result.Success {
		log.Printf("ERROR service %s %s: %s %s: %s", action, unit, result.Outcome, result.Stage, result.Error)
	} else if action == "stop" {
		noteStopLeftUnitFailed(run, serviceID, watched, notFailedBefore, &result)
	}
	return result
}

// A stop that succeeded and left the unit marked as failed (12 Oct 2026).
//
// Measured on Debian 13 and Ubuntu 24.04: Postfix with a main.cf it refuses is
// stopped from the Services page. The master is gone and the answer is a
// truthful success, and `systemctl status postfix` shows `failed (Result:
// exit-code)`: the unit's own stop command is `postfix stop`, which reads
// main.cf before it does anything, exits 1, and systemd then ends the processes
// itself. Nothing the Agent could send avoids it: systemd runs that stop
// command for every way of stopping the unit, also when the master is
// signalled directly.
//
// The mark is left as it is, and said. It is systemd's own record of what the
// unit's command did, and the same record the server owner's own
// `systemctl stop postfix` leaves on this host, so the Panel adds no native
// state of its own. Clearing it (`systemctl reset-failed`) would remove from
// `systemctl --failed` and from monitoring the one native sign that the
// configuration is refused and that the service cannot be started again as it
// is, and it would reset the unit's start-limit counters, which a Stop was not
// asked to do. What was missing was the answer: a success that did not say
// what the owner would find. The answer now carries it
// (transport.ServiceActionNoticeUnitFailed) with the unit, systemd's result
// and, for Postfix, the line its own check prints now.
//
// Only a unit that was read as not failed before the stop and is failed after
// it is reported: a mark that was already there is not this action's.
//
// Başarılı olan ve birimi `failed` işaretli bırakan durdurma. İşaret
// systemd'nin, birimin kendi durdurma komutunun ne yaptığına dair kaydıdır;
// sunucu sahibinin kendi `systemctl stop postfix` komutu da aynı kaydı bırakır.
// Agent onu silmez; yanıt artık onu söyler.
func stopWatchedUnits(serviceID, unit string) []string {
	watched := []string{unit + ".service"}
	if serviceID == "postfix" && unit == "postfix" {
		// Ubuntu's daemon is the instance unit behind the wrapper.
		watched = append(watched, "postfix@-.service")
	}
	return watched
}

// readUnitFailure reads a unit's state and result. known is false when the
// unit is not loaded or could not be read.
func readUnitFailure(run mailServiceRunner, name string) (state memberUnitState, known bool) {
	out, err := run("systemctl", "show", name,
		"--property=LoadState", "--property=ActiveState", "--property=Result")
	if err != nil {
		return memberUnitState{}, false
	}
	values := systemdProperties(out)
	first := func(property string) string {
		if len(values[property]) == 0 {
			return ""
		}
		return values[property][0]
	}
	if first("LoadState") != "loaded" || first("ActiveState") == "" {
		return memberUnitState{}, false
	}
	return memberUnitState{active: first("ActiveState"), result: first("Result")}, true
}

func noteStopLeftUnitFailed(run mailServiceRunner, serviceID string, watched []string, notFailedBefore map[string]bool, result *transport.ServiceActionResult) {
	for _, name := range watched {
		if !notFailedBefore[name] {
			continue
		}
		state, known := readUnitFailure(run, name)
		if !known || state.active != "failed" {
			continue
		}
		result.Notice, result.NoticeUnit, result.NoticeResult = transport.ServiceActionNoticeUnitFailed, name, state.result
		if serviceID == "postfix" {
			// What Postfix's own check prints now; the unit's stop command
			// reads the same main.cf. Empty when the check passes.
			if out, err := run("postfix", "check"); err != nil && mailServiceExited(err) {
				result.NoticeDetail = mailServiceLine(out)
			}
		}
		return
	}
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
		// Only a reload ends here: a stopped daemon was left stopped. It is
		// said as that, not as a reload that failed on a running service
		// (measured 2026-10-09: the answer read "keeps running with the
		// settings it had" for a Postfix and a Dovecot that were not running).
		detail := service + " is not running; nothing was reloaded"
		return transport.ServiceActionResult{
			Error: detail, Outcome: transport.ServiceActionFailed, Stage: transport.ServiceActionStageNotRunning, Detail: detail,
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

// postgresConfigReading is what the running PostgreSQL says about itself and
// about the last time it re-read its configuration files. It is read, never
// caused: no signal is sent to get it.
type postgresConfigReading struct {
	// postmaster is the first line of the data directory's postmaster.pid;
	// started is pg_postmaster_start_time(). Together they say that two
	// readings are of the same server process.
	postmaster int
	started    string
	// loaded is pg_conf_load_time(): PostgreSQL moves it only when a re-read
	// of its configuration files got to the end without an error.
	loaded string
}

// readPostgresConfigReading asks the running PostgreSQL over the local socket,
// the way the configuration writer does (db_config.go). nil when it could not
// be asked or did not answer all three facts.
func readPostgresConfigReading() *postgresConfigReading {
	out, err := dbConfigPostgreSQLQuery(
		"SELECT 'postmaster=' || split_part(pg_read_file('postmaster.pid'), chr(10), 1);\n" +
			"SELECT 'started=' || extract(epoch from pg_postmaster_start_time());\n" +
			"SELECT 'loaded=' || extract(epoch from pg_conf_load_time());")
	if err != nil {
		return nil
	}
	values := map[string]string{}
	for _, raw := range strings.Split(out, "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(raw), "="); ok {
			if _, seen := values[name]; !seen {
				values[name] = strings.TrimSpace(value)
			}
		}
	}
	reading := &postgresConfigReading{started: values["started"], loaded: values["loaded"]}
	reading.postmaster, _ = strconv.Atoi(values["postmaster"])
	if reading.postmaster <= 0 || reading.started == "" {
		return nil
	}
	if _, err := strconv.ParseFloat(reading.loaded, 64); err != nil {
		return nil
	}
	return reading
}

// postgresReloadOutcome says what a reload that the unit reported as failed
// came to on the server itself (11 Oct 2026).
//
// Measured on Debian 13 and Ubuntu 24.04 with an owner's drop-in whose
// ExecReload signals the server and then fails: the unit's ReloadResult is
// `exit-code`, and PostgreSQL had re-read its files. The answer used to say
// "it keeps running with its previous settings", which nobody had read.
//
// before was read before the action. The answer is a stage only when both
// readings are of the same server process and that process is the unit's main
// process (unitPID, from systemd): then a later pg_conf_load_time() is "it
// re-read its files" and an unchanged one is "it did not". In every other
// case the answer is "" and nothing is claimed about the settings in effect.
//
// postgresReloadOutcome, birimin başarısız diye bildirdiği bir yeniden
// yüklemenin sunucunun kendisinde neye vardığını söyler. Yalnızca iki okuma da
// aynı sunucu sürecine ve o süreç birimin ana sürecine aitse bir yanıt verir;
// başka her durumda yürürlükteki ayarlar hakkında hiçbir şey ileri sürülmez.
func postgresReloadOutcome(before *postgresConfigReading, unitPID int) string {
	if before == nil || unitPID <= 0 || before.postmaster != unitPID {
		return ""
	}
	earlier, _ := strconv.ParseFloat(before.loaded, 64)
	// The postmaster takes up the signal a moment after it was sent; one more
	// reading after a short wait before "it did not re-read" is said.
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			mailServiceSleep(mailServiceSettle)
		}
		after := readPostgresConfigReading()
		if after == nil || after.postmaster != before.postmaster || after.started != before.started {
			return ""
		}
		if later, _ := strconv.ParseFloat(after.loaded, 64); after.loaded != before.loaded && later > earlier {
			return transport.ServiceActionStageReloadReread
		}
	}
	return transport.ServiceActionStageReloadNotReread
}

func postgresReloadDetail(stage, member, reported string) string {
	switch stage {
	case transport.ServiceActionStageReloadReread:
		return "the reload of " + member + " was reported as failed (" + reported + "), but PostgreSQL re-read its configuration files after it: pg_conf_load_time() moved"
	case transport.ServiceActionStageReloadNotReread:
		return "the reload of " + member + " failed (" + reported + ") and PostgreSQL did not re-read its configuration files: pg_conf_load_time() did not move"
	}
	return "the reload of " + member + " failed (" + reported + "); which settings it runs with now was not read"
}

// reloadTargetStopped answers, from states read before anything is sent, that
// nothing a reload of unit would reach is running. owner names the unit that
// runs the daemon when it is not the one acted on.
//
// A wrapper: every unit systemd propagates its reload to is `inactive` or
// `failed`. A wrapper with no such unit is not answered here. Any other unit:
// it is loaded and `inactive` or `failed` itself. A unit that is not loaded
// (no such unit on this server) is left to systemd's own words.
func reloadTargetStopped(run mailServiceRunner, unit string, wrapper bool, members []string, before map[string]memberUnitState) (detail, owner string, stopped bool) {
	if wrapper {
		if len(members) == 0 {
			return "", "", false
		}
		for _, member := range members {
			if !before[member].stopped() {
				return "", "", false
			}
		}
		return "none of the units behind " + unit + ".service is running (" + strings.Join(members, ", ") + " " + before[members[0]].describe() + "); nothing was reloaded", members[0], true
	}
	name := unit + ".service"
	out, err := run("systemctl", "show", name,
		"--property=LoadState", "--property=ActiveState", "--property=SubState", "--property=Result")
	if err != nil {
		return "", "", false
	}
	values := systemdProperties(out)
	first := func(property string) string {
		if len(values[property]) == 0 {
			return ""
		}
		return values[property][0]
	}
	state := memberUnitState{active: first("ActiveState"), sub: first("SubState"), result: first("Result")}
	if first("LoadState") != "loaded" || !state.stopped() {
		return "", "", false
	}
	return name + " is " + state.describe() + "; nothing was reloaded", "", true
}

// unitServiceAction acts on a unit that is neither Postfix nor Dovecot.
func unitServiceAction(run mailServiceRunner, serviceID, unit, action string) transport.ServiceActionResult {
	// PostgreSQL can be asked when it last re-read its files. That is read
	// before a reload, so that a reload reported as failed can be answered
	// with what the server did rather than with a guess.
	var postgresBefore *postgresConfigReading
	if serviceID == "postgresql" && action == "reload" {
		postgresBefore = readPostgresConfigReading()
	}
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

	if action == "reload" {
		// A reload of something that is not running reloads nothing (12 Oct
		// 2026). It used to be sent anyway and systemd's own refusal
		// ("postgresql.service is not active, cannot reload.") came back as a
		// failed command, for nginx, MariaDB and the PostgreSQL wrapper alike;
		// only Postfix and Dovecot were answered as not running. The rule is
		// the unit's own state, read before anything is sent: a wrapper is not
		// running when none of the units a reload reaches is active, any other
		// unit when it is `inactive` or `failed` itself. A state that could
		// not be read, or one in between (`activating`, `deactivating`), is
		// not this answer: the command is sent and its own answer stands.
		// Çalışmayan bir şeyin yeniden yüklenmesi hiçbir şeyi yeniden yüklemez.
		// Kural, hiçbir şey gönderilmeden önce okunan unit durumudur.
		if detail, owner, stopped := reloadTargetStopped(run, unit, wrapper != nil, members, before); stopped {
			return transport.ServiceActionResult{
				Error: detail, Outcome: transport.ServiceActionFailed, Stage: transport.ServiceActionStageNotRunning,
				Unit: owner, Detail: detail,
			}
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
			return result
		}
		if wrapper == nil && postgresBefore != nil {
			// A real PostgreSQL unit (Arch) whose reload command failed: the
			// server is asked what it did.
			if state, readErr := readMemberUnit(run, unit+".service"); readErr == nil && state.running() {
				if stage := postgresReloadOutcome(postgresBefore, state.pid); stage != "" {
					reported := state.reloadResult
					if reported == "" || reported == "success" {
						reported = "its reload command exited with an error"
					}
					result.Stage, result.Detail = stage, postgresReloadDetail(stage, unit+".service", reported)
					result.Error = result.Detail
				}
			}
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
	return verifyWrapperAction(run, unit, action, members, before, postgresBefore)
}

// verifyWrapperAction judges an action on a wrapper unit by the units behind
// it. systemctl has already reported success for the wrapper.
func verifyWrapperAction(run mailServiceRunner, unit, action string, members []string, before map[string]memberUnitState, postgresBefore *postgresConfigReading) transport.ServiceActionResult {
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
			return failed(transport.ServiceActionStageNotRunning, members[0], "none of the units behind it is running; nothing was reloaded")
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
				// The unit's reload command failed. That says nothing about
				// which settings the daemon runs with: a command that fails
				// part-way may have signalled it first. Only PostgreSQL can be
				// asked, and only its answer is repeated.
				stage := mailServiceStageReload
				if answered := postgresReloadOutcome(postgresBefore, now.pid); answered != "" {
					stage = answered
				}
				return failed(stage, member, postgresReloadDetail(stage, member, now.reloadResult))
			}
		}
	}
	result := transport.ServiceActionResult{Success: true, Outcome: transport.ServiceActionVerified, Applied: applied}
	if len(members) == 1 {
		result.Unit = members[0]
	}
	return result
}
