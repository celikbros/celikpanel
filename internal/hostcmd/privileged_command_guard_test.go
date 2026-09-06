package hostcmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A helper is only advice. Someone will call exec directly six months from
// now, read err.Error(), and ship "exit status 1" to an operator again - that
// is exactly how R-053 and R-054 happened after R-046 had already been fixed.
// This file is the part that is not advice.
//
// What it can honestly detect is a source fact, not a semantic one: where a
// process is launched, where the hidden stderr is read, and whether a call
// site that repeats a command's own words wrote down why. It cannot prove that
// a failure was reported well. So it freezes the surface instead: every place
// that launches a privileged process is named here with a reason, and a new
// one fails this test until somebody adds it and says what it is for. That is
// a smaller claim than "no reason is ever discarded", and it is a claim that
// is actually true.
//
// Bir yardimci yalnizca ogutur. Alti ay sonra biri dogrudan exec cagirip
// err.Error() okuyacak ve operatore yine "exit status 1" gonderecek - R-053 ve
// R-054 tam olarak boyle oldu. Bu dosya ogut olmayan kisimdir. Durustce tespit
// edebildigi sey anlamsal degil kaynak duzeyinde bir olgudur; bu yuzden yuzeyi
// dondurur: ayricalikli bir surec baslatan her yer burada bir gerekceyle
// adlandirilir ve yenisi, biri ekleyip ne icin oldugunu yazana kadar bu testi
// dusurur.

// scannedTrees are the trees that run privileged commands on an operator's
// server. web/ is not Go, artifacts/ and .attic/ are frozen copies of older
// trees kept for evidence, and neither ships.
// scannedTrees, bir operatorun sunucusunda ayricalikli komut calistiran
// agaclardir.
var scannedTrees = []string{"cmd", "internal", "deploy"}

var skippedDirectories = map[string]bool{
	".git": true, "node_modules": true, "testdata": true, "vendor": true,
	"artifacts": true, ".attic": true, "web": true, ".worktrees": true,
}

