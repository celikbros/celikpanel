package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Dovecot config comes in two dialects. 2.3 (Ubuntu 24.04) uses mail_location,
// passdb{driver=…,args=…} and ssl_cert=<file; 2.4 (Debian 13 "trixie") removed
// all of those: mail_driver/mail_path, named passdb blocks with explicit
// settings, ssl_server_cert_file, and a new %{user | domain} variable syntax.
// A 2.3-dialect file makes 2.4 refuse to start — caught live on the Debian 13
// golden path. So every writer builds the dialect the installed Dovecot
// actually speaks, and applyDovecotConf validates the result with Dovecot's
// own parser before the service is ever restarted (the nginx -t pattern):
// a wrong config becomes an honest error, never a dead mail server.
//
// Dovecot yapılandırması iki lehçede gelir. 2.3 (Ubuntu 24.04) mail_location,
// passdb{driver=…,args=…} ve ssl_cert=<dosya kullanır; 2.4 (Debian 13
// "trixie") bunların hepsini kaldırdı: mail_driver/mail_path, açık ayarlı
// adlandırılmış passdb blokları, ssl_server_cert_file ve yeni
// %{user | domain} değişken sözdizimi. 2.3 lehçesinde bir dosya 2.4'ü
// başlamaz eder — Debian 13 golden path'inde canlı yakalandı. Bu yüzden her
// yazıcı, kurulu Dovecot'un gerçekten konuştuğu lehçeyi üretir ve
// applyDovecotConf sonucu servis yeniden başlatılmadan ÖNCE Dovecot'un kendi
// ayrıştırıcısıyla doğrular (nginx -t deseni): yanlış yapılandırma dürüst bir
// hataya dönüşür, asla ölü bir posta sunucusuna değil.

// dovecotIs24 reports whether the installed Dovecot speaks the 2.4+ config
// dialect. Unknown/unparsable versions count as 2.4: every distro we target
// ships 2.4+ going forward, and on 2.3 the validation step still catches a
// wrong guess with a clear error instead of a dead service.
// dovecotIs24, kurulu Dovecot'un 2.4+ lehçesini konuşup konuşmadığını
// bildirir. Bilinmeyen sürümler 2.4 sayılır: hedeflediğimiz dağıtımlar artık
// 2.4+ taşıyor ve 2.3'te doğrulama adımı yanlış tahmini yine açık bir hatayla
// yakalar.
func dovecotIs24() bool {
	out, err := exec.Command("dovecot", "--version").Output()
	if err != nil {
		return true
	}
	ver := strings.Fields(strings.TrimSpace(string(out)))
	if len(ver) == 0 {
		return true
	}
	parts := strings.SplitN(ver[0], ".", 3)
	if len(parts) < 2 {
		return true
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return true
	}
	return major > 2 || (major == 2 && minor >= 4)
}

// buildDovecotVirtualConf renders the virtual-mailbox override (auth against
// /etc/dovecot/users, maildirs under mailRootDir) in the requested dialect.
// buildDovecotVirtualConf, sanal-posta-kutusu ekini (auth /etc/dovecot/users'a
// karşı, maildir'ler mailRootDir altında) istenen lehçede üretir.
func buildDovecotVirtualConf(is24 bool) string {
	if !is24 {
		return fmt.Sprintf(`# Managed by CelikPanel — do not edit by hand / elle düzenlemeyin
mail_location = maildir:%s/%%d/%%n
mail_uid = %s
mail_gid = %s
first_valid_uid = %s
passdb {
  driver = passwd-file
  args = scheme=CRYPT username_format=%%u %s
}
userdb {
  driver = passwd-file
  args = username_format=%%u %s
  default_fields = uid=%s gid=%s home=%s/%%d/%%n
}
`, mailRootDir, vmailUID, vmailGID, vmailUID,
			dovecotUsersPath, dovecotUsersPath, vmailUID, vmailGID, mailRootDir)
	}
	// 2.4: mail_location → mail_driver+mail_path, %d/%n → %{user | domain} /
	// %{user | username}, args → explicit settings, default_fields → fields
	// with :default. Our stored hashes carry explicit {SCHEME} prefixes, so
	// default_password_scheme is only the fallback (mirrors 2.3's scheme=CRYPT).
	// 2.4: mail_location → mail_driver+mail_path, %d/%n → %{user | domain} /
	// %{user | username}, args → açık ayarlar, default_fields → :default'lu
	// fields. Kayıtlı özetlerimiz açık {ŞEMA} öneki taşır; default_password_
	// scheme yalnız yedektir (2.3'teki scheme=CRYPT'in aynası).
	userDir := "%{user | domain}/%{user | username}"
	return fmt.Sprintf(`# Managed by CelikPanel — do not edit by hand / elle düzenlemeyin
mail_driver = maildir
mail_path = %s/%s
mail_uid = %s
mail_gid = %s
first_valid_uid = %s
passdb passwd-file {
  default_password_scheme = CRYPT
  passwd_file_path = %s
}
userdb passwd-file {
  passwd_file_path = %s
  fields {
    uid:default = %s
    gid:default = %s
    home:default = %s/%s
  }
}
`, mailRootDir, userDir, vmailUID, vmailGID, vmailUID,
		dovecotUsersPath, dovecotUsersPath, vmailUID, vmailGID, mailRootDir, userDir)
}

// buildDovecotTLSConf renders the TLS drop-in (default certificate + one
// local_name block per SNI name) in the requested dialect.
// buildDovecotTLSConf, TLS ekini (varsayılan sertifika + SNI adı başına bir
// local_name bloğu) istenen lehçede üretir.
func buildDovecotTLSConf(is24 bool, certPath, keyPath string, sni []MailSNIEntry) string {
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

// applyDovecotConf writes a drop-in, then validates the WHOLE resulting config
// with Dovecot's own parser. On failure the previous content is restored (or
// the file removed if it did not exist) and the parser's first complaint is
// returned — configuration can fail loudly, the mail service cannot be broken.
// applyDovecotConf bir ek yazar, sonra ortaya çıkan yapılandırmanın TAMAMINI
// Dovecot'un kendi ayrıştırıcısıyla doğrular. Hata durumunda önceki içerik
// geri gelir (dosya yoktuysa silinir) ve ayrıştırıcının ilk şikâyeti döner —
// yapılandırma gürültüyle başarısız olabilir, posta servisi bozulamaz.
func applyDovecotConf(path string, content string) error {
	prev, prevErr := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	out, err := exec.Command("doveconf", "-n").CombinedOutput()
	if err != nil {
		if prevErr == nil {
			_ = os.WriteFile(path, prev, 0o644)
		} else {
			_ = os.Remove(path)
		}
		return fmt.Errorf("dovecot rejected the configuration: %s", dovecotFirstError(string(out)))
	}
	return nil
}

// dovecotFirstError picks the first line that looks like the actual problem,
// so the operator sees "unknown setting X" rather than a wall of output.
// dovecotFirstError, gerçek soruna benzeyen ilk satırı seçer; operatör çıktı
// duvarı yerine "bilinmeyen ayar X" görür.
func dovecotFirstError(out string) string {
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "Error") || strings.Contains(l, "Fatal") || strings.Contains(l, "Invalid") {
			return l
		}
	}
	return firstLine(out)
}
