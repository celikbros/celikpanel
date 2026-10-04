//go:build linux

package dnsenginerecovery

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	_ "modernc.org/sqlite"
)

// ProbePDNSAdoptionDatabase proves the frozen byte preimage around a read-only
// SQLite transaction and compares its zones, peer rows and integrity with the
// accepted manifest. Native process, loaded config and owner authority remain
// separate caller obligations. The caller binds path to the installed policy.
func ProbePDNSAdoptionDatabase(ctx context.Context, path string, size int64, digest string, manifest mutationpayload.DNSEngineSwitchManifestCommitment) error {
	if ctx == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		path == "/" || ctx.Err() != nil {
		return errors.New("PowerDNS adoption database observation request is invalid")
	}
	if err := ProbePDNSDatabasePreimage(ctx, path, size, digest); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_busy_timeout=1000&_foreign_keys=1")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := VerifyPDNSAdoptionDatabaseTx(ctx, tx, manifest); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ProbePDNSDatabasePreimage(ctx, path, size, digest)
}
