package main

import (
	"net"
	"strconv"
	"strings"
)

// pg_hba.conf cannot be checked by PostgreSQL before it is installed: the
// server only reads the file its hba_file setting names, and a reload that
// meets a bad file exits 0 and keeps the old rules, so the damage shows at the
// next start. Two things are therefore decided here, before anything is
// written (9 Oct 2026; D-022, D-025 invariants 1, 3 and 4):
//
//   - every line the write CHANGES OR ADDS must be a line PostgreSQL's own
//     parser accepts. The rules below are the ones of parse_hba_line; each
//     refusal was compared with what a PostgreSQL 17 server reports for the
//     same line in pg_hba_file_rules. Lines carried over unchanged are the
//     owner's and are not judged: the running server already holds them, and
//     a newer PostgreSQL may accept what this list does not know yet.
//   - the write must not take away the local administrator access. The Panel
//     reaches PostgreSQL the way the owner does at the console: the operating
//     system account `postgres` over the local socket. If the current file
//     grants that and the new one does not, or no longer grants it in a way
//     that can be read with certainty, the write is refused.
//
// After the file is installed the running server is asked about it
// (pg_hba_file_rules) when it can be reached; see db_config.go.
//
// pg_hba.conf, kurulmadan önce PostgreSQL'e denetlettirilemez. Bu yüzden
// yazıdan önce burada iki şeye karar verilir: yazının DEĞİŞTİRDİĞİ YA DA
// EKLEDİĞİ her satır PostgreSQL'in kendi ayrıştırıcısının kabul ettiği bir satır
// olmalıdır (değişmeden taşınan satırlar sahibindir ve yargılanmaz); ve yazı
// yerel yönetici erişimini kaldırmamalıdır.

// hbaLogicalLine is one rule of the file: the physical line it starts on, its
// exact text, and its fields with quoting removed. A blank or comment-only line
// has no fields.
type hbaLogicalLine struct {
	number int
	text   string
	fields []hbaField
}

// hbaField is one whitespace-separated field. A field is a comma-separated
// list; each item remembers whether it was quoted, because a quoted keyword is
// an ordinary name.
type hbaField struct {
	items []hbaItem
}

type hbaItem struct {
	text   string
	quoted bool
}

func (f hbaField) single() (hbaItem, bool) {
	if len(f.items) != 1 {
		return hbaItem{}, false
	}
	return f.items[0], true
}

func (f hbaField) plain() string {
	parts := make([]string, 0, len(f.items))
	for _, item := range f.items {
		parts = append(parts, item.text)
	}
	return strings.Join(parts, ",")
}

// parseHBA splits the file into logical lines. A backslash at the very end of a
// physical line continues it on the next one, as PostgreSQL 13 and later read
// it.
func parseHBA(content string) []hbaLogicalLine {
	var lines []hbaLogicalLine
	physical := strings.Split(content, "\n")
	for i := 0; i < len(physical); i++ {
		start := i
		text := strings.TrimSuffix(physical[i], "\r")
		joined := text
		for strings.HasSuffix(joined, "\\") && i+1 < len(physical) {
			i++
			next := strings.TrimSuffix(physical[i], "\r")
			text += "\n" + next
			joined = strings.TrimSuffix(joined, "\\") + next
		}
		lines = append(lines, hbaLogicalLine{number: start + 1, text: text, fields: hbaFields(joined)})
	}
	return lines
}

// hbaFields tokenises one logical line the way PostgreSQL's next_token does:
// blank space separates fields, double quotes keep blank space, commas and `#`
// inside a token, a comma joins list items (also across blank space after it),
// and an unquoted `#` ends the line.
func hbaFields(line string) []hbaField {
	var fields []hbaField
	var current hbaField
	inField := false
	i := 0
	for i < len(line) {
		c := line[i]
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if c == '#' {
			break
		}
		// One token.
		var token strings.Builder
		quoted := false
		inQuote := false
		trailingComma := false
		for i < len(line) {
			c = line[i]
			if inQuote {
				if c == '"' {
					if i+1 < len(line) && line[i+1] == '"' {
						token.WriteByte('"')
						i += 2
						continue
					}
					inQuote = false
					i++
					continue
				}
				token.WriteByte(c)
				i++
				continue
			}
			if c == '"' {
				inQuote, quoted = true, true
				i++
				continue
			}
			if c == ',' {
				trailingComma = true
				i++
				break
			}
			if c == ' ' || c == '\t' || c == '\r' || c == '#' {
				break
			}
			token.WriteByte(c)
			i++
		}
		if !inField {
			current = hbaField{}
			inField = true
		}
		current.items = append(current.items, hbaItem{text: token.String(), quoted: quoted})
		if !trailingComma {
			fields = append(fields, current)
			inField = false
		}
	}
	if inField {
		fields = append(fields, current)
	}
	return fields
}

