package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/bindrndckey"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// The fresh BIND install makes sure named can be asked about zone state
// before it first starts. Arch's `bind` package creates no rndc key, so
// `rndc zonestatus` (the product's deletion proof and the owner's peer
// inspector) cannot work there until one exists; Debian's `bind9` creates
// /etc/bind/rndc.key at package configuration. The rule (D-022, D-025):
//
//   - the default key file already exists, rndc.conf exists beside it, or the
//     owner configured a `controls` statement: touch nothing and record
//     "owner or package provided";
//   - otherwise run the native `rndc-confgen -a`, give the file the ownership
//     and mode the host's named needs, and record it as product-created with
//     its content hash.
//
// The record is a companion file of the install-ownership receipt, bound to
// the same transaction. A rollback of that transaction removes the key only
// when the record says product-created and the content is unchanged.
//
// Taze BIND kurulumu, named ilk kez başlamadan önce ona bölge durumunun
// sorulabilmesini sağlar. Arch `bind` paketi rndc anahtarı oluşturmaz;
// Debian `bind9` oluşturur. Anahtar, rndc.conf ya da sahibin `controls`
// ifadesi varsa hiçbir şeye dokunulmaz; yoksa yerel `rndc-confgen -a`
// çalıştırılır ve anahtar ürünün oluşturduğu olarak içerik özetiyle kaydedilir.
// Geri alma yalnız ürünün oluşturduğu ve değişmemiş anahtarı siler.

// bindRNDCKeyPlan is what the product knows about the host's rndc defaults,
// derived from the certified package layout (not probed): the compiled-in
// sysconfdir of Arch's bind is /etc and Debian's is /etc/bind, which is also
// the directory of each layout's main configuration.
type bindRNDCKeyPlan struct {
	KeyPath    string
	ConfPath   string
	MainConfig string
	// OwnerUser is "" for root.
	OwnerUser string
	Mode      os.FileMode
	pacman    bool
}

func bindRNDCKeyPlanForLayout(layout bindHostLayout) (bindRNDCKeyPlan, error) {
	switch {
	case bindLayoutIsPacman(layout) && layout.MainConfig == "/etc/named.conf":
		// Arch: named -u named reads the key after dropping privileges on
		// reconfiguration, so root:named 0640.
		return bindRNDCKeyPlan{
			KeyPath: bindrndckey.PacmanKeyPath, ConfPath: "/etc/rndc.conf",
			MainConfig: layout.MainConfig, Mode: 0o640, pacman: true,
		}, nil
	case layout.MainConfig == "/etc/bind/named.conf":
		// Debian's bind9 postinst leaves /etc/bind/rndc.key bind:bind 0640
		// (observed on Debian 13, pair3 evidence); match it.
		return bindRNDCKeyPlan{
			KeyPath: bindrndckey.APTKeyPath, ConfPath: "/etc/bind/rndc.conf",
			MainConfig: layout.MainConfig, OwnerUser: "bind", Mode: 0o640,
		}, nil
	default:
		return bindRNDCKeyPlan{}, errors.New("BIND rndc key location is unknown for this package layout")
	}
}

type bindRNDCKeyIdentity struct {
	Qualifier, RequestID, OwnerID string
}

type bindRNDCKeyPresence int

const (
	bindRNDCKeyAbsent bindRNDCKeyPresence = iota
	bindRNDCKeyRegular
	bindRNDCKeyOther
)

type bindRNDCKeyOps struct {
	presence func(path string) (bindRNDCKeyPresence, error)
	hash     func(path string) (string, error)
	// controls reports a top-level `controls` statement in the complete
	// configuration named would load.
	controls func(ctx context.Context) (bool, error)
	// generate runs the native rndc-confgen -a.
	generate func(ctx context.Context) error
	// secure sets the host's owner, group and mode on the created key.
	secure      func(ctx context.Context, path string) error
	readRecord  func() (bindrndckey.Record, bool, error)
	writeRecord func(bindrndckey.Record) error
}

