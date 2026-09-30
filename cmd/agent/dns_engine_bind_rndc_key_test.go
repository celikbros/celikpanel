package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/bindrndckey"
	"github.com/alicelik/celikpanel/internal/transport"
)

var (
	rndcTestQualifier = "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64)
	rndcTestIdentity  = bindRNDCKeyIdentity{
		Qualifier: rndcTestQualifier,
		RequestID: strings.Repeat("1", 32), OwnerID: strings.Repeat("2", 32),
	}
	rndcTestHash  = strings.Repeat("c", 64)
	rndcTestPlan  = bindRNDCKeyPlan{KeyPath: "/etc/rndc.key", ConfPath: "/etc/rndc.conf", MainConfig: "/etc/named.conf", Mode: 0o640, pacman: true}
	rndcPriorSame = bindRNDCKeyPriorInstall{Exists: true, bindRNDCKeyIdentity: bindRNDCKeyIdentity{
		Qualifier: rndcTestQualifier, RequestID: strings.Repeat("3", 32), OwnerID: strings.Repeat("4", 32),
	}}
)

type fakeRNDCKeyHost struct {
	files     map[string]string // path -> content hash ("" = non-regular)
	controls  bool
	record    *bindrndckey.Record
	generated int
	secured   int
	written   []bindrndckey.Record
}

func (h *fakeRNDCKeyHost) ops() bindRNDCKeyOps {
	return bindRNDCKeyOps{
		presence: func(path string) (bindRNDCKeyPresence, error) {
			content, ok := h.files[path]
			switch {
			case !ok:
				return bindRNDCKeyAbsent, nil
			case content == "":
				return bindRNDCKeyOther, nil
			default:
				return bindRNDCKeyRegular, nil
			}
		},
		hash: func(path string) (string, error) {
			if h.files[path] == "" {
				return "", errors.New("not regular")
			}
			return h.files[path], nil
		},
		controls: func(context.Context) (bool, error) { return h.controls, nil },
		generate: func(context.Context) error {
			h.generated++
			h.files[rndcTestPlan.KeyPath] = rndcTestHash
			return nil
		},
		secure: func(context.Context, string) error { h.secured++; return nil },
		readRecord: func() (bindrndckey.Record, bool, error) {
			if h.record == nil {
				return bindrndckey.Record{}, false, nil
			}
			return *h.record, true, nil
		},
		writeRecord: func(record bindrndckey.Record) error {
			if err := record.Validate(); err != nil {
				return err
			}
			h.written = append(h.written, record)
			copied := record
			h.record = &copied
			return nil
		},
	}
}

func TestBINDRNDCKeyCreatedOnlyWhenAbsentWithoutControls(t *testing.T) {
	host := &fakeRNDCKeyHost{files: map[string]string{}}
	record, err := prepareBINDRNDCKeyWithOps(context.Background(), rndcTestPlan, rndcTestIdentity, bindRNDCKeyPriorInstall{}, host.ops())
	if err != nil {
		t.Fatal(err)
	}
	if host.generated != 1 || host.secured != 1 {
		t.Fatalf("generated=%d secured=%d", host.generated, host.secured)
	}
	if record.Provenance != bindrndckey.ProvenanceProductCreated || record.SHA256 != rndcTestHash ||
		record.Path != "/etc/rndc.key" || !record.SameTransaction(rndcTestIdentity.Qualifier, rndcTestIdentity.RequestID, rndcTestIdentity.OwnerID) {
		t.Fatalf("record=%+v", record)
	}
	// The record is durable before the ownership change.
	if len(host.written) != 1 {
		t.Fatalf("writes=%d", len(host.written))
	}
}

