// Package mailtlsconfig defines the native configuration generated from the
// accepted mail TLS plan. It neither executes commands nor reads/writes files.
package mailtlsconfig

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/mailtlsartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// PostfixSettings returns the historical settings in producer order. Initial
// configuration may omit an empty myhostname; retained-plan verification must
// compare it even when empty. Callers supply already validated certificate paths.
func PostfixSettings(host, cert, key string) [][2]string {
	return [][2]string{
		{"smtpd_tls_cert_file", cert}, {"smtpd_tls_key_file", key},
		{"smtpd_tls_security_level", "may"}, {"smtp_tls_security_level", "may"},
		{"smtpd_tls_protocols", ">=TLSv1.2"}, {"smtp_tls_protocols", ">=TLSv1.2"},
		{"smtpd_tls_loglevel", "1"}, {"myhostname", host},
	}
}

// PostfixSNI preserves historical producer normalization. An accepted Plan has
// already canonicalized names; this renderer is not an input authorization gate.
func PostfixSNI(sni []transport.MailSNIEntry) []byte {
	var b strings.Builder
	b.WriteString("# Managed by CelikPanel — per-domain mail certificates (SNI).\n")
	for _, entry := range sni {
		for _, name := range entry.Names {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			fmt.Fprintf(&b, "%s %s %s\n", name, entry.KeyPath, entry.CertPath)
		}
	}
	return []byte(b.String())
}

// Dovecot preserves the historical 2.3/2.4 TLS drop-in bytes. Dialect discovery,
// file authority and native parser/service verification remain caller duties.
func Dovecot(is24 bool, certPath, keyPath string, sni []transport.MailSNIEntry) string {
	cert, key := "ssl_cert = <", "ssl_key = <"
	if is24 {
		cert, key = "ssl_server_cert_file = ", "ssl_server_key_file = "
	}
	var b strings.Builder
	b.WriteString("# Managed by CelikPanel — mail TLS. Do not edit by hand.\n")
	b.WriteString("ssl = yes\n")
	b.WriteString("ssl_min_protocol = TLSv1.2\n")
	fmt.Fprintf(&b, "%s%s\n", cert, certPath)
	fmt.Fprintf(&b, "%s%s\n", key, keyPath)
	for _, e := range sni {
		for _, name := range e.Names {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			fmt.Fprintf(&b, "\nlocal_name %s {\n  %s%s\n  %s%s\n}\n",
				name, cert, e.CertPath, key, e.KeyPath)
		}
	}
	return b.String()
}

// Observation contains only the specific native TLS settings and generated
// source files. It must be collected under the caller's host lease from trusted
// commands/files. Configuration agreement is not present listener health or a
// grant to rewrite an owner's changed configuration.
type Observation struct {
	Postfix    map[string]string
	PostfixSNI []byte
	Dovecot    []byte
}

// Verify compares an observation against canonical accepted intent without any
// command execution or filesystem mutation. Indexed Postfix map contents, other
// Dovecot includes/overrides and listener/crypto checks are not proved here.
func Verify(plan *mailtlsartifact.Plan, cert, key, mapPath string, is24 bool, observed Observation) error {
	if err := mailtlsartifact.Validate(plan); err != nil {
		return err
	}
	for _, setting := range PostfixSettings(plan.Myhostname, cert, key) {
		actual, found := observed.Postfix[setting[0]]
		if !found {
			return fmt.Errorf("Postfix setting %s was not observed", setting[0])
		}
		if strings.TrimSpace(actual) != setting[1] {
			return fmt.Errorf("Postfix setting %s does not match the committed snapshot", setting[0])
		}
	}
	actual, found := observed.Postfix["tls_server_sni_maps"]
	if !found {
		return errors.New("Postfix SNI setting was not observed")
	}
	actual = strings.TrimSpace(actual)
	if len(plan.SNI) == 0 {
		if actual != "" {
			return errors.New("Postfix SNI setting is not empty for the committed fallback-only snapshot")
		}
	} else {
		valid := false
		for _, kind := range []string{"lmdb", "hash", "btree"} {
			if actual == kind+":"+mapPath {
				valid = true
				break
			}
		}
		if !valid {
			return errors.New("Postfix SNI setting does not reference the committed managed map")
		}
		if !bytes.Equal(observed.PostfixSNI, PostfixSNI(plan.SNI)) {
			return errors.New("Postfix SNI source does not match the committed snapshot")
		}
	}
	if string(observed.Dovecot) != Dovecot(is24, cert, key, plan.SNI) {
		return errors.New("Dovecot TLS readback does not match the committed snapshot")
	}
	return nil
}
