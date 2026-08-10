package main

import (
	"strings"
	"testing"
)

func TestRemoveMailDomainSourceLinesUsesExactSourceDomain(t *testing.T) {
	tests := []struct {
		name    string
		kind    mailDomainMapKind
		input   string
		want    string
		removed bool
	}{
		{
			name: "dovecot users",
			kind: mailDomainDovecotUsers,
			input: "target@example.com:{CRYPT}hash\n" +
				"keep@sub.example.com:{CRYPT}hash\n" +
				"keep@other.test:{CRYPT}hash\n",
			want:    "keep@sub.example.com:{CRYPT}hash\nkeep@other.test:{CRYPT}hash\n",
			removed: true,
		},
		{
			name: "postfix mailboxes",
			kind: mailDomainPostfixMailbox,
			input: "target@example.com example.com/target/\n" +
				"keep@sub.example.com sub.example.com/keep/\n" +
				"keep@other.test other.test/keep/\n",
			want:    "keep@sub.example.com sub.example.com/keep/\nkeep@other.test other.test/keep/\n",
			removed: true,
		},
		{
			name:    "postfix domains",
			kind:    mailDomainPostfixDomains,
			input:   "example.com OK\nsub.example.com OK\nother.test OK\n",
			want:    "sub.example.com OK\nother.test OK\n",
			removed: true,
		},
		{
			name: "virtual sources not destinations",
			kind: mailDomainPostfixVirtual,
			input: "alias@example.com dest@other.test\n" +
				"@example.com catch@other.test\n" +
				"keep@other.test archive@example.com\n",
			want:    "keep@other.test archive@example.com\n",
			removed: true,
		},
		{
			name:    "unchanged bytes",
			kind:    mailDomainPostfixVirtual,
			input:   "# retained comment\nkeep@other.test archive@example.com\n",
			want:    "# retained comment\nkeep@other.test archive@example.com\n",
			removed: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, removed, err := removeMailDomainSourceLines([]byte(test.input), "example.com", test.kind)
			if err != nil {
				t.Fatal(err)
			}
			if removed != test.removed || string(got) != test.want {
				t.Fatalf("removed=%v content=%q, want removed=%v content=%q", removed, got, test.removed, test.want)
			}
		})
	}
}

func TestRemoveMailDomainSourceLinesFailsClosedForMalformedTargetSource(t *testing.T) {
	_, _, err := removeMailDomainSourceLines(
		[]byte("bad..local@example.com destination@other.test\n"),
		"example.com",
		mailDomainPostfixVirtual,
	)
	if err == nil || !strings.Contains(err.Error(), "malformed source") {
		t.Fatalf("error = %v", err)
	}
}

func TestMailDomainQuarantineNameIsShortAndDeterministic(t *testing.T) {
	first := mailDomainQuarantineName(strings.Repeat("a", 253), 2147483647)
	second := mailDomainQuarantineName(strings.Repeat("a", 253), 2147483647)
	if first != second || len(first) > 255 {
		t.Fatalf("quarantine names = %q and %q", first, second)
	}
}