func TestBINDRNDCKeyOwnerOrPackageKeyIsNeverTouched(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    map[string]string
		controls bool
		basis    string
	}{
		{"package-key-present", map[string]string{"/etc/rndc.key": rndcTestHash}, false, bindrndckey.BasisKeyPresent},
		{"key-is-a-symlink", map[string]string{"/etc/rndc.key": ""}, false, bindrndckey.BasisKeyPresent},
		{"rndc-conf-present", map[string]string{"/etc/rndc.conf": rndcTestHash}, false, bindrndckey.BasisRNDCConfPresent},
		{"controls-statement", map[string]string{}, true, bindrndckey.BasisControlsStatement},
		{"key-and-controls", map[string]string{"/etc/rndc.key": rndcTestHash}, true, bindrndckey.BasisKeyPresent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := &fakeRNDCKeyHost{files: tc.files, controls: tc.controls}
			record, err := prepareBINDRNDCKeyWithOps(context.Background(), rndcTestPlan, rndcTestIdentity, bindRNDCKeyPriorInstall{}, host.ops())
			if err != nil {
				t.Fatal(err)
			}
			if host.generated != 0 || host.secured != 0 {
				t.Fatalf("touched an owner key: generated=%d secured=%d", host.generated, host.secured)
			}
			if record.Provenance != bindrndckey.ProvenanceOwnerOrPackage || record.Basis != tc.basis || record.SHA256 != "" {
				t.Fatalf("record=%+v", record)
			}
		})
	}
}

func TestBINDRNDCKeyRetryRecognisesOnlyItsOwnUncommittedKey(t *testing.T) {
	ours := bindrndckey.Record{
		Schema: bindrndckey.RecordSchema, Path: "/etc/rndc.key",
		Provenance: bindrndckey.ProvenanceProductCreated, SHA256: rndcTestHash,
		ManifestQualifier: rndcPriorSame.Qualifier, MutationRequestID: rndcPriorSame.RequestID, MutationOwnerID: rndcPriorSame.OwnerID,
	}
	host := &fakeRNDCKeyHost{files: map[string]string{"/etc/rndc.key": rndcTestHash}, record: &ours}
	record, err := prepareBINDRNDCKeyWithOps(context.Background(), rndcTestPlan, rndcTestIdentity, rndcPriorSame, host.ops())
	if err != nil {
		t.Fatal(err)
	}
	if record.Provenance != bindrndckey.ProvenanceProductCreated || record.MutationRequestID != rndcTestIdentity.RequestID ||
		host.generated != 0 || host.secured != 1 {
		t.Fatalf("carry: record=%+v generated=%d secured=%d", record, host.generated, host.secured)
	}

	for name, tc := range map[string]struct {
		prior bindRNDCKeyPriorInstall
		hash  string
	}{
		"changed-content": {rndcPriorSame, strings.Repeat("d", 64)},
		// Commit retires the install receipt: a committed install's key is
		// never residue of a later transaction.
		"committed-install": {bindRNDCKeyPriorInstall{}, rndcTestHash},
		"other-transaction": {bindRNDCKeyPriorInstall{Exists: true, bindRNDCKeyIdentity: rndcTestIdentity}, rndcTestHash},
	} {
		t.Run(name, func(t *testing.T) {
			stale := ours
			host := &fakeRNDCKeyHost{files: map[string]string{"/etc/rndc.key": tc.hash}, record: &stale}
			record, err := prepareBINDRNDCKeyWithOps(context.Background(), rndcTestPlan, rndcTestIdentity, tc.prior, host.ops())
			if err != nil {
				t.Fatal(err)
			}
			if record.Provenance != bindrndckey.ProvenanceOwnerOrPackage || host.generated != 0 || host.secured != 0 {
				t.Fatalf("record=%+v generated=%d secured=%d", record, host.generated, host.secured)
			}
		})
	}
}

