package main

import (
	"context"
	"encoding/json"
)

// Remote proof can take time. Recheck execution ownership in the transaction
// that writes both defaults, so a concurrent revision cannot be followed by an
// old runner committing its obsolete publisher. No host mutation is involved.
func (p *Panel) commitServerSetupDNSPublisherDefault(ctx context.Context, plan serverSetupPlan, executionID, connectionID string) error {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT e.execution_json FROM server_setup_executions e
		JOIN server_setup_state s ON s.id=1 AND s.revision=?
		WHERE e.id=? AND e.plan_id=? AND e.status IN ('running','waiting')
		AND EXISTS (SELECT 1 FROM remote_dns_connections WHERE id=? AND endpoint=? AND status='ready')
		AND EXISTS (SELECT 1 FROM panel_settings WHERE key=?)`, plan.Revision, executionID, plan.ID,
		connectionID, plan.Draft.DNSPublisherEndpoint, serverSetupDNSPublisherKey(executionID)).Scan(&raw)
	if err != nil {
		return errServerSetupConflict
	}
	var execution serverSetupExecution
	if json.Unmarshal([]byte(raw), &execution) != nil || validateServerSetupExecution(plan, execution) != nil {
		return errServerSetupConflict
	}
	// An already authorized gate may have paused for license renewal. Its step
	// order remains binding regardless of that temporary display phase.
	execution.Status, execution.Phase = "waiting", "dns_publisher"
	if !serverSetupAtPublisherGate(plan, execution) {
		return errServerSetupConflict
	}
	for key, value := range map[string]string{settingRemoteDNSConnection: connectionID, settingSetupDNSMode: setupDNSModeExisting} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO panel_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}
