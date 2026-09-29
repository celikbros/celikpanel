//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/bindroot"
)

// dpkg loads its statoverride database strictly before it unpacks or
// configures anything, and aborts the whole transaction when an entry names a
// user or group that no longer exists ("unknown system group 'bind' in
// statoverride file"). Earlier releases registered exactly such an entry for
// /var/cache/bind; after the owner purges bind9, whose postrm deletes the
// `bind` user and group, every package operation on the host fails - the
// owner's own apt-get as well as the product's reinstall (batch 6b cell c6).
//
// Before any product operation asks the package manager to install, this
// preflight reads the whole database (read-only) and resolves every named
// user and group:
//   - the product's own exact legacy entry with its group gone is removed,
//     exactly that line for exactly that path, read back, and one plain
//     sentence is logged;
//   - any other entry whose user or group is gone belongs to the owner or to
//     another package: nothing is changed and the operation is refused before
//     its first mutation, naming the entry and the owner's command, because
//     the package manager would fail anyway;
//   - an entry that cannot be read or resolved is unknown, not broken: it is
//     logged and the package manager is left to speak for itself.
//
// dpkg, statoverride veritabanında artık var olmayan bir kullanıcı ya da grup
// adlandıran bir girdi gördüğünde her işlemi durdurur. Önceki sürümler
// /var/cache/bind için böyle bir girdi kaydediyordu. Bu ön denetim paket
// yöneticisinden kurulum istenmeden önce veritabanını salt-okur okur: ürünün
// kendi tam eski girdisi grubu yoksa silinir; başka her bozuk girdi için hiçbir
// şey değiştirilmez ve işlem ilk değişiklikten önce, girdiyi ve sahibin
// komutunu adlandırarak reddedilir; okunamayan girdi bilinmeyendir, bozuk
// sayılmaz.

const (
	dpkgStatOverrideTimeout       = 20 * time.Second
	dpkgStatOverrideListLimit     = 256 << 10
	dpkgStatOverrideMaxEntries    = 4096
	dpkgStatOverrideLookupLimit   = 4 << 10
	dpkgStatOverrideTextLimit     = 160
	dpkgStatOverrideNamedEntries  = 3
	dpkgStatOverrideMaxIdentities = 512
)

// packageStatOverridePreflight is replaced only by tests.
var packageStatOverridePreflight = hostPackageStatOverridePreflight

type dpkgStatOverrideEntry struct {
	user, group, mode, path string
}

func (entry dpkgStatOverrideEntry) line() string {
	return entry.user + " " + entry.group + " " + entry.mode + " " + entry.path
}

// dpkgIdentityLookup answers exists=true, exists=false (verified absent) or an
// error (unknown).
type dpkgIdentityLookup func(database, name string) (bool, error)

type dpkgStatOverridePreflightOps struct {
	list     func() ([]byte, error)
	listPath func(string) ([]byte, error)
	remove   func(string) ([]byte, error)
	lookup   dpkgIdentityLookup
	logf     func(string, ...any)
}

type dpkgBrokenStatOverride struct {
	entry dpkgStatOverrideEntry
	kind  string // "user" or "group"
	name  string
}

func hostPackageStatOverridePreflight(ctx context.Context) error {
	if ctx == nil {
		return errors.New("package manager preflight requires a context")
	}
	statoverride, err := firstTrustedExecutable(
		[]string{"/usr/bin/dpkg-statoverride", "/usr/sbin/dpkg-statoverride"},
		"dpkg-statoverride",
	)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) ||
			strings.HasPrefix(err.Error(), "no trusted ") {
			// Not a dpkg host: nothing to read.
			return nil
		}
		log.Printf("Package manager preflight could not verify dpkg-statoverride and left the check to the package manager: %v", err)
		return nil
	}
	getent, err := firstTrustedExecutable(
		[]string{"/usr/bin/getent", "/bin/getent"}, "getent",
	)
	if err != nil {
		log.Printf("Package manager preflight could not verify getent and left the check to the package manager: %v", err)
		return nil
	}
	proofCtx, cancel := context.WithTimeout(ctx, dpkgStatOverrideTimeout)
	defer cancel()
	run := func(limit int, name string, args ...string) ([]byte, error) {
		command := serviceMutationCommand(proofCtx, name, args...)
		command.Env = aptBINDStatOverrideCommandEnvironment()
		return command.execute(false, limit)
	}
	return runDpkgStatOverridePreflight(dpkgStatOverridePreflightOps{
		list: func() ([]byte, error) {
			return run(dpkgStatOverrideListLimit, statoverride, "--list")
		},
		listPath: func(path string) ([]byte, error) {
			return run(dpkgStatOverrideLookupLimit, statoverride, "--list", path)
		},
		remove: func(path string) ([]byte, error) {
			command := serviceMutationCommand(proofCtx, statoverride, "--remove", path)
			command.Env = aptBINDStatOverrideCommandEnvironment()
			return command.CombinedOutputLimited(dpkgStatOverrideLookupLimit)
		},
		lookup: func(database, name string) (bool, error) {
			output, err := run(dpkgStatOverrideLookupLimit, getent, database, name)
			return classifyGetentLookup(output, err)
		},
		logf: log.Printf,
	})
}

