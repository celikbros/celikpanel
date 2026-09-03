package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The four one-shot control-plane modes. They run before the panel logs that
// it is starting and before any database is opened, because two of them are
// offline root operations, one only prints a key and one only reads an archive
// header.
//
// Dört tek-atımlık kontrol düzlemi kipi. Panel başladığını yazmadan ve hiçbir
// veritabanı açılmadan önce çalışırlar.
const (
	generateControlPlaneKeyArgument = "--generate-control-plane-key"
	controlPlaneKeyFileArgument     = "--control-plane-key-file=-"
)

type inheritedControlPlaneKeyFileFlag struct {
	set   bool
	value string
}

func (value *inheritedControlPlaneKeyFileFlag) String() string {
	if value == nil {
		return ""
	}
	return value.value
}

func (value *inheritedControlPlaneKeyFileFlag) Set(input string) error {
	if value.set {
		return errors.New("control-plane key file flag may be set only once")
	}
	value.set = true
	value.value = input
	return nil
}

// validateControlPlaneKeyFileArgumentSpellings refuses every near miss of the
// key flag, so a mistyped form can never be read as a path on disk.
func validateControlPlaneKeyFileArgumentSpellings(arguments []string) error {
	for _, argument := range arguments {
		switch {
		case strings.HasPrefix(argument, "--control-plane-key-file"):
			if argument != controlPlaneKeyFileArgument {
				return errors.New("control-plane key file flag must use its exact inherited-stdin form")
			}
		case strings.HasPrefix(argument, "-control-plane-key-file"):
			return errors.New("control-plane key file flag must use its exact inherited-stdin form")
		}
	}
	return nil
}

// validateControlPlaneCommandFlags binds the key flag to the two modes that
// need it and rejects it everywhere else.
func validateControlPlaneCommandFlags(
	generateKey bool,
	createArchivePath string,
	restoreArchivePath string,
	inspectArchivePath string,
	keyFile inheritedControlPlaneKeyFileFlag,
) error {
	create := strings.TrimSpace(createArchivePath) != ""
	restore := strings.TrimSpace(restoreArchivePath) != ""
	inspect := strings.TrimSpace(inspectArchivePath) != ""
	if create && restore {
		return errors.New("creating and restoring a control-plane archive are mutually exclusive")
	}
	if inspect && (create || restore) {
		return errors.New("inspecting a control-plane archive is mutually exclusive with creating or restoring one")
	}
	if keyFile.set {
		if keyFile.value != "-" {
			return errors.New("the control-plane key must be inherited on stdin")
		}
		if !create && !restore {
			return errors.New("the control-plane key file requires an archive create or restore mode")
		}
		if generateKey {
			return errors.New("generating a control-plane key never reads a key")
		}
	}
	if (create || restore) && !keyFile.set {
		return fmt.Errorf(
			"creating or restoring a control-plane archive requires %s",
			controlPlaneKeyFileArgument,
		)
	}
	return nil
}

// runGenerateControlPlaneKey prints one fresh key and nothing else. It needs no
// privileges and touches no state, so an operator can produce a key before the
// host it protects exists.
func runGenerateControlPlaneKey(output io.Writer) error {
	key, err := generateControlPlaneKey()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, key); err != nil {
		return fmt.Errorf("print the control-plane key: %w", err)
	}
	return nil
}

func runCreateControlPlaneArchive(
	destinationPath string,
	keyInput *os.File,
	output io.Writer,
) error {
	if err := requireControlPlaneRoot("control-plane archive creation"); err != nil {
		return err
	}
	if !filepath.IsAbs(destinationPath) {
		return errors.New("the control-plane archive path must be an explicit absolute path")
	}
	destinationPath = filepath.Clean(destinationPath)
	if err := controlPlaneValidateRootOwnedDirectoryChain(filepath.Dir(destinationPath)); err != nil {
		return err
	}
	key, err := readControlPlaneKeyFile(keyInput)
	if err != nil {
		return err
	}
	_, err = createControlPlaneArchive(destinationPath, key, productionControlPlaneRoots(), output)
	return err
}

func runRestoreControlPlaneArchive(
	sourcePath string,
	keyInput *os.File,
	output io.Writer,
) error {
	if err := requireControlPlaneRoot("control-plane archive restore"); err != nil {
		return err
	}
	if !filepath.IsAbs(sourcePath) {
		return errors.New("the control-plane archive path must be an explicit absolute path")
	}
	sourcePath = filepath.Clean(sourcePath)
	if err := controlPlaneValidateRootOwnedDirectoryChain(filepath.Dir(sourcePath)); err != nil {
		return err
	}
	key, err := readControlPlaneKeyFile(keyInput)
	if err != nil {
		return err
	}
	_, err = restoreControlPlaneArchive(sourcePath, key, productionControlPlaneRoots(), output)
	return err
}

// runInspectControlPlaneArchive answers one question and nothing else: is this
// file a control-plane archive this binary understands, and which release wrote
// it? It reads only the plaintext header, so it needs no backup key and can
// never place, decrypt or even open a single member. The installer runs it in
// preflight, before any host mutation, so an operator who names the wrong file
// learns it while the host is still untouched.
//
// runInspectControlPlaneArchive yalnız açık başlığı okur; yedek anahtarı
// gerektirmez ve hiçbir üyeye dokunamaz. Kurulum, makineye dokunmadan önce ön
// kontrolde bunu çalıştırır.
func runInspectControlPlaneArchive(
	sourcePath string,
	output io.Writer,
) error {
	if err := requireControlPlaneRoot("control-plane archive inspection"); err != nil {
		return err
	}
	if !filepath.IsAbs(sourcePath) {
		return errors.New("the control-plane archive path must be an explicit absolute path")
	}
	sourcePath = filepath.Clean(sourcePath)
	if err := controlPlaneValidateRootOwnedDirectoryChain(filepath.Dir(sourcePath)); err != nil {
		return err
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", sourcePath, err)
	}
	defer file.Close()
	header, _, err := readControlPlaneArchivePreamble(file)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		output,
		"format=%d created_at=%s panel_version=%s panel_commit=%s\n",
		header.Format,
		controlPlaneHeaderField(header.CreatedAt),
		controlPlaneHeaderField(header.PanelVersion),
		controlPlaneHeaderField(header.PanelCommit),
	); err != nil {
		return fmt.Errorf("print the control-plane archive header: %w", err)
	}
	return nil
}

// controlPlaneHeaderField keeps one header value on one line. The header is
// attacker-supplied until the first chunk authenticates, so a value carrying a
// newline or a space must never be able to forge a second field.
func controlPlaneHeaderField(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			return '_'
		}
		return r
	}, trimmed)
}

func requireControlPlaneRoot(operation string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("%s must run as root", operation)
	}
	return nil
}

// controlPlaneCommandContract is the plain sentence an operator sees when a
// control-plane mode is refused.
func controlPlaneCommandContract() string {
	return strings.Join([]string{
		"generate the key once with --generate-control-plane-key and keep it off this host",
		"create with --create-control-plane-archive=<absolute path> --control-plane-key-file=-",
		"restore onto a fresh host with --restore-control-plane-archive=<absolute path> --control-plane-key-file=-",
		"read an archive header without any key with --inspect-control-plane-archive=<absolute path>",
		"run both as root with the panel and agent services stopped",
		"pipe the key on stdin; never pass it as an argument",
	}, "; ")
}
