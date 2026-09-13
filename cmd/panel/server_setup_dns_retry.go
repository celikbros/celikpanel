package main

import (
	"context"
	"encoding/json"
	"time"
)

// A prerequisite wait may be revised by the owner. Claim its exact saved state
// before retrying: the old runner must never admit work after that revision.
// Do not hold the setup mutex across network checks or native DNS publication.
func (p *Panel) claimServerSetupDNSRetry(ctx context.Context, plan serverSetupPlan, execution *serverSetupExecution) error {
	p.serverSetupMu.Lock()
	defer p.serverSetupMu.Unlock()
	before, err := json.Marshal(execution)
	if err != nil {
		return err
	}
	next := *execution
	next.Status = "running"
	encoded, err := json.Marshal(next)
	if err != nil {
		return err
	}
	result, err := p.db.GetDB().ExecContext(ctx, `UPDATE server_setup_executions SET status='running',execution_json=?,updated_at=?
 WHERE id=? AND plan_id=? AND status='waiting' AND execution_json=?
 AND EXISTS (SELECT 1 FROM server_setup_state WHERE id=1 AND revision=? AND status='running')`,
		string(encoded), time.Now().UTC().Format(time.RFC3339Nano), execution.ID, plan.ID, string(before), plan.Revision)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errServerSetupConflict
	}
	*execution = next
	return nil
}