// bindRNDCKeyPriorInstall is the BIND install-ownership receipt as it was
// before this transaction wrote or rebound it. Only a key recorded as
// product-created by exactly that earlier, never-committed transaction is
// still the product's: commit retires the install receipt, so a key left by a
// committed install is never mistaken for residue of the current one.
type bindRNDCKeyPriorInstall struct {
	Exists bool
	bindRNDCKeyIdentity
}

// prepareBINDRNDCKeyWithOps runs after the BIND package is installed and
// before named is first started. It never overwrites or rewrites a key.
func prepareBINDRNDCKeyWithOps(
	ctx context.Context,
	plan bindRNDCKeyPlan,
	identity bindRNDCKeyIdentity,
	prior bindRNDCKeyPriorInstall,
	ops bindRNDCKeyOps,
) (bindrndckey.Record, error) {
	if ctx == nil || ops.presence == nil || ops.hash == nil || ops.controls == nil ||
		ops.generate == nil || ops.secure == nil || ops.readRecord == nil || ops.writeRecord == nil ||
		!bindrndckey.KnownKeyPath(plan.KeyPath) {
		return bindrndckey.Record{}, errors.New("BIND rndc key preparation is incomplete")
	}
	record := bindrndckey.Record{
		Schema: bindrndckey.RecordSchema, Path: plan.KeyPath,
		ManifestQualifier: identity.Qualifier,
		MutationRequestID: identity.RequestID, MutationOwnerID: identity.OwnerID,
	}
	owner := func(basis string) (bindrndckey.Record, error) {
		record.Provenance, record.Basis = bindrndckey.ProvenanceOwnerOrPackage, basis
		return record, ops.writeRecord(record)
	}
	keyPresence, err := ops.presence(plan.KeyPath)
	if err != nil {
		return bindrndckey.Record{}, fmt.Errorf("inspect %s: %w", plan.KeyPath, err)
	}
	switch keyPresence {
	case bindRNDCKeyOther:
		return owner(bindrndckey.BasisKeyPresent)
	case bindRNDCKeyRegular:
		carried, err := carriedProductBINDRNDCKey(plan, prior, ops)
		if err != nil {
			return bindrndckey.Record{}, err
		}
		if carried == "" {
			return owner(bindrndckey.BasisKeyPresent)
		}
		record.Provenance, record.SHA256 = bindrndckey.ProvenanceProductCreated, carried
		if err := ops.writeRecord(record); err != nil {
			return bindrndckey.Record{}, err
		}
		// An earlier attempt of this same never-committed install may have
		// stopped between creating the key and securing it.
		return record, secureCreatedBINDRNDCKey(ctx, plan, carried, ops)
	}
	confPresence, err := ops.presence(plan.ConfPath)
	if err != nil {
		return bindrndckey.Record{}, fmt.Errorf("inspect %s: %w", plan.ConfPath, err)
	}
	if confPresence != bindRNDCKeyAbsent {
		return owner(bindrndckey.BasisRNDCConfPresent)
	}
	controls, err := ops.controls(ctx)
	if err != nil {
		return bindrndckey.Record{}, err
	}
	if controls {
		return owner(bindrndckey.BasisControlsStatement)
	}
	if err := ops.generate(ctx); err != nil {
		return bindrndckey.Record{}, err
	}
	created, err := ops.presence(plan.KeyPath)
	if err != nil || created != bindRNDCKeyRegular {
		return bindrndckey.Record{}, errors.Join(fmt.Errorf(
			"rndc-confgen -a did not create %s; the host's rndc default differs from the certified layout", plan.KeyPath,
		), err)
	}
	sum, err := ops.hash(plan.KeyPath)
	if err != nil {
		return bindrndckey.Record{}, err
	}
	// Record before changing ownership: a stop here leaves a key the record
	// proves this transaction created, so a retry recognises it.
	record.Provenance, record.SHA256 = bindrndckey.ProvenanceProductCreated, sum
	if err := ops.writeRecord(record); err != nil {
		return bindrndckey.Record{}, err
	}
	return record, secureCreatedBINDRNDCKey(ctx, plan, sum, ops)
}

