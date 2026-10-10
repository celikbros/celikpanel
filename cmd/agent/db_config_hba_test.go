package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// What a PostgreSQL 17.11 server reported in pg_hba_file_rules for each of
// these lines on 8 Oct 2026 (a private, temporary cluster on a Debian 13
// development guest). "" means the server accepted the line.
//
// The validator must never refuse a line the server accepts: that would keep an
// owner from saving a file their server runs with. It may accept a line the
// server refuses only where the refusal depends on the server and not on the
// line (SSL off, a build without the method, a name file that is not there, a
// PostgreSQL older than the directive); those are marked serverOnly and are
// caught by asking the running server after the file is installed.
var hbaServerVerdicts = []struct {
	line       string
	refused    string
	serverOnly bool
}{
	{line: `local all all peer`},
	{line: `local all postgres peer map=admins`},
	{line: `local sameuser all md5`},
	{line: `local replication all peer`},
	{line: `host all all 127.0.0.1/32 scram-sha-256`},
	{line: `host all all ::1/128 scram-sha-256`},
	{line: `host all all 192.168.1.0 255.255.255.0 md5`},
	{line: `host all all all scram-sha-256`},
	{line: `host all all samehost trust`},
	{line: `host all all samenet reject`},
	{line: `host all all .example.com scram-sha-256`},
	{line: `host all all example.com ident map=x`},
	{line: `hostssl all all 0.0.0.0/0 cert`, refused: `hostssl record cannot match because SSL is disabled`, serverOnly: true},
	{line: `hostnossl all all 0.0.0.0/0 reject`},
	{line: `hostgssenc all all 0.0.0.0/0 gss`},
	{line: `hostnogssenc all all 0.0.0.0/0 md5`},
	{line: `host "my db" "some user" 10.0.0.0/8 md5`},
	{line: `host db1,db2 +admins,@users 10.0.0.0/8 md5`, refused: `could not open file`, serverOnly: true},
	{line: `host all /^app_.*$ 10.0.0.0/8 md5`},
	{line: `host all all 10.0.0.0/8 ldap ldapserver=x ldapprefix="cn=" ldapsuffix=",dc=x"`},
	{line: `host all all 10.0.0.0/8 radius radiusservers=x radiussecrets=y`},
	{line: `host all all 10.0.0.0/8 pam pamservice=postgresql`},
	{line: `host all all 10.0.0.0/8 password`},
	{line: `host all all 10.0.0.0/8 bsd`, refused: `invalid authentication method "bsd": not supported by this build`, serverOnly: true},
	{line: `host all all 10.0.0.0/8 sspi`, refused: `invalid authentication method "sspi": not supported by this build`, serverOnly: true},
	{line: `host all all 10.0.0.0/8 oauth issuer=x scope=y`, refused: `invalid authentication method "oauth"`, serverOnly: true},
	{line: `local all all ident`},
	{line: `local all all cert`, refused: `cert authentication is only supported on hostssl connections`},
	{line: `host all all 10.0.0.0/33 md5`, refused: `invalid CIDR mask in address "10.0.0.0/33"`},
	{line: `host all all 300.1.1.1/8 md5`, refused: `specifying both host name and CIDR mask is invalid: "300.1.1.1/8"`},
	{line: `host all all 10.0.0.0/8`, refused: `end-of-line before authentication method`},
	{line: `host all all md5`, refused: `end-of-line before authentication method`},
	{line: `local all all`, refused: `end-of-line before authentication method`},
	{line: `local all`, refused: `end-of-line before role specification`},
	{line: `bogus all all trust`, refused: `invalid connection type "bogus"`},
	{line: `local all all trust extra`, refused: `authentication option not in name=value format: extra`},
	{line: `local all all peer nosuchoption=1`, refused: `unrecognized authentication option name: "nosuchoption"`},
	{line: `host all all 10.0.0.0/8 scram-sha-256 clientcert=verify-full`, refused: `clientcert can only be configured for "hostssl" rows`},
	{line: `host all all 10.0.0.1 md5`, refused: `invalid IP mask "md5"`},
	{line: `include extra.conf`, refused: `could not open file`, serverOnly: true},
	{line: `include_if_exists extra.conf`},
	{line: `include_dir hba.d`, refused: `could not open directory`, serverOnly: true},
	{line: `local all all trust # trailing comment`},
	{line: "host all all 10.0.0.0/8 md5 \\"},
}

