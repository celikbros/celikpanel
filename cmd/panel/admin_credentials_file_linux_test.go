//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestValidateAdminCredentialsFileMetadata(t *testing.T) {
	const expectedUID = uint32(1234)
	valid := unix.Stat_t{
		Mode:  unix.S_IFREG | 0o600,
		Uid:   expectedUID,
		Nlink: 1,
		Size:  1,
	}
	if err := validateAdminCredentialsFileMetadata(valid, expectedUID); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}

	tests := map[string]func(*unix.Stat_t){
		"not regular": func(stat *unix.Stat_t) { stat.Mode = unix.S_IFIFO | 0o600 },
		"wrong owner": func(stat *unix.Stat_t) { stat.Uid++ },
		"group bit":   func(stat *unix.Stat_t) { stat.Mode = unix.S_IFREG | 0o640 },
		"special bit": func(stat *unix.Stat_t) { stat.Mode = unix.S_IFREG | unix.S_ISUID | 0o600 },
		"hard linked": func(stat *unix.Stat_t) { stat.Nlink = 2 },
		"empty":       func(stat *unix.Stat_t) { stat.Size = 0 },
		"too large":   func(stat *unix.Stat_t) { stat.Size = maxAdminCredentialsFileBytes + 1 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			stat := valid
			mutate(&stat)
			if err := validateAdminCredentialsFileMetadata(stat, expectedUID); err == nil {
				t.Fatal("unsafe metadata was accepted")
			}
		})
	}
}

func TestReadAdminCredentialsFileContentRequiresSecureRegularFile(t *testing.T) {
	content := []byte(`{"username":"first-admin","email":"admin@example.test","password":"secret-password"}`)
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod credentials: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open credentials: %v", err)
	}
	defer file.Close()

	got, err := readAdminCredentialsFileContentForUID(file, uint32(os.Geteuid()))
	if err != nil {
		t.Fatalf("read secure credentials: %v", err)
	}
	if string(got) != string(content) {
		t.Fatal("credentials content changed")
	}
}

func TestReadAdminCredentialsFileAcceptsBoundedPipe(t *testing.T) {
	content := []byte(`{"username":"pipe-admin","email":"pipe@example.test","password":"strong-password"}`)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	go func() {
		_, _ = writer.Write(content)
		_ = writer.Close()
	}()

	credentials, err := readAdminCredentialsFile(reader)
	if err != nil {
		t.Fatalf("read credentials pipe: %v", err)
	}
	if credentials.username != "pipe-admin" ||
		credentials.email != "pipe@example.test" ||
		credentials.password != "strong-password" {
		t.Fatal("credentials pipe content changed")
	}
}

func TestReadAdminCredentialsFileRejectsOversizePipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	go func() {
		_, _ = writer.Write(make([]byte, maxAdminCredentialsFileBytes+1))
		_ = writer.Close()
	}()
	if _, err := readAdminCredentialsFile(reader); err == nil {
		t.Fatal("oversize credentials pipe was accepted")
	}
}

func TestPanelValidateAdminCredentialsCLIReadsPipeWithoutOutput(t *testing.T) {
	content := []byte(`{"username":"cli-admin","email":"cli@example.test","password":"strong-password"}`)
	command := exec.Command(os.Args[0], "-test.run=^TestPanelValidateAdminCredentialsCLIPipeHelper$")
	command.Env = append(os.Environ(), "CELIKPANEL_CREDENTIAL_PIPE_HELPER=1")
	command.Stdin = bytes.NewReader(content)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("validate credentials CLI from pipe: %v; output=%q", err, output)
	}
	if len(output) != 0 || bytes.Contains(output, []byte("strong-password")) {
		t.Fatalf("validation-only CLI emitted output: %q", output)
	}
}

func TestPanelValidateAdminCredentialsCLIPipeHelper(t *testing.T) {
	if os.Getenv("CELIKPANEL_CREDENTIAL_PIPE_HELPER") != "1" {
		return
	}
	os.Args = []string{"panel", validateAdminCredentialsFileArgument}
	main()
	os.Exit(0)
}

func TestReadAdminCredentialsFileContentRejectsHardLink(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"username":"first-admin","email":"admin@example.test","password":"secret-password"}`), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod credentials: %v", err)
	}
	if err := os.Link(path, filepath.Join(directory, "credentials-link.json")); err != nil {
		t.Fatalf("create hard link: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open credentials: %v", err)
	}
	defer file.Close()
	if _, err := readAdminCredentialsFileContentForUID(file, uint32(os.Geteuid())); err == nil {
		t.Fatal("hard-linked credentials file was accepted")
	}
}

func TestSameAdminCredentialsFileStatDetectsReplacementMetadata(t *testing.T) {
	base := unix.Stat_t{Dev: 1, Ino: 2, Mode: unix.S_IFREG | 0o600, Nlink: 1, Uid: 0, Gid: 0, Size: 10}
	if !sameAdminCredentialsFileStat(base, base) {
		t.Fatal("identical metadata was rejected")
	}
	changed := base
	changed.Ino++
	if sameAdminCredentialsFileStat(base, changed) {
		t.Fatal("inode replacement was not detected")
	}
	changed = base
	changed.Ctim.Nsec++
	if sameAdminCredentialsFileStat(base, changed) {
		t.Fatal("content metadata change was not detected")
	}
}
