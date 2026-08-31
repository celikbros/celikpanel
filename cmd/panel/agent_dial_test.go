package main

import (
	"context"
	"errors"
	"net/rpc"
	"testing"
	"time"
)

// A fake clock: the wait is bounded in simulated time, so the test proves the
// bound without spending it.
type dialClock struct {
	now    time.Time
	slept  []time.Duration
	sleeps int
}

func (c *dialClock) Now() time.Time { return c.now }

func (c *dialClock) Sleep(d time.Duration) {
	c.slept = append(c.slept, d)
	c.sleeps++
	c.now = c.now.Add(d)
}

func newDialClock() *dialClock {
	return &dialClock{now: time.Unix(1_700_000_000, 0)}
}

// The agent socket appearing a few seconds after the panel starts is the normal
// systemd case — After= orders execution, not readiness. The panel must wait
// through it rather than exiting and being restarted into the same race.
func TestConnectAgentPatientlyWaitsThroughAStartupRace(t *testing.T) {
	clock := newDialClock()
	attempts := 0
	client, waited, err := connectAgentPatiently(
		context.Background(),
		func(context.Context) (*rpc.Client, error) {
			attempts++
			if attempts < 4 {
				return nil, errors.New("dial unix /run/celikpanel/agent.sock: no such file or directory")
			}
			return &rpc.Client{}, nil
		},
		clock.Now, clock.Sleep,
	)
	if err != nil {
		t.Fatalf("a socket that appears on the fourth attempt must connect: %v", err)
	}
	if client == nil {
		t.Fatal("a successful dial must return a client")
	}
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
	if waited <= 0 {
		t.Fatal("a wait that took several attempts must be reported as non-zero")
	}
}

// An agent that is genuinely absent must not hang the unit forever: the panel
// gives up inside the bound and lets systemd decide what happens next.
func TestConnectAgentPatientlyGivesUpInsideTheBound(t *testing.T) {
	clock := newDialClock()
	cause := errors.New("permission denied")
	attempts := 0
	client, waited, err := connectAgentPatiently(
		context.Background(),
		func(context.Context) (*rpc.Client, error) {
			attempts++
			return nil, cause
		},
		clock.Now, clock.Sleep,
	)
	if err == nil {
		t.Fatal("a permanently absent agent must eventually fail")
	}
	if client != nil {
		t.Fatal("a failed dial must not return a client")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("the reported error must be the last dial error, got %v", err)
	}
	if waited > agentDialTotalWait {
		t.Fatalf("waited %s, which exceeds the %s bound", waited, agentDialTotalWait)
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d; giving up on the first attempt is the defect this fixes", attempts)
	}
}

// The pause grows and then stops growing: a slow-starting agent is retried
// often at first and cheaply later, and no pause exceeds the ceiling.
func TestConnectAgentPatientlyBacksOffToACeiling(t *testing.T) {
	clock := newDialClock()
	_, _, err := connectAgentPatiently(
		context.Background(),
		func(context.Context) (*rpc.Client, error) {
			return nil, errors.New("connection refused")
		},
		clock.Now, clock.Sleep,
	)
	if err == nil {
		t.Fatal("expected failure")
	}
	if len(clock.slept) < 3 {
		t.Fatalf("expected several pauses, got %d", len(clock.slept))
	}
	if clock.slept[0] != agentDialFirstPause {
		t.Fatalf("first pause = %s, want %s", clock.slept[0], agentDialFirstPause)
	}
	for i, d := range clock.slept {
		if d > agentDialMaxPause {
			t.Fatalf("pause %d = %s exceeds the ceiling %s", i, d, agentDialMaxPause)
		}
		if i > 0 && d < clock.slept[i-1] {
			t.Fatalf("pause %d shrank from %s to %s", i, clock.slept[i-1], d)
		}
	}
	if last := clock.slept[len(clock.slept)-1]; last != agentDialMaxPause {
		t.Fatalf("the backoff must reach its ceiling, last pause = %s", last)
	}
}

// A cancelled context stops the wait immediately: shutdown must not be held
// hostage by a retry loop.
func TestConnectAgentPatientlyHonoursCancellation(t *testing.T) {
	clock := newDialClock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := connectAgentPatiently(
		ctx,
		func(context.Context) (*rpc.Client, error) {
			return nil, errors.New("still starting")
		},
		clock.Now, clock.Sleep,
	)
	if err == nil {
		t.Fatal("a cancelled wait must fail")
	}
	if clock.sleeps != 0 {
		t.Fatalf("a cancelled wait must not sleep, slept %d times", clock.sleeps)
	}
}

func TestConnectAgentPatientlyRequiresADialer(t *testing.T) {
	if _, _, err := connectAgentPatiently(context.Background(), nil, nil, nil); err == nil {
		t.Fatal("a nil dialer must be refused, not dereferenced")
	}
}
