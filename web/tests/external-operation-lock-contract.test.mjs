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
  assert.match(completion, /decoded\.operation\.status === 'succeeded'/);
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

test('reload replay releases only an exact definitive pre-persist refusal', () => {
  const verification = section(
    dnsSource,
    "if (guard.replayRequest) {",
    "} else {\n                decoded = await reconcileAndRefresh(true);",
  );

  assert.match(verification, /!apiError\.code/,
    'coded outcomes may represent rollback or recovery and must remain guarded');
  assert.match(verification, /apiError\.mutationApplied !== true/);
  assert.match(verification, /apiError\.partialSuccess !== true/);
  assert.match(verification, /result\.status === 400 \|\| result\.status === 409/,
    'only the endpoint\'s definitive client refusal statuses may release recovery');
  assert.match(verification,
    /operationGuardRef\.current\?\.requestID !== guard\.requestID/,
    'a stale replay must never release a newer operation guard');

  const exactProof = verification.indexOf(
    'operationGuardRef.current?.requestID !== guard.requestID',
  );
  const clear = verification.indexOf('clearDNSOperationMarker(guard.requestID)', exactProof);
  const release = verification.indexOf('operationLeaseRef.current?.release()', clear);
  assert.ok(exactProof >= 0 && clear > exactProof && release > clear,
    'marker and interaction lease cleanup require exact live request ownership first');
});

test('initial commit and recovery replay share a bounded switch request without unlocking on timeout', () => {
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
  assert.equal(calls.length, 3,
    'the helper definition, initial commit, and exact recovery replay must be the only call sites');

  const verification = section(
    dnsSource,
    "if (guard.replayRequest) {",
    "} else {\n                decoded = await reconcileAndRefresh(true);",
  );
  assert.match(verification,
    /catch \{[\s\S]*schedule\(\);[\s\S]*return;/,
    'a replay timeout remains guarded and schedules another exact attempt');

  const commit = section(dnsSource, 'const commitSwitch = async', '\n    return (');
  assert.match(commit,
    /catch \{[\s\S]*holdOperationGuard\(\{[\s\S]*requestID,[\s\S]*mode: 'verifying'[\s\S]*replayRequest: requestBody/,
    'an initial timeout must retain the exact marker and transition to guarded replay');
});
