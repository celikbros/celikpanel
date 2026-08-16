import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

const card = readFileSync(
  new URL('../src/components/DNSEngineCard.tsx', import.meta.url),
  'utf8',
);
const contract = readFileSync(
  new URL('../src/lib/dnsEngineContract.ts', import.meta.url),
  'utf8',
);
const settings = readFileSync(
  new URL('../src/components/DNSServerSettings.tsx', import.meta.url),
  'utf8',
);
const copy = readFileSync(new URL('../src/i18n/dnsEngine.ts', import.meta.url), 'utf8');

async function loadContractRuntime() {
  const javascript = ts.transpileModule(contract, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const url = `data:text/javascript;base64,${Buffer.from(javascript).toString('base64')}`;
  return import(url);
}

function readySnapshot(overrides = {}) {
  return {
    revision: 4,
    engine_epoch: 2,
    active_engine: 'pdns',
    state: 'ready',
    topology: 'standalone',
    dnssec_zone_count: 0,
    zone_count: 1,
    pending_zone_count: 0,
    engines: [
      { id: 'pdns', installed: true, running: true, managed: true, status: 'active' },
      { id: 'bind', installed: true, running: false, managed: true, status: 'installed_standby' },
    ],
    ...overrides,
  };
}

test('DNS engine state decoder fails closed on malformed or contradictory state', () => {
  assert.match(contract, /DNS_ENGINE_IDS = \['pdns', 'bind'\] as const/);
  assert.match(contract, /'unconfigured',[\s\S]*'ready',[\s\S]*'unmanaged',[\s\S]*'conflict',[\s\S]*'switching',[\s\S]*'degraded'/);
  assert.match(contract, /value\.engines\.length !== DNS_ENGINE_IDS\.length/);
  assert.match(contract, /new Set\(engines\.map\(\(entry\) => entry\.id\)\)\.size !== DNS_ENGINE_IDS\.length/);
  assert.match(contract, /value\.pending_zone_count > value\.zone_count/);
  assert.match(contract, /impacts === null \|\| impacts\.length === 0/);
  assert.match(contract, /value\.state === 'switching' && typeof value\.operation_id !== 'string'/);
  assert.match(contract, /return null;/);
  assert.match(card, /const decoded = decodeDNSEngineSnapshot\(payload\)/);
  assert.match(card, /if \(decoded === null\)[\s\S]*onSnapshotChange\?\.\(null\)/);
});

test('DNS engine decoder rejects impossible authority tuples', async () => {
  const { decodeDNSEngineSnapshot } = await loadContractRuntime();
  assert.ok(decodeDNSEngineSnapshot(readySnapshot()));
  assert.equal(decodeDNSEngineSnapshot(readySnapshot({ engine_epoch: 0 })), null);
  assert.equal(decodeDNSEngineSnapshot(readySnapshot({ active_engine: null })), null);
  assert.equal(decodeDNSEngineSnapshot(readySnapshot({ operation_id: 'a'.repeat(32) })), null);
  assert.equal(decodeDNSEngineSnapshot(readySnapshot({ dnssec_zone_count: 2 })), null);
	assert.equal(decodeDNSEngineSnapshot(readySnapshot({ pair_role: 'primary' })), null);
	assert.equal(decodeDNSEngineSnapshot(readySnapshot({
		active_engine: 'bind',
		topology: 'paired',
		engines: [
			{ id: 'pdns', installed: true, running: false, managed: true, status: 'installed_standby' },
			{ id: 'bind', installed: true, running: true, managed: true, status: 'active' },
		],
	})), null);
	assert.ok(decodeDNSEngineSnapshot(readySnapshot({
		active_engine: 'bind',
		topology: 'paired',
		pair_role: 'secondary',
		engines: [
			{ id: 'pdns', installed: true, running: false, managed: true, status: 'installed_standby' },
			{ id: 'bind', installed: true, running: true, managed: true, status: 'active' },
		],
	})));
  assert.equal(decodeDNSEngineSnapshot(readySnapshot({
    engines: [
      { id: 'pdns', installed: true, running: true, managed: true, status: 'active' },
      { id: 'bind', installed: true, running: false, managed: false, status: 'installed_standby' },
    ],
  })), null);
  assert.equal(decodeDNSEngineSnapshot(readySnapshot({
    engines: [
      { id: 'pdns', installed: true, running: true, managed: true, status: 'active' },
      { id: 'bind', installed: false, running: false, managed: true, status: 'available' },
    ],
  })), null);
});

test('DNS engine preview token and counts mirror the commit contract', async () => {
  const { decodeDNSEngineSwitchPreview } = await loadContractRuntime();
  const preview = {
    preview_token: 'b'.repeat(32),
    source_engine: 'pdns',
    target_engine: 'bind',
    expected_revision: 4,
    action: 'switch',
    topology: 'standalone',
    zone_count: 1,
    pending_zone_count: 0,
    dnssec_zone_count: 0,
    estimated_downtime_seconds: 15,
    requires_downtime_acknowledgement: true,
    blockers: [],
    impacts: ['validate_target'],
  };
  assert.ok(decodeDNSEngineSwitchPreview(preview, 'pdns', 'bind', 4));
  assert.equal(decodeDNSEngineSwitchPreview({ ...preview, preview_token: 'A'.repeat(32) }, 'pdns', 'bind', 4), null);
  assert.equal(decodeDNSEngineSwitchPreview({ ...preview, preview_token: 'b'.repeat(33) }, 'pdns', 'bind', 4), null);
  assert.equal(decodeDNSEngineSwitchPreview({ ...preview, dnssec_zone_count: 2 }, 'pdns', 'bind', 4), null);
  assert.equal(decodeDNSEngineSwitchPreview({ ...preview, requires_downtime_acknowledgement: false }, 'pdns', 'bind', 4), null);
  assert.equal(decodeDNSEngineSwitchPreview({ ...preview, estimated_downtime_seconds: 0 }, 'pdns', 'bind', 4), null);
  assert.equal(decodeDNSEngineSwitchPreview({
    ...preview,
    source_engine: null,
    action: 'install',
    requires_downtime_acknowledgement: false,
  }, null, 'bind', 4), null);
  assert.ok(decodeDNSEngineSwitchPreview({
    ...preview,
    source_engine: null,
    action: 'install',
    estimated_downtime_seconds: 0,
    requires_downtime_acknowledgement: false,
  }, null, 'bind', 4));
});

test('first DNS engine click requests a read-only preview and cannot mutate', () => {
  const previewStart = card.indexOf('const requestPreview = async');
  const previewEnd = card.indexOf('\n    const commitSwitch = async', previewStart);
  assert.ok(previewStart >= 0 && previewEnd > previewStart);
  const previewBody = card.slice(previewStart, previewEnd);
  assert.match(previewBody, /fetch\('\/api\/v1\/dns\/engine\/switch\/preview'/);
  assert.match(previewBody, /target_engine: target/);
  assert.match(previewBody, /expected_source: base\.active_engine/);
  assert.match(previewBody, /expected_revision: base\.revision/);
  assert.doesNotMatch(previewBody, /fetch\('\/api\/v1\/dns\/engine\/switch'/);
  assert.match(card, /onClick=\{\(\) => void requestPreview\(id\)\}/);
});

test('authoritative switch is isolated behind verified preview and explicit confirmation', () => {
  const commitStart = card.indexOf('const commitSwitch = async');
  const commitEnd = card.indexOf('\n    return (', commitStart);
  assert.ok(commitStart >= 0 && commitEnd > commitStart);
  const commitBody = card.slice(commitStart, commitEnd);
  assert.match(commitBody, /preview\.blockers\.length > 0/);
  assert.match(commitBody, /preview\.requires_downtime_acknowledgement && !current\.acknowledged/);
  assert.match(commitBody, /fetch\('\/api\/v1\/dns\/engine\/switch'/);
  assert.match(commitBody, /request_id: requestID/);
  assert.match(commitBody, /const requestID = current\.requestID/);
  assert.doesNotMatch(commitBody, /createRequestID\(\)/);
  assert.match(commitBody, /target_engine: current\.target/);
  assert.match(commitBody, /expected_source: current\.base\.active_engine/);
  assert.match(commitBody, /expected_revision: current\.base\.revision/);
  assert.match(commitBody, /preview_token: preview\.preview_token/);
  assert.match(commitBody, /downtime_acknowledged: current\.acknowledged/);
});

test('review dialog is accessible and uses a meaningful acknowledgement, not a typed phrase', () => {
  assert.match(card, /role="dialog"/);
  assert.match(card, /aria-modal="true"/);
  assert.match(card, /aria-labelledby="dns-engine-review-title"/);
  assert.match(card, /aria-describedby="dns-engine-review-description"/);
  assert.match(card, /if \(event\.key === 'Escape' && !review\.committing\) onCancel\(\)/);
  assert.match(card, /variant="secondary" autoFocus/);
  assert.match(card, /type="checkbox"/);
  assert.match(card, /dnsEngine\.downtimeAcknowledgement/);
  assert.doesNotMatch(card, /type="text"[\s\S]{0,200}(?:confirm|version|engine)/i);
});

test('backend blocker text is discarded and paired or DNSSEC support is never invented', () => {
  assert.match(contract, /Backend message text is deliberately discarded/);
  assert.match(contract, /blockers\.push\(\{ code: item\.code \}\)/);
  assert.doesNotMatch(card, /blocker\.message/);
  assert.match(card, /paired_topology_unsupported/);
  assert.match(card, /dnssec_unsupported/);
  assert.match(settings, /engine\?\.active_engine === 'bind'/);
  assert.match(settings, /dnsEngine\.topologyEditorBind/);
  assert.match(settings, /engine\?\.state === 'switching'/);
});

test('ready BIND locks only inherited paired identity and keeps standalone editable', () => {
  assert.match(settings, /const activeEngine: ActiveDNSEngine \| null = engine\?\.state === 'ready'/);
  assert.match(settings, /engine\.active_engine === 'pdns' \|\| engine\.active_engine === 'bind'/);
  assert.match(settings, /<DNSInfrastructureSettings[\s\S]*activeEngine=\{activeEngine\}/);
  assert.match(settings, /const role = normalizeRole\(cluster\.role\)/);
  assert.match(settings, /const bindPairedIdentityLocked = activeEngine === 'bind'[\s\S]*saved\.role === 'paired'/);
  assert.match(settings, /const bindRoleDisabled = \(role: DNSRole\) => bindPairedIdentityLocked[\s\S]*role === 'paired'/);
  assert.match(settings, /const effectiveRole: DNSRole = draft\.role/);
	assert.match(settings, /role: effectiveRole/);
  assert.match(settings, /aria-disabled=\{bindRoleDisabled\(role\)\}/);
  assert.match(settings, /disabled=\{bindRoleDisabled\(role\)\}/);
  assert.match(settings, /if \(bindRoleDisabled\(role\)\) return/);
  assert.match(settings, /dnsEngine\.identity\.bindPairedLocked/);
  assert.match(settings, /bind-identity-note/);
  assert.equal((settings.match(/fetch\('\/api\/v1\/settings\/dns-setup'/g) || []).length, 1);
  assert.equal((settings.match(/dns-wizard-save/g) || []).length, 1);
});

test('DNS engine copy has English and Turkish key parity', () => {
  const english = copy.slice(copy.indexOf('export const dnsEngineEn'), copy.indexOf('} as const;'));
  const turkish = copy.slice(copy.indexOf('export const dnsEngineTr'), copy.indexOf('\n};', copy.indexOf('export const dnsEngineTr')));
  const keys = (source) => new Set(
    [...source.matchAll(/'((?:dnsEngine)\.[^']+)'\s*:/g)].map((match) => match[1]),
  );
  assert.deepEqual([...keys(turkish)].sort(), [...keys(english)].sort());
  assert.match(copy, /dnsEngineTr: Record<DNSEngineCopyKey, string>/);
  assert.ok(keys(english).size >= 50, 'the full status, preview, impact, blocker, and safety copy must be localized');
});
