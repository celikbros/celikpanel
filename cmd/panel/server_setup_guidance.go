package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// Guidance is an administrator's navigation preference, never readiness evidence.
// Tercih yalnızca yönlendirmeyi belirler; kurulumu tamamlamaz.
func (p *Panel) saveServerSetupGuidance(ctx context.Context, revision int, guidance string) (serverSetupState, error) {
	if !stringIn(guidance, "guided", "manual") {
		return serverSetupState{}, errors.New("invalid setup guidance")
	}
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return serverSetupState{}, err
	}
	defer tx.Rollback()
	// Changing preference invalidates outstanding reviews and cannot hide an
	// accepted execution, including a waiting or unresolved child operation.
	result, err := tx.ExecContext(ctx, `UPDATE server_setup_state SET revision=revision+1,updated_at=datetime('now')
        WHERE id=1 AND revision=? AND status NOT IN ('running','waiting')
        AND NOT EXISTS (SELECT 1 FROM server_setup_executions WHERE status IN ('running','waiting'))
        AND NOT EXISTS (SELECT 1 FROM service_operations WHERE status IN ('queued','running'))`, revision)
	if err != nil {
		return serverSetupState{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return serverSetupState{}, err
	}
	if count != 1 {
		return serverSetupState{}, errServerSetupConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO panel_settings(key,value,updated_at) VALUES('server_setup_guidance',?,datetime('now'))
        ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, guidance); err != nil {
		return serverSetupState{}, err
	}
	if err = tx.Commit(); err != nil {
		return serverSetupState{}, err
	}
	return p.loadServerSetup(ctx)
}

func (p *Panel) handleServerSetupGuidance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !requireServerSetupAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Revision int    `json:"revision"`
		Guidance string `json:"guidance"`
	}
	if err := decodeServiceOperationJSON(w, r, &request); err != nil || !stringIn(request.Guidance, "guided", "manual") {
		writeClientError(w, http.StatusBadRequest, "invalid setup guidance")
		return
	}
	state, err := p.saveServerSetupGuidance(r.Context(), request.Revision, request.Guidance)
	if errors.Is(err, errServerSetupConflict) {
		writeCodedError(w, http.StatusConflict, "setup_conflict", err.Error(), "/setup")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "server.setup.guidance", request.Guidance, 0)
	json.NewEncoder(w).Encode(state)
}
