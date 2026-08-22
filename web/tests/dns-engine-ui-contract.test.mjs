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
const settingsPage = readFileSync(
  new URL('../src/components/Settings.tsx', import.meta.url),
  'utf8',
);
const identityPlan = readFileSync(
  new URL('../src/lib/dnsIdentityPlan.ts', import.meta.url),
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

async function loadIdentityPlanRuntime() {
  const javascript = ts.transpileModule(identityPlan, {
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

function stagedPairSnapshot(overrides = {}) {
  return {
    revision: 1,
    engine_epoch: 0,
    active_engine: null,
    state: 'unconfigured',
    topology: 'paired',
    pair_role: 'primary',
    dnssec_zone_count: 0,
    zone_count: 0,
    pending_zone_count: 0,
    engines: [
      { id: 'pdns', installed: false, running: false, managed: false, status: 'available' },
      { id: 'bind', installed: false, running: false, managed: false, status: 'available' },
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

test('hidden and initial DNS state load is read-only; explicit Refresh reconciles before GET', () => {
  const loadStart = card.indexOf('const refresh = useCallback');
  const reconcileStart = card.indexOf('const reconcileAndRefresh = useCallback', loadStart);
  const effectStart = card.indexOf('useEffect(() => {', reconcileStart);
  assert.ok(loadStart >= 0 && reconcileStart > loadStart && effectStart > reconcileStart);

  const loadBody = card.slice(loadStart, reconcileStart);
  assert.match(loadBody, /fetch\('\/api\/v1\/dns\/engine'/);
  assert.match(loadBody, /method: 'GET'/);
  assert.doesNotMatch(loadBody, /engine\/reconcile/);

  const reconcileBody = card.slice(reconcileStart, effectStart);
  const post = reconcileBody.indexOf("fetch('/api/v1/dns/engine/reconcile'");
  const get = reconcileBody.indexOf('await refresh()');
  assert.ok(post >= 0 && get > post,
    'Refresh must attempt reconciliation before reading a fresh snapshot');
  assert.match(reconcileBody, /method: 'POST'/);
  assert.match(reconcileBody, /if \(!response\.ok\)[\s\S]*await readApiError\(response\)/);
  assert.match(reconcileBody, /catch \{[\s\S]*\}[\s\S]*await refresh\(\)/,
    'a failed POST must still render the fail-closed GET truth');
  assert.match(reconcileBody,
    /setReview\(null\)[\s\S]*fetch\('\/api\/v1\/dns\/engine\/reconcile'/,
    'Refresh must discard stale review authority before reconciliation');
  assert.equal((reconcileBody.match(/await refresh\(\)/g) ?? []).length, 1,
    'an explicit Refresh must perform exactly one snapshot GET after the POST');

  const initialEffect = card.slice(effectStart, card.indexOf('useEffect(() => {', effectStart + 1));
  assert.match(initialEffect, /void refresh\(\)/);
  assert.doesNotMatch(initialEffect, /reconcileAndRefresh/);
  assert.match(settingsPage,
    /id="settings-dns-panel"[\s\S]*hidden=\{activeID !== 'dns'\}[\s\S]*<DNSServerSettings \/>/,
    'the DNS card remains mounted while hidden, so its mount effect must stay GET-only');
  assert.match(card, /onClick=\{\(\) => void reconcileAndRefresh\(\)\}/);
});

test('only source-free BIND installed standby is labelled as an installation retry', () => {
  const labelStart = card.indexOf('const reviewLabel =');
  const labelEnd = card.indexOf('return (', labelStart);
  assert.ok(labelStart >= 0 && labelEnd > labelStart);
  const label = card.slice(labelStart, labelEnd);

  assert.match(label,
    /engine[.]status === 'available'[^]*[|][|] [(]engine[.]status === 'installed_standby'[^]*&& snapshot[.]active_engine === null[^]*&& snapshot[.]state === 'unconfigured'[^]*&& id === 'bind'[)][^]*[?] et[(]'dnsEngine[.]reviewInstall'[)]/,
    'the initial managed stopped BIND retry must match the backend install action');
  assert.match(label, /[:] et[(]'dnsEngine[.]reviewSwitch'[)]/,
    'active-source standby engines must retain the switch label');
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
		pair_ready: false,
		engines: [
			{ id: 'pdns', installed: true, running: false, managed: true, status: 'installed_standby' },
			{ id: 'bind', installed: true, running: true, managed: true, status: 'active' },
		],
	})));
	assert.ok(decodeDNSEngineSnapshot(readySnapshot({
		topology: 'paired',
		pair_role: 'primary',
		pair_ready: true,
	})));
	assert.equal(decodeDNSEngineSnapshot(readySnapshot({
		topology: 'paired',
		pair_role: 'primary',
	})), null);
	assert.equal(decodeDNSEngineSnapshot(readySnapshot({
		topology: 'paired',
		pair_role: 'secondary',
		pair_ready: true,
	})), null);
	assert.equal(decodeDNSEngineSnapshot(readySnapshot({ pair_ready: false })), null);
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

test('DNS engine decoder accepts only exact staged paired authority tuples', async () => {
  const { decodeDNSEngineSnapshot } = await loadContractRuntime();

  assert.ok(decodeDNSEngineSnapshot(stagedPairSnapshot()));
  assert.ok(decodeDNSEngineSnapshot(stagedPairSnapshot({ pair_role: 'secondary' })));
  assert.ok(decodeDNSEngineSnapshot(stagedPairSnapshot({
    state: 'unmanaged',
    pair_role: 'secondary',
    engines: [
      {
        id: 'pdns',
        installed: true,
        running: true,
        managed: true,
        status: 'unmanaged',
        detail_code: 'unmanaged_dns_detected',
      },
      { id: 'bind', installed: false, running: false, managed: false, status: 'available' },
    ],
  })));

  assert.equal(decodeDNSEngineSnapshot(stagedPairSnapshot({ pair_role: undefined })), null);
  assert.equal(decodeDNSEngineSnapshot(stagedPairSnapshot({ pair_ready: false })), null);
  assert.equal(decodeDNSEngineSnapshot(stagedPairSnapshot({ pair_ready: true })), null);
  assert.equal(decodeDNSEngineSnapshot(stagedPairSnapshot({ topology: 'standalone' })), null);

  const standalone = stagedPairSnapshot({ topology: 'standalone' });
  delete standalone.pair_role;
  assert.ok(decodeDNSEngineSnapshot(standalone));
  assert.equal(decodeDNSEngineSnapshot({ ...standalone, pair_ready: false }), null);
});

test('DNS identity review unlocks only for the exact saved plan', async () => {
  const {
    dnsEngineIdentityReviewLocked,
    exactStagedIdentityIsCurrent,
  } = await loadIdentityPlanRuntime();
  const stagedEngine = { active_engine: null, topology: 'unconfigured' };

  assert.equal(dnsEngineIdentityReviewLocked(true, stagedEngine), true,
    'the initial unconfigured snapshot must disable engine review on its own');
  assert.equal(exactStagedIdentityIsCurrent(true, null, null, false), false);

  const standaloneSaved = {
    configured: true,
    namesDerived: false,
    ns1: 'ns1.example.com',
    ns2: 'ns2.example.com',
    role: 'standalone',
    peer_ip: '',
    peer_ns: '',
    server_ip: '72.62.38.15',
  };
  const standaloneDraft = {
    ns1: standaloneSaved.ns1,
    ns2: standaloneSaved.ns2,
    role: 'standalone',
    peer_ip: '',
    peer_ns: '',
  };
  const standaloneCurrent = exactStagedIdentityIsCurrent(
    true, standaloneSaved, standaloneDraft, false,
  );
  assert.equal(standaloneCurrent, true);
  assert.equal(dnsEngineIdentityReviewLocked(
    standaloneCurrent,
    { active_engine: null, topology: 'standalone' },
  ), false, 'an exact saved standalone plan unlocks review');

  const editedDraft = { ...standaloneDraft, ns1: 'edited.example.com' };
  const editedCurrent = exactStagedIdentityIsCurrent(
    true, standaloneSaved, editedDraft, false,
  );
  assert.equal(editedCurrent, false);
  assert.equal(dnsEngineIdentityReviewLocked(
    editedCurrent,
    { active_engine: null, topology: 'standalone' },
  ), true, 'editing the staged draft must lock review again');
  assert.equal(exactStagedIdentityIsCurrent(
    true,
    standaloneSaved,
    { ...standaloneDraft, role: 'paired', peer_ip: '2.25.80.4', peer_ns: standaloneSaved.ns2 },
    false,
    'primary',
  ), false, 'a draft role mismatch cannot reuse the saved plan');

  const pairedPrimarySaved = {
    ...standaloneSaved,
    role: 'paired',
    peer_ip: '2.25.80.4',
    peer_ns: standaloneSaved.ns2,
  };
  const pairedPrimaryDraft = {
    ...standaloneDraft,
    role: 'paired',
    peer_ip: pairedPrimarySaved.peer_ip,
    peer_ns: pairedPrimarySaved.peer_ns,
  };
  assert.equal(exactStagedIdentityIsCurrent(
    true, pairedPrimarySaved, pairedPrimaryDraft, false, 'primary',
  ), true, 'an exact saved paired-primary plan unlocks review');
  assert.equal(exactStagedIdentityIsCurrent(
    true, pairedPrimarySaved, { ...pairedPrimaryDraft, peer_ip: '2.25.80.5' }, false, 'primary',
  ), false, 'a peer mismatch must lock review');
  assert.equal(exactStagedIdentityIsCurrent(
    true, pairedPrimarySaved, pairedPrimaryDraft, false, 'secondary',
  ), false, 'a snapshot role mismatch must lock review');

  const pairedSecondarySaved = {
    ...pairedPrimarySaved,
    peer_ns: standaloneSaved.ns1,
  };
  const pairedSecondaryDraft = {
    ...pairedPrimaryDraft,
    peer_ns: pairedSecondarySaved.peer_ns,
  };
  const pairedSecondaryCurrent = exactStagedIdentityIsCurrent(
    true, pairedSecondarySaved, pairedSecondaryDraft, false, 'secondary',
  );
  assert.equal(pairedSecondaryCurrent, true);
  assert.equal(dnsEngineIdentityReviewLocked(
    pairedSecondaryCurrent,
    { active_engine: null, topology: 'paired' },
  ), false, 'an exact saved paired-secondary plan unlocks review');
});

test('DNS settings flow separates fresh, exact legacy, active, and manual recovery states', async () => {
  const { dnsEngineSettingsFlow } = await loadIdentityPlanRuntime();
  const availableEngines = [
    { id: 'pdns', installed: false, running: false, managed: false, status: 'available' },
    { id: 'bind', installed: false, running: false, managed: false, status: 'available' },
  ];
  const unmanagedEngines = [
    { id: 'pdns', installed: true, running: true, managed: false, status: 'unmanaged' },
    { id: 'bind', installed: false, running: false, managed: false, status: 'available' },
  ];

  assert.equal(dnsEngineSettingsFlow(null), 'unavailable');
  assert.equal(dnsEngineSettingsFlow({
    state: 'unconfigured', active_engine: null, topology: 'unconfigured', engines: availableEngines,
  }), 'identityStaging');
  assert.equal(dnsEngineSettingsFlow({
    state: 'ready', active_engine: 'pdns', topology: 'standalone', engines: [
      { id: 'pdns', installed: true, running: true, managed: true, status: 'active' },
      availableEngines[1],
    ],
  }), 'active');
  assert.equal(dnsEngineSettingsFlow({
    state: 'unmanaged', active_engine: null, topology: 'paired', engines: [
      { ...unmanagedEngines[0], managed: true },
      unmanagedEngines[1],
    ],
  }), 'legacyPowerDNSReconfigure', 'the exact managed legacy PowerDNS exception remains staged');

  for (const topology of ['unconfigured', 'standalone', 'paired']) {
    assert.equal(dnsEngineSettingsFlow({
      state: 'unmanaged', active_engine: null, topology, engines: unmanagedEngines,
    }), 'manualRecovery', `unverified ${topology} ownership must be manual recovery`);
  }
  assert.equal(dnsEngineSettingsFlow({
    state: 'unmanaged', active_engine: null, topology: 'paired', engines: [
      { ...unmanagedEngines[0], managed: true },
      { ...unmanagedEngines[1], installed: true, running: true, status: 'unmanaged' },
    ],
  }), 'manualRecovery', 'a second running engine invalidates the managed legacy exception');
  assert.equal(dnsEngineSettingsFlow({
    state: 'conflict', active_engine: null, topology: 'standalone', engines: unmanagedEngines,
  }), 'locked');
  assert.equal(dnsEngineSettingsFlow({
    state: 'degraded', active_engine: 'pdns', topology: 'standalone', engines: unmanagedEngines,
  }), 'locked');
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
  assert.ok(decodeDNSEngineSwitchPreview({
    ...preview,
    source_engine: null,
    target_engine: 'pdns',
    action: 'reconfigure',
  }, null, 'pdns', 4));
  assert.equal(decodeDNSEngineSwitchPreview({
    ...preview,
    source_engine: null,
    target_engine: 'bind',
    action: 'reconfigure',
  }, null, 'bind', 4), null);
  assert.equal(decodeDNSEngineSwitchPreview({
    ...preview,
    source_engine: null,
    target_engine: 'pdns',
    action: 'reconfigure',
    requires_downtime_acknowledgement: false,
  }, null, 'pdns', 4), null);
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

test('failed DNS commits discard consumed authority, distinguish proof flags, and refresh', () => {
  const commitStart = card.indexOf('const commitSwitch = async');
  const commitEnd = card.indexOf('\n    return (', commitStart);
  const commitBody = card.slice(commitStart, commitEnd);

  assert.match(commitBody, /const apiError = await readApiError\(response\)/);
  assert.match(commitBody, /const mutationApplied = apiError\.mutationApplied === true/);
  assert.match(commitBody, /const partialSuccess = apiError\.partialSuccess === true/);
  assert.doesNotMatch(
    commitBody,
    /mutationApplied\s*=\s*apiError\.partialSuccess/,
    'partial success alone must not prove a host mutation',
  );
  assert.match(commitBody, /if \(mutationApplied\)[\s\S]*dnsEngine\.switchAppliedNeedsRefresh/);
  assert.match(commitBody, /else if \(partialSuccess\)[\s\S]*dnsEngine\.switchPartialUnverified/);
  assert.match(commitBody, /apiError\.code[\s\S]*apiErrorText\(apiError, t\)/);

  const failedResponse = commitBody.indexOf('if (!response.ok)');
  const failedClose = commitBody.indexOf('setReview(null)', failedResponse);
  const failedRefresh = commitBody.indexOf('await refresh()', failedClose);
  assert.ok(failedResponse >= 0 && failedClose > failedResponse && failedRefresh > failedClose);

  const catchStart = commitBody.indexOf('} catch {', failedRefresh);
  const ambiguousClose = commitBody.indexOf('setReview(null)', catchStart);
  const ambiguousRefresh = commitBody.indexOf('await refresh()', ambiguousClose);
  assert.ok(catchStart > failedRefresh && ambiguousClose > catchStart && ambiguousRefresh > ambiguousClose);
  assert.doesNotMatch(
    commitBody,
    /committing:\s*false,\s*error:\s*et\('dnsEngine\.switch/,
    'a consumed or ambiguous preview must not leave a retry button in the old dialog',
  );
  assert.match(copy, /dnsEngine\.switchAppliedNeedsRefresh/);
  assert.match(copy, /dnsEngine\.switchPartialUnverified/);
  assert.match(copy, /Verify current DNS state before continuing/);
  assert.match(copy, /Devam etmeden önce güncel DNS durumunu doğrulayın/);
  assert.doesNotMatch(copy, /State was refreshed|Durum yenilendi/,
    'a refresh attempt must not be described as a completed refresh');
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
  assert.match(card, /agent_incompatible/);
  assert.match(card, /dns_identity_required: 'dnsEngine\.blocker\.identityRequired'/);
  assert.match(settings, /engine\?\.active_engine === 'bind'/);
  assert.match(settings, /dnsEngine\.topologyEditorBind/);
  assert.match(settings, /engine\?\.state === 'switching'/);
});

test('fresh servers stage an exact DNS identity before the first engine install', () => {
  assert.match(settings, /const \[engineRefreshKey, setEngineRefreshKey\] = useState\(0\)/);
  assert.match(settings, /const settingsFlow = dnsEngineSettingsFlow\(engine\)/);
  assert.match(settings, /const legacyPowerDNSReconfigureStaging = settingsFlow === 'legacyPowerDNSReconfigure'/);
  assert.match(settings, /const identityStaging = settingsFlow === 'identityStaging' \|\| legacyPowerDNSReconfigureStaging/);
  assert.match(identityPlan, /legacyPowerDNS\.installed && legacyPowerDNS\.running && legacyPowerDNS\.managed/);
  assert.match(identityPlan, /snapshot\.engines\.every\(\(entry\) => entry\.id === 'pdns' \|\| !entry\.running\)/);
  assert.match(settings, /<DNSEngineCard[\s\S]*key=\{engineRefreshKey\}/);
  assert.match(settings, /data-testid=\x22dns-identity-before-engine\x22[\s\S]*<DNSInfrastructureSettings/);
  assert.match(settings, /activeEngine=\{activeEngine\}/);
  assert.match(settings, /data-testid=\x22dns-identity-before-engine\x22[\s\S]*stagingOnly[\s\S]*<DNSEngineCard/);
  assert.match(settings, /legacyPowerDNSReconfigure=\{legacyPowerDNSReconfigureStaging\}/);
  assert.match(settings, /onIdentityStaged=\{\(\) => setEngineRefreshKey/);
  assert.match(settings, /activeEngine: ActiveDNSEngine \| null/);
  assert.match(settings, /data-testid="dns-identity-staging-note"/);
  assert.match(settings, /\(payload as \{ success\?: unknown \}\)\.success !== true/);
  assert.match(settings, /\(payload as \{ staged\?: unknown \}\)\.staged !== true/);
  assert.match(settings, /dnsEngine\.identity\.stageInvalid/);
  assert.match(settings, /dnsEngine\.identity\.stageSuccess/);
  assert.match(settings, /onIdentityStaged\(\)/);
  assert.match(settings, /stagedIdentityCurrent/);
  assert.match(copy, /dnsEngine\.identity\.stageDescription/);
  assert.match(copy, /This step does not install, start, or publish a DNS server/);
  assert.match(copy, /Bu adım DNS sunucusu kurmaz, başlatmaz veya yayın yapmaz/);
  assert.match(copy, /reconfigure it directly as the secondary/);
  assert.match(copy, /doğrudan ikincil olarak yapılandıracak/);
  assert.match(settings, /dnsEngine\.identity\.legacyPairedDirect/);
  assert.match(contract, /'install' \| 'switch' \| 'adopt' \| 'reconfigure'/);
  assert.match(card, /dnsEngine\.reviewReconfigure/);
  assert.match(copy, /dnsEngine\.blocker\.identityRequired/);
});

test('identity staging precedes engine choices and fail-closes every review path', () => {
  const renderStart = settings.indexOf('return (', settings.indexOf('export function DNSServerSettings'));
  const renderEnd = settings.indexOf('\n}\n\nfunction DNSInfrastructureSettings', renderStart);
  const render = settings.slice(renderStart, renderEnd);
  const stagingIndex = render.indexOf('dns-identity-before-engine');
  const engineIndex = render.indexOf('<DNSEngineCard');
  const activeEditorIndex = render.indexOf('{engine && !identityStaging && (activeEngine ? (');

  assert.ok(stagingIndex >= 0 && stagingIndex < engineIndex,
    'an unconfigured or legacy server must show identity setup before engine choices');
  assert.ok(engineIndex < activeEditorIndex,
    'an active engine must keep the existing engine-card-first layout');
  assert.match(render, /\{engine && !identityStaging && \(activeEngine \? \(/,
    'identity fallback must wait for a verified engine snapshot');
  assert.match(settings, /identityPlanCurrent=\{!actionsLocked && \(!identityStaging \|\| identityPlanCurrent\)\}/);
  assert.match(identityPlan, /function exactStagedIdentityIsCurrent\(/);
  assert.match(identityPlan, /saved\.peer_ns === savedPeerNS/);
  assert.match(identityPlan, /pairRole === expectedPairRole/);
  assert.match(settings, /onIdentityPlanCurrentChange\(stagedIdentityCurrent\)/);

  assert.match(card, /identityPlanCurrent = true/);
  assert.match(card, /dnsEngineIdentityReviewLocked\(identityPlanCurrent, snapshot\)/);
  assert.match(identityPlan, /snapshot\?\.active_engine === null && snapshot\.topology === 'unconfigured'/);
  assert.match(card, /actionsLocked \|\| !snapshot \|\| snapshot\.state === 'switching' \|\| identityReviewLocked/);
  assert.match(card, /const canReview = !actionsLocked[\s\S]*&& !identityReviewLocked/);
  assert.match(card, /disabled=\{!canReview \|\| review !== null\}/);
  assert.match(card, /data-testid=\x22dns-engine-identity-lock\x22/);
  assert.match(copy, /dnsEngine\.identity\.reviewLocked/);
});

test('paired engines expose exact peer readiness without granting secondary writes', () => {
  assert.match(contract, /pair_ready\?: boolean/);
  assert.match(contract, /value\.active_engine !== null && value\.topology === 'paired'[\s\S]*typeof value\.pair_ready !== 'boolean'/);
  assert.match(contract, /value\.pair_role === 'secondary' && value\.pair_ready !== false/);
  assert.match(card, /data-testid="dns-pair-readiness"/);
  assert.match(card, /dnsEngine\.pair\.primaryWaiting/);
  assert.match(card, /dnsEngine\.pair\.primaryReady/);
  assert.match(card, /dnsEngine\.pair\.secondaryReadOnly/);
  assert.match(copy, /waiting for the secondary to prove the exact catalog/);
  assert.match(copy, /ikincilin kesin kataloğu kanıtlaması bekleniyor/);
});

test('paired identity remains locked across independent BIND or PowerDNS choices', () => {
  assert.match(settings, /const activeEngine: ActiveDNSEngine \| null = settingsFlow === 'active'/);
  assert.match(settings, /engine\?\.active_engine === 'pdns' \|\| engine\?\.active_engine === 'bind'/);
  assert.match(settings, /<DNSInfrastructureSettings[\s\S]*activeEngine=\{activeEngine\}/);
  assert.match(settings, /const role = normalizeRole\(cluster\.role\)/);
  assert.match(settings, /pairRole=\{engine\?\.pair_role\}/);
  assert.match(settings, /const pairedIdentityLocked = pairRole !== undefined[\s\S]*saved\.role === 'paired'/);
  assert.match(settings, /const roleSelectionDisabled = \(role: DNSRole\) => pairedIdentityLocked[\s\S]*role === 'paired'/);
  assert.match(settings, /activeEngine === 'bind' && role === 'paired'/);
  assert.match(settings, /const effectiveRole: DNSRole = draft\.role/);
	assert.match(settings, /role: effectiveRole/);
  assert.match(settings, /aria-disabled=\{roleSelectionDisabled\(role\)\}/);
  assert.match(settings, /disabled=\{roleSelectionDisabled\(role\)\}/);
  assert.match(settings, /if \(roleSelectionDisabled\(role\)\) return/);
  assert.match(settings, /dnsEngine\.identity\.pairedLocked/);
  assert.match(settings, /paired-identity-note/);
  assert.equal((settings.match(/fetch\('\/api\/v1\/settings\/dns-setup'/g) || []).length, 1);
  assert.equal((settings.match(/dns-wizard-save/g) || []).length, 1);
});

test('manual recovery and unsafe transitions expose Refresh only and invalidate stale review', () => {
  assert.match(settings, /const manualRecovery = settingsFlow === 'manualRecovery'/);
  assert.match(settings, /const actionsLocked = settingsFlow === 'unavailable' \|\| manualRecovery \|\| settingsFlow === 'locked'/);
  assert.match(settings, /actionsLocked=\{actionsLocked\}/);
  assert.match(settings, /data-testid=\{manualRecovery \? 'dns-manual-recovery' : undefined\}/);
  assert.match(settings, /dnsEngine\.manualRecoveryTitle/);
  assert.match(settings, /dnsEngine\.manualRecoveryDescription/);
  assert.match(card, /if \(actionsLocked\) setReview\(null\)/,
    'an unsafe parent snapshot must invalidate an already-open review');
  assert.match(card, /if \(actionsLocked \|\| !snapshot/);
  assert.match(card, /if \(actionsLocked \|\| !current \|\| !preview/);
  assert.match(card, /\) : actionsLocked \? null : \(/,
    'locked states must render neither install nor adoption review buttons');
  assert.match(card, /\{review && !actionsLocked && \(/,
    'a stale dialog must disappear before it can confirm an unsafe transition');
  assert.match(card, /identityReviewLocked && !actionsLocked/,
    'manual recovery must not show the impossible save-above instruction');
  assert.match(copy, /DNS ownership could not be verified/);
  assert.match(copy, /then use Refresh state/);
  assert.match(copy, /DNS sahipliği doğrulanamadı/);
  assert.match(copy, /ardından Durumu yenile düğmesini kullanın/);
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
