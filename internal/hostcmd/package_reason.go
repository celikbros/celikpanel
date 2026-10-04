package hostcmd

import (
	"regexp"
	"strings"
)

// PackageManagerReasonLimit bounds the package manager's own words wherever a
// product sentence carries them into a durable record.
const PackageManagerReasonLimit = 180

// PackageManagerReason returns the one line of apt, dpkg or pacman output that
// states why a package transaction failed, as a single bounded line with URL
// credentials, paths and query strings removed. It returns "" when the output
// holds no line at all.
//
// "exit status 100" is apt-get's answer to every failure. The reason that
// names the fault is somewhere in its combined output: dpkg's own fatal
// message (the line after "dpkg: unrecoverable fatal error, aborting:"), the
// continuation of "dpkg: error processing ...", an apt "E:" line, or pacman's
// "error:" line. The generic "E: Sub-process /usr/bin/dpkg returned an error
// code" is taken only when nothing more specific exists. Repository URLs can
// carry tokens in their user part, path or query, so only scheme and host are
// kept.
//
// PackageManagerReason, bir paket işleminin neden başarısız olduğunu söyleyen
// apt, dpkg veya pacman satırını; URL kimlik bilgileri, yolları ve sorguları
// çıkarılmış, sınırlı tek bir satır olarak döndürür. "exit status 100"
// apt-get'in her hatadaki yanıtıdır; hatayı adlandıran neden birleşik
// çıktının içindedir.
func PackageManagerReason(out []byte, limit int) string {
	lines := strings.Split(strings.ReplaceAll(string(out), "\r", "\n"), "\n")
	next := func(from int) string {
		for j := from + 1; j < len(lines); j++ {
			if trimmed := strings.TrimSpace(lines[j]); trimmed != "" {
				return trimmed
			}
		}
		return ""
	}
	pick := ""
	genericAPT := ""
	firstAPT := ""
	pacman := ""
	last := ""
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		last = line
		if strings.HasPrefix(line, "dpkg: ") && strings.HasSuffix(line, ":") &&
			(strings.Contains(line, "fatal error") || strings.HasPrefix(line, "dpkg: error")) {
			if continuation := next(i); continuation != "" {
				if strings.Contains(line, "fatal error") {
					pick = "dpkg: " + continuation
				} else {
					pick = strings.TrimSuffix(line, ":") + ": " + continuation
				}
				break
			}
		}
		if strings.HasPrefix(line, "dpkg: error") || strings.HasPrefix(line, "dpkg: unrecoverable") {
			pick = line
			break
		}
		if strings.HasPrefix(line, "E: ") {
			if strings.HasPrefix(line, "E: Sub-process ") {
				if genericAPT == "" {
					genericAPT = line
				}
			} else if firstAPT == "" {
				firstAPT = line
			}
			continue
		}
		if pacman == "" && strings.HasPrefix(line, "error: ") {
			pacman = line
		}
	}
	for _, candidate := range []string{pick, firstAPT, pacman, genericAPT, last} {
		if candidate != "" {
			return boundPackageReason(redactPackageReasonURLs(flatten(controlFree(candidate))), limit)
		}
	}
	return ""
}

var packageReasonURL = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://[^\s'"<>]+`)

func redactPackageReasonURLs(text string) string {
	return packageReasonURL.ReplaceAllStringFunc(text, func(raw string) string {
		scheme, rest, _ := strings.Cut(raw, "://")
		authority := rest
		if cut := strings.IndexAny(authority, "/?#"); cut >= 0 {
			authority = authority[:cut]
		}
		if at := strings.LastIndex(authority, "@"); at >= 0 {
			authority = authority[at+1:]
		}
		if authority == "" {
			return scheme + "://..."
		}
		return scheme + "://" + authority + "/..."
	})
}

func controlFree(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, text)
}

func boundPackageReason(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return text
}
