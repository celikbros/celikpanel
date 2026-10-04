package hostcmd

import (
	"strings"
	"testing"
)

func TestPackageManagerReasonNamesTheStaleStatOverride(t *testing.T) {
	// Verbatim shape of batch 6b cell c6: apt-get exit status 100.
	out := []byte("Reading package lists...\nBuilding dependency tree...\n" +
		"Reading state information...\nThe following NEW packages will be installed:\n  bind9\n" +
		"dpkg: unrecoverable fatal error, aborting:\n" +
		" unknown system group 'bind' in statoverride file; the system group got removed\n" +
		"before the override, which is most probably a packaging bug, to recover you\n" +
		"can remove the override manually with dpkg-statoverride\n" +
		"E: Sub-process /usr/bin/dpkg returned an error code (2)\n")
	got := PackageManagerReason(out, PackageManagerReasonLimit)
	want := "dpkg: unknown system group 'bind' in statoverride file; the system group got removed"
	if got != want {
		t.Fatalf("reason = %q, want %q", got, want)
	}
}

func TestPackageManagerReasonPrefersSpecificLines(t *testing.T) {
	for _, test := range []struct {
		name string
		out  string
		want string
	}{
		{
			name: "dpkg-archive-continuation",
			out: "Unpacking foo ...\ndpkg: error processing archive /var/cache/apt/archives/foo.deb (--unpack):\n" +
				" trying to overwrite '/usr/bin/x', which is also in package y 1.0\n" +
				"E: Sub-process /usr/bin/dpkg returned an error code (1)\n",
			want: "dpkg: error processing archive /var/cache/apt/archives/foo.deb (--unpack): trying to overwrite '/usr/bin/x', which is also in package y 1.0",
		},
		{
			name: "apt-specific-before-generic",
			out:  "E: Unable to locate package bind9\nE: Sub-process /usr/bin/dpkg returned an error code (1)\n",
			want: "E: Unable to locate package bind9",
		},
		{
			name: "apt-generic-only",
			out:  "Reading package lists...\nE: Sub-process /usr/bin/dpkg returned an error code (1)\n",
			want: "E: Sub-process /usr/bin/dpkg returned an error code (1)",
		},
		{
			name: "pacman",
			out:  ":: Synchronizing package databases...\nerror: target not found: bindx\n",
			want: "error: target not found: bindx",
		},
		{
			name: "last-line-fallback",
			out:  "first\nsecond line\n\n",
			want: "second line",
		},
		{name: "empty", out: "\n \n", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := PackageManagerReason([]byte(test.out), 400); got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPackageManagerReasonRemovesURLSecretsAndBounds(t *testing.T) {
	out := []byte("E: Failed to fetch https://user:s3cret@repo.example.com/token123/pool/b.deb?sig=abc  404  Not Found [IP: 192.0.2.1 443]\n")
	got := PackageManagerReason(out, PackageManagerReasonLimit)
	for _, secret := range []string{"s3cret", "user:", "token123", "sig=abc"} {
		if strings.Contains(got, secret) {
			t.Fatalf("reason repeated %q: %q", secret, got)
		}
	}
	if !strings.Contains(got, "https://repo.example.com/...") {
		t.Fatalf("reason lost the repository host: %q", got)
	}
	long := []byte("E: " + strings.Repeat("x", 1000) + "\x1b[31m\n")
	bounded := PackageManagerReason(long, 50)
	if len([]rune(bounded)) != 53 || strings.ContainsAny(bounded, "\x1b\n") {
		t.Fatalf("reason was not bounded to one clean line: %q", bounded)
	}
}
