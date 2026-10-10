package transport

import "strings"

// A read that failed leaves the state unknown, and "unknown" was all the Agent
// said: the Panel then told the owner to check that the service was running,
// which was not the cause in any measured case (10 Oct 2026: /etc/cron.allow
// without the user, a relocated cron spool, a main.cf Postfix refuses). The
// fixed sentence stays the first line of the answer, so what it means does not
// change; two optional lines may follow it:
//
//	cause=<token>   a cause the Agent verified itself (an Unreadable* value)
//	detail=<line>   the first line the server's own program printed, bounded,
//	                with password assignments blanked
//
// A Panel that does not know a cause token says only that the state could not
// be read and shows the line. No cause is ever asserted that was not verified.
//
// Başarısız okuma durumu bilinmez bırakır. Sabit cümle yanıtın ilk satırı
// olarak kalır; ardından isteğe bağlı iki satır gelebilir: Agent'ın kendisinin
// doğruladığı bir neden ve sunucunun kendi programının yazdığı ilk satır.
// Doğrulanmamış bir neden asla ileri sürülmez.
const (
	// The site user is not listed in /etc/cron.allow, which exists: crontab
	// refuses that user, also when root asks on the user's behalf (Debian and
	// Ubuntu cron). The Agent read the file and the user is not in it.
	UnreadableCronAllow = "cron_allow"
	// /etc/cron.deny lists the site user and there is no /etc/cron.allow.
	UnreadableCronDeny = "cron_deny"
	// Postfix's own program stopped on its configuration ("fatal: bad ...
	// configuration", or a fatal that names main.cf or master.cf and a line).
	UnreadablePostfixConfig = "postfix_config"
)

const (
	unreadableCausePrefix  = "cause="
	unreadableDetailPrefix = "detail="
)

// UnreadableWithEvidence is the Agent's answer: the fixed sentence, then the
// cause and the detail when there are any.
// UnreadableWithEvidence, Agent'ın yanıtıdır: sabit cümle, varsa neden ve satır.
func UnreadableWithEvidence(sentence, cause, detail string) string {
	answer := sentence
	if cause = strings.TrimSpace(cause); cause != "" && !strings.ContainsAny(cause, "\r\n") {
		answer += "\n" + unreadableCausePrefix + cause
	}
	if detail = strings.Join(strings.Fields(detail), " "); detail != "" {
		answer += "\n" + unreadableDetailPrefix + detail
	}
	return answer
}

// UnreadableEvidence reports whether answer is the Agent's fixed sentence,
// alone or followed by evidence lines, and returns that evidence. Any other
// wording is not the known condition.
// UnreadableEvidence, yanıtın Agent'ın sabit cümlesi olup olmadığını bildirir
// ve varsa kanıtı döndürür.
func UnreadableEvidence(answer, sentence string) (known bool, cause, detail string) {
	lines := strings.Split(strings.TrimSpace(answer), "\n")
	if strings.TrimSpace(lines[0]) != sentence {
		return false, "", ""
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, unreadableCausePrefix) && cause == "":
			cause = strings.TrimPrefix(line, unreadableCausePrefix)
		case strings.HasPrefix(line, unreadableDetailPrefix) && detail == "":
			detail = strings.TrimPrefix(line, unreadableDetailPrefix)
		case line == "":
		default:
			// Extra wording the contract does not define: not the known answer.
			return false, "", ""
		}
	}
	return true, cause, detail
}
