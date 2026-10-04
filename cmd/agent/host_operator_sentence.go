package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostcmd"
)

// A privileged operation that fails for a reason the server owner can act on
// must say so where the owner reads it: the ledger job and the Panel. Before
// this, a DNS engine install whose apt-get failed reached both as "did not
// complete; inspect the agent log" (batch 6b cell c6), the third time a
// command's own output was discarded on its way up (after R-053/R-054/R-056).
//
// operatorSentencer marks an error whose sentence was authored for the
// operator: what happened, who acts, the next step, and only then any bounded,
// sanitized words of the tool itself. Error() keeps the full diagnostic for the
// Agent log; OperatorSentence() is what may be recorded durably and shown.
//
// Sunucu sahibinin düzeltebileceği bir nedenle başarısız olan ayrıcalıklı bir
// işlem bunu sahibin okuduğu yerde söylemelidir: defter işi ve Panel.
// OperatorSentence() kalıcı olarak kaydedilebilecek ve gösterilebilecek
// cümledir; Error() Agent günlüğü için tam teşhisi korur.
type operatorSentencer interface {
	OperatorSentence() string
}

// operatorSentenceOf returns the first operator sentence in err's chain.
func operatorSentenceOf(err error) string {
	var sentencer operatorSentencer
	if err == nil || !errors.As(err, &sentencer) {
		return ""
	}
	return strings.TrimSpace(sentencer.OperatorSentence())
}

// hostOperatorRefusal is a refusal the Agent authored completely; it carries
// no tool output.
type hostOperatorRefusal struct {
	sentence string
	cause    error
}

func (refusal *hostOperatorRefusal) Error() string {
	if refusal.cause == nil {
		return refusal.sentence
	}
	return refusal.sentence + ": " + refusal.cause.Error()
}

func (refusal *hostOperatorRefusal) Unwrap() error { return refusal.cause }

func (refusal *hostOperatorRefusal) OperatorSentence() string { return refusal.sentence }

// packageManagerCommandError is a failed package manager transaction. Error()
// is unchanged from the text every caller logged before ("<exit>: <output>");
// the operator sentence names the packages and ends with the package
// manager's own reason, bounded and sanitized.
type packageManagerCommandError struct {
	detail   string
	packages []string
	exit     string
	reason   string
	cause    error
}

func newPackageManagerCommandError(
	packages []string,
	out []byte,
	cause error,
) error {
	return &packageManagerCommandError{
		detail:   fmt.Sprintf("%v: %s", cause, strings.TrimSpace(string(out))),
		packages: append([]string(nil), packages...),
		exit:     hostcmd.Bounded(causeLine(cause), 60),
		reason:   hostcmd.PackageManagerReason(out, hostcmd.PackageManagerReasonLimit),
		cause:    cause,
	}
}

func causeLine(cause error) string {
	if cause == nil {
		return ""
	}
	return strings.Join(strings.Fields(cause.Error()), " ")
}

func (failure *packageManagerCommandError) Error() string { return failure.detail }

func (failure *packageManagerCommandError) Unwrap() error { return failure.cause }

func (failure *packageManagerCommandError) OperatorSentence() string {
	reason := failure.reason
	if reason == "" {
		reason = "no output"
	}
	advice := ""
	if strings.Contains(reason, "statoverride") {
		advice = ownerBINDRemovalStatOverrideAdvice + " "
	}
	return fmt.Sprintf(
		"The server's package manager did not install %s (%s). "+
			"The server owner fixes the problem it names on the server, then starts the same change again. %s"+
			"Package manager: %s",
		strings.Join(failure.packages, ", "), failure.exit, advice, reason,
	)
}

const (
	// dnsEngineSwitchIncompleteAgentLog is the wire text for a switch that
	// ended before its target with no operator sentence to carry. Wire
	// contract with cmd/panel (newDNSEngineAgentRejectedError): do not change.
	dnsEngineSwitchIncompleteAgentLog = "DNS engine switch did not complete; inspect the agent log"
	// dnsEngineSwitchIncompleteNamedPrefix precedes an operator sentence the
	// Panel records in the ledger job and shows. Wire contract with cmd/panel.
	dnsEngineSwitchIncompleteNamedPrefix = "DNS engine switch did not complete: "
	dnsEngineSwitchOperatorSentenceLimit = 600
)

// dnsEngineSwitchIncompleteText is the response text for a switch whose
// same-request proof showed it ended before its target (nothing switched).
func dnsEngineSwitchIncompleteText(err error) string {
	sentence := operatorSentenceOf(err)
	if sentence == "" {
		return dnsEngineSwitchIncompleteAgentLog
	}
	sentence = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, sentence)), " ")
	if runes := []rune(sentence); len(runes) > dnsEngineSwitchOperatorSentenceLimit {
		sentence = string(runes[:dnsEngineSwitchOperatorSentenceLimit]) + "..."
	}
	return dnsEngineSwitchIncompleteNamedPrefix + sentence
}

// ownerBINDRemovalStatOverrideAdvice is the residual exposure of hosts set up
// by releases that registered the override (Decision A, 2026-09-30): the entry
// stays in place for rollback compatibility, so an owner who removes BIND by
// hand also removes the entry.
const ownerBINDRemovalStatOverrideAdvice = "On a server set up by an earlier CelikPanel release, if you remove bind9 yourself, also run `dpkg-statoverride --remove /var/cache/bind`."