func classifyGetentLookup(output []byte, err error) (bool, error) {
	if err == nil && len(strings.TrimSpace(string(output))) != 0 {
		return true, nil
	}
	var exitCoder interface{ ExitCode() int }
	if len(output) == 0 && errors.As(err, &exitCoder) && exitCoder.ExitCode() == 2 {
		// getent(1): "One or more supplied key could not be found".
		return false, nil
	}
	if err == nil {
		err = errors.New("getent returned an empty record")
	}
	return false, err
}

func runDpkgStatOverridePreflight(ops dpkgStatOverridePreflightOps) error {
	if ops.list == nil || ops.listPath == nil || ops.remove == nil ||
		ops.lookup == nil || ops.logf == nil {
		return errors.New("invalid package manager preflight")
	}
	output, listErr := ops.list()
	entries, err := parseDpkgStatOverrideList(output, listErr)
	if err != nil {
		ops.logf("Package manager preflight could not read dpkg's permission overrides and left the check to the package manager: %v", err)
		return nil
	}
	resolved := map[string]bool{}
	lookups := 0
	exists := func(kind, name string) (bool, bool) {
		if strings.HasPrefix(name, "#") {
			// Numeric ids never need a lookup; dpkg accepts them as they are.
			return true, true
		}
		key := kind + ":" + name
		if known, ok := resolved[key]; ok {
			return known, true
		}
		if !validDpkgIdentityName(name) || lookups >= dpkgStatOverrideMaxIdentities {
			ops.logf("Package manager preflight could not check the %s %q named by a dpkg permission override and left it to the package manager", kind, boundedStatOverrideText(name))
			return false, false
		}
		lookups++
		database := "passwd"
		if kind == "group" {
			database = "group"
		}
		found, lookupErr := ops.lookup(database, name)
		if lookupErr != nil {
			ops.logf("Package manager preflight could not resolve the %s %q named by a dpkg permission override and left it to the package manager: %v", kind, boundedStatOverrideText(name), lookupErr)
			return false, false
		}
		resolved[key] = found
		return found, true
	}
	var own *dpkgStatOverrideEntry
	var foreign []dpkgBrokenStatOverride
	for index := range entries {
		entry := entries[index]
		userExists, userKnown := exists("user", entry.user)
		groupExists, groupKnown := exists("group", entry.group)
		switch {
		case entry.line()+"\n" == bindroot.APTExactStatOverrideLine &&
			userKnown && userExists && groupKnown && !groupExists:
			own = &entries[index]
		case userKnown && !userExists:
			foreign = append(foreign, dpkgBrokenStatOverride{entry: entry, kind: "user", name: entry.user})
		case groupKnown && !groupExists:
			foreign = append(foreign, dpkgBrokenStatOverride{entry: entry, kind: "group", name: entry.group})
		}
	}
	if len(foreign) != 0 {
		return &hostOperatorRefusal{sentence: brokenStatOverrideRefusal(foreign)}
	}
	if own == nil {
		return nil
	}
	return removeOwnStaleBINDStatOverride(ops)
}

