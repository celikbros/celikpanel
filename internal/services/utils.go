package services

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

// GeneratePassword generates a secure random password
func GeneratePassword(length int) (string, error) {
	if length < 8 {
		length = 16
	}

	// Generate random bytes
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Convert to base64 and clean up
	password := base64.URLEncoding.EncodeToString(bytes)
	password = strings.ReplaceAll(password, "-", "")
	password = strings.ReplaceAll(password, "_", "")

	// Truncate to desired length
	if len(password) > length {
		password = password[:length]
	}

	// Ensure it has at least one special char
	password = password + "!@#"[:1]

	return password, nil
}

// updateOrAddSetting updates or adds a setting in config content
func updateOrAddSetting(content, key, value string) string {
	// Try to update existing setting (handles commented and uncommented lines)
	re := regexp.MustCompile(fmt.Sprintf(`(?m)^([ \t]*[;#]?[ \t]*)%s([ \t]*=[ \t]*.*)$`, regexp.QuoteMeta(key)))
	replacement := fmt.Sprintf("%s = %s", key, value)
	if re.MatchString(content) {
		return re.ReplaceAllStringFunc(content, func(string) string { return replacement })
	}
	// If not found, add at the end
	return strings.TrimRight(content, "\n") + "\n" + replacement + "\n"
}
