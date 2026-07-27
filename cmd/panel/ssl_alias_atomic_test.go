package main

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"
)

func aliasCertificateInstallFixture() certificateInstall {
	return certificateInstall{
		Type:           "letsencrypt",
		CertPath:       "/certs/staged/fullchain.pem",
		KeyPath:        "/certs/staged/privkey.pem",
		ChainPath:      "/certs/staged/chain.pem",
		LineageName:    "cp-ssl-state-example-a1b2c3d4",
		ACMEProviderID: "letsencrypt",
		Issuer:         "Let's Encrypt",
		Subject:        "ssl-state.example",
		IssuedAt:       time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
		ExpiresAt:      time.Date(2026, time.October, 25, 10, 0, 0, 0, time.UTC),
		AutoRenew:      true,
		SecureMail:     true,
	}
}

func assertAliasCertificateMutationUnchanged(
	t *testing.T,
	p *Panel,
	domainID int,
	oldCertificateID int64,
	alias string,
	wantAliasCount int,
) {
	t.Helper()

	if got := aliasCount(t, p, domainID, alias); got != wantAliasCount {
		t.Fatalf("alias count after failed transaction = %d, want %d", got, wantAliasCount)
	}
	assertCertificateStatus(t, p, oldCertificateID, "active")

	var totalCertificates, activeCertificates int
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*), SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END)
		FROM ssl_certificates WHERE domain_id = ?`, domainID).
		Scan(&totalCertificates, &activeCertificates); err != nil {
		t.Fatalf("count certificates after failed transaction: %v", err)
	}
	if totalCertificates != 1 || activeCertificates != 1 {
		t.Fatalf(
			"certificate ledger after failed transaction = total:%d active:%d, want total:1 active:1",
			totalCertificates,
			activeCertificates,
		)
	}

	assertSiteSSLState(t, p, domainID, siteSSLState{
		Enabled:     true,
		Type:        "custom",
		CertPath:    sql.NullString{String: "/certs/old/fullchain.pem", Valid: true},
		KeyPath:     sql.NullString{String: "/certs/old/privkey.pem", Valid: true},
		ForceHTTPS:  true,
		HSTSEnabled: true,
		HSTSMaxAge:  300,
	})
}

func TestMutateAliasAndActivateCertificateCommitsOneState(t *testing.T) {
	for _, tc := range []struct {
		name           string
		add            bool
		initialAliases int
		wantAliases    int
	}{
		{name: "add", add: true, initialAliases: 0, wantAliases: 1},
		{name: "delete", add: false, initialAliases: 1, wantAliases: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, domainID, oldCertificateID := newSSLStateFixture(t)
			const alias = "alias.ssl-state.example"
			if tc.initialAliases == 1 {
				if _, err := p.db.GetDB().Exec(`
					INSERT INTO domain_aliases (domain_id, alias)
					VALUES (?, ?)`, domainID, alias); err != nil {
					t.Fatalf("insert alias fixture: %v", err)
				}
			}

			install := aliasCertificateInstallFixture()
			activation, err := p.mutateAliasAndActivateCertificate(
				context.Background(),
				domainID,
				alias,
				tc.add,
				install,
			)
			if err != nil {
				t.Fatalf("mutate alias and activate certificate: %v", err)
			}
			if got := aliasCount(t, p, domainID, alias); got != tc.wantAliases {
				t.Fatalf("committed alias count = %d, want %d", got, tc.wantAliases)
			}
			if !reflect.DeepEqual(activation.OldCertIDs, []int64{oldCertificateID}) {
				t.Fatalf(
					"activation old certificate IDs = %v, want [%d]",
					activation.OldCertIDs,
					oldCertificateID,
				)
			}
			assertCertificateStatus(t, p, oldCertificateID, "revoked")
			assertCertificateStatus(t, p, activation.NewCertID, "active")

			var (
				certificateType string
				certPath        string
				keyPath         string
				chainPath       string
				lineageName     string
				providerID      string
				autoRenew       bool
				secureMail      bool
				renewalStatus   string
			)
			if err := p.db.GetDB().QueryRow(`
				SELECT type, cert_path, key_path, COALESCE(chain_path, ''),
				       COALESCE(lineage_name, ''), COALESCE(acme_provider_id, ''),
				       auto_renew, secure_mail, COALESCE(renewal_status, '')
				FROM ssl_certificates WHERE id = ?`, activation.NewCertID).
				Scan(
					&certificateType,
					&certPath,
					&keyPath,
					&chainPath,
					&lineageName,
					&providerID,
					&autoRenew,
					&secureMail,
					&renewalStatus,
				); err != nil {
				t.Fatalf("read activated certificate: %v", err)
			}
			if certificateType != install.Type ||
				certPath != install.CertPath ||
				keyPath != install.KeyPath ||
				chainPath != install.ChainPath ||
				lineageName != install.LineageName ||
				providerID != install.ACMEProviderID ||
				autoRenew != install.AutoRenew ||
				secureMail != install.SecureMail ||
				renewalStatus != sslPendingActivation {
				t.Fatalf(
					"activated certificate identity = type:%q cert:%q key:%q chain:%q lineage:%q provider:%q auto-renew:%t secure-mail:%t pending:%q",
					certificateType,
					certPath,
					keyPath,
					chainPath,
					lineageName,
					providerID,
					autoRenew,
					secureMail,
					renewalStatus,
				)
			}

			assertSiteSSLState(t, p, domainID, siteSSLState{
				Enabled:     true,
				Type:        install.Type,
				CertPath:    sql.NullString{String: install.CertPath, Valid: true},
				KeyPath:     sql.NullString{String: install.KeyPath, Valid: true},
				ForceHTTPS:  true,
				HSTSEnabled: true,
				HSTSMaxAge:  300,
			})
		})
	}
}

func TestMutateAliasAndActivateCertificateRollsBackAliasConstraintFailures(t *testing.T) {
	t.Run("insert", func(t *testing.T) {
		p, domainID, oldCertificateID := newSSLStateFixture(t)
		const alias = "blocked-add.ssl-state.example"
		if _, err := p.db.GetDB().Exec(`
			CREATE TRIGGER test_block_alias_insert
			BEFORE INSERT ON domain_aliases
			WHEN NEW.alias = 'blocked-add.ssl-state.example'
			BEGIN
				SELECT RAISE(ABORT, 'forced alias insert constraint');
			END`); err != nil {
			t.Fatalf("create insert constraint trigger: %v", err)
		}

		if _, err := p.mutateAliasAndActivateCertificate(
			context.Background(),
			domainID,
			alias,
			true,
			aliasCertificateInstallFixture(),
		); err == nil {
			t.Fatal("alias insert constraint unexpectedly succeeded")
		}
		assertAliasCertificateMutationUnchanged(
			t,
			p,
			domainID,
			oldCertificateID,
			alias,
			0,
		)
	})

	t.Run("delete", func(t *testing.T) {
		p, domainID, oldCertificateID := newSSLStateFixture(t)
		const alias = "blocked-delete.ssl-state.example"
		if _, err := p.db.GetDB().Exec(`
			INSERT INTO domain_aliases (domain_id, alias)
			VALUES (?, ?)`, domainID, alias); err != nil {
			t.Fatalf("insert alias fixture: %v", err)
		}
		if _, err := p.db.GetDB().Exec(`
			CREATE TRIGGER test_block_alias_delete
			BEFORE DELETE ON domain_aliases
			WHEN OLD.alias = 'blocked-delete.ssl-state.example'
			BEGIN
				SELECT RAISE(ABORT, 'forced alias delete constraint');
			END`); err != nil {
			t.Fatalf("create delete constraint trigger: %v", err)
		}

		if _, err := p.mutateAliasAndActivateCertificate(
			context.Background(),
			domainID,
			alias,
			false,
			aliasCertificateInstallFixture(),
		); err == nil {
			t.Fatal("alias delete constraint unexpectedly succeeded")
		}
		assertAliasCertificateMutationUnchanged(
			t,
			p,
			domainID,
			oldCertificateID,
			alias,
			1,
		)
	})
}

func TestMutateAliasAndActivateCertificateRollsBackActivationConstraint(t *testing.T) {
	for _, tc := range []struct {
		name           string
		add            bool
		initialAliases int
	}{
		{name: "after add", add: true, initialAliases: 0},
		{name: "after delete", add: false, initialAliases: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, domainID, oldCertificateID := newSSLStateFixture(t)
			const alias = "activation-failure.ssl-state.example"
			if tc.initialAliases == 1 {
				if _, err := p.db.GetDB().Exec(`
					INSERT INTO domain_aliases (domain_id, alias)
					VALUES (?, ?)`, domainID, alias); err != nil {
					t.Fatalf("insert alias fixture: %v", err)
				}
			}
			install := aliasCertificateInstallFixture()
			install.Type = "invalid-certificate-type"

			if _, err := p.mutateAliasAndActivateCertificate(
				context.Background(),
				domainID,
				alias,
				tc.add,
				install,
			); err == nil {
				t.Fatal("certificate activation constraint unexpectedly succeeded")
			}
			assertAliasCertificateMutationUnchanged(
				t,
				p,
				domainID,
				oldCertificateID,
				alias,
				tc.initialAliases,
			)
		})
	}
}

func TestExactCertificateDNSNamesRequiresSetEquality(t *testing.T) {
	t.Run("case trailing dot order and duplicates are normalized", func(t *testing.T) {
		actual := []string{
			"WWW.SSL-STATE.EXAMPLE.",
			"ssl-state.example",
			"www.ssl-state.example",
		}
		expected := []string{
			"ssl-state.example",
			"www.ssl-state.example",
		}
		if err := exactCertificateDNSNames(actual, expected); err != nil {
			t.Fatalf("equal normalized DNS name sets were rejected: %v", err)
		}
	})

	for _, tc := range []struct {
		name     string
		actual   []string
		expected []string
	}{
		{
			name:     "missing requested alias",
			actual:   []string{"ssl-state.example"},
			expected: []string{"ssl-state.example", "kept.ssl-state.example"},
		},
		{
			name: "extra removed alias",
			actual: []string{
				"ssl-state.example",
				"kept.ssl-state.example",
				"removed.ssl-state.example",
			},
			expected: []string{
				"ssl-state.example",
				"kept.ssl-state.example",
			},
		},
		{
			name: "different alias with equal cardinality",
			actual: []string{
				"ssl-state.example",
				"unexpected.ssl-state.example",
			},
			expected: []string{
				"ssl-state.example",
				"expected.ssl-state.example",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := exactCertificateDNSNames(tc.actual, tc.expected); err == nil {
				t.Fatalf(
					"mismatched DNS name sets were accepted: actual=%v expected=%v",
					tc.actual,
					tc.expected,
				)
			}
		})
	}
}
