package services

import (
	"fmt"
	"strings"
	"testing"
)

// TestEmbeddedNginxTemplateHasNoCarriageReturns is the sibling of
// TestEmbeddedMigrationsHaveNoCarriageReturns in internal/db: the nginx vhost
// template is the product's only other go:embed asset, and it was exposed to
// exactly the same hazard - until .gitattributes pinned *.tmpl to LF, every
// Windows checkout embedded it with CRLF. A panel built that way writes vhost
// files that differ byte for byte from the ones a released panel writes for
// the same site, so any diff, checksum or restore comparison of /etc/nginx
// reports drift that does not exist.
//
// Gömülü nginx şablonu CRLF taşıyorsa, bu panelin yazdığı vhost dosyaları
// yayınlanmış panelinkinden bayt bayt ayrılır; test bunu yapı zamanında
// yakalar.
func TestEmbeddedNginxTemplateHasNoCarriageReturns(t *testing.T) {
	if vhostTemplate == "" {
		t.Fatal("this build embeds no nginx vhost template")
	}

	offset := strings.IndexByte(vhostTemplate, '\r')
	if offset < 0 {
		return
	}
	t.Fatal(crlfEmbedFailureMessage("the nginx vhost template", []string{
		fmt.Sprintf(
			"internal/services/templates/nginx/vhost.conf.tmpl (first CR byte at offset %d, line %d)",
			offset,
			1+strings.Count(vhostTemplate[:offset], "\n"),
		),
	}))
}

// crlfEmbedFailureMessage explains the defect and the repair instead of
// leaving a developer to decode a byte offset. It is deliberately duplicated
// from internal/db, where the migration embed lives, so that neither package
// has to import the other for a test-only string.
func crlfEmbedFailureMessage(assets string, offenders []string) string {
	var message strings.Builder
	message.WriteString("this build embeds " + assets +
		" with CRLF line endings, so it does not match any released binary:\n")
	for _, offender := range offenders {
		message.WriteString("  " + offender + "\n")
	}
	message.WriteString(`
Cause: this working copy was checked out before .gitattributes pinned these
files to LF (or by a git that converts line endings), so the bytes on disk -
the bytes go:embed compiles in - are not the LF bytes git tracks. Nothing in
the repository is wrong; only this checkout is.

Fix, from the repository root. One file, keeping every other local change:
  git rm --cached <file> && git checkout HEAD -- <file>
Every tracked file at once, which DISCARDS uncommitted changes to them:
  git rm --cached -r . && git reset --hard

"git checkout -- <file>" alone cannot repair this, and neither can
"git checkout-index -f": git already believes the file is unmodified, because
it normalizes the CRLF away before it compares. The index entry has to go
first.`)
	return message.String()
}
