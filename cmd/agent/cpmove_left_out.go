package main

import (
	"archive/tar"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/alicelik/celikpanel/internal/transport"
)

// What the files step of a cPanel import leaves out, counted and named
// (12 Oct 2026; D-025 invariant 2).
//
// Measured on Debian 13 and Ubuntu 24.04: an archive with a member named by an
// absolute path (`/etc/...`) was imported, the member was left out, and the
// answer was `200` / `active` without a word about it. Nothing had been
// written for it; nothing said so either.
//
// Every member of the archive ends in exactly one of these, and none of them
// is silent any more:
//
//   - below `homedir/public_html`, a directory or a regular file: imported;
//   - below it, anything else (a symbolic link, a hard link, a device node, a
//     named pipe), a name with `..`, a backslash or a NUL, a payload over the
//     size or count limit: the whole files step is refused, as before, and the
//     step's line now names the member and what it is;
//   - a name that is an absolute path: refused by its name, left out, counted
//     and listed (cpmoveLeftOut.refuse). The import goes on and ends as
//     `partial`: a member of the archive was not imported;
//   - not below `homedir/public_html`: not this step's to copy. Counted, by
//     the folder of the archive it is in (cpmoveLeftOut.outside), and said in
//     the step's line. This is every real cPanel archive (the mail
//     directories, the home directory's other folders, the account's
//     metadata), so it does not make an import partial; the databases,
//     mailboxes, forwarders and DNS records are read from their own members by
//     their own steps.
//
// Bir cPanel içe aktarımının dosya adımının dışarıda bıraktıkları, sayılmış ve
// adlandırılmış olarak. Ölçüldü: mutlak yolla adlandırılmış bir üye dışarıda
// bırakılıyor ve yanıt bundan tek söz etmeden `200` / `active` oluyordu.
// Arşivin her üyesi artık şunlardan tam birinde biter ve hiçbiri sessiz
// değildir: içe aktarıldı; adımın tümü reddedildi (üye adıyla); adı yüzünden
// reddedildi ve listelendi; ya da bu adımın kopyaladığı klasörün dışında
// olduğu için sayıldı.
type cpmoveLeftOut struct {
	refused      []transport.CpmoveRefusedMember
	refusedCount int
	outsideCount int
	groups       map[string]int
}

// cpmoveOutsideGroupsTracked bounds the folders counted separately; a hostile
// archive cannot grow the table without limit.
const cpmoveOutsideGroupsTracked = 256

const cpmoveOtherFolders = "(other folders)"

// cpmoveMemberName is an archive member's name as it may be shown: bounded,
// with every control or unprintable character replaced.
func cpmoveMemberName(name string) string {
	const limit = 200
	runes := []rune(strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return '?'
		}
		return r
	}, name))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}

// cpmoveEntryKind names what a tar member is, for the line of a refused step.
func cpmoveEntryKind(typeflag byte) string {
	switch typeflag {
	case tar.TypeSymlink:
		return "a symbolic link"
	case tar.TypeLink:
		return "a hard link"
	case tar.TypeChar, tar.TypeBlock:
		return "a device node"
	case tar.TypeFifo:
		return "a named pipe"
	}
	return fmt.Sprintf("an entry of tar type %q", string(typeflag))
}

func (l *cpmoveLeftOut) refuse(name, reason string) {
	l.refusedCount++
	if len(l.refused) < transport.CpmoveRefusedMemberLimit {
		l.refused = append(l.refused, transport.CpmoveRefusedMember{Name: cpmoveMemberName(name), Reason: reason})
	}
}

// cpmoveOutsideGroup is the folder of the archive a member outside the site
// folder is counted under: the first folder below the account's top folder,
// and for the home directory its first folder too ("homedir/mail").
func cpmoveOutsideGroup(member string) string {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(member, "./"), "/"), "/")
	if len(parts) > 1 && (strings.HasPrefix(parts[0], "cpmove-") || strings.HasPrefix(parts[0], "backup-")) {
		parts = parts[1:]
	}
	switch {
	case len(parts) <= 1:
		return "(top folder of the archive)"
	case parts[0] == "homedir" && len(parts) == 2:
		return "homedir"
	case parts[0] == "homedir":
		return cpmoveMemberName("homedir/" + parts[1])
	}
	return cpmoveMemberName(parts[0])
}

func (l *cpmoveLeftOut) outside(hdr *tar.Header) {
	if hdr.Typeflag == tar.TypeDir || hdr.Typeflag == tar.TypeXGlobalHeader {
		// A folder holds nothing itself, and a global header is not a member.
		return
	}
	l.outsideCount++
	if l.groups == nil {
		l.groups = map[string]int{}
	}
	group := cpmoveOutsideGroup(hdr.Name)
	if _, known := l.groups[group]; !known && len(l.groups) >= cpmoveOutsideGroupsTracked {
		group = cpmoveOtherFolders
	}
	l.groups[group]++
}

func (l *cpmoveLeftOut) report(resp *transport.CpmoveExtractResponse) {
	resp.Refused, resp.RefusedCount, resp.OutsideCount = l.refused, l.refusedCount, l.outsideCount
	groups := make([]transport.CpmoveMemberGroup, 0, len(l.groups))
	other := 0
	for name, count := range l.groups {
		if name == cpmoveOtherFolders {
			other = count
			continue
		}
		groups = append(groups, transport.CpmoveMemberGroup{Name: name, Count: count})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Name < groups[j].Name
	})
	if len(groups) > transport.CpmoveOutsideGroupLimit {
		for _, group := range groups[transport.CpmoveOutsideGroupLimit:] {
			other += group.Count
		}
		groups = groups[:transport.CpmoveOutsideGroupLimit]
	}
	if other > 0 {
		groups = append(groups, transport.CpmoveMemberGroup{Name: cpmoveOtherFolders, Count: other})
	}
	resp.OutsideGroups = groups
}