// carriedProductBINDRNDCKey returns the recorded hash when the present key is
// still exactly the one an earlier attempt of the prior, never-committed
// install created; "" otherwise.
func carriedProductBINDRNDCKey(
	plan bindRNDCKeyPlan, prior bindRNDCKeyPriorInstall, ops bindRNDCKeyOps,
) (string, error) {
	if !prior.Exists {
		return "", nil
	}
	existing, exists, err := ops.readRecord()
	if err != nil || !exists {
		// An unreadable record proves nothing: the key is then treated as
		// the owner's and is never removed.
		return "", nil
	}
	if existing.Provenance != bindrndckey.ProvenanceProductCreated ||
		existing.Path != plan.KeyPath ||
		!existing.SameTransaction(prior.Qualifier, prior.RequestID, prior.OwnerID) {
		return "", nil
	}
	sum, err := ops.hash(plan.KeyPath)
	if err != nil {
		return "", err
	}
	if sum != existing.SHA256 {
		return "", nil
	}
	return sum, nil
}

func secureCreatedBINDRNDCKey(ctx context.Context, plan bindRNDCKeyPlan, sum string, ops bindRNDCKeyOps) error {
	if err := ops.secure(ctx, plan.KeyPath); err != nil {
		return fmt.Errorf("set ownership of the created %s: %w", plan.KeyPath, err)
	}
	after, err := ops.hash(plan.KeyPath)
	if err != nil {
		return err
	}
	if after != sum {
		return fmt.Errorf("%s changed while its ownership was set", plan.KeyPath)
	}
	return nil
}

// namedConfigHasControls reads `named-checkconf -p` output: one top-level
// statement per unindented line. The output may contain key material; it is
// parsed here and never logged or returned.
func namedConfigHasControls(printed []byte) bool {
	for _, line := range strings.Split(string(printed), "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if line == "controls" || strings.HasPrefix(line, "controls ") || strings.HasPrefix(line, "controls{") {
			return true
		}
	}
	return false
}

func bindRNDCKeyRecordPath() string {
	return filepath.Join(serviceMutationStateDirectory(), bindrndckey.RecordFileName)
}

func readBINDRNDCKeyRecord() (bindrndckey.Record, bool, error) {
	data, err := secureReadConfig(bindRNDCKeyRecordPath())
	if errors.Is(err, os.ErrNotExist) {
		return bindrndckey.Record{}, false, nil
	}
	if err != nil {
		return bindrndckey.Record{}, false, err
	}
	record, err := bindrndckey.Decode(data)
	if err != nil {
		return bindrndckey.Record{}, false, err
	}
	return record, true, nil
}

func writeBINDRNDCKeyRecord(record bindrndckey.Record) error {
	encoded, err := bindrndckey.Encode(record)
	if err != nil {
		return err
	}
	if err := secureWriteConfig(bindRNDCKeyRecordPath(), encoded, 0o600); err != nil {
		return err
	}
	actual, exists, err := readBINDRNDCKeyRecord()
	if err != nil || !exists || actual != record {
		return errors.Join(errors.New("BIND rndc key record readback mismatch"), err)
	}
	return nil
}

func hostBINDRNDCKeyPresence(path string) (bindRNDCKeyPresence, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return bindRNDCKeyAbsent, nil
	}
	if err != nil {
		return bindRNDCKeyAbsent, err
	}
	if info.Mode().IsRegular() {
		return bindRNDCKeyRegular, nil
	}
	return bindRNDCKeyOther, nil
}

