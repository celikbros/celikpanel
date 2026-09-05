package services

import (
	"fmt"
	"strconv"
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

// ---------------------------------------------------------------------------
// Names the panel generates for a subscription
// ---------------------------------------------------------------------------
//
// R-051: the panel built these names with fmt.Sprintf("%d_%s", subscriptionID,
// requested), which always begins with a digit, while ValidateSQLIdentifier
// above refuses an identifier that begins with a digit. Every create on a
// registered server therefore answered HTTP 500 and nothing was ever created,
// on either engine, from either screen.
//
// The prefix below is CHOSEN rather than formatted. It is the literal letter
// "s", for subscription, and it is a constant precisely so that no future edit
// to the number can move a digit into the first position again: whatever the
// subscription id is, the first character of the generated name is that letter.
// One letter, not a word, because the tightest limit these names must fit is a
// MySQL account name at 32 characters and every character spent on decoration
// is a character the operator cannot use.
//
// R-051: panel bu adları fmt.Sprintf("%d_%s", ...) ile kuruyordu; ad hep bir
// rakamla başlıyordu ve yukarıdaki doğrulayıcı rakamla başlayan tanımlayıcıyı
// reddediyor. Bu yüzden kayıtlı bir sunucuda hiçbir veritabanı ya da kullanıcı
// oluşturulamıyordu. Aşağıdaki önek biçimlendirilmiş değil SEÇİLMİŞTİR: "s"
// harfi sabittir, böylece abonelik numarası ne olursa olsun adın ilk karakteri
// bir harftir.
const subscriptionIdentifierPrefix = "s"

const (
	// maxGeneratedDatabaseNameLen is the floor of the three limits a generated
	// database name has to satisfy at once: PostgreSQL 63, MySQL/MariaDB 64,
	// and ValidateSQLIdentifier's own 63.
	// Üretilen veritabanı adının aynı anda sağlaması gereken üç sınırın en
	// düşüğü: PostgreSQL 63, MySQL/MariaDB 64, doğrulayıcı 63.
	maxGeneratedDatabaseNameLen = maxIdentifierLen

	// maxGeneratedUserNameLen is the floor of the ACCOUNT-name limits, which
	// are much tighter than the database-name ones: MySQL 32, MariaDB 80,
	// PostgreSQL 63. A subscription can be moved from one engine to the other,
	// so the panel must never mint an account name only one of them can hold;
	// 32 governs both drivers.
	// Hesap adı sınırlarının en düşüğü: MySQL 32, MariaDB 80, PostgreSQL 63.
	// Bir abonelik motorlar arasında taşınabildiği için panel yalnız birinin
	// tutabileceği bir hesap adı üretmemelidir; iki sürücüde de 32 geçerlidir.
	maxGeneratedUserNameLen = 32
)

// SubscriptionDatabaseName returns the engine-side name for a database created
// on a registered server, scoped to the subscription that owns the server.
// SubscriptionDatabaseName, kayıtlı bir sunucuda oluşturulan veritabanının
// motor tarafındaki adını, sunucunun sahibi aboneliğe göre kapsamlandırarak
// döndürür.
func SubscriptionDatabaseName(subscriptionID int, requested string) (string, error) {
	return subscriptionScopedName(
		"database name", subscriptionID, requested, maxGeneratedDatabaseNameLen,
	)
}

// SubscriptionUserName returns the engine-side account name for a database user
// created on a registered server. It is the same decision as
// SubscriptionDatabaseName, deliberately sharing one implementation so the two
// can never drift apart again.
// SubscriptionUserName, kayıtlı bir sunucuda oluşturulan veritabanı
// kullanıcısının motor tarafındaki hesap adını döndürür; aynı kararı paylaşır
// ki ikisi bir daha ayrışmasın.
func SubscriptionUserName(subscriptionID int, requested string) (string, error) {
	return subscriptionScopedName(
		"user name", subscriptionID, requested, maxGeneratedUserNameLen,
	)
}

// subscriptionScopedName composes and then re-validates. The prefix already
// guarantees the first character, but the finished name still goes through the
// single validator every driver calls, so this function can never hand a driver
// a name that driver will refuse - which is the whole defect it exists to end.
//
// Every message it returns is a fixed developer-authored sentence and embeds no
// part of the caller's input, so a handler may pass it straight to the operator
// as a 400 (see writeClientError's contract) instead of the 500 an engine-side
// refusal used to produce.
//
// subscriptionScopedName önce birleştirir, sonra yeniden doğrular. Önek ilk
// karakteri zaten garanti eder; yine de tamamlanmış ad her sürücünün çağırdığı
// tek doğrulayıcıdan geçer. Döndürdüğü her mesaj sabittir ve çağıranın
// girdisinden hiçbir parça taşımaz.
func subscriptionScopedName(
	kind string,
	subscriptionID int,
	requested string,
	limit int,
) (string, error) {
	if subscriptionID <= 0 {
		return "", fmt.Errorf("the %s cannot be built: the subscription is unknown", kind)
	}
	name := strings.TrimSpace(requested)
	if name == "" {
		return "", fmt.Errorf("the %s must not be empty", kind)
	}
	for _, r := range name {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		if !isLetter && !isDigit && r != '_' {
			return "", fmt.Errorf(
				"the %s may contain only letters, digits and underscores", kind,
			)
		}
	}
	scoped := subscriptionIdentifierPrefix + strconv.Itoa(subscriptionID) + "_" + name
	if len(scoped) > limit {
		return "", fmt.Errorf(
			"the %s is too long: with the subscription prefix it has to fit in %d characters",
			kind, limit,
		)
	}
	if err := ValidateSQLIdentifier(scoped); err != nil {
		return "", err
	}
	return scoped, nil
}
