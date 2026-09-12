package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestServerSetupPublisherCannotChangeDNSDefaultAfterConcurrentRevision(t *testing.T) {
	f := newSecondaryHostingFixture(t)
	f.wait(t)
	if response := f.bind(t, f.id); response.Code != http.StatusAccepted {
		t.Fatal(response.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	baseExchange := remoteDNSExchange
	remoteDNSExchange = func(ctx context.Context, endpoint, path, credential string, request, response any) error {
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-release:
			return baseExchange(ctx, endpoint, path, credential, request, response)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	type outcome struct {
		done bool
		err  error
	}
	finished := make(chan outcome, 1)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		done, err := f.f.panel.runServerSetupDNSPublisher(ctx, f.plan, f.execution.ID)
		finished <- outcome{done: done, err: err}
	}()
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		cancel()
		select {
		case <-workerDone:
		case <-time.After(5 * time.Second):
			t.Error("publisher worker did not stop before fixture cleanup")
		}
		remoteDNSExchange = baseExchange
	})
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("publisher never reached its remote readiness check")
	}

	// The administrator revises while the old worker has only stale execution
	// state and is waiting on network I/O. Revision must be able to finish.
	body, _ := json.Marshal(map[string]any{"execution_id": f.execution.ID, "revision": f.plan.Revision})
	response := httptest.NewRecorder()
	f.f.panel.handleServerSetupRevise(response, serviceOperationAdminRequest(t, http.MethodPost, "/api/v1/setup/revise", string(body), f.f.userID))
	if response.Code != http.StatusOK {
		t.Fatalf("could not revise blocked publisher: %d %s", response.Code, response.Body.String())
	}
	state, err := f.f.panel.loadServerSetup(ctx)
	if err != nil || state.Revision != f.plan.Revision+1 || state.Status != "draft" {
		t.Fatalf("revision did not commit before releasing old worker: %+v %v", state, err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case result := <-finished:
		if result.done || result.err == nil {
			t.Fatalf("superseded worker claimed a DNS default commit: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("publisher did not finish after its proof returned")
	}
	mode, err := f.f.panel.setupDNSManagementMode(ctx)
	if err != nil || mode != setupDNSModeLocal {
		t.Fatalf("superseded publisher changed the DNS default: %q %v", mode, err)
	}
	if id, err := f.f.panel.defaultRemoteDNSConnectionID(ctx); !errors.Is(err, sql.ErrNoRows) || id != "" {
		t.Fatalf("superseded publisher persisted its connector: %q %v", id, err)
	}
	current, err := f.f.panel.latestServerSetupExecution(ctx)
	if err != nil || current == nil || current.ID != f.execution.ID || current.Status != "failed" {
		t.Fatalf("superseded worker changed its terminal execution: %+v %v", current, err)
	}
}