func TestHBALineVerdictsAgreeWithPostgreSQL(t *testing.T) {
	for _, tc := range hbaServerVerdicts {
		lines := parseHBA(tc.line + "\n")
		got := hbaLineError(lines[0].fields)
		switch {
		case tc.refused == "" || tc.serverOnly:
			if got != "" {
				t.Errorf("%q: refused with %q, but the server accepts the line (or only the server can refuse it)", tc.line, got)
			}
		case !strings.HasPrefix(tc.refused, got) || got == "":
			t.Errorf("%q: validator said %q, the server says %q", tc.line, got, tc.refused)
		}
	}
}

const debianHBA = `# PostgreSQL Client Authentication Configuration File
# ===================================================
#
# DO NOT DISABLE!
# If you change this first entry you will need to make sure that the
# database superuser can access the database using some other method.
local   all             postgres                                peer

# TYPE  DATABASE        USER            ADDRESS                 METHOD

# "local" is for Unix domain socket connections only
local   all             all                                     peer
# IPv4 local connections:
host    all             all             127.0.0.1/32            scram-sha-256
# IPv6 local connections:
host    all             all             ::1/128                 scram-sha-256
local   replication     all                                     peer
host    replication     all             127.0.0.1/32            scram-sha-256
`

// Lines that are carried over unchanged are the owner's: the running server
// already holds them, and a newer PostgreSQL may accept what this list does not
// know. Only what the write changes or adds is judged.
func TestValidateHBAJudgesOnlyTheLinesTheWriteChanges(t *testing.T) {
	current := debianHBA + "host all all 10.0.0.0/8 methodofthefuture\nlocal all all peer option_of_the_future=1\n"

	if refusal := validateHBACandidate(current, current+"host all app 192.0.2.0/24 scram-sha-256\n"); refusal != nil {
		t.Fatalf("a valid added line was refused because of lines that were already there: %+v", refusal)
	}

	candidate := current + "host all app 192.0.2.0/24\n"
	refusal := validateHBACandidate(current, candidate)
	if refusal == nil {
		t.Fatal("an added line without a method was accepted")
	}
	wantLine := strings.Count(current, "\n") + 1
	if refusal.code != transport.ConfigErrorValidationFail || refusal.reason != transport.ConfigInvalidSyntax ||
		refusal.line != wantLine || refusal.detail != "end-of-line before authentication method" {
		t.Fatalf("refusal = %+v, want a syntax refusal for line %d", refusal, wantLine)
	}

	// The same unknown method on a line that is new is judged.
	if validateHBACandidate(debianHBA, current) == nil {
		t.Fatal("a new line with an unknown method was accepted")
	}
	// A copy of an existing line is one more line, not the carried one.
	if validateHBACandidate("local all all bogus\n", "local all all bogus\nlocal all all bogus\n") == nil {
		t.Fatal("a second copy of a line the server does not accept was carried over")
	}
}

func TestHBALocalAdminAccess(t *testing.T) {
	cases := []struct {
		name, file, want string
	}{
		{"the Debian default", debianHBA, hbaAdminGranted},
		{"initdb's trust default", "local all all trust\nhost all all 127.0.0.1/32 trust\n", hbaAdminGranted},
		{"peer for everyone", "local   all   all   peer\n", hbaAdminGranted},
		{"ident on a local socket is peer", "local all postgres ident\n", hbaAdminGranted},
		{"a quoted name", `local "postgres" "postgres" peer` + "\n", hbaAdminGranted},
		{"the replication rule does not match an ordinary connection", "local replication all reject\nlocal all postgres peer\n", hbaAdminGranted},
		{"another user's rule comes first", "local all app md5\nlocal all postgres peer\n", hbaAdminGranted},
		{"a comma list that names postgres", "local app,postgres postgres peer\n", hbaAdminGranted},
		{"a password is asked first", "local all postgres scram-sha-256\nlocal all all peer\n", hbaAdminDenied},
		{"everyone is refused first", "local all all reject\nlocal all postgres peer\n", hbaAdminDenied},
		{"sameuser matches postgres to postgres", "local sameuser all md5\nlocal all postgres peer\n", hbaAdminDenied},
		{"no local rule at all", "host all all 127.0.0.1/32 scram-sha-256\n", hbaAdminDenied},
		{"only comments: what the old editor wrote after a failed read", "# PostgreSQL Client Authentication Configuration File\n# Managed by CelikPanel\n#\n# TYPE  DATABASE        USER            ADDRESS                 METHOD\n", hbaAdminDenied},
		{"an empty file", "", hbaAdminDenied},
		{"a quoted keyword is a name", `local "all" "all" peer` + "\n", hbaAdminDenied},
		{"peer through a user-name map", "local all postgres peer map=admins\n", hbaAdminUnknown},
		{"a name list in another file comes first", "local @admins all md5\nlocal all postgres peer\n", hbaAdminUnknown},
		{"a role membership comes first", "local all +admins md5\nlocal all postgres peer\n", hbaAdminUnknown},
		{"a pattern comes first", "local all /^post md5\nlocal all postgres peer\n", hbaAdminUnknown},
		{"an included file comes first", "include_dir hba.d\nlocal all postgres peer\n", hbaAdminUnknown},
		{"an included file after the deciding rule does not matter", "local all postgres peer\ninclude_dir hba.d\n", hbaAdminGranted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := hbaLocalAdminAccess(tc.file); got != tc.want {
				t.Fatalf("access = %q, want %q", got, tc.want)
			}
		})
	}
}

