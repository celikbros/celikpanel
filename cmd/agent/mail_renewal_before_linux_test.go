//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"github.com/alicelik/celikpanel/internal/mailrenewalintent"
)

func mailRenewalBeforeTestMaterial(t *testing.T) (state, tls string, request *ServiceMutationBeginRequest, leaf []byte) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root fixture")
	}
	fixture := createTestPanelCertificateSource(t)
	cert, key, oldLeaf, _, err := readPanelCertificateSource(fixture.domain)
	if err != nil {
		t.Fatal(err)
	}
	old, err := mailhostartifact.NewReceipt(testMutationRequestID, testMailHostCertificateQualifier(t), fixture.domain, oldLeaf)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := mailhostartifact.CanonicalReceipt(old)
	if err != nil {
		t.Fatal(err)
	}
	state = t.TempDir()
	tls = t.TempDir()
	if err = os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(state, 0, int(serviceMutationRequiredOwnerGID)); err != nil {
		t.Fatal(err)
	}
	version := managedPanelCertVersionPrefix + strings.Repeat("3", 32)
	if err = os.Mkdir(filepath.Join(tls, version), 0750); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"fullchain.pem": cert, "privkey.pem": key, "mail.domain": []byte(fixture.domain + "\n"), mailHostCertificateReceiptName: raw} {
		if err = os.WriteFile(filepath.Join(tls, version, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Symlink(version, filepath.Join(tls, "current")); err != nil {
		t.Fatal(err)
	}
	// Only the fresh-source reader is substituted. Selection and before-image
	// use real root-owned material, its trusted pair, and durable filesystem I/O.
	leaf = []byte("distinct source DER supplied by the already-verified source adapter")
	id, owner, qualifier, err := mailrenewalintent.Identity(fixture.domain, buildCommit, leaf)
	if err != nil {
		t.Fatal(err)
	}
	request = &ServiceMutationBeginRequest{RequestID: id, OwnerID: owner, Kind: "mail_host_certificate", Target: fixture.domain, PackageName: qualifier}
	pending := mailhostartifact.Pending{Lineage: mailhostartifact.LineageName(fixture.domain), LeafSHA256: mailhostartifact.LeafSHA256(leaf)}
	raw, err = mailhostartifact.CanonicalPending(pending)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "mail-host-certificate-renewal.pending")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(path, 0, int(serviceMutationRequiredOwnerGID)); err != nil {
		t.Fatal(err)
	}
	return
}

func TestMailRenewalBeforeAdmissionRequiresFreshMaterialAndImmutableEvidence(t *testing.T) {
	for _, scenario := range []string{"normal", "foreign-request", "foreign-owner", "foreign-queue", "source-changed", "selection-changed", "release-unknown", "missing-selected", "owner-before", "same-bytes-owner-replacement"} {
		t.Run(scenario, func(t *testing.T) {
			state, tls, request, leaf := mailRenewalBeforeTestMaterial(t)
			name, _ := mailrenewalintent.FileName(request.RequestID)
			path := filepath.Join(state, name)
			calls := 0
			read := func(string) ([]byte, error) {
				calls++
				if scenario == "source-changed" && calls > 1 {
					return []byte("other source"), nil
				}
				if scenario == "selection-changed" && calls == 2 {
					link := filepath.Join(tls, "current")
					target, e := os.Readlink(link)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.Rename(link, link+"-owner-kept"); e != nil {
						t.Fatal(e)
					}
					if e = os.Symlink(target, link); e != nil {
						t.Fatal(e)
					}
				}
				return leaf, nil
			}
			check := func() error {
				if scenario == "release-unknown" {
					return errors.New("release evidence unavailable")
				}
				return nil
			}
			switch scenario {
			case "foreign-request":
				request.RequestID = strings.Repeat("f", 32)
			case "foreign-owner":
				request.OwnerID = strings.Repeat("f", 32)
			case "foreign-queue":
				raw, _ := mailhostartifact.CanonicalPending(mailhostartifact.Pending{Lineage: mailhostartifact.LineageName(request.Target), LeafSHA256: strings.Repeat("e", 64)})
				if e := os.WriteFile(filepath.Join(state, "mail-host-certificate-renewal.pending"), raw, 0600); e != nil {
					t.Fatal(e)
				}
			case "missing-selected":
				if e := os.Remove(filepath.Join(tls, "current")); e != nil {
					t.Fatal(e)
				}
			case "owner-before":
				if e := os.WriteFile(path, []byte("owner evidence"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			err := persistMailRenewalBeforeAt(state, tls, request, buildCommit, read, check)
			if scenario != "normal" && scenario != "same-bytes-owner-replacement" {
				if err == nil {
					t.Fatal("unsafe admission accepted")
				}
				if scenario == "owner-before" {
					got, e := os.ReadFile(path)
					if e != nil || string(got) != "owner evidence" {
						t.Fatal("owner evidence changed", e)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			before, e := mailrenewalintent.Decode(raw)
			if e != nil || mailrenewalintent.VerifySource(before, buildCommit, leaf) != nil {
				t.Fatal("wrong prior proof", e)
			}
			if scenario == "same-bytes-owner-replacement" {
				link := filepath.Join(tls, "current")
				target, e := os.Readlink(link)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.Rename(link, link+"-owner-kept"); e != nil {
					t.Fatal(e)
				}
				if e = os.Symlink(target, link); e != nil {
					t.Fatal(e)
				}
			}
			err = persistMailRenewalBeforeAt(state, tls, request, buildCommit, read, check)
			if (scenario == "normal") != (err == nil) {
				t.Fatal("wrong immutable retry decision", err)
			}
			after, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(after, raw) {
				t.Fatal("prior selection rebound", e)
			}
		})
	}
}

func TestMailRenewalLedgerCannotBecomeActiveBeforeDurableBeforeImage(t *testing.T) {
	base, _ := newMutationTestManager(t)
	request := renewalScopeTestRequest(t)
	for _, scenario := range []string{"missing-adapter", "unavailable-proof"} {
		t.Run(scenario, func(t *testing.T) {
			m, err := newMailRenewalMutationManager(filepath.Dir(base.ledgerPath), base.lockPath, request)
			if err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(base.ledgerPath)
			if err != nil {
				t.Fatal(err)
			}
			m.mailRenewalBeforeAdmission = nil
			if scenario != "missing-adapter" {
				m.mailRenewalBeforeAdmission = func(*ServiceMutationBeginRequest) error { return errors.New("before-image could not be confirmed") }
			}
			if _, err = m.begin(request); err == nil {
				t.Fatal("unproven before-image admitted")
			}
			after, err := os.ReadFile(base.ledgerPath)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatal("admission changed ledger without proof", err)
			}
			if m.active != nil || m.ledger.ActiveRequestID != "" {
				t.Fatal("before-image failure left active operation")
			}
		})
	}
}

func TestMailRenewalBeforeImagePrecedesEveryLedgerPublication(t *testing.T) {
	state, tls, request, leaf := mailRenewalBeforeTestMaterial(t)
	base, _ := newMutationTestManager(t)
	raw, e := os.ReadFile(base.ledgerPath)
	if e != nil {
		t.Fatal(e)
	}
	ledgerPath := filepath.Join(state, "service-mutations.json")
	if e = os.WriteFile(ledgerPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Chown(ledgerPath, 0, int(serviceMutationRequiredOwnerGID)); e != nil {
		t.Fatal(e)
	}
	m, e := newMailRenewalMutationManager(state, base.lockPath, request)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	m.mailRenewalBeforeAdmission = func(got *ServiceMutationBeginRequest) error {
		calls++
		if got != request {
			t.Fatal("admission scope changed")
		}
		if lock, e := acquireServiceMutationHostAndPublicationLocks(base.lockPath); e == nil {
			lock.Close()
			t.Fatal("before-image lacks host exclusion")
		}
		return persistMailRenewalBeforeAt(state, tls, got, buildCommit, func(string) ([]byte, error) { return leaf, nil }, func() error { return nil })
	}
	name, _ := mailrenewalintent.FileName(request.RequestID)
	m.writeFault = func(point string) error {
		value, found, e := mailrenewalintent.Read(filepath.Join(state, name), uint32(serviceMutationRequiredOwnerGID))
		if e != nil || !found || mailrenewalintent.VerifySource(value, buildCommit, leaf) != nil {
			t.Fatal("ledger preceded durable before-image", point, e)
		}
		return nil
	}
	job, e := m.begin(request)
	if e != nil || job.Attempt != 1 || calls != 1 {
		t.Fatal("admission failed", e)
	}
	if _, e = m.finish(&ServiceMutationFinishRequest{RequestID: request.RequestID, OwnerID: request.OwnerID, FailureCode: "fixture", Message: "no selection change"}); e != nil {
		t.Fatal(e)
	}
	request.Resume = true
	job, e = m.begin(request)
	if e != nil || job.Attempt != 2 || calls != 2 {
		t.Fatal("retry lost before-image or budget", e)
	}
	if _, e = m.finish(&ServiceMutationFinishRequest{RequestID: request.RequestID, OwnerID: request.OwnerID, FailureCode: "fixture", Message: "no selection change"}); e != nil {
		t.Fatal(e)
	}
}

func TestMailRenewalBeforePublicationRechecksOwnerSelection(t *testing.T) {
	for _, change := range []bool{false, true} {
		t.Run(map[bool]string{false: "stable", true: "owner-replaced"}[change], func(t *testing.T) {
			state, tls, request, leaf := mailRenewalBeforeTestMaterial(t)
			read := func(string) ([]byte, error) { return leaf, nil }
			gate := func() error { return nil }
			if e := persistMailRenewalBeforeAt(state, tls, request, buildCommit, read, gate); e != nil {
				t.Fatal(e)
			}
			if change {
				link := filepath.Join(tls, "current")
				target, e := os.Readlink(link)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.Rename(link, link+"-owner-kept"); e != nil {
					t.Fatal(e)
				}
				if e = os.Symlink(target, link); e != nil {
					t.Fatal(e)
				}
			}
			if e := verifyMailRenewalBeforeAt(state, tls, request, buildCommit, read, gate); change != (e != nil) {
				t.Fatal("wrong publication decision", e)
			}
		})
	}
}