// privilegedCommandAllowlist is the explicit, named list of places that launch
// a process directly instead of going through a launcher whose failures are
// read by this package. Each key is "<path>:<command>", where the command is
// the literal argv[0] when there is one, "<computed>" when the name is a
// variable - which is what a launcher looks like - and "<reference>" when
// exec.Command is taken as a function value rather than called.
//
// Each value says why. A reason of the "not yet" kind is debt, is written down
// as debt, and is the list a later change works from.
//
// privilegedCommandAllowlist, bir sureci dogrudan baslatan yerlerin acik ve
// adlandirilmis listesidir. Her deger nedenini soyler; "henuz" turunden bir
// gerekce borctur ve borc olarak yazilmistir.
var privilegedCommandAllowlist = map[string]string{
	// The launchers. A direct exec here is the point of the file: each one
	// exists for a reason the others do not share, which is why this package
	// is a failure value and not a runner.
	// Calistiricilar. Buradaki dogrudan exec, dosyanin varlik nedenidir.
	"cmd/agent/mutation_command.go:<computed>":                         "the agent's tracked launcher: this is the exec the durable service mutation ledger owns, so a lost lease can kill the child",
	"cmd/agent/service_mutation_supervisor_linux.go:<computed>":        "the supervisor that re-executes the agent as its own mutation worker; it is the process boundary the ledger is built on",
	"cmd/agent/system_update_worker_linux.go:<computed>":               "the update worker's launcher, which must survive the panel it is replacing",
	"cmd/agent/mail_command.go:<computed>":                             "the mail launcher: gives a dispatched net/rpc handler an agent-owned deadline, which no shared runner could supply",
	"cmd/agent/panel_cert_command.go:<computed>":                       "the certificate launcher, with its own much longer deadline for certbot",
	"cmd/agent/backup_db.go:<reference>":                               "exec.CommandContext taken as a value so a test can substitute the launcher; the backup path's own seam",
	"cmd/panel/service_operation_services_stopped_linux.go:<computed>": "the panel's own systemd probe, which runs in the panel process and has no agent ledger to attach to",
	"internal/services/nginx_command.go:<computed>":                    "the nginx launcher: an agent-owned deadline for validation and reload",
	"internal/binddns/filesystem.go:<computed>":                        "the BIND launcher, in a package that cannot import cmd/agent",
	"internal/hostplatform/detect_linux.go:<computed>":                 "the platform probe's launcher; it runs before anything is installed",
	"internal/systemsqlite/owner_worker_linux.go:/proc/self/exe":       "re-executes this binary as the SQLite owner worker; not a host tool at all",

	// Read-only probes. A failure here means "this tool is not installed" or
	// "this setting is not set", the answer is the output, and there is no
	// operator instruction beyond what the caller already gives.
	// Salt-okunur yoklamalar. Buradaki basarisizlik "bu arac kurulu degil"
	// demektir; yanit ciktinin kendisidir.
	"cmd/agent/ssl_rpc.go:which":                       "presence probe for certbot",
	"cmd/agent/system_check_rpc.go:which":              "presence probes for nginx, apache, mysql, psql and php",
	"cmd/agent/runtime_rpc.go:node":                    "reads the installed node version",
	"cmd/agent/instance_rpc.go:node":                   "reads the installed node version",
	"cmd/agent/mail_rpc.go:doveconf":                   "reads a dovecot setting",
	"cmd/agent/mail_health_rpc.go:postconf":            "reads a postfix setting",
	"cmd/agent/mail_stack_rpc.go:postconf":             "reads postfix settings",
	"cmd/agent/introspect_rpc.go:postqueue":            "reads the mail queue",
	"cmd/agent/introspect_rpc.go:doveadm":              "reads dovecot state",
	"cmd/agent/introspect_rpc.go:nginx":                "reads the nginx configuration test result",
	"cmd/agent/service_journal_rpc.go:journalctl":      "reads a unit's log for the operator, who is shown it whole",
	"cmd/agent/app_rpc.go:journalctl":                  "reads an application's log for the operator",
	"cmd/agent/usage_rpc.go:du":                        "measures a home directory",
	"cmd/agent/instance_rpc.go:du":                     "measures a home directory",
	"cmd/agent/vpn_rpc.go:ip":                          "reads the default route",
	"internal/services/version_detector.go:which":      "presence probe",
	"internal/services/version_detector.go:mysql":      "reads the mysql client version",
	"internal/services/version_detector.go:mariadb":    "reads the mariadb client version",
	"internal/services/version_detector.go:postconf":   "reads the postfix version",
	"internal/services/version_detector.go:<computed>": "reads a service binary's version string",
	"internal/services/service_scanner.go:systemctl":   "reads a unit's ExecStart",
	"internal/services/php_manager.go:<computed>":      "reads or lists PHP extensions",
	"internal/services/user_manager.go:id":             "asks whether a system user exists",
	"cmd/agent/instance_rpc.go:<computed>":             "reads a runtime binary's version string",

	// Privileged mutations that are not read through this package yet. Each
	// one is the same shape the three fixes found, and each is written down as
	// debt rather than quietly allowed.
	// Bu paket uzerinden okunmayan ayricalikli mutasyonlar. Her biri borctur.
	"cmd/agent/database_rpc.go:sudo":                    "psql as the postgres role; its failures ARE read through hostcmd.Fail, but the launch is still direct - it predates the tracked launcher",
	"cmd/agent/database_rpc.go:mysql":                   "the mysql client; the caller replaces each failure with a fixed sentence of its own, so no client text escapes, but no reason survives either - debt",
	"cmd/agent/vpn_rpc.go:wg":                           "reads the live WireGuard interface; its dump carries private keys, so its failure must never be forwarded - debt tracked with R-055",
	"cmd/agent/cron_rpc.go:crontab":                     "reads and writes a user crontab; failures are not read yet - debt",
	"cmd/agent/site_rpc.go:pkill":                       "kills a site user's processes; a failure here is expected when there are none - debt",
	"cmd/agent/hosting_layout.go:chown":                 "sets ownership of a new hosting tree - debt",
	"cmd/agent/ssl_rpc.go:certbot":                      "issues certificates; failures go through certbotFirstError, which reads the output but only from stdout - debt",
	"cmd/agent/mail_rpc.go:doveadm":                     "creates mail credentials; its stdin carries a password - debt",
	"cmd/agent/mail_policy_rpc.go:postconf":             "writes postfix policy - debt",
	"cmd/agent/mail_policy_rpc.go:systemctl":            "reloads postfix after a policy write - debt",
	"cmd/agent/mail_stack_rpc.go:systemctl":             "reads and drives mail units - debt",
	"cmd/agent/app_rpc.go:systemctl":                    "drives an application's unit - debt",
	"cmd/agent/instance_rpc.go:systemctl":               "drives an instance's unit - debt",
	"cmd/agent/introspect_rpc.go:systemctl":             "drives units from the introspection RPC - debt",
	"cmd/agent/introspect_rpc.go:postsuper":             "deletes queued mail - debt",
	"cmd/agent/introspect_rpc.go:fail2ban-client":       "unbans an address - debt",
	"cmd/agent/main.go:<computed>":                      "the legacy configuration validator's runner, which predates every launcher above - debt",
	"cmd/agent/cpmove_rpc.go:<computed>":                "account transfer archives - debt",
	"cmd/agent/dnssec_rpc.go:<computed>":                "pdnsutil, whose failures the DNS paths read themselves - debt",
	"internal/services/config_validation.go:<computed>": "validates a service configuration file - debt",
	"internal/services/config_validation.go:systemctl":  "restarts mariadb after a validated write - debt",
	"internal/services/mariadb_driver.go:mysql":         "the MariaDB client; its failure IS read, by WrapDatabaseEngineFailure, but it writes its own defaults-extra-file and so keeps its own launcher",
	"internal/services/php_utils.go:systemctl":          "reloads a PHP-FPM pool - debt",
	"internal/services/user_manager.go:useradd":         "creates a system user - debt",
	"internal/services/user_manager.go:userdel":         "deletes a system user - debt",
	"internal/services/user_manager.go:chpasswd":        "sets a system password; its stdin carries the password - debt",
	"internal/services/user_manager.go:chown":           "sets ownership of a user's tree - debt",
	"internal/systemd/manager.go:systemctl":             "the systemd manager - debt",
	"internal/systemd/state.go:systemctl":               "reads and sets unit state - debt",
}