func TestBINDRNDCKeyPlanFollowsTheCertifiedLayout(t *testing.T) {
	pacman, err := bindRNDCKeyPlanForLayout(bindHostLayout{MainConfig: "/etc/named.conf", OptionsConfig: "/etc/named.conf", AnchorConfig: "/etc/named.conf"})
	if err != nil || pacman.KeyPath != "/etc/rndc.key" || pacman.OwnerUser != "" || pacman.Mode != 0o640 {
		t.Fatalf("pacman=%+v err=%v", pacman, err)
	}
	apt, err := bindRNDCKeyPlanForLayout(bindHostLayout{MainConfig: "/etc/bind/named.conf", OptionsConfig: "/etc/bind/named.conf.options", AnchorConfig: "/etc/bind/named.conf.local"})
	if err != nil || apt.KeyPath != "/etc/bind/rndc.key" || apt.OwnerUser != "bind" || apt.Mode != 0o640 {
		t.Fatalf("apt=%+v err=%v", apt, err)
	}
	if _, err := bindRNDCKeyPlanForLayout(bindHostLayout{MainConfig: "/opt/named.conf"}); err == nil {
		t.Fatal("unknown layout accepted")
	}
}

func TestNamedConfigControlsStatementDetection(t *testing.T) {
	printed := "options {\n\tdirectory \"/var/named\";\n};\nzone \"localhost\" {\n\ttype primary;\n};\n"
	if namedConfigHasControls([]byte(printed)) {
		t.Fatal("no controls statement expected")
	}
	if namedConfigHasControls([]byte(printed + "key \"controls-key\" {\n\tsecret \"x\";\n};\n")) {
		t.Fatal("a key named controls is not a controls statement")
	}
	if !namedConfigHasControls([]byte(printed + "controls {\n\tinet 127.0.0.1 port 953 allow { 127.0.0.1; } keys { \"k\"; };\n};\n")) {
		t.Fatal("controls statement not detected")
	}
}

func rndcRollbackJournal() dnsEngineSwitchJournal {
	return dnsEngineSwitchJournal{
		ManifestQualifier: rndcTestIdentity.Qualifier,
		MutationRequestID: rndcTestIdentity.RequestID, MutationOwnerID: rndcTestIdentity.OwnerID,
	}
}

func TestBINDRNDCKeyRollbackRemovesOnlyAnUnchangedProductKey(t *testing.T) {
	product := bindrndckey.Record{
		Schema: bindrndckey.RecordSchema, Path: "/etc/rndc.key",
		Provenance: bindrndckey.ProvenanceProductCreated, SHA256: rndcTestHash,
		ManifestQualifier: rndcTestIdentity.Qualifier, MutationRequestID: rndcTestIdentity.RequestID, MutationOwnerID: rndcTestIdentity.OwnerID,
	}
	owner := product
	owner.Provenance, owner.SHA256, owner.Basis = bindrndckey.ProvenanceOwnerOrPackage, "", bindrndckey.BasisKeyPresent
	other := product
	other.MutationRequestID = strings.Repeat("9", 32)
	for _, tc := range []struct {
		name      string
		record    *bindrndckey.Record
		outcome   bindrndckey.RemovalOutcome
		named     error
		removes   bool
		removed   bool
		mentioned string
	}{
		{"product-unchanged", &product, bindrndckey.RemovalRemoved, nil, true, true, "that this operation created; it was unchanged"},
		{"product-changed", &product, bindrndckey.RemovalKeptChanged, nil, true, false, "it changed since, so it is treated as the owner's"},
		{"product-already-absent", &product, bindrndckey.RemovalAlreadyAbsent, nil, true, false, "already absent"},
		{"owner-provided", &owner, "", nil, false, false, "provided by the package or the owner (key_present)"},
		{"other-operation", &other, "", nil, false, false, "belongs to another operation"},
		{"no-record", nil, "", nil, false, false, "recorded no rndc key provenance"},
		{"named-running", &product, "", errors.New("named process 42 is running"), false, false, "named may still be running"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			removeCalls := 0
			outcome := retireBINDRNDCKeyAfterRollbackWithOps(context.Background(), rndcRollbackJournal(), bindRNDCKeyRollbackOps{
				readRecord: func() (bindrndckey.Record, bool, error) {
					if tc.record == nil {
						return bindrndckey.Record{}, false, nil
					}
					return *tc.record, true, nil
				},
				noNamedProcess: func(context.Context) error { return tc.named },
				remove: func(path, sha string) (bindrndckey.RemovalOutcome, error) {
					removeCalls++
					if path != "/etc/rndc.key" || sha != rndcTestHash {
						t.Fatalf("remove %s %s", path, sha)
					}
					return tc.outcome, nil
				},
			})
			if (removeCalls == 1) != tc.removes || outcome.Removed != tc.removed ||
				!strings.Contains(outcome.Text, tc.mentioned) {
				t.Fatalf("calls=%d outcome=%+v", removeCalls, outcome)
			}
		})
	}
}