// removeOwnStaleBINDStatOverride removes exactly the product's legacy entry,
// re-proving it for its path immediately before the removal and reading the
// result back.
func removeOwnStaleBINDStatOverride(ops dpkgStatOverridePreflightOps) error {
	path := bindroot.APTStatOverridePath
	before, beforeErr := ops.listPath(path)
	state, err := bindroot.ClassifyAPTStatOverride(before, beforeErr)
	if err != nil {
		return &hostOperatorRefusal{
			sentence: "Package installation was not started: dpkg's permission override for /var/cache/bind changed while CelikPanel was checking it, " +
				"so CelikPanel changed nothing. Start the same change again; if this repeats, the server owner inspects it with `dpkg-statoverride --list /var/cache/bind`.",
			cause: err,
		}
	}
	if state == bindroot.APTStatOverrideAbsent {
		return nil
	}
	removeOutput, removeErr := ops.remove(path)
	after, afterErr := ops.listPath(path)
	state, err = bindroot.ClassifyAPTStatOverride(after, afterErr)
	if err == nil && state == bindroot.APTStatOverrideAbsent {
		ops.logf("%s", ownStaleBINDStatOverrideRemovedLog)
		return nil
	}
	return &hostOperatorRefusal{
		sentence: "Package installation was not started: dpkg's permission override for /var/cache/bind, which an earlier CelikPanel release registered, " +
			"names the group 'bind', which no longer exists, and CelikPanel could not remove it, so dpkg would refuse every package change on this server. " +
			"Nothing else was changed. The server owner runs `dpkg-statoverride --remove /var/cache/bind`, then starts the same change again.",
		cause: errors.Join(removeErr, err, fmt.Errorf("dpkg-statoverride --remove output: %s", boundedStatOverrideText(string(removeOutput)))),
	}
}

const ownStaleBINDStatOverrideRemovedLog = "dpkg's permission override for /var/cache/bind that an earlier CelikPanel release registered named the group 'bind', which no longer exists " +
	"(the bind9 package was purged); CelikPanel removed exactly that override so the package manager can run again. " +
	ownerBINDRemovalStatOverrideAdvice

func brokenStatOverrideRefusal(broken []dpkgBrokenStatOverride) string {
	first := broken[0]
	path := boundedStatOverrideText(first.entry.path)
	sentence := fmt.Sprintf(
		"Package installation was not started: dpkg's permission override for %s names the %s '%s', which no longer exists, "+
			"so dpkg would refuse every package change on this server. CelikPanel did not create that override and changed nothing. "+
			"The server owner runs `dpkg-statoverride --remove %s` (or recreates that %s), then starts the same change again.",
		path, first.kind, boundedStatOverrideText(first.name), path, first.kind,
	)
	if len(broken) > 1 {
		others := make([]string, 0, dpkgStatOverrideNamedEntries)
		for _, item := range broken[1:] {
			if len(others) == dpkgStatOverrideNamedEntries {
				break
			}
			others = append(others, boundedStatOverrideText(item.entry.path))
		}
		sentence += fmt.Sprintf(
			" %d more override(s) have the same problem (%s); `dpkg-statoverride --list` shows them.",
			len(broken)-1, strings.Join(others, ", "),
		)
	}
	return sentence
}

// parseDpkgStatOverrideList reads `dpkg-statoverride --list`: one
// "user group mode path" entry per line; exit status 1 with no output means
// the database is empty.
func parseDpkgStatOverrideList(output []byte, err error) ([]dpkgStatOverrideEntry, error) {
	if err != nil {
		var exitCoder interface{ ExitCode() int }
		if len(output) == 0 && errors.As(err, &exitCoder) && exitCoder.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	text := string(output)
	if text == "" {
		return nil, nil
	}
	if !strings.HasSuffix(text, "\n") {
		return nil, errors.New("dpkg-statoverride --list output is not newline terminated")
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) > dpkgStatOverrideMaxEntries {
		return nil, fmt.Errorf("dpkg-statoverride lists more than %d overrides", dpkgStatOverrideMaxEntries)
	}
	entries := make([]dpkgStatOverrideEntry, 0, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, " ", 4)
		if len(fields) != 4 || fields[0] == "" || fields[1] == "" ||
			!validDpkgOverrideMode(fields[2]) || !strings.HasPrefix(fields[3], "/") ||
			strings.ContainsAny(line, "\t\r\x00") {
			return nil, fmt.Errorf("dpkg-statoverride returned an entry this check cannot read: %q", boundedStatOverrideText(line))
		}
		entries = append(entries, dpkgStatOverrideEntry{
			user: fields[0], group: fields[1], mode: fields[2], path: fields[3],
		})
	}
	return entries, nil
}

func validDpkgOverrideMode(mode string) bool {
	if mode == "" || len(mode) > 6 {
		return false
	}
	for _, r := range mode {
		if r < '0' || r > '7' {
			return false
		}
	}
	return true
}

func validDpkgIdentityName(name string) bool {
	if name == "" || len(name) > 64 || strings.HasPrefix(name, "-") {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '_' || r == '-' || r == '.' || r == '$') {
			return false
		}
	}
	return true
}

func boundedStatOverrideText(text string) string {
	text = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, text)), " ")
	runes := []rune(text)
	if len(runes) > dpkgStatOverrideTextLimit {
		return string(runes[:dpkgStatOverrideTextLimit]) + "..."
	}
	return text
}
