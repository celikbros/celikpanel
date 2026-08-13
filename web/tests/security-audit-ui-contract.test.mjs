import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const card = readFileSync(new URL('../src/components/SecurityAuditCard.tsx', import.meta.url), 'utf8');
const settings = readFileSync(new URL('../src/components/Settings.tsx', import.meta.url), 'utf8');
const english = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const turkish = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

test('security audit UI is an admin Settings section with a fixed read-only GET', () => {
    assert.match(settings, /role === 'admin'/);
    assert.match(settings, /id: 'security' as const/);
    assert.match(settings, /<SecurityAuditCard \/>/);
    assert.match(card, /fetch\('\/api\/v1\/security\/audit'/);
    assert.match(card, /method: 'GET'/);
    assert.match(card, /cache: 'no-store'/);
    assert.doesNotMatch(card, /method: '(?:POST|PUT|PATCH|DELETE)'/);
    assert.doesNotMatch(card, /auto.?fix|\/api\/v1\/security\/(?:fix|apply)|child_process|exec(?:File)?\s*\(|spawn\s*\(/i);
});

test('security audit UI preserves distinct listener findings and closed statuses', () => {
    assert.match(card, /listener_not_allowed/);
    assert.match(card, /allowed_no_listener/);
    assert.match(card, /'pass' \| 'warning' \| 'fail' \| 'unknown'/);
    assert.match(card, /security_audit_v1/);
    assert.match(card, /findings\.length > 512/);
	assert.match(card, /CODE_STATUSES\[check\.code\] === check\.status/);
	assert.match(card, /panel_tls_chain_unverified: 'unknown'/);
	assert.match(card, /CHECK_CODES/);
	assert.match(card, /ssh_policy_live_unverified/);
	assert.match(card, /signed_update_identity_unverified/);
	assert.doesNotMatch(card, /firewallPersistence: new Set\(\['firewall_persistence_ready'/);
	assert.doesNotMatch(card, /ssh: new Set\(\['ssh_key_only'/);
	assert.doesNotMatch(card, /\^\[a-z\]\[a-z0-9_\]/);
});

test('security audit labels ship in both English and Turkish', () => {
    for (const source of [english, turkish]) {
        assert.match(source, /'settings\.section\.security'/);
        assert.match(source, /'securityAudit\.title'/);
        assert.match(source, /'securityAudit\.code\.listenerNotAllowed'/);
        assert.match(source, /'securityAudit\.code\.allowedNoListener'/);
        assert.match(source, /'securityAudit\.code\.signedUpdateIdentityUnverified'/);
        assert.match(source, /'securityAudit\.code\.sshPolicyLiveUnverified'/);
    }
});
