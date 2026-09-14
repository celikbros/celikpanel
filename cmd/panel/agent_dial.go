package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/rpc"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	agentDialFirstPause  = 500 * time.Millisecond
	agentDialMaxPause    = 8 * time.Second
	agentDialAttemptWait = 10 * time.Second
)

type recoveryAgentDialPolicy struct {
	attemptWait time.Duration
	firstPause  time.Duration
	maxPause    time.Duration
}

var defaultRecoveryAgentDialPolicy = recoveryAgentDialPolicy{
	attemptWait: agentDialAttemptWait,
	firstPause:  agentDialFirstPause,
	maxPause:    agentDialMaxPause,
}

type recoveryAgentDialResult struct {
	client *rpc.Client
	err    error
}

// Connect only after HTTPS and its restricted recovery handler are serving.
// Every attempt is bounded; failure retries observation, never a host mutation.
// A missing Agent does not restart the panel or discard authenticated access.
// HTTPS hazırken sınırlı bağlantı denemeleri yapılır; Agent yokluğu paneli
// yeniden başlatmaz ve bu döngü sunucuda bir değişiklik işlemi yapmaz.
func connectAgentWithRecovery(running *runningPanelHTTPServer, dial func(context.Context) (*rpc.Client, error)) (*rpc.Client, error) {
	if running == nil || running.server == nil || running.serveResult == nil {
		return nil, errors.New("recovery listener is not running")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return connectAgentPreservingRecovery(ctx, running.serveResult, dial, defaultRecoveryAgentDialPolicy)
}

func startupListenerError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return errors.New("recovery listener stopped during Agent connection")
	}
	return fmt.Errorf("recovery listener failed during Agent connection: %w", err)
}

// Dialers must honour their context, as the production Unix connector does.
// Drain a cancelled attempt before returning so no late client is leaked and
// normal startup can never proceed after cancellation or listener failure.
func connectAgentPreservingRecovery(
	ctx context.Context,
	serveResult <-chan error,
	dial func(context.Context) (*rpc.Client, error),
	policy recoveryAgentDialPolicy,
) (*rpc.Client, error) {
	if ctx == nil || dial == nil || policy.attemptWait <= 0 || policy.firstPause <= 0 || policy.maxPause < policy.firstPause {
		return nil, errors.New("invalid recovery Agent connection configuration")
	}
	pause := policy.firstPause
	announced := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, policy.attemptWait)
		result := make(chan recoveryAgentDialResult, 1)
		go func() {
			client, err := dial(attemptCtx)
			result <- recoveryAgentDialResult{client, err}
		}()
		var attempt recoveryAgentDialResult
		select {
		case attempt = <-result:
			cancel()
		case <-ctx.Done():
			cancel()
			attempt = <-result
			if attempt.client != nil {
				_ = attempt.client.Close()
			}
			return nil, ctx.Err()
		case err := <-serveResult:
			cancel()
			attempt = <-result
			if attempt.client != nil {
				_ = attempt.client.Close()
			}
			return nil, startupListenerError(err)
		}
		if err := ctx.Err(); err != nil {
			if attempt.client != nil {
				_ = attempt.client.Close()
			}
			return nil, err
		}
		select {
		case err := <-serveResult:
			if attempt.client != nil {
				_ = attempt.client.Close()
			}
			return nil, startupListenerError(err)
		default:
		}
		if attempt.err == nil && attempt.client != nil {
			return attempt.client, nil
		}
		if attempt.client != nil {
			_ = attempt.client.Close()
		}
		if !announced {
			log.Println("Agent is unavailable; authenticated read-only recovery remains available while connection is retried")
			announced = true
		}
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case err := <-serveResult:
			timer.Stop()
			return nil, startupListenerError(err)
		case <-timer.C:
		}
		if pause < policy.maxPause {
			if pause > policy.maxPause/2 {
				pause = policy.maxPause
			} else {
				pause *= 2
			}
		}
	}
}

func dialAgentOnce(ctx context.Context) (*rpc.Client, error) {
	return transport.ConnectAgentContext(ctx)
}