func TestBINDDeletionProofReturnsTypedRNDCReasonImmediately(t *testing.T) {
	const domain = "pair-accept.test"
	for name, output := range map[string]string{
		"missing-key":  "rndc: neither /etc/rndc.conf nor /etc/rndc.key was found\n",
		"auth-refused": "rndc: connection to remote host closed.\nThis may indicate that\n* the remote server is using an older version of the command protocol,\n* this host is not authorized to connect,\n* the clocks are not synchronized,\n* the key signing algorithm is incorrect, or\n* the key is invalid.\n",
	} {
		t.Run(name, func(t *testing.T) {
			status := func(context.Context, string) ([]byte, error) {
				return []byte(output), errors.New("exit status 1")
			}
			probe := func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
				t.Fatal("no DNS probe after a failed control query")
				return dnsSOAProbeResult{}, nil
			}
			started := time.Now()
			err := verifyBINDV3AuthoritiesAt(context.Background(), "192.0.2.11",
				[]expectedDNSZoneAuthority{{Domain: domain, Delete: true}}, probe, status)
			if time.Since(started) > 2*time.Second {
				t.Fatalf("waited %s for a key that cannot appear", time.Since(started))
			}
			// The publisher wraps the apply error and joins its rollback.
			wrapped := errors.Join(errors.New("previous BIND generation restored and applied"), errors.Join(err))
			unavailable, ok := bindrndckey.ReasonOf(wrapped)
			if !ok || unavailable.Reason() != transport.DNSPublicationFailureBINDRNDCUnavailable ||
				unavailable.Detail != strings.SplitN(output, "\n", 2)[0] {
				t.Fatalf("err=%v", err)
			}
			if strings.Contains(err.Error(), "key is invalid") {
				t.Fatalf("detail is more than the first line: %v", err)
			}
		})
	}
}

func TestBINDNotificationFailureKeepsOnlyTheTypedReason(t *testing.T) {
	typed := typedBINDNotificationFailure("BIND paired catalog notification failed",
		bindrndckey.ClassifyControlFailure([]byte("rndc: neither /etc/rndc.conf nor /etc/rndc.key was found\n"), errors.New("exit status 1")))
	if _, ok := bindrndckey.ReasonOf(typed); !ok {
		t.Fatalf("typed=%v", typed)
	}
	generic := typedBINDNotificationFailure("BIND paired catalog notification failed", errors.New("private output"))
	if _, ok := bindrndckey.ReasonOf(generic); ok || strings.Contains(generic.Error(), "private") {
		t.Fatalf("generic=%v", generic)
	}
}

func TestResolveServiceUserUIDReadsOneCanonicalRecord(t *testing.T) {
	runner := func(output string) func(context.Context, string, ...string) ([]byte, error) {
		return func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if strings.Join(args, " ") != "passwd bind" {
				t.Fatalf("args=%v", args)
			}
			return []byte(output), nil
		}
	}
	uid, err := resolveServiceUserUIDWithRunner(context.Background(), "/usr/bin/getent", "bind",
		runner("bind:x:104:106::/var/cache/bind:/usr/sbin/nologin\n"))
	if err != nil || uid != 104 {
		t.Fatalf("uid=%d err=%v", uid, err)
	}
	for _, bad := range []string{"", "root:x:0:0::/root:/bin/sh\n", "bind:x:0:0::/:/bin/sh\n", "bind:x:104:106::/:/bin/sh\nbind:x:105:106::/:/bin/sh\n"} {
		if _, err := resolveServiceUserUIDWithRunner(context.Background(), "/usr/bin/getent", "bind", runner(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
