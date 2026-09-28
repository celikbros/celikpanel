//go:build linux

package dnspeerenrollowner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestSSHDResumeRecognizesDurablePublicationBoundaries(t *testing.T) {
	original := []byte("Port 22\nPasswordAuthentication no\n")
	active := append(append([]byte(nil), original...), []byte(sshdInclude)...)
	oldHash, newHash := sha256.Sum256(original), sha256.Sum256(active)
	receipt, _ := json.Marshal(sshdReceipt{Schema: sshdReceiptSchema, OriginalSHA256: hex.EncodeToString(oldHash[:]), ActiveSHA256: hex.EncodeToString(newHash[:])})
	for _, tc := range []struct {
		name        string
		live, proof []byte
		published   bool
	}{
		{"backup-before-receipt", original, nil, false},
		{"receipt-before-rename", original, receipt, false},
		{"rename-before-return", active, receipt, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifySSHDPublication(original, tc.live, tc.proof)
			if err != nil || got != tc.published {
				t.Fatalf("published=%v err=%v", got, err)
			}
		})
	}
	for _, tc := range []struct {
		name                string
		before, live, proof []byte
	}{
		{"unrecorded-publication", original, active, nil},
		{"owner-edit-before", original, append(append([]byte(nil), original...), []byte("# owner edit\n")...), receipt},
		{"owner-edit-after", original, append(append([]byte(nil), active...), []byte("# owner edit\n")...), receipt},
		{"changed-receipt", original, active, bytes.Replace(receipt, []byte(oldHashString(oldHash)), []byte(strings.Repeat("0", 64)), 1)},
		{"duplicate-include", active, active, receipt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := classifySSHDPublication(tc.before, tc.live, tc.proof); err == nil {
				t.Fatal("unsafe resume accepted")
			}
		})
	}
}

func oldHashString(hash [32]byte) string { return hex.EncodeToString(hash[:]) }

func TestResumeAccountMustRemainRestricted(t *testing.T) {
	pass := []byte(Account + ":x:991:991::" + HomePath + ":/bin/sh\n")
	shadow := []byte(Account + ":!:20000:0:99999:7:::\n")
	groups := []byte("991\n")
	if err := validateRestrictedAccount(pass, shadow, groups, []byte(Account+":x:991:\n")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                 string
		pass, shadow, groups []byte
	}{
		{"root-uid", bytes.Replace(pass, []byte(":991:991:"), []byte(":0:991:"), 1), shadow, groups},
		{"root-gid", bytes.Replace(pass, []byte(":991:991:"), []byte(":991:0:"), 1), shadow, []byte("0")},
		{"noncanonical-id", bytes.Replace(pass, []byte(":991:991:"), []byte(":0991:991:"), 1), shadow, groups},
		{"owner-home", bytes.Replace(pass, []byte(HomePath), []byte("/home/owner"), 1), shadow, groups},
		{"unlocked-password", pass, bytes.Replace(shadow, []byte(":!:"), []byte(":hash:"), 1), groups},
		{"supplementary-group", pass, shadow, []byte("991 27\n")},
		{"different-primary-group", pass, shadow, []byte("992\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRestrictedAccount(tc.pass, tc.shadow, tc.groups, []byte(Account+":x:991:\n")); err == nil {
				t.Fatal("unsafe account accepted")
			}
		})
	}
}

func TestResumeAccountCannotUseForeignPrimaryGroup(t *testing.T) {
	pass := []byte(Account + ":x:991:991::" + HomePath + ":/bin/sh\n")
	shadow := []byte(Account + ":!:20000:0:99999:7:::\n")
	for _, group := range []string{"sudo:x:991:", Account + ":x:992:", Account + ":x:991:owner"} {
		if err := validateRestrictedAccount(pass, shadow, []byte("991"), []byte(group)); err == nil {
			t.Fatal("foreign primary group accepted")
		}
	}
}