var hbaConnectionTypes = map[string]bool{
	"local": true, "host": true, "hostssl": true, "hostnossl": true,
	"hostgssenc": true, "hostnogssenc": true,
}

// Every method a PostgreSQL release has had. Whether this server's build has
// one of the optional ones (bsd, sspi, oauth) is the server's answer, asked
// after the file is installed.
var hbaMethods = map[string]bool{
	"trust": true, "reject": true, "md5": true, "password": true, "scram-sha-256": true,
	"gss": true, "sspi": true, "ident": true, "peer": true, "pam": true, "bsd": true,
	"ldap": true, "radius": true, "cert": true, "oauth": true,
}

var hbaOptionNames = map[string]bool{
	"map": true, "clientcert": true, "clientname": true, "pamservice": true, "pam_use_hostname": true,
	"ldaptls": true, "ldapscheme": true, "ldapserver": true, "ldapport": true, "ldapbinddn": true,
	"ldapbindpasswd": true, "ldapsearchattribute": true, "ldapsearchfilter": true, "ldapbasedn": true,
	"ldapprefix": true, "ldapsuffix": true, "ldapurl": true, "krb_realm": true, "include_realm": true,
	"radiusservers": true, "radiusports": true, "radiussecrets": true, "radiusidentifiers": true,
	"issuer": true, "scope": true, "validator": true, "delegate_ident_mapping": true,
}

// hbaLineError returns why PostgreSQL would refuse this line, in PostgreSQL's
// own words where it has them, or "" when the line is one it accepts.
func hbaLineError(fields []hbaField) string {
	if len(fields) == 0 {
		return ""
	}
	first, ok := fields[0].single()
	if !ok {
		return "multiple values specified for connection type"
	}
	if !first.quoted {
		switch first.text {
		case "include", "include_if_exists", "include_dir":
			if len(fields) != 2 {
				return "an include directive takes exactly one file or directory name"
			}
			return ""
		}
	}
	if !hbaConnectionTypes[first.text] {
		return `invalid connection type "` + first.text + `"`
	}
	local := first.text == "local"
	if len(fields) < 2 {
		return "end-of-line before database specification"
	}
	if len(fields) < 3 {
		return "end-of-line before role specification"
	}
	next := 3
	if !local {
		if len(fields) < 4 {
			return "end-of-line before IP address specification"
		}
		address, ok := fields[3].single()
		if !ok {
			return "multiple values specified for host address"
		}
		next = 4
		keyword := !address.quoted && (address.text == "all" || address.text == "samehost" || address.text == "samenet")
		if !keyword {
			if slash := strings.IndexByte(address.text, '/'); slash >= 0 {
				ip := net.ParseIP(address.text[:slash])
				if ip == nil {
					return `specifying both host name and CIDR mask is invalid: "` + address.text + `"`
				}
				bits, err := strconv.Atoi(address.text[slash+1:])
				limit := 128
				if ip.To4() != nil {
					limit = 32
				}
				if err != nil || bits < 0 || bits > limit || strings.TrimSpace(address.text[slash+1:]) != address.text[slash+1:] {
					return `invalid CIDR mask in address "` + address.text + `"`
				}
			} else if ip := net.ParseIP(address.text); ip != nil {
				if len(fields) < 5 {
					return "end-of-line before netmask specification"
				}
				mask, ok := fields[4].single()
				if !ok {
					return "multiple values specified for netmask"
				}
				maskIP := net.ParseIP(mask.text)
				if maskIP == nil {
					return `invalid IP mask "` + mask.text + `"`
				}
				if (ip.To4() == nil) != (maskIP.To4() == nil) {
					return "IP address and mask do not match"
				}
				next = 5
			} else if address.text == "" {
				return "end-of-line before IP address specification"
			}
		}
	}
	if len(fields) <= next {
		return "end-of-line before authentication method"
	}
	method, ok := fields[next].single()
	if !ok {
		return "multiple values specified for authentication type"
	}
	next++
	if !hbaMethods[method.text] {
		return `invalid authentication method "` + method.text + `"`
	}
	switch {
	case local && (method.text == "gss" || method.text == "sspi"):
		return method.text + " authentication is not supported on local sockets"
	case !local && method.text == "peer":
		return "peer authentication is only supported on local sockets"
	case first.text != "hostssl" && method.text == "cert":
		return "cert authentication is only supported on hostssl connections"
	}
	for _, option := range fields[next:] {
		text := option.plain()
		equals := strings.IndexByte(text, '=')
		if equals <= 0 {
			return "authentication option not in name=value format: " + text
		}
		name := text[:equals]
		if !hbaOptionNames[name] {
			return `unrecognized authentication option name: "` + name + `"`
		}
		if name == "clientcert" && first.text != "hostssl" {
			return `clientcert can only be configured for "hostssl" rows`
		}
	}
	return ""
}

