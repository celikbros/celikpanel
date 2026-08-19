import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

const card = readFileSync(new URL('../src/components/SecurityAuditCard.tsx', import.meta.url), 'utf8');
const settings = readFileSync(new URL('../src/components/Settings.tsx', import.meta.url), 'utf8');
const english = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const turkish = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

async function loadSecurityAuditDecoder() {
    const start = card.indexOf('type AuditStatus');
    const end = card.indexOf('function statusClasses');
    assert.ok(start >= 0 && end > start, 'security audit decoder source boundary is missing');
    const javascript = ts.transpileModule(
        `${card.slice(start, end)}\nexport { isSecurityAuditResponse };`,
        { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } },
    ).outputText;
    const url = `data:text/javascript;base64,${Buffer.from(javascript).toString('base64')}`;
    return import(url);
}

function validSecurityAuditResponse() {
    const unknown = { status: 'unknown', code: 'platform_unsupported' };
    return {
        contract_version: 1,
        generated_at: '2026-08-19T12:00:00Z',
        agent: {
            contract_version: 1,
            capability: 'security_audit_v1',
            build_version: 'v0.1.0-alpha.30',
            build_commit: 'f'.repeat(40),
            generated_at: '2026-08-19T12:00:00Z',
            firewall: {
                engine: unknown,
                default_drop: unknown,
                persistence: unknown,
                tcp_allowlist: [],
                udp_allowlist: [],
            },
            listeners: { check: unknown, findings: [] },
            ssh: {
                check: unknown,
                password_authentication: 'unknown',
                keyboard_interactive_authentication: 'unknown',
                permit_root_login: 'unknown',
                pubkey_authentication: 'unknown',
                hostbased_authentication: 'unknown',
                gssapi_authentication: 'unknown',
            },
            reboot: { check: unknown, required: false },
            signed_update: { check: unknown, enrolled: false },
        },
        tls: {
            certificate: { status: 'unknown', code: 'panel_tls_not_managed' },
            self_signed: { status: 'unknown', code: 'panel_tls_unknown' },
            expiry: { status: 'unknown', code: 'panel_tls_unknown' },
            key_match: { status: 'unknown', code: 'panel_tls_unknown' },
            is_self_signed: false,
        },
    };
}

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

test('security audit decoder accepts canonical empty arrays and rejects JSON null collections', async () => {
    const { isSecurityAuditResponse } = await loadSecurityAuditDecoder();
    const valid = validSecurityAuditResponse();
    assert.equal(isSecurityAuditResponse(valid), true);

    for (const path of ['tcp_allowlist', 'udp_allowlist']) {
        const candidate = structuredClone(valid);
        candidate.agent.firewall[path] = null;
        assert.equal(isSecurityAuditResponse(candidate), false, `${path}: null must fail closed`);
    }
    const nullFindings = structuredClone(valid);
    nullFindings.agent.listeners.findings = null;
    assert.equal(isSecurityAuditResponse(nullFindings), false, 'findings: null must fail closed');
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
