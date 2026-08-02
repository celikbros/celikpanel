package services

import (
	"context"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestMariaDBCommandKeepsSecretsOutOfProcessArguments(t *testing.T) {
	driver := &MariaDBDriver{
		host:         `db.internal`,
		port:         3307,
		rootPassword: `root-secret`,
	}
	sql := `ALTER USER 'tenant'@'localhost' IDENTIFIED BY 'tenant-secret';`
	cmd, cleanup, err := driver.mysqlCommand(context.Background(), sql)
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(cmd.Args[1], `--defaults-extra-file=`)
	t.Cleanup(cleanup)

	arguments := strings.Join(cmd.Args, `|`)
	for _, secret := range []string{driver.rootPassword, `tenant-secret`, sql} {
		if strings.Contains(arguments, secret) {
			t.Errorf(`mysql process arguments leaked %q: %v`, secret, cmd.Args)
		}
	}
	if path == cmd.Args[1] || path == `` {
		t.Fatalf(`first mysql option is not a defaults-extra-file: %v`, cmd.Args)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != `windows` && info.Mode().Perm() != 0o600 {
		t.Errorf(`client file mode = %o, want 600`, info.Mode().Perm())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	quotedRoot := string(rune(34)) + driver.rootPassword + string(rune(34))
	for _, want := range []string{
		`user=` + string(rune(34)) + `root` + string(rune(34)),
		`password=` + quotedRoot,
		`host=` + string(rune(34)) + driver.host + string(rune(34)),
		`port=3307`,
		`protocol=tcp`,
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf(`protected client file missing %q`, want)
		}
	}

	stdin, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != sql {
		t.Errorf(`mysql stdin = %q, want SQL`, stdin)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf(`protected client file remains after cleanup: %v`, err)
	}
}

func TestQuoteMySQLOptionValueRejectsNUL(t *testing.T) {
	if _, err := quoteMySQLOptionValue(string([]byte{1, 0, 2})); err == nil {
		t.Error(`NUL in a MySQL option value must be refused`)
	}
}
