package mailrenewalkit

import (
	"bytes"
	"strings"
	"testing"
)

func transitionKit(t *testing.T, binary string) ([]byte, map[string][]byte) {
	t.Helper()
	m, f, e := Payload([]byte(binary))
	if e != nil {
		t.Fatal(e)
	}
	r, e := Encode(m)
	if e != nil {
		t.Fatal(e)
	}
	return r, f
}
func TestMailTransitionBootstrapAndOwnerSchedule(t *testing.T) {
	next, nf := transitionKit(t, "new helper")
	old, of := transitionKit(t, "previous helper")
	for _, legacy := range []bool{false, true} {
		before := map[string][]byte{}
		if legacy {
			before[HookName] = LegacyHook()
		}
		tr, e := NewTransition(strings.Repeat("a", 32), before, TimerState{"absent", "inactive"}, nil, nil, next, nf)
		if e != nil {
			t.Fatal(e)
		}
		if tr.TimerAfter != (TimerState{"enabled", "active"}) {
			t.Fatal("initial schedule missing")
		}
		raw, e := EncodeTransition(tr, nil, nil, next, nf)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = DecodeTransition(raw, nil, nil, next, nf); e != nil {
			t.Fatal(e)
		}
		for _, name := range []string{HookName, ServiceName, TimerName} {
			content, present := before[name]
			if side, e := tr.FileSide(name, content, present); e != nil || side != FileBefore {
				t.Fatal("before image lost", name, side, e)
			}
			if side, e := tr.FileSide(name, nf[name], true); e != nil || side != FileAfter {
				t.Fatal("target image lost", name, side, e)
			}
		}
	}
	for _, state := range []TimerState{{"enabled", "active"}, {"enabled", "inactive"}, {"disabled", "active"}, {"disabled", "inactive"}} {
		tr, e := NewTransition(strings.Repeat("b", 32), nativeFiles(of), state, old, of, next, nf)
		if e != nil {
			t.Fatal(e)
		}
		if tr.TimerAfter != state {
			t.Fatal("owner timer state overwritten")
		}
		// All eight file mixtures are meaningful interrupted transitions. Shared
		// timer bytes are classified as either, not invented progress evidence.
		for mask := 0; mask < 8; mask++ {
			for index, name := range []string{HookName, ServiceName, TimerName} {
				raw := of[name]
				want := FileBefore
				if mask&(1<<index) != 0 {
					raw = nf[name]
					want = FileAfter
				}
				if bytes.Equal(of[name], nf[name]) {
					want = FileEither
				}
				got, e := tr.FileSide(name, raw, true)
				if e != nil || got != want {
					t.Fatal("mixed publication refused", mask, name, got, e)
				}
			}
		}
		if _, e = tr.FileSide(HookName, append(bytes.Clone(nf[HookName]), []byte("# owner\n")...), true); e == nil {
			t.Fatal("owner edit accepted")
		}
		if _, e = tr.FileSide(HookName, nil, false); e == nil {
			t.Fatal("unrecorded absence accepted")
		}
	}
}
func TestMailTransitionRejectsUnprovenAuthorityAndMaterial(t *testing.T) {
	next, nf := transitionKit(t, "next")
	old, of := transitionKit(t, "old")
	for _, before := range []map[string][]byte{{HookName: []byte("owner hook")}, {ServiceName: of[ServiceName]}, {HookName: LegacyHook(), TimerName: of[TimerName]}, {"foreign": LegacyHook()}} {
		if _, e := NewTransition(strings.Repeat("a", 32), before, TimerState{"absent", "inactive"}, nil, nil, next, nf); e == nil {
			t.Fatal("unproven bootstrap adopted")
		}
	}
	for _, state := range []TimerState{{"unknown", "inactive"}, {"enabled", "unknown"}, {"masked", "inactive"}, {"absent", "inactive"}} {
		if _, e := NewTransition(strings.Repeat("a", 32), nativeFiles(of), state, old, of, next, nf); e == nil {
			t.Fatal("unknown owner schedule accepted")
		}
	}
	if _, e := NewTransition("wrong", map[string][]byte{}, TimerState{"absent", "inactive"}, nil, nil, next, nf); e == nil {
		t.Fatal("invalid identity accepted")
	}
	if _, e := NewTransition(strings.Repeat("a", 32), nativeFiles(of), TimerState{"enabled", "active"}, old, of, old, of); e == nil {
		t.Fatal("same-generation transition invented")
	}
	if _, e := NewTransition(strings.Repeat("a", 32), nativeFiles(of), TimerState{"enabled", "active"}, old, nf, next, nf); e == nil {
		t.Fatal("mixed prior helper accepted")
	}
	tr, e := NewTransition(strings.Repeat("a", 32), nativeFiles(of), TimerState{"disabled", "inactive"}, old, of, next, nf)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := EncodeTransition(tr, old, of, next, nf)
	if e != nil {
		t.Fatal(e)
	}
	for _, edit := range []func(*Transition){func(t *Transition) { t.Schema = "future" }, func(t *Transition) { t.Before = nil }, func(t *Transition) { t.After[HookName] = LegacyHook() }, func(t *Transition) { t.TimerAfter = TimerState{"enabled", "active"} }, func(t *Transition) { t.Previous = "" }, func(t *Transition) { t.OperationID = "" }} {
		copy, e := DecodeTransition(raw, old, of, next, nf)
		if e != nil {
			t.Fatal(e)
		}
		edit(&copy)
		if _, e := EncodeTransition(copy, old, of, next, nf); e == nil {
			t.Fatal("changed accepted contract encoded")
		}
	}
	for _, bad := range [][]byte{nil, bytes.Replace(raw, []byte(`"schema":`), []byte(`"extra":0,"schema":`), 1), bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"wrong","schema":`), 1), append(bytes.Clone(raw), '\n'), bytes.Repeat([]byte("x"), MaxTransitionSize+1)} {
		if _, e := DecodeTransition(bad, old, of, next, nf); e == nil {
			t.Fatal("noncanonical or unknown journal accepted")
		}
	}
	if _, e := tr.FileSide("/etc/owner", nil, false); e == nil {
		t.Fatal("arbitrary native path accepted")
	}
	if _, e := tr.FileSide(ServiceName, []byte("partial read"), false); e == nil {
		t.Fatal("unreadable partial bytes became absence")
	}
}
