package services

import (
	"fmt"
	"strings"
)

// SQL identifiers (database and user names) cannot be passed as bound
// parameters, and neither can passwords in CREATE/ALTER USER statements.
// So we defend in two layers: strict validation of identifiers against a
// conservative whitelist, and correct quoting/escaping of the values that
// do get embedded. Callers must route every dynamic identifier and secret
// through these helpers.
//
// SQL tanımlayıcıları (veritabanı ve kullanıcı adları) bağlı parametre
// olarak geçirilemez; CREATE/ALTER USER içindeki parolalar da geçemez.
// Bu yüzden iki katmanda savunuruz: tanımlayıcıları muhafazakâr bir izin
// listesine göre sıkı doğrulamak ve gömülen değerleri doğru tırnaklamak/
// kaçışlamak. Çağıranlar her dinamik tanımlayıcıyı ve sırrı bu yardımcı
// fonksiyonlardan geçirmelidir.

const maxIdentifierLen = 63 // PostgreSQL limit; MySQL allows 64

// ValidateSQLIdentifier accepts only names that start with a letter or
// underscore and contain solely letters, digits and underscores. This is
// intentionally stricter than what the engines allow, because these names
// map to OS-level artifacts (roles, databases) and predictability matters
// more than flexibility here.
//
// ValidateSQLIdentifier yalnızca harf ya da alt çizgiyle başlayan ve
// sadece harf, rakam ve alt çizgi içeren adları kabul eder. Bu, motorların
// izin verdiğinden bilinçli olarak daha katıdır; çünkü bu adlar işletim
// sistemi düzeyindeki nesnelere (rol, veritabanı) karşılık gelir ve burada
// öngörülebilirlik esneklikten önemlidir.
func ValidateSQLIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("identifier must not be empty")
	}
	if len(name) > maxIdentifierLen {
		return fmt.Errorf("identifier %q exceeds %d characters", name, maxIdentifierLen)
	}
	for i, r := range name {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		if i == 0 {
			if !isLetter && r != '_' {
				return fmt.Errorf("identifier %q must start with a letter or underscore", name)
			}
			continue
		}
		if !isLetter && !isDigit && r != '_' {
			return fmt.Errorf("identifier %q contains invalid character %q", name, r)
		}
	}
	return nil
}

// QuotePGIdentifier validates then double-quotes an identifier for
// PostgreSQL. The inner-quote doubling is belt-and-suspenders on top of
// validation.
// QuotePGIdentifier bir tanımlayıcıyı doğrular, ardından PostgreSQL için
// çift tırnak içine alır. İçteki tırnağı ikizleme, doğrulamanın üstüne
// ek bir güvencedir.
func QuotePGIdentifier(name string) (string, error) {
	if err := ValidateSQLIdentifier(name); err != nil {
		return "", err
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`, nil
}

// QuoteMySQLIdentifier validates then backtick-quotes an identifier.
// QuoteMySQLIdentifier bir tanımlayıcıyı doğrular, ardından ters tırnak
// içine alır.
func QuoteMySQLIdentifier(name string) (string, error) {
	if err := ValidateSQLIdentifier(name); err != nil {
		return "", err
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`", nil
}

// validateSecret rejects empty secrets and control characters. A password
// may legitimately contain punctuation, so we escape rather than reject
// those, but control bytes have no business in a credential and often
// signal an injection attempt.
// validateSecret boş sırları ve kontrol karakterlerini reddeder. Bir parola
// meşru olarak noktalama içerebilir, bu yüzden onları reddetmek yerine
// kaçışlarız; ancak kontrol baytlarının bir kimlik bilgisinde yeri yoktur
// ve çoğu zaman bir enjeksiyon girişimine işaret eder.
func validateSecret(secret string) error {
	if secret == "" {
		return fmt.Errorf("secret must not be empty")
	}
	for _, r := range secret {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("secret contains a control character")
		}
	}
	return nil
}

// QuotePGStringLiteral escapes a value for use inside single quotes in
// PostgreSQL. With standard_conforming_strings on (the default), only the
// single quote needs doubling.
// QuotePGStringLiteral bir değeri PostgreSQL'de tek tırnak içinde
// kullanmak için kaçışlar. standard_conforming_strings açıkken (varsayılan)
// yalnızca tek tırnağın ikizlenmesi gerekir.
func QuotePGStringLiteral(value string) (string, error) {
	if err := validateSecret(value); err != nil {
		return "", err
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'", nil
}

// QuoteMySQLStringLiteral escapes a value for use inside single quotes in
// MySQL/MariaDB, where the backslash is also an escape character.
// QuoteMySQLStringLiteral bir değeri MySQL/MariaDB'de tek tırnak içinde
// kullanmak için kaçışlar; burada ters bölü de bir kaçış karakteridir.
func QuoteMySQLStringLiteral(value string) (string, error) {
	if err := validateSecret(value); err != nil {
		return "", err
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)
	return "'" + escaped + "'", nil
}

// allowedPrivileges is the whitelist for the non-ALL grant path. Anything
// outside this set is rejected rather than interpolated.
// allowedPrivileges, ALL olmayan yetkilendirme yolu için izin listesidir.
// Bu kümenin dışındaki her şey gömülmek yerine reddedilir.
var allowedPrivileges = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"TRUNCATE": true, "REFERENCES": true, "TRIGGER": true, "CREATE": true,
	"CONNECT": true, "TEMPORARY": true, "EXECUTE": true, "USAGE": true,
}

// ValidatePrivileges checks a comma-separated privilege list against the
// whitelist and returns the normalized, uppercased form.
// ValidatePrivileges, virgülle ayrılmış bir yetki listesini izin listesine
// göre kontrol eder ve normalleştirilmiş, büyük harfli biçimi döndürür.
func ValidatePrivileges(privileges string) (string, error) {
	parts := strings.Split(privileges, ",")
	normalized := make([]string, 0, len(parts))
	for _, p := range parts {
		token := strings.ToUpper(strings.TrimSpace(p))
		if token == "" {
			continue
		}
		if !allowedPrivileges[token] {
			return "", fmt.Errorf("privilege %q is not allowed", token)
		}
		normalized = append(normalized, token)
	}
	if len(normalized) == 0 {
		return "", fmt.Errorf("no valid privileges given")
	}
	return strings.Join(normalized, ", "), nil
}