// stderrReaderAllowlist names every file allowed to touch the stderr os/exec
// hides inside *exec.ExitError. There is one reader and one writer, and the
// writer has to be named too: the tracked runner fills the field precisely so
// the reader can find it.
//
// stderrReaderAllowlist, os/exec'in *exec.ExitError icinde sakladigi stderr'e
// dokunabilecek her dosyayi adlandirir: bir okuyucu ve bir yazici.
var stderrReaderAllowlist = map[string]string{
	"internal/hostcmd/hostcmd.go":          "the reader: hostcmd.Stderr, which is the whole point of this package",
	"cmd/agent/service_mutation_worker.go": "the writer: the tracked runner copies the child's stderr into the exit error so the reader above can find it",
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("no go.mod above the test's working directory")
		}
		directory = parent
	}
}

type sourceFile struct {
	relativePath string
	file         *ast.File
	fileSet      *token.FileSet
}

func releaseSourceFiles(t *testing.T, root string) []sourceFile {
	t.Helper()
	var files []sourceFile
	for _, tree := range scannedTrees {
		base := filepath.Join(root, tree)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		walkErr := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if skippedDirectories[entry.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fileSet := token.NewFileSet()
			parsed, parseErr := parser.ParseFile(fileSet, path, nil, 0)
			if parseErr != nil {
				return parseErr
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			files = append(files, sourceFile{
				relativePath: filepath.ToSlash(relative),
				file:         parsed,
				fileSet:      fileSet,
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", tree, walkErr)
		}
	}
	if len(files) == 0 {
		t.Fatal("the guard scanned nothing, which would make it pass by accident")
	}
	return files
}

// execImportName reports the local name os/exec is imported under, if it is.
// execImportName, os/exec'in hangi yerel adla ice aktarildigini bildirir.
func execImportName(file *ast.File) string {
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil || path != "os/exec" {
			continue
		}
		if imported.Name != nil {
			return imported.Name.Name
		}
		return "exec"
	}
	return ""
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// TestEveryPrivilegedLaunchIsNamedAndJustified is the guard. It fails when a
// process is launched from a place this list does not name.
//
// TestEveryPrivilegedLaunchIsNamedAndJustified muhafizdir.
func TestEveryPrivilegedLaunchIsNamedAndJustified(t *testing.T) {
	root := repositoryRoot(t)
	found := map[string]bool{}

	for _, source := range releaseSourceFiles(t, root) {
		alias := execImportName(source.file)
		if alias == "" {
			continue
		}
		ast.Inspect(source.file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok || receiver.Name != alias {
				return true
			}
			nameArgument := -1
			switch selector.Sel.Name {
			case "Command":
				nameArgument = 0
			case "CommandContext":
				nameArgument = 1
			default:
				return true
			}
			// A call names its command; a bare reference hands the launcher
			// itself to somebody else, which is a launch too.
			// Bir cagri komutunu adlandirir; ciplak bir referans ise
			// calistiricinin kendisini baskasina verir.
			command := "<reference>"
			if call := enclosingCall(source.file, selector); call != nil {
				command = "<computed>"
				if nameArgument < len(call.Args) {
					if literal, ok := stringLiteral(call.Args[nameArgument]); ok {
						command = literal
					}
				}
			}
			found[source.relativePath+":"+command] = true
			return true
		})
	}

	var unnamed []string
	for key := range found {
		if _, ok := privilegedCommandAllowlist[key]; !ok {
			unnamed = append(unnamed, key)
		}
	}
	sort.Strings(unnamed)
	if len(unnamed) > 0 {
		t.Fatalf("these launch a process from a place nothing accounts for:\n  %s\n\n"+
			"Run it through the launcher its package already has, and read its failure with "+
			"hostcmd.Fail (safe) or hostcmd.FailVerbatim (which makes you say why the words "+
			"may be repeated). If it genuinely cannot use either, add it to "+
			"privilegedCommandAllowlist in this file with the reason.",
			strings.Join(unnamed, "\n  "))
	}

	var stale []string
	for key := range privilegedCommandAllowlist {
		if !found[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("privilegedCommandAllowlist names launches that are no longer there; "+
			"delete them so the list keeps meaning something:\n  %s",
			strings.Join(stale, "\n  "))
	}
}

// enclosingCall returns the call expression a selector is the function of.
// enclosingCall, bir seciciyi islevi olarak kullanan cagriyi dondurur.
func enclosingCall(file *ast.File, selector *ast.SelectorExpr) *ast.CallExpr {
	var found *ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && call.Fun == ast.Expr(selector) {
			found = call
			return false
		}
		return true
	})
	return found
}

// TestTheHiddenStderrIsReadInOnePlace: R-054's whole lesson was that os/exec
// hides a command's explanation inside the exit error. A second reader of that
// field is a second copy of this package's reason to exist, and copies stop
// being maintained - which is how the same defect reached three paths.
//
// TestTheHiddenStderrIsReadInOnePlace: R-054'un dersi, os/exec'in bir komutun
// aciklamasini cikis hatasi icinde sakladigiydi. O alanin ikinci bir okuyucusu
// bu paketin varlik nedeninin ikinci bir kopyasidir.
func TestTheHiddenStderrIsReadInOnePlace(t *testing.T) {
	root := repositoryRoot(t)
	var offenders []string

	for _, source := range releaseSourceFiles(t, root) {
		alias := execImportName(source.file)
		if alias == "" {
			continue
		}
		exitErrorNames := map[string]bool{}
		ast.Inspect(source.file, func(node ast.Node) bool {
			star, ok := node.(*ast.StarExpr)
			if !ok {
				return true
			}
			selector, ok := star.X.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "ExitError" {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok || receiver.Name != alias {
				return true
			}
			// The name bound to this type in the declaration that owns it.
			// Bu turu sahiplenen bildirimde ona baglanan ad.
			for _, name := range declaredNames(source.file, star) {
				exitErrorNames[name] = true
			}
			return true
		})
		if len(exitErrorNames) == 0 {
			continue
		}
		ast.Inspect(source.file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Stderr" {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok || !exitErrorNames[receiver.Name] {
				return true
			}
			if _, allowed := stderrReaderAllowlist[source.relativePath]; allowed {
				return true
			}
			offenders = append(offenders,
				source.relativePath+":"+strconv.Itoa(source.fileSet.Position(selector.Pos()).Line))
			return true
		})
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("these touch the stderr os/exec hides inside *exec.ExitError:\n  %s\n\n"+
			"Read it through hostcmd.Stderr, hostcmd.Diagnostic or hostcmd.Reason instead, "+
			"so the recovery has one home. If this really is a second home, name it in "+
			"stderrReaderAllowlist with the reason.",
			strings.Join(offenders, "\n  "))
	}

	for path := range stderrReaderAllowlist {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("stderrReaderAllowlist names %s, which is not there: %v", path, err)
		}
	}
}

