package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/mailtlsconfig"
)

const independentMailPath = "/usr/sbin:/usr/bin:/sbin:/bin"

// This is a separately built, one-shot owner process. It has no RPC listener,
// panel database, license gate, installation or general recovery entrypoint.
// Existing reviewed mail intent, common locks and versioned evidence still apply.
func runIndependentMailRenewal(args []string, euid int, environment []string) int {
	if err := validateIndependentMailEntry(args, euid, environment); err != nil {
		fmt.Fprintln(os.Stderr, "mail renewal refused: "+err.Error())
		return 2
	}
	if err := os.Setenv("PATH", independentMailPath); err != nil {
		return 2
	}
	var err error
	switch args[0] {
	case "--inspect-build-identity":
		fmt.Printf("component=mail-renewal\nversion=%s\ncommit=%s\n", buildVersion, buildCommit)
		return 0
	case "--queue":
		err = queueMailHostCertificateRenewal(args[1])
	case "--process-pending":
		err = deployPendingMailHostCertificate()
	}
	if err != nil {
		// Native output/configuration never becomes a command-line error response.
		fmt.Fprintln(os.Stderr, "mail renewal remains pending; the server owner must review the retained operation and native mail service status before retrying; no general recovery was started")
		return 1
	}
	return 0
}

func validateIndependentMailEnvironment(euid int, environment []string) error {
	if euid != 0 {
		return errors.New("root owner authentication is required")
	}
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "CELIKPANEL_") || strings.HasPrefix(name, "LD_") || strings.HasPrefix(name, "DYLD_") {
			return errors.New("development, fault-injection and loader environment overrides are not supported")
		}
	}
	return nil
}

func validateIndependentMailEntry(args []string, euid int, environment []string) error {
	if err := validateIndependentMailEnvironment(euid, environment); err != nil {
		return err
	}
	if len(args) == 1 && (args[0] == "--process-pending" || args[0] == "--inspect-build-identity") {
		return nil
	}
	if len(args) == 2 && args[0] == "--queue" {
		value := args[1]
		if len(value) == len("celikpanel-mail-")+24 && strings.HasPrefix(value, "celikpanel-mail-") {
			for _, c := range strings.TrimPrefix(value, "celikpanel-mail-") {
				if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
					return errors.New("invalid managed mail lineage")
				}
			}
			return nil
		}
	}
	return errors.New("supported actions are --process-pending, --queue <managed-mail-lineage>, and --inspect-build-identity")
}

func validateIndependentMailSupervisor(args []string, euid int, environment []string) error {
	if err := validateIndependentMailEnvironment(euid, environment); err != nil {
		return err
	}
	if len(args) < 2 || args[0] != "/run/celikpanel/service-mutation.lock" {
		return errors.New("mail renewal supervisor requires the fixed host lock")
	}
	if err := validateIndependentMailCommand(args[1], args[2:]); err != nil {
		return err
	}
	return nil // The shared supervisor next proves the inherited locked descriptor.
}

func validateIndependentMailCommand(path string, args []string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("mail renewal command must use a canonical native executable")
	}
	name := filepath.Base(path)
	dir := filepath.Dir(path)
	if dir != "/usr/sbin" && dir != "/usr/bin" && dir != "/sbin" && dir != "/bin" {
		return errors.New("mail renewal command is outside native executable directories")
	}
	allowed := false
	switch name {
	case "dovecot":
		allowed = len(args) == 1 && args[0] == "--version"
	case "doveconf":
		allowed = len(args) == 1 && args[0] == "-n"
	case "postconf":
		if len(args) == 2 && args[0] == "-h" {
			allowed = args[1] == "tls_server_sni_maps"
			for _, setting := range mailtlsconfig.PostfixSettings("", "", "") {
				if args[1] == setting[0] {
					allowed = true
				}
			}
		}
	case "systemctl":
		if len(args) == 2 && args[0] == "reload" {
			allowed = args[1] == "postfix.service" || args[1] == "dovecot.service"
		}
		if len(args) == 3 && args[0] == "is-active" && args[1] == "--quiet" {
			allowed = args[2] == "postfix.service" || args[2] == "dovecot.service"
		}
	}
	if !allowed {
		return errors.New("command is outside mail renewal observation and reload scope")
	}
	return nil
}
