package auth

import "fmt"

// ValidateUsername enforces a conservative username policy: 3–32 characters
// of letters, digits, underscore, hyphen or dot, starting with a letter or
// digit. Usernames may map to display and lookup, so predictability is
// preferred over permissiveness.
//
// ValidateUsername, muhafazakâr bir kullanıcı adı politikası uygular: harf,
// rakam, alt çizgi, tire veya nokta içeren 3–32 karakter; harf ya da
// rakamla başlar. Kullanıcı adları görüntüleme ve aramaya karşılık
// gelebildiğinden, öngörülebilirlik esnekliğe tercih edilir.
func ValidateUsername(username string) error {
	if len(username) < 3 || len(username) > 32 {
		return fmt.Errorf("username must be 3–32 characters")
	}
	for i, r := range username {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		if i == 0 {
			if !isLetter && !isDigit {
				return fmt.Errorf("username must start with a letter or digit")
			}
			continue
		}
		if !isLetter && !isDigit && r != '_' && r != '-' && r != '.' {
			return fmt.Errorf("username contains an invalid character %q", r)
		}
	}
	return nil
}
