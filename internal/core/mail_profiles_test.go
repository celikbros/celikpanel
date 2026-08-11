package core

import (
	"slices"
	"testing"
)

func TestMailProfileServiceIDsExposeExactReleasePolicy(t *testing.T) {
	tests := []struct {
		profileID string
		want      []string
	}{
		{MailProfileCore, []string{"postfix", "dovecot"}},
		{MailProfileWebmail, []string{"postfix", "dovecot", "nginx", "php-fpm", "roundcube"}},
		{MailProfileProtected, []string{"postfix", "dovecot", "rspamd"}},
	}

	for _, tt := range tests {
		t.Run(tt.profileID, func(t *testing.T) {
			got, ok := MailProfileServiceIDs(tt.profileID)
			if !ok {
				t.Fatalf("MailProfileServiceIDs(%q) reports unknown", tt.profileID)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("MailProfileServiceIDs(%q)=%v want=%v", tt.profileID, got, tt.want)
			}
			for _, serviceID := range tt.want {
				if !MailProfileContainsService(tt.profileID, serviceID) {
					t.Errorf("MailProfileContainsService(%q, %q)=false", tt.profileID, serviceID)
				}
			}
		})
	}

	for _, profileID := range []string{"", "unknown", "Core-Mail", " core-mail"} {
		if services, ok := MailProfileServiceIDs(profileID); ok || services != nil {
			t.Errorf("MailProfileServiceIDs(%q)=(%v, %v) want (nil, false)", profileID, services, ok)
		}
		if MailProfileContainsService(profileID, "postfix") {
			t.Errorf("unknown profile %q contains postfix", profileID)
		}
	}
	if MailProfileContainsService(MailProfileCore, "Postfix") ||
		MailProfileContainsService(MailProfileCore, " postfix") ||
		MailProfileContainsService(MailProfileCore, "") {
		t.Fatal("profile membership accepted a non-exact service identity")
	}
}

func TestMailProfileServiceIDsReturnsAnImmutableCopy(t *testing.T) {
	first, ok := MailProfileServiceIDs(MailProfileWebmail)
	if !ok || len(first) == 0 {
		t.Fatalf("webmail profile=%v ok=%v", first, ok)
	}
	first[0] = "rspamd"
	first = append(first, "attacker-service")

	second, ok := MailProfileServiceIDs(MailProfileWebmail)
	if !ok {
		t.Fatal("webmail profile disappeared after returned slice mutation")
	}
	want := []string{"postfix", "dovecot", "nginx", "php-fpm", "roundcube"}
	if !slices.Equal(second, want) {
		t.Fatalf("caller mutation changed compiled profile: got=%v want=%v", second, want)
	}
	if MailProfileContainsService(MailProfileWebmail, "rspamd") ||
		MailProfileContainsService(MailProfileWebmail, "attacker-service") {
		t.Fatal("caller widened compiled profile membership")
	}
}
