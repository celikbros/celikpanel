package bindconfig

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

type StaticZone struct{ Name, Class, Type, File string }

// ParseAdoptionStaticZones accepts a deliberately small Debian named.conf
// envelope. It is an admission parser, never a general BIND parser.
func ParseAdoptionStaticZones(local, options, leaf string) ([]StaticZone, error) {
	opts, e := adoptionTokens(options)
	if e != nil {
		return nil, e
	}
	if e = adoptionOptions(opts); e != nil {
		return nil, e
	}
	zones, e := adoptionZoneFile(local, false)
	if e != nil {
		return nil, e
	}
	packaged, e := adoptionZoneFile(leaf, true)
	if e != nil {
		return nil, e
	}
	if len(packaged) != 1 && len(packaged) != 4 {
		return nil, errors.New("BIND packaged leaf has incomplete zone set")
	}
	if len(packaged) == 1 {
		if packaged[0].Name != "." {
			return nil, errors.New("BIND root hints leaf differs from package contract")
		}
	} else {
		required := map[string]bool{"localhost": true, "127.in-addr.arpa": true, "0.in-addr.arpa": true, "255.in-addr.arpa": true}
		for _, z := range packaged {
			if !required[z.Name] {
				return nil, errors.New("BIND default leaf differs from package contract")
			}
			delete(required, z.Name)
		}
		if len(required) != 0 {
			return nil, errors.New("BIND default leaf is incomplete")
		}
	}
	zones = append(zones, packaged...)
	seen := map[string]bool{}
	for _, z := range zones {
		k := z.Name + "/" + z.Class
		if seen[k] {
			return nil, errors.New("duplicate BIND static zone")
		}
		seen[k] = true
	}
	return zones, nil
}
func adoptionTokens(s string) ([]string, error) {
	if len(s) > 1<<20 {
		return nil, errors.New("BIND config exceeds static parser limit")
	}
	var out []string
	for i := 0; i < len(s); {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		if c == '#' || (c == '/' && i+1 < len(s) && s[i+1] == '/') {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			if i+1 >= len(s) {
				return nil, errors.New("unterminated BIND comment")
			}
			i += 2
			continue
		}
		if c == '"' {
			start := i + 1
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' {
					return nil, errors.New("escaped BIND string unsupported")
				}
				i++
			}
			if i >= len(s) {
				return nil, errors.New("unterminated BIND string")
			}
			out = append(out, s[start:i])
			i++
			continue
		}
		if c == '{' || c == '}' || c == ';' {
			out = append(out, string(c))
			i++
			continue
		}
		start := i
		for i < len(s) && !strings.ContainsRune(" \t\r\n{};\"#/", rune(s[i])) {
			i++
		}
		if i == start {
			return nil, fmt.Errorf("unsupported BIND syntax at byte %d", i)
		}
		out = append(out, s[start:i])
		if len(out) > 8192 {
			return nil, errors.New("BIND config token limit")
		}
	}
	return out, nil
}
func adoptionOptions(t []string) error {
	if len(t) < 4 || t[0] != "options" || t[1] != "{" || t[len(t)-2] != "}" || t[len(t)-1] != ";" {
		return errors.New("BIND options have unsupported envelope")
	}
	// A narrow set of non-mutating native options. Other inherited semantics
	// require owner review rather than an optimistic takeover.
	allowed := map[string]bool{"directory": true, "dnssec-validation": true, "listen-on": true, "listen-on-v6": true, "recursion": true, "allow-query": true, "allow-transfer": true, "notify": true, "allow-update": true}
	for i := 2; i < len(t)-2; {
		key := t[i]
		if !allowed[key] {
			return fmt.Errorf("unsupported inherited BIND option %s", key)
		}
		i++
		start := i
		depth := 0
		for i < len(t)-2 {
			if t[i] == "{" {
				depth++
			}
			if t[i] == "}" {
				depth--
				if depth < 0 {
					return errors.New("unbalanced BIND option")
				}
			}
			if t[i] == ";" && depth == 0 {
				break
			}
			i++
		}
		if i >= len(t)-2 || i == start {
			return errors.New("incomplete BIND option")
		}
		if key == "allow-update" && (i-start != 4 || t[start] != "{" || t[start+1] != "none" || t[start+2] != ";" || t[start+3] != "}") {
			return errors.New("BIND inherited allow-update is unsafe")
		}
		i++
	}
	return nil
}
func adoptionZoneFile(s string, packaged bool) ([]StaticZone, error) {
	t, e := adoptionTokens(s)
	if e != nil {
		return nil, e
	}
	var zones []StaticZone
	for i := 0; i < len(t); {
		if t[i] != "zone" || i+3 >= len(t) {
			return nil, errors.New("BIND static config has unsupported statement")
		}
		z := StaticZone{Name: strings.ToLower(strings.TrimSuffix(t[i+1], ".")), Class: "IN"}
		if t[i+1] == "." {
			z.Name = "."
		}
		i += 2
		if t[i] == "IN" {
			i++
		}
		if i >= len(t) || t[i] != "{" {
			return nil, errors.New("BIND zone has unsupported declaration")
		}
		i++
		seen := map[string]bool{}
		for i < len(t) && t[i] != "}" {
			if i+2 >= len(t) {
				return nil, errors.New("BIND zone is incomplete")
			}
			key := t[i]
			i++
			if seen[key] {
				return nil, errors.New("duplicate BIND zone option")
			}
			seen[key] = true
			switch key {
			case "type", "file":
				zval := t[i]
				i++
				if key == "type" {
					z.Type = zval
				} else {
					z.File = zval
				}
			case "allow-update":
				if i+3 >= len(t) || t[i] != "{" || t[i+1] != "none" || t[i+2] != ";" || t[i+3] != "}" {
					return nil, errors.New("dynamic BIND update is unsupported")
				}
				i += 4
			default:
				return nil, fmt.Errorf("unsupported BIND zone option %s", key)
			}
			if i >= len(t) || t[i] != ";" {
				return nil, errors.New("BIND zone option lacks semicolon")
			}
			i++
		}
		if i+1 >= len(t) || t[i] != "}" || t[i+1] != ";" {
			return nil, errors.New("BIND zone lacks terminator")
		}
		i += 2
		if !adoptionValidZoneName(z.Name, packaged) || !strings.HasPrefix(z.File, "/") || path.Clean(z.File) != z.File {
			return nil, errors.New("BIND zone has unsafe name or file")
		}
		if packaged {
			if z.Type != "master" && z.Type != "primary" && z.Type != "hint" {
				return nil, errors.New("BIND packaged leaf has unsupported type")
			}
			if !adoptionPackaged(z) {
				return nil, errors.New("BIND packaged zone differs from Debian contract")
			}
		} else {
			if z.Type != "master" && z.Type != "primary" {
				return nil, errors.New("BIND owner zone is not static primary")
			}
			if !strings.HasPrefix(z.File, "/etc/bind/") || strings.HasPrefix(z.File, "/etc/bind/named.conf") {
				return nil, errors.New("BIND owner zone file is outside the protected Debian source directory")
			}
		}
		zones = append(zones, z)
	}
	return zones, nil
}
func adoptionPackaged(z StaticZone) bool {
	allowed := map[string]string{"localhost": "/etc/bind/db.local", "127.in-addr.arpa": "/etc/bind/db.127", "0.in-addr.arpa": "/etc/bind/db.0", "255.in-addr.arpa": "/etc/bind/db.255", ".": "/usr/share/dns/root.hints"}
	if z.Name == "" {
		z.Name = "."
	}
	return allowed[z.Name] == z.File && ((z.Name == "." && z.Type == "hint") || (z.Name != "." && z.Type == "master"))
}

func adoptionValidZoneName(name string, packaged bool) bool {
	if name == "." {
		return packaged
	}
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}
