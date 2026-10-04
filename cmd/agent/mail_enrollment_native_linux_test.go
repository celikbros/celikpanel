//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

func TestMailEnrollmentNativeCommandBoundary(t *testing.T) {
	for _, pair := range [][2]string{{"restart", mailrenewalkit.TimerName}, {"start", mailrenewalkit.ServiceName}, {"stop", "postfix.service"}, {"daemon-reload", mailrenewalkit.TimerName}, {"show", "sshd.service"}, {"show", "--all"}, {"enable", mailrenewalkit.TimerName}} {
		if _, err := mailEnrollmentSystemctlArgs(pair[0], pair[1]); err == nil {
			t.Fatal("unexpected native authority", pair)
		}
	}
	for _, pair := range [][2]string{{"show", mailrenewalkit.ServiceName}, {"show", mailrenewalkit.TimerName}, {"daemon-reload", ""}, {"start", mailrenewalkit.TimerName}, {"stop", mailrenewalkit.TimerName}} {
		if _, err := mailEnrollmentSystemctlArgs(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
}
func TestMailEnrollmentNativeObservationAndFailureGuidance(t *testing.T) {
	absent := []byte("LoadState=not-found\nFragmentPath=\nDropInPaths=\nNeedDaemonReload=no\nActiveState=inactive\nUnitFileState=\n")
	if _, err := readMailEnrollmentSystemctlResult("show", mailrenewalkit.TimerName, absent, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := readMailEnrollmentSystemctlResult("show", mailrenewalkit.TimerName, []byte("LoadState=not-found\n"), nil, nil); err == nil {
		t.Fatal("incomplete observation became absence")
	}
	secret := "password=do-not-repeat"
	_, err := readMailEnrollmentSystemctlResult("start", mailrenewalkit.TimerName, nil, []byte(secret+" permission denied"), errors.New("could not start"))
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "denied") || !strings.Contains(err.Error(), "same operation") {
		t.Fatal("unsafe or unactionable native error", err)
	}
}

func TestMailEnrollmentNativeSettlesBusyServiceWithoutMutation(t *testing.T) {
	unit := mailrenewalkit.ServiceName
	idle := []byte("LoadState=loaded\nFragmentPath=/etc/systemd/system/" + unit + "\nDropInPaths=\nNeedDaemonReload=no\nActiveState=inactive\nUnitFileState=static\n")
	for _, state := range []string{"active", "activating", "deactivating"} {
		t.Run(state, func(t *testing.T) {
			calls := 0
			raw, err := observeSettledMailEnrollmentUnit(context.Background(), unit, time.Second, time.Millisecond, func(context.Context, string) ([]byte, error) {
				calls++
				if calls < 3 {
					return bytes.Replace(idle, []byte("ActiveState=inactive"), []byte("ActiveState="+state), 1), nil
				}
				return idle, nil
			})
			if err != nil || calls != 3 || !bytes.Equal(raw, idle) {
				t.Fatalf("calls=%d raw=%s error=%v", calls, raw, err)
			}
		})
	}
	busy := bytes.Replace(idle, []byte("ActiveState=inactive"), []byte("ActiveState=activating"), 1)
	t.Run("bounded-busy-remains-busy", func(t *testing.T) {
		raw, err := observeSettledMailEnrollmentUnit(context.Background(), unit, 15*time.Millisecond, time.Millisecond, func(context.Context, string) ([]byte, error) { return busy, nil })
		if err != nil || !bytes.Equal(raw, busy) {
			t.Fatal(string(raw), err)
		}
		if !strings.Contains(mailEnrollmentWorkerGuidance(mailrenewalkit.ErrScheduleBusy), "waiting for the native renewal service") {
			t.Fatal("busy guidance missing")
		}
	})
	for _, change := range []struct{ old, new string }{{"DropInPaths=", "DropInPaths=/owner.conf"}, {"NeedDaemonReload=no", "NeedDaemonReload=yes"}, {"LoadState=loaded", "LoadState=error"}, {"UnitFileState=static", "UnitFileState=masked"}, {"ActiveState=activating", "ActiveState=failed"}, {"FragmentPath=/etc/systemd/system/", "FragmentPath=/usr/lib/systemd/system/"}} {
		t.Run(change.new, func(t *testing.T) {
			changed := bytes.Replace(busy, []byte(change.old), []byte(change.new), 1)
			calls := 0
			raw, err := observeSettledMailEnrollmentUnit(context.Background(), unit, time.Second, time.Millisecond, func(context.Context, string) ([]byte, error) { calls++; return changed, nil })
			if err != nil || calls != 1 || !bytes.Equal(raw, changed) {
				t.Fatal("changed evidence was hidden", calls, err)
			}
		})
	}
	t.Run("cancelled-wait", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, err := observeSettledMailEnrollmentUnit(ctx, unit, time.Second, time.Millisecond, func(context.Context, string) ([]byte, error) { cancel(); return busy, nil })
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("read-failure-is-not-retried", func(t *testing.T) {
		calls := 0
		sentinel := errors.New("unknown read")
		_, err := observeSettledMailEnrollmentUnit(context.Background(), unit, time.Second, time.Millisecond, func(context.Context, string) ([]byte, error) { calls++; return nil, sentinel })
		if err != sentinel || calls != 1 {
			t.Fatal(err, calls)
		}
	})
}
