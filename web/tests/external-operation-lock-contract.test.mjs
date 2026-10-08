import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const routerSource = readFileSync(new URL('../src/router.tsx', import.meta.url), 'utf8');
const operationSource = readFileSync(
  new URL('../src/components/ComponentOperation.tsx', import.meta.url),
  'utf8',
);
const dnsSource = readFileSync(
  new URL('../src/components/DNSEngineCard.tsx', import.meta.url),
  'utf8',
);
const overlaySource = readFileSync(
  new URL('../src/components/OperationOverlay.tsx', import.meta.url),
  'utf8',
);

function section(source, startText, endText) {
  const start = source.indexOf(startText);
  const end = source.indexOf(endText, start + startText.length);
  assert.ok(start >= 0 && end > start, `missing source section: ${startText}`);
  return source.slice(start, end);
}

test('external interaction leases are identity-owned and cannot release a different operation', () => {
  const acquire = section(
    operationSource,
    'const acquireInteractionBlock =',
    '// Discovery is a fail-closed mutation gate',
  );

  assert.match(operationSource, /new Map<object, InteractionBlockView>\(\)/);
  assert.match(acquire, /const id = \{\}/);
  assert.match(acquire, /interactionBlocksRef\.current\.set\(id, view\)/);
  assert.match(acquire, /!interactionBlocksRef\.current\.has\(id\)/,
    'a stale lease must not update a block it no longer owns');
  assert.match(acquire, /interactionBlocksRef\.current\.delete\(id\)/,
    'release must delete only the exact acquired lease');
  assert.doesNotMatch(acquire, /interactionBlocksRef\.current\.clear\(\)/,
    'one operation must never clear another operation\'s lock');
  assert.match(operationSource, /interactionBlocksRef\.current\.size > 0/,
    'the global lock remains held until every exact lease is released');
});

test('the DNS external-operation identity survives reload in a validated tab-scoped marker', () => {
  const recoverySource = dnsSource;
  const marker = recoverySource.match(
    /const\s+([A-Z][A-Z0-9_]*(?:OPERATION|GUARD)[A-Z0-9_]*(?:KEY|MARKER))\s*=\s*['"]([^'"]*(?:dns|operation)[^'"]*)['"]/i,
  );
  assert.ok(marker, 'a named DNS operation recovery marker is required');
  const markerName = marker[1];
  const escapedName = markerName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

  assert.match(recoverySource, new RegExp(`sessionStorage\\.getItem\\(${escapedName}\\)`),
    'reload must read the tab-scoped DNS operation marker');
  assert.match(recoverySource, new RegExp(`sessionStorage\\.setItem\\(${escapedName},\\s*JSON\\.stringify`),
    'the exact marker must be durable before the mutation can outlive the route');
  assert.match(recoverySource, /operationID/,
    'the durable marker must retain the operation identity it owns');
  const guardView = section(dnsSource, 'const guardView =', 'const holdOperationGuard =');
  const holdGuard = section(dnsSource, 'const holdOperationGuard =', 'useLayoutEffect(() => {');
  assert.match(guardView, /operationID:\s*guard\.requestID/,
    'the central overlay must present the exact request identity it owns');
  assert.match(holdGuard, /acquireInteractionBlock\(guardView\(guard\)\)/,
    'the DNS route must attach that exact identity to the central lock');

  const removeAt = recoverySource.search(new RegExp(`sessionStorage\\.removeItem\\(${escapedName}\\)`));
  assert.ok(removeAt >= 0, 'the exact marker needs a terminal cleanup path');
  const exactClearProof = recoverySource.slice(Math.max(0, removeAt - 650), removeAt);
  assert.match(exactClearProof, /operationID|requestID/);
  assert.match(exactClearProof, /===/,
    'cleanup must compare the current marker identity before removing it');
});

test('the central operation lock blocks in-app navigation and hard unload together', () => {
  assert.match(operationSource, /useNavigationBlocker\(interactionBlockedRef\)/,
    'the provider must register its live central lock with the router');
  assert.match(operationSource,
    /if \(!interactionBlocked\) return;[\s\S]*const onBeforeUnload = \(event: BeforeUnloadEvent\)[\s\S]*event\.preventDefault\(\)[\s\S]*event\.returnValue = ''/,
    'reload and cross-document navigation require the browser warning while locked');

  const navigate = section(
    routerSource,
    'const navigate = useCallback<NavigateFunction>',
    'const value = useMemo',
  );
  const blocked = navigate.indexOf('if (navigationBlocker?.current) return');
  const parseTarget = navigate.indexOf('new URL(');
  const mutateHistory = navigate.search(/window\.history\.(?:pushState|replaceState)/);
  assert.ok(blocked >= 0 && parseTarget > blocked && mutateHistory > parseTarget,
    'blocked navigate() calls must return before URL parsing or history mutation');
});

