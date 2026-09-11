import assert from 'node:assert/strict';
import test from 'node:test';
import { systemUpdateFailureMessage } from '../src/lib/systemUpdateFailure.ts';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';

test('verified package conflict has actionable localized guidance', () => {
    const raw = 'reviewed updater failed: exit status 1: !! CELIKPANEL_UPDATE_FAILURE code=package_manager_busy state=unchanged reason=agent/package state changed before freeze detail=idle: the host package manager is active';
    for (const locale of [en, tr]) {
        assert.equal(systemUpdateFailureMessage(raw, key => locale[key]), locale['panelUpdate.packageManagerBusy']);
    }
});

test('ambiguous, historical, and recovery-required failures retain their evidence', () => {
    for (const raw of [
        'the host package manager is active',
        'reviewed updater failed: exit status 1: !! Kurulu baytlar deimeden nce gncelleme durdu.',
        '!! CELIKPANEL_UPDATE_FAILURE code=package_manager_busy state=recovery_required reason=abort failed',
        '!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=unchanged reason=ledger is invalid',
        '!! CELIKPANEL_UPDATE_FAILURE code=package_manager_busy state=unchangedX reason=invalid',
    ]) {
        assert.equal(systemUpdateFailureMessage(raw, () => assert.fail('must not infer safe retry')), raw);
    }
});