func hostBINDRNDCKeyOps(layout bindHostLayout, plan bindRNDCKeyPlan) bindRNDCKeyOps {
	return bindRNDCKeyOps{
		presence: hostBINDRNDCKeyPresence,
		hash:     bindrndckey.HashKeyFile,
		controls: func(ctx context.Context) (bool, error) {
			checkconf, err := firstTrustedExecutable(
				[]string{"/usr/sbin/named-checkconf", "/usr/bin/named-checkconf"}, "named-checkconf",
			)
			if err != nil {
				return false, err
			}
			commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			command := serviceMutationCommand(commandCtx, checkconf, "-p", plan.MainConfig)
			command.Env = bindSafeAPTCommandEnvironment()
			output, err := command.CombinedOutputLimited(4 << 20)
			if err != nil {
				// named-checkconf errors name a file and line; the printed
				// configuration (which can hold key secrets) is not returned.
				return false, fmt.Errorf("read the BIND configuration to decide on the rndc key: %w: %s",
					err, bindrndckey.FirstLine(output))
			}
			return namedConfigHasControls(output), nil
		},
		generate: func(ctx context.Context) error {
			confgen, err := firstTrustedExecutable(
				[]string{"/usr/sbin/rndc-confgen", "/usr/bin/rndc-confgen"}, "rndc-confgen",
			)
			if err != nil {
				return err
			}
			commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			command := serviceMutationCommand(commandCtx, confgen, "-a")
			command.Env = bindSafeAPTCommandEnvironment()
			output, err := command.CombinedOutputLimited(4 << 10)
			if err != nil {
				return fmt.Errorf("rndc-confgen -a: %w: %s", err, bindrndckey.FirstLine(output))
			}
			return nil
		},
		secure: func(ctx context.Context, path string) error {
			gid, err := bindServiceGroupResolverForLayout(layout)(ctx)
			if err != nil {
				return err
			}
			uid := uint32(0)
			if plan.OwnerUser != "" {
				uid, err = resolveServiceUserUID(ctx, plan.OwnerUser)
				if err != nil {
					return err
				}
			}
			return secureBINDRNDCKeyFile(path, uid, gid, plan.Mode)
		},
		readRecord:  readBINDRNDCKeyRecord,
		writeRecord: writeBINDRNDCKeyRecord,
	}
}

func secureBINDRNDCKeyFile(path string, uid, gid uint32, mode os.FileMode) error {
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return errors.Join(fmt.Errorf("%s changed while it was opened", path), err)
	}
	if err := file.Chown(int(uid), int(gid)); err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	return file.Sync()
}

// resolveServiceUserUID reads exactly one canonical passwd record.
func resolveServiceUserUID(ctx context.Context, user string) (uint32, error) {
	getent, err := firstTrustedExecutable([]string{"/usr/bin/getent", "/bin/getent"}, "getent")
	if err != nil {
		return 0, err
	}
	return resolveServiceUserUIDWithRunner(ctx, getent, user,
		func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
			command := serviceMutationCommand(commandCtx, name, args...)
			command.Env = bindSafeAPTCommandEnvironment()
			return command.CombinedOutputLimited(4 << 10)
		})
}

func resolveServiceUserUIDWithRunner(
	ctx context.Context, getent, user string,
	runner func(context.Context, string, ...string) ([]byte, error),
) (uint32, error) {
	if ctx == nil || getent == "" || runner == nil || user == "" ||
		strings.ContainsAny(user, ":\n\x00 ") {
		return 0, errors.New("invalid service user proof")
	}
	output, err := runner(ctx, getent, "passwd", user)
	if err != nil {
		return 0, fmt.Errorf("resolve %s service user: %w", user, err)
	}
	line := string(output)
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		return 0, fmt.Errorf("getent returned a non-canonical %s passwd record", user)
	}
	fields := strings.Split(strings.TrimSuffix(line, "\n"), ":")
	if len(fields) != 7 || fields[0] != user || fields[2] == "" {
		return 0, fmt.Errorf("getent returned an unsafe %s passwd record", user)
	}
	value, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil || value == 0 || value > uint64(1<<31-1) ||
		strconv.FormatUint(value, 10) != fields[2] {
		return 0, fmt.Errorf("%s service user has an invalid numeric identity", user)
	}
	return uint32(value), nil
}

// prepareBINDRNDCKeyBeforeFirstStart is the host entry of the rule above.
func prepareBINDRNDCKeyBeforeFirstStart(
	ctx context.Context,
	layout bindHostLayout,
	identity bindRNDCKeyIdentity,
	prior bindRNDCKeyPriorInstall,
) (bindrndckey.Record, error) {
	plan, err := bindRNDCKeyPlanForLayout(layout)
	if err != nil {
		return bindrndckey.Record{}, err
	}
	record, err := prepareBINDRNDCKeyWithOps(ctx, plan, identity, prior, hostBINDRNDCKeyOps(layout, plan))
	if err != nil {
		return bindrndckey.Record{}, err
	}
	log.Printf("DNS switch (request %s): BIND rndc key %s is %s%s",
		identity.RequestID, record.Path, record.Provenance, bindRNDCKeyBasisText(record))
	return record, nil
}

