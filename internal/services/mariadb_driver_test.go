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
		host:     `db.internal`,
		port:     3307,
		password: `root-secret`,
	}
	sql := `ALTER USER 'tenant'@'localhost' IDENTIFIED BY 'tenant-secret';`
	cmd, cleanup, err := driver.mysqlCommand(context.Background(), sql)
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(cmd.Args[1], `--defaults-extra-file=`)
	t.Cleanup(cleanup)

	arguments := strings.Join(cmd.Args, `|`)
	for _, secret := range []string{driver.password, `tenant-secret`, sql} {
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
	quotedRoot := string(rune(34)) + driver.password + string(rune(34))
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

// R-057. The panel now has an account of its own, so the account is a value
// that travels with the credential rather than the constant "root". Empty
// still means root - that is what every credential stored before R-057 is,
// and TestMariaDBCommandKeepsSecretsOutOfProcessArguments above proves it by
// building a driver with no username at all.
//
// R-057. Panelin artik kendi hesabi var; hesap, "root" sabiti yerine kimlik
// bilgisiyle birlikte tasinan bir degerdir. Bos deger hala root demektir.
func TestMariaDBConnectsAsTheConfiguredAccount(t *testing.T) {
	driver := NewMariaDBDriver(DriverConfig{
		Host:     `db.internal`,
		Port:     3307,
		Username: `celikpanel_admin`,
		Password: `admin-secret`,
	})
	cmd, cleanup, err := driver.mysqlCommand(context.Background(), `SELECT 1;`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	path := strings.TrimPrefix(cmd.Args[1], `--defaults-extra-file=`)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `user=` + string(rune(34)) + `celikpanel_admin` + string(rune(34))
	if !strings.Contains(string(content), want) {
		t.Errorf(`protected client file missing %q`, want)
	}
	if strings.Contains(string(content), `user=`+string(rune(34))+`root`+string(rune(34))) {
		t.Error(`client file still names root when another account was configured`)
	}
}