// The Panel reaches PostgreSQL as the operating system account `postgres` over
// the local socket, and so does the owner at the console. A write that takes
// that away locks both out.
func TestHBALockoutRefusal(t *testing.T) {
	headerOnly := "# PostgreSQL Client Authentication Configuration File\n# Managed by CelikPanel\n#\n# TYPE  DATABASE        USER            ADDRESS                 METHOD\n"
	refused := map[string]string{
		"what the old editor wrote after a failed read":    headerOnly,
		"the administrator rule deleted":                   strings.Replace(strings.Replace(debianHBA, "local   all             postgres                                peer\n", "", 1), "local   all             all                                     peer\n", "", 1),
		"the administrator rule changed to a password":     strings.Replace(debianHBA, "postgres                                peer", "postgres                                scram-sha-256", 1),
		"a reject rule put above it":                       "local all all reject\n" + debianHBA,
		"access that can no longer be read with certainty": "local @admins all md5\n" + debianHBA,
	}
	for name, candidate := range refused {
		t.Run(name, func(t *testing.T) {
			refusal := hbaLockoutRefusal(debianHBA, candidate)
			if refusal == nil {
				t.Fatal("the change was accepted")
			}
			if refusal.code != transport.ConfigErrorValidationFail || refusal.reason != transport.ConfigInvalidLockout {
				t.Fatalf("refusal = %+v, want a lockout refusal", refusal)
			}
		})
	}

	allowed := map[string]string{
		"a rule added for an application":         debianHBA + "host all app 192.0.2.0/24 scram-sha-256\n",
		"the second peer rule changed":            strings.Replace(debianHBA, "all                                     peer", "all                                     scram-sha-256", 1),
		"comments rewritten":                      strings.Replace(debianHBA, "# DO NOT DISABLE!\n", "", 1),
		"the administrator rule written as trust": strings.Replace(debianHBA, "postgres                                peer", "postgres                                trust", 1),
	}
	for name, candidate := range allowed {
		t.Run(name, func(t *testing.T) {
			if candidate == debianHBA {
				t.Fatal("the fixture did not change the file")
			}
			if refusal := hbaLockoutRefusal(debianHBA, candidate); refusal != nil {
				t.Fatalf("refused: %+v", refusal)
			}
		})
	}

	// Where the current file does not grant it, there is nothing for the Panel
	// to take away, and the owner's arrangement is not second-guessed.
	passwordOnly := "local all all scram-sha-256\n"
	if refusal := hbaLockoutRefusal(passwordOnly, passwordOnly+"host all all 10.0.0.0/8 scram-sha-256\n"); refusal != nil {
		t.Fatalf("an owner arrangement without peer access was refused: %+v", refusal)
	}
}

func TestParseHBAKeepsLineNumbersAcrossContinuations(t *testing.T) {
	lines := parseHBA("# one\nhost all all \\\n  10.0.0.0/8 \\\n  md5\nlocal all all peer\n")
	var rules []hbaLogicalLine
	for _, line := range lines {
		if len(line.fields) > 0 {
			rules = append(rules, line)
		}
	}
	if len(rules) != 2 || rules[0].number != 2 || rules[1].number != 5 {
		t.Fatalf("rules = %+v, want the continued rule on line 2 and the next on line 5", rules)
	}
	if got := hbaLineError(rules[0].fields); got != "" {
		t.Fatalf("a continued rule was refused: %q", got)
	}
	if len(rules[0].fields) != 5 || rules[0].fields[4].plain() != "md5" {
		t.Fatalf("fields of the continued rule = %+v", rules[0].fields)
	}
}