// validateHBACandidate refuses the first changed or added line PostgreSQL
// would not accept. A line whose exact text is in the current file is carried
// over and not judged.
func validateHBACandidate(current, candidate string) *configRefusal {
	carried := map[string]int{}
	for _, line := range parseHBA(current) {
		carried[line.text]++
	}
	for _, line := range parseHBA(candidate) {
		if carried[line.text] > 0 {
			carried[line.text]--
			continue
		}
		if reason := hbaLineError(line.fields); reason != "" {
			refusal := configInvalid("syntax",
				"a changed line of pg_hba.conf is not one PostgreSQL accepts; nothing was changed")
			refusal.detail = reason
			refusal.line = line.number
			return refusal
		}
	}
	return nil
}

const (
	hbaAdminGranted = "granted"
	hbaAdminDenied  = "denied"
	hbaAdminUnknown = "unknown"
)

// hbaLocalAdminAccess reads what the file decides for the one connection the
// Panel and the owner's console use: the operating system account `postgres`
// connecting as database user `postgres` to database `postgres` over the local
// socket. PostgreSQL uses the first rule that matches, so this walks the file
// in order and stops at the first rule that matches for certain. It answers
// "unknown" when a rule before that one might match and cannot be decided from
// the file alone (a name list in another file, a role membership, a pattern, an
// included file, a user-name map).
//
// It returns the line of the deciding rule, 0 when no rule decided.
func hbaLocalAdminAccess(content string) (string, int) {
	for _, line := range parseHBA(content) {
		if len(line.fields) == 0 {
			continue
		}
		first, ok := line.fields[0].single()
		if !ok {
			return hbaAdminUnknown, line.number
		}
		if !first.quoted && (first.text == "include" || first.text == "include_if_exists" || first.text == "include_dir") {
			return hbaAdminUnknown, line.number
		}
		if first.text != "local" {
			continue
		}
		if len(line.fields) < 4 {
			// Not a complete local rule; PostgreSQL would refuse the file.
			return hbaAdminUnknown, line.number
		}
		database := hbaMatches(line.fields[1], true)
		user := hbaMatches(line.fields[2], false)
		if database == hbaNo || user == hbaNo {
			continue
		}
		if database == hbaMaybe || user == hbaMaybe {
			return hbaAdminUnknown, line.number
		}
		method := line.fields[3].plain()
		switch method {
		case "trust":
			return hbaAdminGranted, line.number
		case "peer", "ident":
			for _, option := range line.fields[4:] {
				if strings.HasPrefix(option.plain(), "map=") {
					return hbaAdminUnknown, line.number
				}
			}
			return hbaAdminGranted, line.number
		default:
			return hbaAdminDenied, line.number
		}
	}
	return hbaAdminDenied, 0
}

const (
	hbaNo = iota
	hbaYes
	hbaMaybe
)

// hbaMatches reports whether a database or user field covers `postgres`.
func hbaMatches(field hbaField, database bool) int {
	result := hbaNo
	for _, item := range field.items {
		verdict := hbaNo
		switch {
		case item.quoted:
			if item.text == "postgres" {
				verdict = hbaYes
			}
		case item.text == "all" || item.text == "postgres":
			verdict = hbaYes
		case database && item.text == "sameuser":
			// The database asked for has the name of the user asking.
			verdict = hbaYes
		case database && (item.text == "samerole" || item.text == "samegroup"):
			verdict = hbaMaybe
		case database && item.text == "replication":
			verdict = hbaNo
		case strings.HasPrefix(item.text, "@") || strings.HasPrefix(item.text, "/") || strings.HasPrefix(item.text, "+"):
			verdict = hbaMaybe
		}
		if verdict == hbaYes {
			return hbaYes
		}
		if verdict == hbaMaybe {
			result = hbaMaybe
		}
	}
	return result
}

// hbaLockoutRefusal refuses a pg_hba.conf that takes the local administrator
// access away, or leaves it undecidable, when the current file grants it.
func hbaLockoutRefusal(current, candidate string) *configRefusal {
	if now, _ := hbaLocalAdminAccess(current); now != hbaAdminGranted {
		// The owner already arranged this differently; there is nothing here
		// for the Panel to take away.
		return nil
	}
	after, line := hbaLocalAdminAccess(candidate)
	if after == hbaAdminGranted {
		return nil
	}
	refusal := configInvalid("lockout",
		"the change would take away the local administrator access to PostgreSQL; nothing was changed")
	refusal.name = after
	refusal.line = line
	return refusal
}
