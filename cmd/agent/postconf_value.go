package main

import (
	"regexp"
	"strings"
)

// One value read from postconf (2026-10-09; D-025 invariants 2 and 4).
//
// The Agent's mail commands are run with both output streams in one buffer
// (runMailTLSCommand, runMailTLSMutationCommand, CombinedOutput), and postconf
// writes what it has to say about main.cf to its error stream while it prints
// the value that was asked for. Measured with the real postconf (Postfix
// 3.10.13 on Debian 13, a private configuration directory; evidence
// deploy/e2e/release-recovery/evidence/set4c-20261009; Postfix 3.8.6 on Ubuntu
// 24.04 in set4b), exit status 0 every time:
//
//   - a line with a comment after other text: the warning comes first, then
//     the value;
//   - an unused parameter: the value comes first, then the warning;
//   - a setting that is not set: an empty line, then the warning;
//   - started by its path, the message begins `/usr/sbin/postconf: warning: `,
//     started by its name, `postconf: warning: `.
//
// The readers took the trimmed buffer as the value. For a setting that is set
// that is two lines; for one that is not set it is the warning line alone,
// which is one line. The same reading on a private main.cf: `postconf -e
// tls_server_sni_maps=<that line>` exits 0 and writes the warning into
// main.cf as the setting's value; with a two-line value postconf refuses
// ("accepts no multi-line input") and the setting is not restored.
//
// The rule, for every such reading: a line that is postconf's own message is
// not part of the answer; what remains must be exactly one line, ended by its
// line break. That line is the value, and it may be empty (a setting that is
// not set). Anything else (no line, two lines, a line that was cut, a line of
// another program) is not a value: the reading is unknown, and the caller
// stops before it changes anything. Nothing is guessed from it.
//
// The streams are not read apart instead: the three runners and their test
// doubles share one signature that returns a single buffer, used for every
// mail command; changing it would reach every caller, and a command that was
// not ours to change could still mix the streams. One rule at the reading is
// the smaller change and is the rule the queue directory already follows.
//
// postconf'tan okunan tek bir değer. Agent posta komutlarını iki çıktı akışını
// tek arabellekte toplayarak çalıştırır; postconf ise istenen değeri yazarken
// main.cf hakkındaki uyarısını hata akışına yazar. Okuyucular kırpılmış
// arabelleği değer sayıyordu. Kural: postconf'un kendi iletisi olan satır
// yanıtın parçası değildir; geriye, satır sonuyla biten tam olarak bir satır
// kalmalıdır. O satır değerdir ve boş olabilir (ayarlanmamış bir ayar). Başka
// her şey bir değer değildir: okuma bilinmeyendir ve çağıran, hiçbir şeyi
// değiştirmeden durur.

// A line postconf writes about its own run: "postconf: warning: ...", or the
// same after the path it was started under.
var postconfOwnMessage = regexp.MustCompile(`^(?:\S*/)?postconf: (?:warning|error|fatal|panic): `)

// postconfOneValue picks the one value out of what `postconf -h <name>` (or
// `postconf -x -h <name>`) printed. known is false when the output, with
// postconf's own messages left out, is not exactly one line ended by its line
// break.
func postconfOneValue(out []byte) (value string, known bool) {
	found := false
	for _, line := range strings.SplitAfter(string(out), "\n") {
		if line == "" {
			continue
		}
		text, ended := strings.CutSuffix(line, "\n")
		text = strings.TrimSuffix(text, "\r")
		if postconfOwnMessage.MatchString(text) {
			if !ended {
				// A message that was cut: what else is missing is not known.
				return "", false
			}
			continue
		}
		if found || !ended {
			return "", false
		}
		value, found = strings.TrimSpace(text), true
	}
	return value, found
}

// postconfUnreadError is a Postfix setting whose value could not be read as
// one value. It says what was asked and what the server owner can run; the
// caller says what it did or did not change.
// postconfUnreadError, değeri tek bir değer olarak okunamayan Postfix ayarıdır.
type postconfUnreadError struct {
	// setting is the setting's name; command is the reading that was sent.
	setting, command string
}

func (e *postconfUnreadError) Error() string {
	return "the value of the Postfix setting " + e.setting + " is not known: `" + e.command +
		"` did not print exactly one value line besides its own messages. " +
		"The server owner runs `postconf -n` and `postfix check` on the server; they name the line of /etc/postfix/main.cf that Postfix objects to. " +
		"After it is corrected the same operation can be started again; nothing retries by itself"
}

// postconfRestorable reports that value is something a reading could have
// returned: one line that is not postconf's own message. A restore writes
// nothing else.
func postconfRestorable(value string) bool {
	return !strings.ContainsAny(value, "\r\n") && !postconfOwnMessage.MatchString(value)
}
