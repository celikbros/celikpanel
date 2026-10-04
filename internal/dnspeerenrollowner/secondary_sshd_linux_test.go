//go:build linux

package dnspeerenrollowner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestSSHDReceiptBindsExactOriginalAndTailInclude(t *testing.T) {
	original := []byte("Include /etc/ssh/sshd_config.d/*.conf\nPasswordAuthentication no\n")
	active := append(append([]byte(nil), original...), []byte(sshdInclude)...)
	oldHash := sha256.Sum256(original)
	activeHash := sha256.Sum256(active)
	receipt, _ := json.Marshal(sshdReceipt{Schema: sshdReceiptSchema, OriginalSHA256: hex.EncodeToString(oldHash[:]), ActiveSHA256: hex.EncodeToString(activeHash[:])})
	if err := validateSSHDReceipt(original, active, receipt); err != nil {
		t.Fatal(err)
	}
	if err := validateSSHDReceipt(original, append(active, []byte("# owner edit\n")...), receipt); err == nil {
		t.Fatal("owner edit accepted")
	}
	if err := validateSSHDReceipt([]byte("different"), active, receipt); err == nil {
		t.Fatal("wrong backup accepted")
	}
	if err := validateSSHDReceipt(original, active, append(append([]byte(nil), receipt...), []byte(" ")...)); err == nil {
		t.Fatal("noncanonical receipt accepted")
	}
	if bytes.Count(active, []byte("Include "+SSHDConfigPath)) != 1 {
		t.Fatal("managed Include not unique")
	}
}

func TestMixedCaseSSHDSettingsAndAlternateKeySource(t *testing.T) {
	got := parseSSHDSettings([]byte("KbdInteractiveAuthentication no\nAuthorizedKeysCommand /usr/bin/false\nAuthorizedKeysCommandUser nobody\nPubkeyAcceptedAlgorithms ssh-ed25519\n"))
	if got["kbdinteractiveauthentication"] != "no" || got["authorizedkeyscommand"] != "/usr/bin/false" || got["authorizedkeyscommanduser"] != "nobody" || got["pubkeyacceptedalgorithms"] != "ssh-ed25519" {
		t.Fatalf("mixed-case settings lost: %v", got)
	}
}
