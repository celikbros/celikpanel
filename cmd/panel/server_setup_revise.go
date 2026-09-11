package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// Release only a plan whose host-changing steps have all finished. Revision
// does not undo installed services or interrupt a committed operation.
func (p *Panel) handleServerSetupRevise(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !requireServerSetupAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Revision    int    `json:"revision"`
		ExecutionID string `json:"execution_id"`
	}
	if decodeServiceOperationJSON(w, r, &request) != nil || !validServiceOperationID(request.ExecutionID) {
		writeClientError(w, http.StatusBadRequest, "invalid setup revision request")
		return
	}
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	conflict := func() {
		writeCodedError(w, http.StatusConflict, "setup_conflict", "The current setup must finish reconciling before its plan can change.", "/setup")
	}
	state, err := p.loadServerSetup(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	if state.Revision != request.Revision || state.Status == "ready" {
		conflict()
		return
	}
	var raw string
	if err := p.db.GetDB().QueryRowContext(r.Context(), `SELECT execution_json FROM server_setup_executions WHERE id=? AND status='waiting'`, request.ExecutionID).Scan(&raw); err != nil {
		conflict()
		return
	}
	var execution serverSetupExecution
	if json.Unmarshal([]byte(raw), &execution) != nil || execution.ID != request.ExecutionID || execution.Status != "waiting" || execution.Phase != "verification" {
		conflict()
		return
	}
	plan, err := p.loadServerSetupPlan(r.Context(), execution.PlanID)
	if err != nil || plan.Revision != state.Revision || validateServerSetupExecution(plan, execution) != nil || len(execution.Steps) == 0 {
		conflict()
		return
	}
	for _, step := range execution.Steps {
		if step.Status != "succeeded" {
			conflict()
			return
		}
	}
	job, err := p.statusAgentMutation(r.Context(), "")
	if err != nil || (job != nil && agentMutationActive(job.Status)) {
		conflict()
		return
	}
	execution.Status = "failed"
	execution.Error = &serviceOperationError{Code: "server_setup_plan_revised", Message: "The administrator reopened the plan. Completed host changes remain in place."}
	encoded, err := json.Marshal(execution)
	if err != nil {
		writeServerError(w, err)
		return
	}
	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE server_setup_executions SET status='failed',execution_json=?,updated_at=? WHERE id=? AND status='waiting' AND execution_json=? AND NOT EXISTS (SELECT 1 FROM service_operations WHERE status IN ('queued','running'))`, string(encoded), time.Now().UTC().Format(time.RFC3339Nano), request.ExecutionID, raw)
	if err != nil {
		writeServerError(w, err)
		return
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		conflict()
		return
	}
	result, err = tx.ExecContext(r.Context(), `UPDATE server_setup_state SET status='draft',revision=revision+1,completed_at='',updated_at=datetime('now') WHERE id=1 AND revision=? AND status!='ready'`, request.Revision)
	if err != nil {
		writeServerError(w, err)
		return
	}
	count, err = result.RowsAffected()
	if err != nil || count != 1 {
		conflict()
		return
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "server.setup.revise", "", 0)
	state, err = p.loadServerSetup(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(state)
}
