package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/rpc"
	"sync/atomic"
	"testing"
	"time"
)

func recoveryDialTestPolicy() recoveryAgentDialPolicy {
	return recoveryAgentDialPolicy{attemptWait: 30 * time.Millisecond, firstPause: time.Millisecond, maxPause: 4 * time.Millisecond}
}

func recoveryDialTestClient(t *testing.T) (*rpc.Client, net.Conn) {
	t.Helper()
	clientSide, serverSide := net.Pipe()
	client := rpc.NewClient(clientSide)
	t.Cleanup(func() { _ = client.Close(); _ = serverSide.Close() })
	return client, serverSide
}

func TestConnectAgentRecoveryWaitsWithoutRestartingAndReturnsOneConnection(t *testing.T) {
	client, _ := recoveryDialTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var attempts atomic.Int32
	got, err := connectAgentPreservingRecovery(ctx, nil, func(context.Context) (*rpc.Client, error) {
		if attempts.Add(1) < 5 {
			return nil, errors.New("socket unavailable")
		}
		return client, nil
	}, recoveryDialTestPolicy())
	if err != nil || got != client || attempts.Load() != 5 {
		t.Fatalf("client=%p err=%v attempts=%d", got, err, attempts.Load())
	}
}

func TestConnectAgentRecoveryMissingAgentRemainsBoundedByOwnerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	got, err := connectAgentPreservingRecovery(ctx, nil, func(context.Context) (*rpc.Client, error) {
		if attempts.Add(1) == 12 {
			cancel()
		}
		return nil, errors.New("socket absent")
	}, recoveryDialTestPolicy())
	if got != nil || !errors.Is(err, context.Canceled) || attempts.Load() != 12 {
		t.Fatalf("client=%p err=%v attempts=%d", got, err, attempts.Load())
	}
}

func TestConnectAgentRecoveryTimesOutEachAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	_, err := connectAgentPreservingRecovery(ctx, nil, func(attempt context.Context) (*rpc.Client, error) {
		deadline, ok := attempt.Deadline()
		if !ok || time.Until(deadline) > 35*time.Millisecond {
			t.Error("dial attempt lacks its bounded deadline")
		}
		<-attempt.Done()
		if attempts.Add(1) == 2 {
			cancel()
		}
		return nil, attempt.Err()
	}, recoveryDialTestPolicy())
	if !errors.Is(err, context.Canceled) || attempts.Load() != 2 {
		t.Fatalf("err=%v attempts=%d", err, attempts.Load())
	}
}

func TestConnectAgentRecoveryClosesLateConnectionAfterCancellation(t *testing.T) {
	client, peer := recoveryDialTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := connectAgentPreservingRecovery(ctx, nil, func(attempt context.Context) (*rpc.Client, error) {
		cancel()
		<-attempt.Done()
		return client, nil
	}, recoveryDialTestPolicy())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, readErr := peer.Read(make([]byte, 1)); !errors.Is(readErr, io.EOF) {
		t.Fatalf("cancelled attempt did not close its connected client: %v", readErr)
	}
}

func TestConnectAgentRecoveryStopsWhenListenerFails(t *testing.T) {
	failure := errors.New("listener closed unexpectedly")
	serveResult := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := connectAgentPreservingRecovery(ctx, serveResult, func(attempt context.Context) (*rpc.Client, error) {
		serveResult <- failure
		<-attempt.Done()
		return nil, attempt.Err()
	}, recoveryDialTestPolicy())
	if !errors.Is(err, failure) {
		t.Fatalf("listener failure lost: %v", err)
	}
}

func TestConnectAgentRecoveryDoesNotAcceptConnectionAfterListenerStopped(t *testing.T) {
	client, _ := recoveryDialTestClient(t)
	serveResult := make(chan error, 1)
	_, err := connectAgentPreservingRecovery(context.Background(), serveResult, func(context.Context) (*rpc.Client, error) {
		serveResult <- http.ErrServerClosed
		return client, nil
	}, recoveryDialTestPolicy())
	if err == nil {
		t.Fatal("connection admitted without a recovery listener")
	}
}

func TestConnectAgentRecoveryNilSuccessIsNotAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var attempts atomic.Int32
	got, err := connectAgentPreservingRecovery(ctx, nil, func(context.Context) (*rpc.Client, error) {
		if attempts.Add(1) == 3 {
			cancel()
		}
		return nil, nil
	}, recoveryDialTestPolicy())
	if got != nil || !errors.Is(err, context.Canceled) || attempts.Load() != 3 {
		t.Fatalf("client=%p err=%v attempts=%d", got, err, attempts.Load())
	}
}

func TestConnectAgentRecoveryInvalidConfigurationAndCancelledContextNeverDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := connectAgentPreservingRecovery(ctx, nil, func(context.Context) (*rpc.Client, error) { t.Fatal("cancelled request dialled"); return nil, nil }, recoveryDialTestPolicy())
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = connectAgentPreservingRecovery(context.Background(), nil, nil, recoveryDialTestPolicy()); err == nil {
		t.Fatal("nil dialer accepted")
	}
	if _, err = connectAgentWithRecovery(nil, dialAgentOnce); err == nil {
		t.Fatal("missing listener accepted")
	}
}