// declaredNames finds the identifiers a var declaration binds to a type.
// declaredNames, bir var bildiriminin bir ture bagladigi tanimlayicilari bulur.
func declaredNames(file *ast.File, typeExpression ast.Expr) []string {
	var names []string
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || spec.Type != typeExpression {
			return true
		}
		for _, name := range spec.Names {
			names = append(names, name.Name)
		}
		return true
	})
	return names
}

// TestRepeatingACommandsOwnWordsAlwaysStatesWhy: the raw-forwarding path is
// the one that has to be asked for, and asking means saying why. A call site
// that passes an empty reason has not asked; it has only typed. hostcmd fails
// that closed at run time, and this fails it at build time, where it is
// cheaper to notice.
//
// TestRepeatingACommandsOwnWordsAlwaysStatesWhy: ham iletim yolu istenmesi
// gereken yoldur ve istemek nedenini soylemektir.
func TestRepeatingACommandsOwnWordsAlwaysStatesWhy(t *testing.T) {
	root := repositoryRoot(t)
	reasonArgument := map[string]int{"Verbatim": 2, "FailVerbatim": 3}
	var offenders []string

	for _, source := range releaseSourceFiles(t, root) {
		if strings.HasPrefix(source.relativePath, "internal/hostcmd/") {
			continue
		}
		ast.Inspect(source.file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok || receiver.Name != "hostcmd" {
				return true
			}
			index, watched := reasonArgument[selector.Sel.Name]
			if !watched || index >= len(call.Args) {
				return true
			}
			position := source.relativePath + ":" +
				strconv.Itoa(source.fileSet.Position(call.Pos()).Line)
			switch reason := call.Args[index].(type) {
			case *ast.BasicLit:
				if text, ok := stringLiteral(reason); !ok || strings.TrimSpace(text) == "" {
					offenders = append(offenders, position+" (an empty reason)")
				}
			case *ast.Ident:
				// A named constant is the preferred form: it puts the reason
				// somewhere a reviewer reads it once and everyone reuses it.
				// Adlandirilmis bir sabit tercih edilen bicimdir.
			case *ast.BinaryExpr:
				// A reason assembled from string literals.
			default:
				offenders = append(offenders,
					position+" (a reason that is not a literal or a named constant)")
			}
			return true
		})
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("these repeat a command's own words without saying why it is safe:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
