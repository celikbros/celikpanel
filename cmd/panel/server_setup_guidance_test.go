package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerSetupGuidancePersistsWithoutCompletingOrChangingDraft(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	ctx := context.Background()
	state, err := f.panel.loadServerSetup(ctx)
	if err != nil || state.Guidance != "undecided" {
		t.Fatalf("initial: %+v %v", state, err)
	}
	original := state
	state, err = f.panel.saveServerSetupGuidance(ctx, state.Revision, "manual")
	if err != nil {
		t.Fatal(err)
	}
	// Read again independently, as a new session does. It is not a browser flag.
	state, err = f.panel.loadServerSetup(ctx)
	if err != nil || state.Guidance != "manual" || state.Revision != original.Revision+1 || state.Status != original.Status || state.Required != original.Required || state.Draft != original.Draft || state.CompletedAt != original.CompletedAt {
		t.Fatalf("manual choice changed setup or was not persisted: %+v %v", state, err)
	}
	if _, err := f.panel.saveServerSetupGuidance(ctx, original.Revision, "guided"); !errors.Is(err, errServerSetupConflict) {
		t.Fatalf("stale choice: %v", err)
	}
	if _, err := f.panel.saveServerSetupDraft(ctx, original.Revision, defaultServerSetupDraft()); !errors.Is(err, errServerSetupConflict) {
		t.Fatalf("old review still valid: %v", err)
	}
	state, err = f.panel.saveServerSetupGuidance(ctx, state.Revision, "guided")
	if err != nil || state.Guidance != "guided" || state.Status != original.Status {
		t.Fatalf("reopen: %+v %v", state, err)
	}
	if f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
		t.Fatal("preference performed a host mutation")
	}
}

func TestServerSetupGuidanceCannotDismissActiveOrCompleteFreshSetup(t *testing.T) {
	for _, status := range []string{"new", "running", "waiting"} {
		t.Run(status, func(t *testing.T) {
			f := newServiceOperationTestFixture(t)
			ctx := context.Background()
			if _, err := f.database.GetDB().Exec(`UPDATE server_setup_state SET origin='fresh',status=? WHERE id=1`, status); err != nil {
				t.Fatal(err)
			}
			state, err := f.panel.saveServerSetupGuidance(ctx, 0, "manual")
			if status != "new" {
				if !errors.Is(err, errServerSetupConflict) {
					t.Fatalf("active setup dismissed: %+v %v", state, err)
				}
				return
			}
			if err != nil || !state.Required || state.Status != "new" {
				t.Fatalf("manual became ready: %+v %v", state, err)
			}
			f.panel.serverSetupProbe = func(context.Context, serverSetupDraft) ([]serverSetupCheck, error) {
				return []serverSetupCheck{{ID: "firewall", State: "action_required"}}, nil
			}
			if err := f.panel.completeServerSetup(ctx, state.Revision); err == nil {
				t.Fatal("manual bypassed readiness")
			}
		})
	}
}

func TestServerSetupGuidanceAuthorizationAndValidation(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	for _, role := range []string{"reseller", "customer", "additional_user"} {
		r := httptest.NewRequest(http.MethodPut, serverSetupPath+"/guidance", strings.NewReader(`{"revision":0,"guidance":"manual"}`))
		r = r.WithContext(context.WithValue(r.Context(), callerKey, &Caller{Role: role}))
		w := httptest.NewRecorder()
		f.panel.handleServerSetupGuidance(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("role %s: %d", role, w.Code)
		}
	}
	for _, body := range []string{`{"revision":0,"guidance":"ready"}`, `{"revision":0,"guidance":"manual","skip_security":true}`} {
		w := httptest.NewRecorder()
		f.panel.handleServerSetupGuidance(w, serviceOperationAdminRequest(t, http.MethodPut, serverSetupPath+"/guidance", body, f.userID))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid preference: %d %s", w.Code, w.Body.String())
		}
	}
}
