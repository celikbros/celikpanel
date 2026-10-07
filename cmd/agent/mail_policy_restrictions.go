package main

import (
	"regexp"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// smtpd_recipient_restrictions belongs to the server owner. The Panel manages
// one thing in it: the reject_rbl_client entries for plain DNSBL zones. Until
// 8 Oct 2026 every save replaced the whole value with three fixed entries plus
// the zones, which dropped anything the owner had added (check_policy_service,
// reject_unknown_sender_domain, an access table) on the next save (D-022).
//
// What this file does instead:
//
//   - Postfix splits the list on commas and whitespace alike, with braces
//     grouping one element, and takes an argument as simply the next element.
//     So keeping every element in its order keeps the owner's meaning, and the
//     Panel never needs to understand an entry it did not write.
//   - A change removes the managed zone entries that are no longer wanted and
//     adds the new ones next to the existing DNSBL entries, or at the end.
//     Nothing else moves. When the wanted zones are the ones already there the
//     value is not written at all.
//   - An empty value (stock Debian/Ubuntu) still receives the Panel's baseline.
//   - Where the right result is not certain the value is locked: the Panel
//     refuses the DNSBL change with a reason and the owner edits main.cf.
//
// smtpd_recipient_restrictions sunucu sahibinindir. Panel onun içinde tek bir
// şeyi yönetir: düz DNSBL bölgeleri için reject_rbl_client girdileri. 8 Eki
// 2026'ya kadar her kayıt bütün değeri üç sabit girdi ve bölgelerle
// değiştiriyor, sahibin eklediği her şeyi düşürüyordu (D-022). Artık her öge
// sırasıyla korunur; yalnız yönetilen bölge girdileri eklenir ya da çıkarılır;
// istenen bölgeler zaten oradaysa değer hiç yazılmaz; doğru sonuç kesin değilse
// değer kilitlenir ve Panel gerekçesiyle reddeder.

// The baseline written into an empty value: locals and authenticated users
// pass, open relay stays shut, and DNSBL rejections come after, so the
// server's own users are never DNSBL-blocked.
// Boş bir değere yazılan taban: yereller ve kimlikli kullanıcılar geçer, açık
// aktarım kapalı kalır, DNSBL retleri sonra gelir.
var mailPolicyBaseline = []string{"permit_mynetworks", "permit_sasl_authenticated", "reject_unauth_destination"}

const mailPolicyDNSBLRestriction = "reject_rbl_client"

// A zone the Panel manages is a plain host name. An entry with a reply filter
// (zen.spamhaus.org=127.0.0.[2..11]) is the owner's and is left alone.
// Panel'in yönettiği bölge düz bir alan adıdır; yanıt süzgeçli girdi sahibindir.
var mailPolicyZoneRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

func validDNSBLZone(zone string) bool {
	return len(zone) <= 253 && mailPolicyZoneRe.MatchString(zone)
}

type recipientRestrictionElement struct {
	text string
	// commaAfter records whether a comma separated this element from the next
	// one, so an untouched list keeps the owner's own style.
	commaAfter bool
}

type recipientRestrictions struct {
	elements []recipientRestrictionElement
	// zones are the managed DNSBL zones in the order found, lower case, once.
	zones []string
	// zoneAt maps the index of a managed reject_rbl_client element to its zone;
	// the zone itself is the next element.
	zoneAt map[int]string
	// lastManaged is the index of the last managed zone element, or -1.
	lastManaged int
	// lock is empty when the Panel can change the DNSBL entries with certainty.
	lock string
}

// splitRecipientRestrictions splits the way Postfix does: on commas and
// whitespace, except inside braces. ok is false for unbalanced braces.
// splitRecipientRestrictions, Postfix gibi böler: süslü ayraç dışında virgül ve
// boşlukta. Dengesiz ayraçta ok yanlıştır.
func splitRecipientRestrictions(value string) (elements []recipientRestrictionElement, ok bool) {
	depth, start := 0, -1
	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c == '{':
			if start < 0 {
				start = i
			}
			depth++
		case c == '}':
			if depth == 0 {
				return nil, false
			}
			depth--
		case depth == 0 && (c == ',' || c == ' ' || c == '\t' || c == '\r' || c == '\n'):
			if start >= 0 {
				elements = append(elements, recipientRestrictionElement{text: value[start:i]})
				start = -1
			}
			if c == ',' && len(elements) > 0 {
				elements[len(elements)-1].commaAfter = true
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if depth != 0 {
		return nil, false
	}
	if start >= 0 {
		elements = append(elements, recipientRestrictionElement{text: value[start:]})
	}
	if len(elements) > 0 {
		elements[len(elements)-1].commaAfter = false
	}
	return elements, true
}

// parseRecipientRestrictions reads the current value into the managed zones and
// everything else. It never fails: a value it cannot be certain about is
// returned locked, with no zones claimed.
// parseRecipientRestrictions, geçerli değeri yönetilen bölgelere ve geri kalana
// ayırır. Emin olamadığı değeri kilitli döndürür.
func parseRecipientRestrictions(value string) recipientRestrictions {
	parsed := recipientRestrictions{zoneAt: map[int]string{}, lastManaged: -1}
	elements, ok := splitRecipientRestrictions(value)
	if !ok {
		parsed.lock = transport.MailPolicyLockMalformed
		return parsed
	}
	parsed.elements = elements
	if strings.Contains(value, "$") {
		parsed.lock = transport.MailPolicyLockVariable
		return parsed
	}

	seenZone := map[string]bool{}
	have := map[string]bool{}
	// warn_if_reject turns the next restriction into a log-only test. A DNSBL
	// entry behind it is the owner's trial, and removing the entry alone would
	// move the modifier onto the owner's next restriction.
	// warn_if_reject sonraki kısıtı yalnız günlüğe yazan bir denemeye çevirir;
	// arkasındaki DNSBL girdisi sahibinindir.
	warned := false
	for i := 0; i < len(elements); i++ {
		name := strings.ToLower(elements[i].text)
		switch name {
		case "warn_if_reject":
			warned = true
			continue
		case mailPolicyDNSBLRestriction:
			if i+1 >= len(elements) {
				return recipientRestrictions{
					elements: elements, zoneAt: map[int]string{}, lastManaged: -1,
					lock: transport.MailPolicyLockMalformed,
				}
			}
			zone := strings.ToLower(elements[i+1].text)
			if !warned && validDNSBLZone(zone) {
				parsed.zoneAt[i] = zone
				parsed.lastManaged = i + 1
				if !seenZone[zone] {
					seenZone[zone] = true
					parsed.zones = append(parsed.zones, zone)
				}
			}
			i++
		default:
			if !warned {
				have[name] = true
			}
		}
		warned = false
	}

	switch {
	case len(elements) == 0:
	case !have["permit_mynetworks"] || !have["permit_sasl_authenticated"]:
		parsed.lock = transport.MailPolicyLockNoBaseline
	case parsed.lastManaged < 0 && isTerminalRestriction(elements[len(elements)-1].text):
		parsed.lock = transport.MailPolicyLockTerminal
	}
	return parsed
}

func isTerminalRestriction(text string) bool {
	switch strings.ToLower(text) {
	case "permit", "reject", "defer":
		return true
	}
	return false
}

// planRecipientRestrictions answers what a save that wants exactly these DNSBL
// zones writes. changed is false when the value already has them, whatever
// else it holds and even when it is locked: an unchanged setting never touches
// the owner's value. A non-empty lock means the change is refused.
// planRecipientRestrictions, tam bu DNSBL bölgelerini isteyen bir kaydın ne
// yazacağını yanıtlar. Değer onları zaten taşıyorsa changed yanlıştır; kilit
// doluysa değişiklik reddedilir.
func planRecipientRestrictions(current string, wanted []string) (next string, changed bool, lock string) {
	parsed := parseRecipientRestrictions(current)
	keep := map[string]bool{}
	for _, zone := range wanted {
		keep[zone] = true
	}
	present := map[string]bool{}
	for _, zone := range parsed.zones {
		present[zone] = true
	}
	var added []string
	for _, zone := range wanted {
		if !present[zone] {
			present[zone] = true
			added = append(added, zone)
		}
	}
	removes := false
	for _, zone := range parsed.zones {
		if !keep[zone] {
			removes = true
		}
	}
	if len(added) == 0 && !removes {
		return current, false, ""
	}
	if parsed.lock != "" {
		return "", false, parsed.lock
	}

	if len(parsed.elements) == 0 {
		parts := append([]string(nil), mailPolicyBaseline...)
		for _, zone := range added {
			parts = append(parts, mailPolicyDNSBLRestriction+" "+zone)
		}
		return strings.Join(parts, ", "), true, ""
	}

	// New entries follow the owner's separator style: commas unless the list
	// is written with spaces only.
	// Yeni girdiler sahibin ayraç biçimini izler.
	comma := len(parsed.elements) < 2
	for _, element := range parsed.elements {
		comma = comma || element.commaAfter
	}
	insertAt := len(parsed.elements) - 1
	if parsed.lastManaged >= 0 {
		insertAt = parsed.lastManaged
	}

	var out []recipientRestrictionElement
	for i := 0; i < len(parsed.elements); i++ {
		element := parsed.elements[i]
		if zone, managed := parsed.zoneAt[i]; managed {
			argument := parsed.elements[i+1]
			if keep[zone] {
				out = append(out, element, argument)
			} else if len(out) > 0 {
				out[len(out)-1].commaAfter = out[len(out)-1].commaAfter || argument.commaAfter
			}
			i++
		} else {
			out = append(out, element)
		}
		if i == insertAt {
			for _, zone := range added {
				if len(out) > 0 {
					out[len(out)-1].commaAfter = comma
				}
				out = append(out,
					recipientRestrictionElement{text: mailPolicyDNSBLRestriction},
					recipientRestrictionElement{text: zone, commaAfter: comma},
				)
			}
		}
	}

	var b strings.Builder
	for i, element := range out {
		b.WriteString(element.text)
		if i == len(out)-1 {
			break
		}
		if element.commaAfter {
			b.WriteByte(',')
		}
		b.WriteByte(' ')
	}
	return b.String(), true, ""
}