test('blocked Back navigation returns by managed history index, including a double Back', () => {
  assert.match(routerSource, /const ROUTER_STATE_KEY = ['"]__celikpanel_router_v1['"]/);
  assert.match(routerSource, /wrappedHistoryState\(historyIndexRef\.current, window\.history\.state\)/);
  assert.match(routerSource, /historyIndexRef\.current \+ 1/,
    'each in-app push must receive a monotonic managed index');

  const pop = section(routerSource, 'const onPopState =', "window.addEventListener('popstate'");
  const blockedStart = pop.indexOf('if (navigationBlocker?.current)');
  const indexedReturn = pop.indexOf('window.history.go(historyIndexRef.current - target.index)', blockedStart);
  const blockedReturn = pop.indexOf('return;', indexedReturn);
  const acceptTarget = pop.indexOf('historyIndexRef.current = target.index', blockedReturn);
  const renderTarget = pop.indexOf('setLocation(browserLocation())', blockedReturn);
  assert.ok(
    blockedStart >= 0
      && indexedReturn > blockedStart
      && blockedReturn > indexedReturn
      && acceptTarget > blockedReturn
      && renderTarget > blockedReturn,
    'every blocked pop, even after multiple Back steps, must return by the exact index delta before accepting a route',
  );
});

test('DNS unlock is owned by the exact request and happens only after terminal truth is adopted', () => {
  const completion = section(
    dnsSource,
    'const completeGuardedVerification =',
    'useEffect(() => {',
  );
  assert.match(completion, /decoded\.operation\?\.request_id === guard\.requestID/,
    'a terminal snapshot for another operation must not release this guard');
  assert.match(completion, /exactOperation\?\.status === 'succeeded'/);
  assert.match(completion, /decoded\.operation\.target_engine === guard\.target/);

  const adopt = completion.indexOf('setSnapshot(decoded)');
  const notify = completion.indexOf('onSnapshotChange?.(decoded)', adopt);
  const release = completion.indexOf(
    "current?.requestID === guard.requestID ? null : current",
    notify,
  );
  assert.ok(adopt >= 0 && notify > adopt && release > notify,
    'verified terminal state must be adopted before the exact guard can unlock');
});

test('failed and rolled-back DNS terminals never take a success path', () => {
  const completion = section(
    dnsSource,
    'const completeGuardedVerification =',
    'useEffect(() => {',
  );
  assert.match(completion, /if \(operationSucceeded\)/,
    'success UI must be controlled only by the exact succeeded predicate');
  assert.doesNotMatch(completion, /replayVerified\s*\|\|\s*operationSucceeded/,
    'an idempotent replay proves identity, not success');

  const commit = section(dnsSource, 'const commitSwitch = async', '\n    return (');
  const successDecode = commit.indexOf('decodeDNSEngineSnapshot(');
  const catchStart = commit.indexOf('} catch {', successDecode);
  const successfulResponse = commit.slice(successDecode, catchStart);
  assert.doesNotMatch(successfulResponse, /showToast\('success'/,
    'a 2xx response can still describe failed or rolled_back; it must use exact terminal verification');
  assert.match(successfulResponse, /completeGuardedVerification\(decoded/,
    'the direct POST result and recovery polling must share the same exact terminal predicate');
});

test('the switch mutation POST is single-shot and ambiguous outcomes use exact read-only status', () => {
  const boundedRequest = section(
    dnsSource,
    'async function submitDNSEngineSwitch',
    'function engineName',
  );
  assert.match(boundedRequest, /const requestController = new AbortController\(\)/);
  assert.match(boundedRequest, /fetch\('\/api\/v1\/dns\/engine\/switch'/);
  assert.match(boundedRequest,
    /setTimeout\([\s\S]*requestController\.abort\(\)[\s\S]*dnsEngineStatusRequestTimeoutMs/);
  assert.match(boundedRequest, /signal: requestController\.signal/);
  const consumeBody = boundedRequest.indexOf('const bodyText = await response.text()');
  const clearDeadline = boundedRequest.indexOf('clearTimeout(requestTimeout)');
  assert.ok(consumeBody >= 0 && clearDeadline > consumeBody,
    'the deadline must remain active until the complete response body is consumed');
  assert.match(boundedRequest, /readApiError\(new Response\(bodyText\)\)/,
    'only a fully consumed error envelope may prove a pre-persist refusal');
  assert.match(boundedRequest, /finally \{[\s\S]*clearTimeout\(requestTimeout\)/);

  const calls = dnsSource.match(/submitDNSEngineSwitch\(/g) ?? [];
  assert.equal(calls.length, 2,
    'the helper definition and initial commit must be the only switch mutation call sites');
  assert.doesNotMatch(dnsSource, /replayRequest/,
    'a lost response must never replay the DNS switch POST');

  const commit = section(dnsSource, 'const commitSwitch = async', '\n    return (');
  assert.match(commit,
    /catch \{[\s\S]*returnedGuard\?\.requestID !== requestID[\s\S]*mode: 'verifying'/,
    'an initial timeout retains the exact local guard without recreating a stale guard');

  const polling = section(dnsSource, 'const stopAtDeadline =', '\n    useEffect(() => {\n        if (actionsLocked)');
  assert.match(polling, /const decoded = await refresh\(true\)/,
    'recovery uses the read-only authoritative snapshot, and nothing in the loop can replace it with another answer');
  assert.match(polling,
    /decoded\?\.operation\?\.request_id === requestID[\s\S]*decoded\.operation\.target_engine === target/,
    'only the exact request and target are adopted');
  assert.doesNotMatch(polling, /submitDNSEngineSwitch|\/dns\/engine\/switch/,
    'the verification loop cannot issue the mutation POST');
});

test('DNS verification is bounded, stalls visibly, and releases only the appropriate global lease', () => {
  assert.match(dnsSource, /const dnsEngineGuardStalledAfterMs = 2 \* 60_000/);
  assert.match(dnsSource, /const dnsEngineGuardMaxElapsedMs = 31 \* 60_000/);
  assert.match(dnsSource, /const dnsEngineGuardMaxAttempts = 180/);

  // Polling only reads (docs/OPERATION-GUIDANCE.md, decision of 2026-10-09).
  // Until then this loop sent the reconcile POST by itself once an exact
  // operation had recorded nothing for two minutes, at most three times and a
  // minute apart, and this test pinned those bounds. The request changes the
  // server's saved record and is audited under the signed-in administrator, so
  // the bound is now the strictest one: a timer sends it zero times. What the
  // loop still decides is WHEN the request may be offered to the person at the
  // screen - the same condition as before, an exact operation that stalled.
  const polling = section(dnsSource, 'const stopAtDeadline =', '\n    useEffect(() => {\n        if (actionsLocked)');
  assert.doesNotMatch(polling, /reconcileAndRefresh|\/dns\/engine\/reconcile|checkStalledOperation|\bfetch\(/,
    'the verification loop reads through refresh(true) and sends nothing else');
  assert.doesNotMatch(dnsSource, /dnsEngineGuardMaxReconcileAttempts|dnsEngineGuardReconcileDelayMs|reconcileAttempts|lastReconcileAt/,
    'no automatic reconcile budget is left to spend');
  assert.match(polling,
    /const reconcileOffered = durableStalled && exactOperation !== null;/,
    'the reconcile request is offered only after an exact durable operation stalls');
  assert.match(polling, /if \(guard\.reconciling\) \{\s*schedule\(dnsEngineGuardPollDelayMs\);\s*return;\s*\}/,
    'a tick does not read beside the request the owner sent');

  // The one way the request leaves the browser while a change is tracked: the
  // owner's action - in the lock while the exact operation is stalled, and on
  // the card once the loop has stopped at its safety limit (the lock is
  // released there, and without it nothing could send the request any more).
  // It is single-flight, tied to the exact operation, shares the exact
  // terminal predicate, and cannot reach the switch request.
  const owner = section(dnsSource, 'const checkStalledOperation = async', 'checkStalledOperationRef.current = checkStalledOperation;');
  assert.match(owner,
    /if \(!guard \|\| \(guard\.mode !== 'stalled' && guard\.mode !== 'deadline'\) \|\| !guard\.reconcileOffered \|\| guard\.reconciling\) return;[\s\S]*holdOperationGuard\(\{ \.\.\.guard, reconciling: true \}\);[\s\S]*await reconcileAndRefresh\(true\)/,
    'one request at a time, and only for an exact operation that is stalled or past the safety limit');
  assert.match(owner,
    /if \(!settled\.reconcileOffered \|\| \(settled\.mode !== 'stalled' && settled\.mode !== 'deadline'\)\) \{\s*holdOperationGuard\(settled\);\s*return;\s*\}/,
    'an operation that moved on while the request was out is not described as still offered');
  assert.match(owner,
    /current\.requestID !== guard\.requestID \|\| current\.target !== guard\.target\) return;[\s\S]*completeGuardedVerification\(decoded, settled\)/,
    'the answer is applied only to the same request and target, through the exact terminal predicate');
  assert.doesNotMatch(owner, /submitDNSEngineSwitch|\/dns\/engine\/switch|setTimeout|setInterval/,
    'the owner action cannot issue the mutation POST and schedules nothing');
  assert.equal((dnsSource.match(/reconcileAndRefresh\(/g) ?? []).length, 2,
    'the reconcile request has two call sites: the Refresh button and the owner action in the lock');
  assert.equal((dnsSource.match(/checkStalledOperationRef\.current\(/g) ?? []).length, 2,
    'the owner action is reachable from two places, both a button: the lock, and the card past the safety limit');
  assert.match(dnsSource,
    /action: guard\.mode === 'stalled' && guard\.reconcileOffered\s*\? \{\s*label: et\('dnsEngine\.guard\.checkNow'\),\s*busy: guard\.reconciling,\s*onAct: \(\) => \{ void checkStalledOperationRef\.current\(\); \},\s*\}\s*: undefined,/,
    'the first is the action of the lock view, present only while the exact operation is stalled');
  // Past the safety limit the loop reads no more and the lock is released.
  // Until 9 Oct 2026 the loop had by then sent its own requests; now the
  // owner's check must stay reachable, or the saved record of an accepted
  // change whose worker is gone could never be closed from the interface.
  assert.match(polling,
    /mode: 'deadline',\s*attempts,\s*reconcileOffered: guard\.operation !== null,\s*reconcileNote: undefined,/,
    'at the safety limit the check stays offered for an exact operation, and only for one');
  assert.match(dnsSource,
    /const deadlineCheckOffered = operationGuard\?\.mode === 'deadline' && operationGuard\.reconcileOffered;/);
  assert.match(dnsSource,
    /\{deadlineCheckOffered && operationGuard && \([\s\S]{0,1500}loading=\{operationGuard\.reconciling\}\s*onClick=\{\(\) => \{ void checkStalledOperationRef\.current\(\); \}\}/,
    'the second is a button on the card, drawn only past the safety limit');
  assert.match(dnsSource, /trackingDelayed=\{trackingDelayed && !deadlineCheckOffered\}/,
    'the card does not say "tracking continues" beside "stopped checking"');
  assert.match(overlaySource,
    /view\?\.action && \([\s\S]*<Button variant="secondary" loading=\{view\.action\.busy\} onClick=\{view\.action\.onAct\}>/,
    'the lock draws the action as a button a person presses');
  assert.match(polling, /mode: durableStalled \? 'stalled' : 'verifying'/);
  assert.match(polling,
    /schedule\(durableStalled \? dnsEngineGuardSlowPollDelayMs : dnsEngineGuardPollDelayMs\)/);
  assert.match(polling, /attempts > 0 && \(/,
    'a reloaded deadline marker receives one fresh authoritative read first');
  assert.match(polling,
    /const decoded = await refresh\(true\)[\s\S]*completeGuardedVerification[\s\S]*Date\.now\(\) - guard\.startedAt >= dnsEngineGuardMaxElapsedMs[\s\S]*stopAtDeadline/,
    'after that fresh authoritative read, an overdue non-terminal marker must immediately stop and release navigation');

  const guardView = section(dnsSource, 'const guardView =', 'const holdOperationGuard =');
  assert.match(guardView,
    /busy: !\['stalled', 'deadline', 'recovery_required'\]\.includes\(guard\.mode\)/);
  assert.match(guardView, /details: \[/);
  assert.match(guardView, /message: guard\.trackingMessage/);

  const hold = section(dnsSource, 'const holdOperationGuard =', 'useLayoutEffect(() => {');
  assert.match(hold, /guard\.mode !== 'deadline' && guard\.mode !== 'recovery_required'/);
  assert.match(hold, /operationLeaseRef\.current\?\.release\(\)/,
    'hard terminal/action-required states must not trap navigation');

  const completion = section(dnsSource, 'const completeGuardedVerification =', 'useEffect(() => {');
  const recovery = section(completion, "exactOperation?.status === 'recovery_required'", 'const operationSucceeded');
  assert.match(recovery, /clearDNSOperationMarker\(guard\.requestID\)/);
  assert.match(recovery, /mode: 'recovery_required'/);
  assert.doesNotMatch(recovery, /setOperationGuard\(null\)/);

  assert.match(overlaySource, /const busy = view\?\.busy/);
  assert.match(overlaySource, /busy[\s\S]*LoaderCircle[\s\S]*AlertTriangle/);
  assert.match(overlaySource, /view\?\.details && view\.details\.length > 0/);
  assert.match(overlaySource, /view\?\.message/);
});
