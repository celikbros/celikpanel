package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
)

func TestRuntimeStatusUnknownCannotBecomeSuccess(t *testing.T) {
	for _, tc := range []struct {
		status recoveryruntime.PromotionStatus
		err    error
	}{
		{recoveryruntime.PromotionStatus{Phase: "committed", Previous: "unproved"}, errors.New("private OS path")},
		{recoveryruntime.PromotionStatus{Phase: "future-complete", Target: "unproved"}, nil},
	} {
		var out, stderr bytes.Buffer
		code := runRuntimeStatus([]string{"runtime-status", "--json"}, 0, func() (recoveryruntime.PromotionStatus, error) { return tc.status, tc.err }, &out, &stderr)
		var value runtimeStatusResult
		if err := json.Unmarshal(out.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if code != exitUnavailable || value.Observation != "unavailable" || value.Phase != "" || value.Previous != "" || value.Target != "" || strings.Contains(out.String(), "private OS") {
			t.Fatal(code, out.String())
		}
	}
}
func TestRuntimeStatusOwnerAndClosedArguments(t *testing.T) {
	for _, args := range [][]string{{"runtime-status", "--force"}, {"runtime-status", "--json", "--json"}, {"runtime-status", "--lang"}, {"runtime-status", "--lang", "de"}, {"runtime-status", "--source", "/foreign"}} {
		if code := runRuntimeStatus(args, 0, func() (recoveryruntime.PromotionStatus, error) {
			t.Fatal("invalid reached observer")
			return recoveryruntime.PromotionStatus{}, nil
		}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitUsage {
			t.Fatal(args, code)
		}
	}
	if code := runRuntimeStatus([]string{"runtime-status"}, 1000, func() (recoveryruntime.PromotionStatus, error) {
		t.Fatal("unprivileged observation")
		return recoveryruntime.PromotionStatus{}, nil
	}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitNotOwner {
		t.Fatal(code)
	}
}
func TestRuntimeStatusActionsAndLanguages(t *testing.T) {
	for _, tc := range []struct{ phase, action string }{{"none", "none"}, {"prepared", "review_same_release_in_panel"}, {"launcher_published", "owner_recovery"}, {"selection_published", "owner_recovery"}, {"committed", "none"}} {
		var out, stderr bytes.Buffer
		inspect := func() (recoveryruntime.PromotionStatus, error) {
			return recoveryruntime.PromotionStatus{Phase: tc.phase}, nil
		}
		if code := runRuntimeStatus([]string{"runtime-status", "--json"}, 0, inspect, &out, &stderr); code != exitOK {
			t.Fatal(code)
		}
		var result runtimeStatusResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != tc.action || result.Observation != "known" {
			t.Fatal(result)
		}
		messages := []string{}
		for _, lang := range []string{"en", "tr"} {
			out.Reset()
			if code := runRuntimeStatus([]string{"runtime-status", "--lang", lang}, 0, inspect, &out, &stderr); code != exitOK {
				t.Fatal(code)
			}
			messages = append(messages, out.String())
			if tc.action == "owner_recovery" && !strings.Contains(out.String(), "sudo /usr/libexec/celikpanel/recovery recover") {
				t.Fatal("missing owner action", out.String())
			}
		}
		if messages[0] == messages[1] {
			t.Fatal("missing translated guidance")
		}
	}
}