func bindRNDCKeyBasisText(record bindrndckey.Record) string {
	if record.Basis == "" {
		return ""
	}
	return " (" + record.Basis + ")"
}

// bindRNDCKeyRollbackOps are the host effects of the rollback rule.
type bindRNDCKeyRollbackOps struct {
	readRecord     func() (bindrndckey.Record, bool, error)
	noNamedProcess func(context.Context) error
	remove         func(path, sha string) (bindrndckey.RemovalOutcome, error)
}

// bindRNDCKeyRollbackOutcome is the rollback's statement about the key.
type bindRNDCKeyRollbackOutcome struct {
	Removed bool
	Text    string
}

// retireBINDRNDCKeyAfterRollbackWithOps runs after a completed rollback of the
// BIND switch transaction named by the journal. It removes the key only when
// the record binds it to this transaction as product-created and the content
// is unchanged; every other case keeps the key and says why. It never fails
// the rollback.
func retireBINDRNDCKeyAfterRollbackWithOps(
	ctx context.Context, journal dnsEngineSwitchJournal, ops bindRNDCKeyRollbackOps,
) bindRNDCKeyRollbackOutcome {
	keep := func(text string) bindRNDCKeyRollbackOutcome {
		return bindRNDCKeyRollbackOutcome{Text: text}
	}
	if ctx == nil || ops.readRecord == nil || ops.noNamedProcess == nil || ops.remove == nil {
		return keep("BIND rndc key was left in place: the rollback key check is unavailable")
	}
	record, exists, err := ops.readRecord()
	switch {
	case err != nil:
		return keep("BIND rndc key was left in place: its provenance record is unreadable: " + err.Error())
	case !exists:
		return keep("BIND rndc key was left in place: this operation recorded no rndc key provenance")
	case !record.SameTransaction(journal.ManifestQualifier, journal.MutationRequestID, journal.MutationOwnerID):
		return keep("BIND rndc key " + record.Path + " was left in place: its provenance record belongs to another operation")
	case record.Provenance != bindrndckey.ProvenanceProductCreated:
		return keep("BIND rndc key " + record.Path + " was left in place: it was provided by the package or the owner" + bindRNDCKeyBasisText(record))
	}
	if err := ops.noNamedProcess(ctx); err != nil {
		return keep("BIND rndc key " + record.Path + " was left in place: named may still be running: " + err.Error())
	}
	outcome, err := ops.remove(record.Path, record.SHA256)
	switch {
	case err != nil:
		return keep("BIND rndc key " + record.Path + " was left in place: removal failed: " + err.Error())
	case outcome == bindrndckey.RemovalRemoved:
		return bindRNDCKeyRollbackOutcome{Removed: true, Text: "removed BIND rndc key " + record.Path + " that this operation created; it was unchanged"}
	case outcome == bindrndckey.RemovalAlreadyAbsent:
		return keep("BIND rndc key " + record.Path + " that this operation created is already absent")
	default:
		return keep("BIND rndc key " + record.Path + " was left in place: this operation created it, but it changed since, so it is treated as the owner's")
	}
}

// retireBINDRNDCKeyAfterRollback is the host entry of the rollback rule. The
// outcome is part of the rollback's logged result.
func retireBINDRNDCKeyAfterRollback(ctx context.Context, journal dnsEngineSwitchJournal) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	outcome := retireBINDRNDCKeyAfterRollbackWithOps(cleanupCtx, journal, bindRNDCKeyRollbackOps{
		readRecord:     readBINDRNDCKeyRecord,
		noNamedProcess: dnsenginerecovery.ProbeNoNamedProcess,
		remove:         bindrndckey.RemoveIfUnchanged,
	})
	log.Printf("DNS switch rollback (request %s): %s", journal.MutationRequestID, outcome.Text)
}
