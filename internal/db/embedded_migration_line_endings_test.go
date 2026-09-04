package db

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestEmbeddedMigrationsHaveNoCarriageReturns fails a build whose embedded
// migrations carry CRLF line endings.
//
// go:embed compiles in whatever bytes are on disk. A working copy checked out
// before .gitattributes pinned these files to LF keeps their CRLF bytes, so a
// panel built from it hashes migration content no released binary ever hashes.
// The panel then refuses every database a released panel created and says only
// "migration integrity mismatch for version 1: ledger has .../ff31f0b6...,
// embedded release has .../0a01e17f..." - a hash, not a cause. This test names
// the cause on the developer's own machine, before the binary is ever run.
//
// The check is a CR-byte rule rather than a comparison against the bytes git
// tracks, because a test that shells out to git is worthless in a release
// tarball, where .git is absent. Line-ending translation is also the only
// corruption a developer cannot see: any other local edit to a migration shows
// up in "git status", while a CRLF checkout does not - which is exactly why
// this cost a day of a disaster-recovery drill.
//
// Gömülü migration'lar CRLF satır sonu taşıyorsa bu test yapıyı reddeder;
// aksi halde yerel yapı, yayınlanmış panelin oluşturduğu veritabanını yalnızca
// bir özet uyuşmazlığı göstererek reddeder.
func TestEmbeddedMigrationsHaveNoCarriageReturns(t *testing.T) {
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("this build embeds no migrations")
	}

	offenders := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		offset := bytes.IndexByte(migration.content, '\r')
		if offset < 0 {
			continue
		}
		offenders = append(offenders, fmt.Sprintf(
			"internal/db/migrations/%s (first CR byte at offset %d, line %d)",
			migration.filename,
			offset,
			1+bytes.Count(migration.content[:offset], []byte("\n")),
		))
	}
	if len(offenders) > 0 {
		t.Fatal(crlfEmbedFailureMessage("migration files", offenders))
	}
}

// crlfEmbedFailureMessage explains the defect and the repair instead of
// leaving a developer to decode a digest. It is deliberately duplicated in
// internal/services, where the nginx template embed lives, so that neither
// package has to import the other for a test-only string.
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

// TestEmbeddedLineEndingHintNamesTheCause covers the other half of the guard:
// the failure a released binary can still hit, where a locally built panel
// meets a database a release created. The digest mismatch is unavoidable and
// correct, but the message must say why rather than print two hashes.
func TestEmbeddedLineEndingHintNamesTheCause(t *testing.T) {
	clean := embeddedMigration{
		filename: "001_full_schema.sql",
		content:  []byte("CREATE TABLE t (id INTEGER);\n"),
	}
	if hint := embeddedLineEndingHint(clean); hint != "" {
		t.Fatalf("hint for LF migration = %q, want empty", hint)
	}

	crlf := embeddedMigration{
		filename: "001_full_schema.sql",
		content:  []byte("CREATE TABLE t (id INTEGER);\r\n"),
	}
	hint := embeddedLineEndingHint(crlf)
	for _, want := range []string{
		"CRLF line endings",
		".gitattributes",
		"git rm --cached internal/db/migrations/001_full_schema.sql",
		"git checkout HEAD -- internal/db/migrations/001_full_schema.sql",
	} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint = %q, want it to contain %q", hint, want)
		}
	}
}
