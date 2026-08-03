package services

import (
	"os/exec"
	"regexp"
	"strings"
)

// VersionDetector provides generic version detection for any service
type VersionDetector struct{}

func NewVersionDetector() *VersionDetector {
	return &VersionDetector{}
}

// DetectVersion attempts to auto-detect version using common patterns
func (vd *VersionDetector) DetectVersion(serviceName string) string {
	// Special cases first (faster)
	if version := vd.detectSpecialCases(serviceName); version != "" {
		return version
	}

	// Try common version flags
	patterns := [][]string{
		{serviceName, "--version"},
		{serviceName, "-v"},
		{serviceName, "-V"},
		{serviceName, "version"},
	}

	for _, args := range patterns {
		cmd := exec.Command(args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err == nil && len(output) > 0 {
			if version := vd.parseVersionString(string(output)); version != "" {
				return version
			}
		}
	}

	return "unknown"
}

// detectSpecialCases handles services with non-standard version commands
func (vd *VersionDetector) detectSpecialCases(serviceName string) string {
	switch {
	case strings.Contains(serviceName, "php") && strings.Contains(serviceName, "fpm"):
		// php8.3-fpm -> 8.3
		parts := strings.Split(serviceName, "-")
		if len(parts) > 0 && strings.HasPrefix(parts[0], "php") {
			return strings.TrimPrefix(parts[0], "php")
		}

	case serviceName == "phpsessionclean":
		return "-"

	case strings.Contains(serviceName, "postgres") && strings.Contains(serviceName, "@"):
		// postgresql@16-main -> 16
		parts := strings.Split(serviceName, "@")
		if len(parts) > 1 {
			versionParts := strings.Split(parts[1], "-")
			if len(versionParts) > 0 {
				return versionParts[0]
			}
		}

	case strings.Contains(serviceName, "mariadb") || strings.Contains(serviceName, "mysql"):
		cmd := exec.Command("mariadb", "--version")
		if exec.Command("which", "mariadb").Run() != nil {
			cmd = exec.Command("mysql", "--version")
		}
		if output, err := cmd.CombinedOutput(); err == nil {
			return vd.parseVersionString(string(output))
		}

	case strings.Contains(serviceName, "postfix"):
		cmd := exec.Command("postconf", "-d", "mail_version")
		if output, err := cmd.CombinedOutput(); err == nil {
			parts := strings.Split(string(output), "=")
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}

	return ""
}

// parseVersionString extracts version number from command output
func (vd *VersionDetector) parseVersionString(output string) string {
	output = strings.TrimSpace(output)

	// Common patterns
	patterns := []struct {
		regex *regexp.Regexp
		group int
	}{
		// "nginx version: nginx/1.24.0" -> 1.24.0
		{regexp.MustCompile(`(?i)nginx/(\d+\.\d+[\.\d]*)`), 1},

		// "mariadb Ver 10.11.13-MariaDB" -> 10.11.13
		{regexp.MustCompile(`(?i)Ver\s+(\d+\.\d+[\.\d]*)`), 1},

		// "certbot 2.7.4" -> 2.7.4
		{regexp.MustCompile(`(?i)certbot\s+(\d+\.\d+[\.\d]*)`), 1},

		// "Fail2Ban v1.0.2" -> 1.0.2
		{regexp.MustCompile(`(?i)v(\d+\.\d+[\.\d]*)`), 1},

		// "vsftpd: version 3.0.5" -> 3.0.5
		{regexp.MustCompile(`(?i)version\s+(\d+\.\d+[\.\d]*)`), 1},

		// "2.3.19.1 (9b53102964)" -> 2.3.19.1 (dovecot style)
		{regexp.MustCompile(`^(\d+\.\d+[\.\d]*)`), 1},

		// SpamAssassin 3.4.6 or similar
		{regexp.MustCompile(`(?i)spamassassin\s+(\d+\.\d+[\.\d]*)`), 1},
	}

	for _, p := range patterns {
		if matches := p.regex.FindStringSubmatch(output); matches != nil && len(matches) > p.group {
			version := matches[p.group]
			// Remove any suffix like -MariaDB
			version = strings.Split(version, "-")[0]
			return version
		}
	}

	return ""
}
