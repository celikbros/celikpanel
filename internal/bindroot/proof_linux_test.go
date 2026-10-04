//go:build linux

package bindroot

import (
	"errors"
	"strings"
	"testing"
)

type proofExitError int

func (e proofExitError) Error() string { return "exit" }
func (e proofExitError) ExitCode() int { return int(e) }

func TestClassifyAPTStatOverride(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		err    error
		want   APTStatOverrideState
		ok     bool
	}{
		{"exact", APTExactStatOverrideLine, nil, APTStatOverrideExact, true},
		{"absent", "", proofExitError(1), APTStatOverrideAbsent, true},
		{"empty-success", "", nil, 0, false},
		{"other-exit", "", proofExitError(2), 0, false},
		{"output-on-exit", APTExactStatOverrideLine, proofExitError(1), 0, false},
		{"wrong-mode", "root bind 0775 /var/cache/bind\n", nil, 0, false},
		{"extra-line", APTExactStatOverrideLine + "root bind 1775 /tmp/bind\n", nil, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ClassifyAPTStatOverride([]byte(test.output), test.err)
			if (err == nil) != test.ok || test.ok && got != test.want {
				t.Fatalf("state=%v err=%v, want state=%v ok=%v", got, err, test.want, test.ok)
			}
		})
	}
}

func TestVerifyAPTPackageOwner(t *testing.T) {
	if err := VerifyAPTPackageOwner([]byte(APTExactPackageOwnerLine), nil); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{
		"other: /var/cache/bind\n",
		strings.TrimSuffix(APTExactPackageOwnerLine, "\n"),
		APTExactPackageOwnerLine + "other: /var/cache/bind\n",
	} {
		if err := VerifyAPTPackageOwner([]byte(output), nil); err == nil {
			t.Fatalf("accepted non-canonical package owner %q", output)
		}
	}
	if err := VerifyAPTPackageOwner([]byte(APTExactPackageOwnerLine), errors.New("query failed")); err == nil {
		t.Fatal("accepted failed package query")
	}
}

func TestVerifyPacmanPackageOwner(t *testing.T) {
	if err := VerifyPacmanPackageOwner([]byte("/var/named/ is owned by bind 9.20.27-1\n"), nil); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{
		"/var/named/ is owned by unbound 1.0-1\n",
		"/var/named/ is owned by bind 9.20.27-1",
		"/var/named/ is owned by bind \n",
		"/var/named/ is owned by bind 9.20.27-1\nextra\n",
		"/var/named/ is owned by bind 9.20.27-1 \n",
	} {
		if err := VerifyPacmanPackageOwner([]byte(output), nil); err == nil {
			t.Fatalf("accepted non-canonical package owner %q", output)
		}
	}
	if err := VerifyPacmanPackageOwner([]byte("/var/named/ is owned by bind 9.20.27-1\n"), errors.New("query failed")); err == nil {
		t.Fatal("accepted failed package query")
	}
}
