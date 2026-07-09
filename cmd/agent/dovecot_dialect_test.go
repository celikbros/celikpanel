package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The two dialects must not leak into each other: a 2.3 keyword inside the
// 2.4 output is exactly the class of bug that left Dovecot dead on Debian 13.
// İki lehçe birbirine sızmamalı: 2.4 çıktısında bir 2.3 anahtar sözcüğü, tam
// da Debian 13'te Dovecot'u ölü bırakan hata sınıfıdır.
func TestDovecotVirtualConfDialects(t *testing.T) {
	v23 := buildDovecotVirtualConf(false)
	for _, want := range []string{"mail_location = maildir:", "driver = passwd-file", "default_fields = uid="} {
		if !strings.Contains(v23, want) {
			t.Errorf("2.3 dialect missing %q:\n%s", want, v23)
		}
	}
	for _, forbid := range []string{"mail_driver", "passwd_file_path", "%{user"} {
		if strings.Contains(v23, forbid) {
			t.Errorf("2.3 dialect must not contain %q:\n%s", forbid, v23)
		}
	}

	v24 := buildDovecotVirtualConf(true)
	for _, want := range []string{
		"mail_driver = maildir",
		"mail_path = ",
		"%{user | domain}/%{user | username}",
		"passdb passwd-file {",
		"passwd_file_path = ",
		"uid:default = ",
	} {
		if !strings.Contains(v24, want) {
			t.Errorf("2.4 dialect missing %q:\n%s", want, v24)
		}
	}
	for _, forbid := range []string{"mail_location", "args =", "default_fields", "%d", "%n", "%u"} {
		if strings.Contains(v24, forbid) {
			t.Errorf("2.4 dialect must not contain %q:\n%s", forbid, v24)
		}
	}
}

func TestDovecotTLSConfDialects(t *testing.T) {
	sni := []MailSNIEntry{{Names: []string{"mail.example.com"}, CertPath: "/c.pem", KeyPath: "/k.pem"}}

	v23 := buildDovecotTLSConf(false, "/def-c.pem", "/def-k.pem", sni)
	if !strings.Contains(v23, "ssl_cert = </def-c.pem") || !strings.Contains(v23, "ssl_key = </k.pem") {
		t.Errorf("2.3 TLS dialect wrong:\n%s", v23)
	}
	if strings.Contains(v23, "ssl_server_cert_file") {
		t.Errorf("2.3 TLS dialect must not contain 2.4 names:\n%s", v23)
	}

	v24 := buildDovecotTLSConf(true, "/def-c.pem", "/def-k.pem", sni)
	if !strings.Contains(v24, "ssl_server_cert_file = /def-c.pem") ||
		!strings.Contains(v24, "local_name mail.example.com {") ||
		!strings.Contains(v24, "ssl_server_key_file = /k.pem") {
		t.Errorf("2.4 TLS dialect wrong:\n%s", v24)
	}
	if strings.Contains(v24, "ssl_cert = <") {
		t.Errorf("2.4 TLS dialect must not contain 2.3 names:\n%s", v24)
	}
}

// TestDovecot24AgainstRealParser validates the generated 2.4 dialect with a
// real Dovecot 2.4 doveconf binary — the same check applyDovecotConf performs
// on a live host. Gated on env vars because CI has no Dovecot 2.4:
//
//	CELIKPANEL_DOVECONF=/path/to/doveconf   (from Debian trixie dovecot-core)
//	CELIKPANEL_DOVECONF_LIBS=/lib/dir:...   (its LD_LIBRARY_PATH, optional)
//
// TestDovecot24AgainstRealParser, üretilen 2.4 lehçesini gerçek bir Dovecot
// 2.4 doveconf ikilisiyle doğrular — applyDovecotConf'un canlı makinede
// yaptığı denetimin aynısı. CI'da Dovecot 2.4 olmadığından env ile kapılıdır.
func TestDovecot24AgainstRealParser(t *testing.T) {
	doveconf := os.Getenv("CELIKPANEL_DOVECONF")
	if doveconf == "" {
		t.Skip("CELIKPANEL_DOVECONF not set; skipping real-parser validation")
	}

	dir := t.TempDir()
	confD := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confD, 0o755); err != nil {
		t.Fatal(err)
	}

	// Same skeleton Debian trixie ships in /usr/share/dovecot/dovecot.conf.
	// Debian trixie'nin /usr/share/dovecot/dovecot.conf iskeletiyle aynı.
	main := "dovecot_config_version = 2.4.0\ndovecot_storage_version = 2.4.0\n!include conf.d/*.conf\n"
	if err := os.WriteFile(filepath.Join(dir, "dovecot.conf"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}

	// The generated configs reference real /etc paths; point them into the
	// sandbox so the parser checks syntax, not this machine's filesystem.
	// Üretilen yapılandırmalar gerçek /etc yollarına başvurur; ayrıştırıcı bu
	// makinenin dosya sistemini değil sözdizimini denetlesin diye kum havuzuna
	// yönlendir.
	users := filepath.Join(dir, "users")
	cert := filepath.Join(dir, "cert.pem")
	if err := os.WriteFile(users, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cert, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	virtual := strings.ReplaceAll(buildDovecotVirtualConf(true), dovecotUsersPath, users)
	tls := buildDovecotTLSConf(true, cert, cert, []MailSNIEntry{
		{Names: []string{"mail.example.com"}, CertPath: cert, KeyPath: cert},
	})
	submission := "service auth {\n  unix_listener " + filepath.Join(dir, "auth") + " {\n    mode = 0660\n  }\n}\n"

	for name, content := range map[string]string{
		"99-celikpanel.conf":            virtual,
		"98-celikpanel-tls.conf":        tls,
		"97-celikpanel-submission.conf": submission,
	} {
		if err := os.WriteFile(filepath.Join(confD, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command(doveconf, "-n", "-c", filepath.Join(dir, "dovecot.conf"))
	if libs := os.Getenv("CELIKPANEL_DOVECONF_LIBS"); libs != "" {
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+libs)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real Dovecot 2.4 parser rejected the generated config:\n%s", out)
	}
}
