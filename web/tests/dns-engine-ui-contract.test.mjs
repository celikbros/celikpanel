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

function operationSnapshot(overrides = {}) {
  return {
    id: 'c'.repeat(32),
    request_id: 'd'.repeat(32),
    target_engine: 'bind',
    phase: 'activating',
    status: 'running',
    started_at: '2026-08-26T09:00:00Z',
    updated_at: '2026-08-26T09:00:05Z',
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
  assert.match(contract, /value\.state === 'switching'[\s\S]*typeof value\.operation_id !== 'string'/);
  assert.match(contract, /return null;/);
  assert.match(card, /const decoded = decodeDNSEngineSnapshot\(payload\)/);
  assert.match(card, /if \(decoded === null\) \{[\s\S]*failRefresh\(et\('dnsEngine\.stateInvalid'\)\)/);
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
  const get = reconcileBody.indexOf('await refresh(automaticTracking)');
  assert.ok(post >= 0 && get > post,
    'Refresh must attempt reconciliation before reading a fresh snapshot');
  assert.match(loadBody, /const refresh = useCallback\(async \(automaticTracking = false\)/);
  assert.match(reconcileBody, /const reconcileAndRefresh = useCallback\(async \(automaticTracking = false\)/);
  assert.match(reconcileBody, /method: 'POST'/);
  assert.match(reconcileBody, /if \(!response\.ok\)[\s\S]*await readApiError\(response\)/);
  assert.match(reconcileBody, /catch \{[\s\S]*\}[\s\S]*await refresh\(automaticTracking\)/,
    'a failed POST must still render the fail-closed GET truth');
  assert.match(reconcileBody,
    /setReview\(null\)[\s\S]*fetch\('\/api\/v1\/dns\/engine\/reconcile'/,
    'Refresh must discard stale review authority before reconciliation');
  assert.equal((reconcileBody.match(/await refresh\(automaticTracking\)/g) ?? []).length, 1,
    'an explicit Refresh must perform exactly one snapshot GET after the POST');

  const initialEffect = card.slice(effectStart, card.indexOf('useEffect(() => {', effectStart + 1));
  assert.match(initialEffect, /void refresh\(\)/);
  assert.doesNotMatch(initialEffect, /reconcileAndRefresh/);
  assert.match(settingsPage,
    /id="settings-dns-panel"[\s\S]*hidden=\{activeID !== 'dns'\}[\s\S]*<DNSServerSettings \/>/,
    'the DNS card remains mounted while hidden, so its mount effect must stay GET-only');
  assert.match(card, /onClick=\{\(\) => void reconcileAndRefresh\(\)\}/);
});

test('refresh failures retain the last verified snapshot while authoritative reads fail closed', () => {
  const loadStart = card.indexOf('const refresh = useCallback');
  const loadEnd = card.indexOf('const reconcileAndRefresh = useCallback', loadStart);
  assert.ok(loadStart >= 0 && loadEnd > loadStart);
  const loadBody = card.slice(loadStart, loadEnd);
  const helperStart = loadBody.indexOf('const failRefresh =');
  const helperEnd = loadBody.indexOf('\n        try {', helperStart);
  assert.ok(helperStart >= 0 && helperEnd > helperStart);
  const failRefresh = loadBody.slice(helperStart, helperEnd);

  assert.match(failRefresh,
    /if \(automaticTracking\) \{[\s\S]*setTrackingReadError\(et\('dnsEngine\.operation\.trackingReadFailed'\)\)[\s\S]*return;[\s\S]*\}[\s\S]*setLoadError\(message\)/,
    'an automatic poll reports a tracking warning while an explicit read reports its load error');
  assert.doesNotMatch(failRefresh, /setSnapshot\(null\)|onSnapshotChange\?\.\(null\)/,
    'a transient read failure must not erase the last verified DNS authority or operation evidence');
  assert.equal((loadBody.match(/failRefresh\(et\('dnsEngine\.stateUnavailable'\)\)/g) ?? []).length, 2,
    'non-2xx/network failures must share the automatic-retention boundary');
  assert.equal((loadBody.match(/failRefresh\(et\('dnsEngine\.stateInvalid'\)\)/g) ?? []).length, 1,
    'malformed JSON must share the automatic-retention boundary');
  assert.match(loadBody,
    /if \(automaticTracking\) setTrackingReadError\(''\);[\s\S]*setSnapshot\(decoded\)[\s\S]*onSnapshotChange\?\.\(decoded\)/,
    'a later valid poll replaces the retained snapshot and clears its read warning');
  assert.match(loadBody, /if \(!automaticTracking\) setTrackingReadError\(''\)/,
    'mount and manual authoritative reads start from a clean tracking-read warning');
	assert.match(loadBody,
		/new AbortController\(\)[\s\S]*setTimeout\([\s\S]*requestController\.abort\(\)[\s\S]*dnsEngineStatusRequestTimeoutMs[\s\S]*signal: requestController\.signal[\s\S]*finally \{[\s\S]*clearTimeout\(requestTimeout\)/,
		'a hung snapshot request must abort and settle instead of freezing automatic tracking');
  assert.match(card,
    /const canReview = !actionsLocked[\s\S]*&& !loading[\s\S]*&& !loadError/,
    'retained evidence must remain non-actionable until a fresh authoritative read succeeds');
  assert.match(copy, /dnsEngine\.operation\.trackingReadFailed/);
});

test('a loaded switching snapshot resumes continuous polling and periodic reconciliation', () => {
  const pollingStart = card.indexOf(`if (snapshot?.state !== 'switching' || !snapshot.operation_id)`);
  const pollingEnd = card.indexOf('\n    useEffect(() => {', pollingStart);
  assert.ok(pollingStart >= 0 && pollingEnd > pollingStart);
  const polling = card.slice(pollingStart, pollingEnd);
  assert.match(polling, /let attempts = 0/);
  assert.match(polling, /attempts \+= 1/);
  assert.match(polling, /if \(attempts % 5 === 0\)[\s\S]*await reconcileAndRefresh\(true\)[\s\S]*else[\s\S]*await refresh\(true\)/);
  const awaitedRead = polling.indexOf('await refresh(true)');
  const cancellationCheck = polling.indexOf('if (cancelled) return;', awaitedRead);
  const nextPoll = polling.indexOf('timer = setTimeout(() => void poll(), nextDelay)', cancellationCheck);
  assert.ok(awaitedRead >= 0 && cancellationCheck > awaitedRead && nextPoll > cancellationCheck,
    'the next automatic poll is scheduled only after the current read settles');
	assert.match(polling,
		/try \{[\s\S]*await reconcileAndRefresh\(true\)[\s\S]*await refresh\(true\)[\s\S]*\} finally \{[\s\S]*if \(cancelled\) return;[\s\S]*timer = setTimeout\(\(\) => void poll\(\), nextDelay\)/,
		'tracking must schedule its next bounded request from finally');
  assert.doesNotMatch(polling, /setInterval/);
  assert.match(polling, /timer = setTimeout\(\(\) => void poll\(\), 3000\)/,
    'resumed tracking starts with a three-second poll');
  assert.match(polling,
    /if \(attempts >= 120\) setTrackingDelayed\(true\)[\s\S]*const nextDelay = attempts >= 120 \? 15000 : 3000[\s\S]*setTimeout\(\(\) => void poll\(\), nextDelay\)/,
    'tracking must continue at a slower cadence after warning that the operation is delayed');
  assert.doesNotMatch(polling,
    /if \(attempts >= 120\)[^\n]*[\s\S]{0,80}return/,
    'the delayed warning must not end polling while the server remains switching');
  assert.match(polling, /cancelled = true[\s\S]*clearTimeout\(timer\)/);
  assert.match(polling, /\[reconcileAndRefresh, refresh, snapshot\?\.operation_id, snapshot\?\.state\]/);
  assert.doesNotMatch(polling, /review\?\.committing|switchAccepted/);
});

test('reconciliation failures remain visible until reconciliation succeeds or terminal truth arrives', () => {
  const reconcileStart = card.indexOf('const reconcileAndRefresh = useCallback');
  const effectStart = card.indexOf('useEffect(() => {', reconcileStart);
  assert.ok(reconcileStart >= 0 && effectStart > reconcileStart);
  const reconcileBody = card.slice(reconcileStart, effectStart);
  assert.match(reconcileBody,
    /if \(!response\.ok\) \{[\s\S]*setTrackingError\([\s\S]*dnsEngine\.trackingReconcileFailed[\s\S]*\} else \{[\s\S]*setTrackingError\(''\)/);
  assert.match(reconcileBody, /catch \{[\s\S]*setTrackingError\(et\('dnsEngine\.trackingReconcileFailed'\)\)/);
	assert.match(reconcileBody,
		/new AbortController\(\)[\s\S]*setTimeout\([\s\S]*requestController\.abort\(\)[\s\S]*dnsEngineStatusRequestTimeoutMs[\s\S]*signal: requestController\.signal[\s\S]*finally \{[\s\S]*clearTimeout\(requestTimeout\)/,
		'a hung reconciliation request must abort before the authoritative GET');
  assert.match(reconcileBody, /await refresh\(automaticTracking\)/);

  const loadStart = card.indexOf('const refresh = useCallback');
  const loadBody = card.slice(loadStart, reconcileStart);
  assert.doesNotMatch(loadBody, /setTrackingError/,
    'a successful snapshot read must not erase a reconciliation failure before a successful reconciliation');
  assert.match(card, /trackingError=\{trackingError \|\| trackingReadError\}/);

  const pollingStart = card.indexOf("if (snapshot?.state !== 'switching' || !snapshot.operation_id)");
  const pollingEnd = card.indexOf('\n    useEffect(() => {', pollingStart);
  const polling = card.slice(pollingStart, pollingEnd);
  assert.match(polling,
    /if \(snapshot\?\.state !== 'switching' \|\| !snapshot\.operation_id\) \{[\s\S]*setTrackingDelayed\(false\)[\s\S]*setTrackingError\(''\)[\s\S]*setTrackingReadError\(''\)[\s\S]*return;/,
    'verified terminal state clears every active-tracking warning and cancels its timer');
});

test('operation progress card presents durable active and terminal evidence', () => {
  assert.match(card,
    /\{snapshot\.operation && \([\s\S]*<DNSEngineOperationProgress[\s\S]*operation=\{snapshot\.operation\}/);
  const progressStart = card.indexOf('function DNSEngineOperationProgress');
  const progressEnd = card.indexOf('\nfunction DNSEngineReviewDialog', progressStart);
  assert.ok(progressStart >= 0 && progressEnd > progressStart);
  const progress = card.slice(progressStart, progressEnd);

  assert.match(progress, /data-testid=\x22dns-engine-operation-progress\x22/);
  assert.match(progress, /role=\x22status\x22/);
  assert.match(progress, /aria-live=\x22polite\x22/);
  assert.match(progress, /\['running', 'rolling_back', 'recovery_required'\]\.includes\(operation\.status\)/);
  assert.match(card,
    /function operationElapsedSeconds[\s\S]*\['running', 'rolling_back', 'recovery_required'\]\.includes\(operation\.status\)[\s\S]*\? Date\.now\(\)[\s\S]*: Date\.parse\(operation\.updated_at\)/,
    'elapsed time must stop at the durable terminal update');
  assert.match(progress, /dnsEngine\.operation\.status\.\$\{operation\.status\}/);
  assert.match(progress, /dnsEngine\.operation\.phase\.\$\{operation\.phase\}/);
  assert.match(progress, /engineName\(operation\.target_engine\)/);
  assert.match(progress, /operationTimestamp\(operation\.updated_at, locale\)/);
  assert.match(progress, /\{operation\.id\}/);
  assert.match(progress, /operation\.last_error && \([\s\S]*role=\x22alert\x22/);
  assert.match(progress, /trackingError && active/);
  assert.match(progress, /trackingDelayed && active/);

  for (const status of ['running', 'rolling_back', 'recovery_required', 'succeeded', 'rolled_back', 'failed']) {
    assert.match(copy, new RegExp(`dnsEngine\\.operation\\.status\\.${status}`));
  }
  for (const phase of [
    'planned', 'staging', 'staged', 'activating', 'verifying',
    'rolling_back', 'committed', 'rolled_back', 'failed',
  ]) {
    assert.match(copy, new RegExp(`dnsEngine\\.operation\\.phase\\.${phase}`));
  }
  assert.match(copy, /dnsEngine\.operation\.trackingDelayed/);
  assert.match(copy, /dnsEngine\.trackingReconcileFailed/);
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

test('DNS engine operation decoder accepts exact active and terminal phase tuples only', async () => {
  const { decodeDNSEngineSnapshot } = await loadContractRuntime();
  const activeOperation = operationSnapshot();
  const switching = readySnapshot({
    state: 'switching',
    operation_id: activeOperation.id,
    operation: activeOperation,
  });

  assert.deepEqual(decodeDNSEngineSnapshot(switching)?.operation, activeOperation);
  assert.equal(decodeDNSEngineSnapshot({
    ...switching,
    operation_id: 'd'.repeat(32),
  }), null, 'the switching lock and presented operation must identify the same mutation');
  assert.equal(decodeDNSEngineSnapshot({
    ...switching,
    operation: operationSnapshot({ phase: 'committed', status: 'running' }),
  }), null, 'phase and status cannot contradict one another');
  assert.equal(decodeDNSEngineSnapshot({
    ...switching,
    operation: operationSnapshot({ updated_at: '2026-08-26T08:59:59Z' }),
  }), null, 'operation time cannot move backwards');
  assert.equal(decodeDNSEngineSnapshot({
    ...switching,
    operation: operationSnapshot({ last_error: 'untrusted running error' }),
  }), null, 'a forward-running phase cannot carry terminal error text');

  const recoveryOperation = operationSnapshot({
    status: 'recovery_required',
    last_error: 'host outcome requires panel reconciliation',
  });
  assert.deepEqual(decodeDNSEngineSnapshot({
    ...switching,
    operation: recoveryOperation,
  })?.operation, recoveryOperation,
    'an attached interrupted phase remains locked and visible for recovery');
  assert.equal(decodeDNSEngineSnapshot({
    ...switching,
    operation: operationSnapshot({ status: 'recovery_required' }),
  }), null, 'recovery-required state must explain why verification cannot finish');

  const completedOperation = operationSnapshot({
    phase: 'committed',
    status: 'succeeded',
    updated_at: '2026-08-26T09:01:00Z',
  });
  assert.deepEqual(decodeDNSEngineSnapshot(readySnapshot({
    operation: completedOperation,
  }))?.operation, completedOperation,
    'the latest terminal operation remains present after the switching lock is released');
  assert.equal(decodeDNSEngineSnapshot(readySnapshot({
    operation_id: completedOperation.id,
    operation: completedOperation,
  })), null, 'terminal history cannot retain the active operation lock');
  assert.equal(decodeDNSEngineSnapshot(readySnapshot({
    operation: operationSnapshot(),
  })), null, 'non-switching snapshots cannot present an active operation');
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
  assert.match(commitBody, /submitDNSEngineSwitch\(requestBody\)/);
  assert.match(commitBody, /request_id: requestID/);
  assert.match(commitBody, /const requestID = current\.requestID/);
  assert.doesNotMatch(commitBody, /createRequestID\(\)/);
  assert.match(commitBody, /target_engine: current\.target/);
  assert.match(commitBody, /expected_source: current\.base\.active_engine/);
  assert.match(commitBody, /expected_revision: current\.base\.revision/);
  assert.match(commitBody, /preview_token: preview\.preview_token/);
  assert.match(commitBody, /downtime_acknowledged: current\.acknowledged/);
});

test('successful DNS commit adopts its verified terminal snapshot before unlocking without a redundant GET', () => {
  const commitStart = card.indexOf('const commitSwitch = async');
  const commitEnd = card.indexOf('\n    return (', commitStart);
  const commitBody = card.slice(commitStart, commitEnd);
  const successDecode = commitBody.indexOf('decodeDNSEngineSnapshot(', commitBody.indexOf('if (!result.ok)'));
  const catchStart = commitBody.indexOf('} catch {', successDecode);
  assert.ok(successDecode >= 0 && catchStart > successDecode,
    'a successful switch response must be decoded before the commit handler can finish');
  const successBody = commitBody.slice(successDecode, catchStart);

  assert.match(successBody, /decodeDNSEngineSnapshot\(result\.payload\)/);
  assert.match(successBody, /if \(decoded === null\)/,
    'a malformed success body must never unlock the page as a completed operation');
  assert.match(successBody, /completeGuardedVerification\(decoded,/,
    'the direct success response must use the same exact terminal verifier as recovery polling');
  assert.doesNotMatch(successBody, /await (?:refresh|reconcileAndRefresh)\(/,
    'the switch response already contains terminal truth; a second GET creates an avoidable unlock race');

  const completionStart = card.indexOf('const completeGuardedVerification =');
  const completionEnd = card.indexOf('\n    useEffect(() => {', completionStart);
  const completionBody = card.slice(completionStart, completionEnd);
  const adoptSnapshot = completionBody.indexOf('setSnapshot(decoded)');
  const notifyParent = completionBody.indexOf('onSnapshotChange?.(decoded)', adoptSnapshot);
  const closeReview = completionBody.indexOf('setOperationGuard(', notifyParent);
  const releaseGuard = completionBody.indexOf('operationLeaseRef.current?.release()', closeReview);
  assert.ok(adoptSnapshot >= 0 && notifyParent > adoptSnapshot && closeReview > notifyParent && releaseGuard > closeReview,
    'the verified terminal snapshot must be visible before the exact central lease unlocks');
  assert.match(completionBody, /showToast\('success'/);
});

test('failed or ambiguous DNS commits retain the guard unless a pre-persist refusal is proven', () => {
  const commitStart = card.indexOf('const commitSwitch = async');
  const commitEnd = card.indexOf('\n    return (', commitStart);
  const commitBody = card.slice(commitStart, commitEnd);

  assert.match(commitBody, /const apiError = result\.error/);
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

  const failedResponse = commitBody.indexOf('if (!result.ok)');
  const successDecode = commitBody.indexOf('decodeDNSEngineSnapshot(', failedResponse);
  const failedBody = commitBody.slice(failedResponse, successDecode);
  const failedClose = failedBody.indexOf('setReview(null)');
  assert.ok(failedResponse >= 0 && successDecode > failedResponse && failedClose >= 0,
    'a consumed failed preview must not remain confirmable');
  assert.match(failedBody,
    /const prePersistRefusal = !apiError\.code[\s\S]*result\.status === 400 \|\| result\.status === 409/,
    'only an uncoded client refusal before persistence may unlock without a terminal operation');
  assert.match(failedBody, /if \(prePersistRefusal\)[\s\S]*clearDNSOperationMarker\(requestID\)/);
  assert.match(failedBody, /holdOperationGuard\([\s\S]*replayRequest: requestBody/,
    'coded rollback and ambiguous server outcomes must retain the exact replayable guard');

  const catchStart = commitBody.indexOf('} catch {', successDecode);
  const ambiguousBody = commitBody.slice(catchStart);
  assert.ok(catchStart > successDecode && ambiguousBody.includes('setReview(null)'),
    'an ambiguous response must consume the old review authority');
  assert.doesNotMatch(ambiguousBody, /setOperationGuard\(null\)/,
    'a lost response cannot unlock the panel while the host outcome is unknown');
  assert.match(commitBody, /setOperationGuard\([\s\S]*requestID[\s\S]*target/,
    'the durable request identity must be captured independently before the mutation begins');
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

test('DNS actions lock while state is loading and an independent full-page guard survives submission', () => {
  const previewStart = card.indexOf('const requestPreview = async');
  const previewEnd = card.indexOf('\n    const commitSwitch = async', previewStart);
  const previewBody = card.slice(previewStart, previewEnd);
  const cardsStart = card.indexOf('const canReview =');
  const cardsEnd = card.indexOf('const reviewLabel =', cardsStart);
  const canReview = card.slice(cardsStart, cardsEnd);

  assert.match(card, /const \[operationGuard, setOperationGuard\] = useState/,
    'submission/outcome ownership must not depend on the disposable preview dialog');
  assert.match(previewBody, /actionsLocked \|\| loading \|\| operationGuardRef\.current !== null/,
    'preview creation must be impossible during a state read or guarded mutation');
  assert.match(canReview, /!loading/);
  assert.match(canReview, /operationGuard === null/);
  assert.match(card, /disabled=\{loading \|\| review\?\.committing === true \|\| operationGuard !== null\}/,
    'Refresh cannot race a state read, submission, or unresolved mutation outcome');

  assert.match(card, /const \{ acquireInteractionBlock \} = useComponentOperation\(\)/);
  assert.match(card, /operationLeaseRef\.current = acquireInteractionBlock\(guardView\(guard\)\)/,
    'the exact DNS request must acquire the shared full-page interaction lease');
  assert.doesNotMatch(card, /function DNSEngineOperationGuard|createPortal\(/,
    'DNS must not own a second portal, inert root, or history blocker');
  assert.doesNotMatch(card,
    /review && !actionsLocked && operationGuard/,
    'parent fail-closed state must not accidentally unmount an in-flight outcome guard');
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
