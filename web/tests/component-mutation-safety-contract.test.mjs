import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const serviceSource = readFileSync(
  new URL('../src/components/ServiceList.tsx', import.meta.url),
  'utf8',
);
const operationSource = readFileSync(
  new URL('../src/components/ComponentOperation.tsx', import.meta.url),
  'utf8',
);
const enSource = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const trSource = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

function sourceSection(source, start, end) {
  const startIndex = source.indexOf(start);
  assert.ok(startIndex >= 0, `missing section start: ${start}`);
  const endIndex = source.indexOf(end, startIndex + start.length);
  assert.ok(endIndex > startIndex, `missing section end: ${end}`);
  return source.slice(startIndex, endIndex);
}

test('fresh Components visits open the full catalog and remember only an explicit installed choice', () => {
  assert.match(
    serviceSource,
    /typeof localStorage === 'undefined' \? false : localStorage\.getItem\(viewKey\) === 'installed'/,
  );
  assert.match(serviceSource, /localStorage\.setItem\(viewKey, installedOnly \? 'installed' : 'catalog'\)/);
});

test('component mutations are gated by fail-closed host readiness', () => {
  assert.match(serviceSource, /fetch\('\/api\/v1\/host-mutation-readiness'/);
  assert.match(serviceSource, /cache: 'no-store'/);
  assert.match(serviceSource, /hostMutationReadiness\?\.ready !== true/);
  assert.match(serviceSource, /HOST_MUTATION_BUSY/);
  assert.match(serviceSource, /HOST_MUTATION_UNAVAILABLE/);
  assert.match(serviceSource, /package_manager_active/);
  assert.match(serviceSource, /state_unverified/);
  assert.match(serviceSource, /<fieldset[\s\S]{0,120}disabled=\{mutationControlsDisabled\}/);
});

test('active-operation discovery is silent while mutations remain fail-closed', () => {
  assert.match(
    operationSource,
    /const interactionBlocked = \([\s\S]{0,240}submitting[\s\S]{0,240}operation !== null[\s\S]{0,240}refreshingCatalog[\s\S]{0,240}interactionBlocksRef\.current\.size > 0/,
  );
  assert.match(operationSource, /const locked = discoveringActive \|\| interactionBlocked/);
  assert.match(operationSource, /if \(!interactionBlocked\) return/);
  assert.match(operationSource, /\{interactionBlocked && createPortal\(/);
  assert.doesNotMatch(operationSource, /\{locked && createPortal\(/);
  assert.match(operationSource, /activeSyncInFlightRef\.current\s*\? 'services\.operation\.activeCheckInProgress'/);
});

test('background discovery uncertainty cannot publish or clear a real operation failure', () => {
  const discovery = sourceSection(
    operationSource,
    'const syncActiveOperation = async',
    'const onFocus = () =>',
  );
  const verifiedAbsence = sourceSection(
    discovery,
    '} else if (verifiedNoActive) {',
    '} else {',
  );
  const realFailure = sourceSection(
    operationSource,
    'const finishFailure = (error: ApiError) =>',
    'const adoptOperation =',
  );

  assert.doesNotMatch(discovery, /setFailure\(/, 'discovery must not use the operation failure channel');
  assert.match(discovery, /const keepDiscoveryFailClosed = \(\) =>/);
  assert.match(discovery, /lockedRef\.current = true;[\s\S]*setDiscoveringActive\(true\)/);
  assert.match(discovery, /activeDiscoveryRetryTimer = window\.setTimeout\([\s\S]*RETRY_DELAY_MS/);
  assert.match(verifiedAbsence, /lockedRef\.current = false/);
  assert.match(verifiedAbsence, /setDiscoveringActive\(false\)/);
  assert.match(verifiedAbsence, /setConnectionInterrupted\(false\)/);
  assert.doesNotMatch(verifiedAbsence, /setFailure\(null\)/);
  assert.match(realFailure, /setFailure\(error\)/, 'real operation failures must remain user-visible');
});

test('lifecycle, repair, runtime install and firewall actions require an explicit dialog confirmation', () => {
  assert.match(serviceSource, /requestComponentAction\(\{ kind: 'service'/);
  assert.match(serviceSource, /requestComponentAction\(\{ kind: 'instance'/);
  assert.match(serviceSource, /requestComponentAction\(\{ kind: 'repair'/);
  assert.match(serviceSource, /requestComponentAction\(\{ kind: 'install-package'/);
  assert.match(serviceSource, /requestComponentAction\(\{ kind: 'install-node'/);
  assert.match(serviceSource, /<ComponentActionConfirmationDialog/);
  assert.match(serviceSource, /role='dialog'/);
  assert.match(serviceSource, /aria-modal='true'/);
  assert.match(serviceSource, /autoFocus onClick=\{onCancel\}/);
  assert.match(serviceSource, /if \(event\.key === 'Escape'\) onCancel\(\)/);

  assert.match(serviceSource, /requestAction\('save'\)/);
  assert.match(serviceSource, /requestAction\(st\.enabled \? 'disable' : 'enable'\)/);
  assert.match(serviceSource, /<FirewallActionConfirmationDialog/);
  assert.match(serviceSource, /<RepositoryActionConfirmationDialog/);
  assert.match(serviceSource, /setRepoAction\('enable'\)/);
  assert.match(serviceSource, /setRepoAction\('disable'\)/);
  assert.doesNotMatch(serviceSource, /!confirm\(/);
});

test('mail profiles stay visible but cannot start before verified DNS identity is ready', () => {
  assert.match(operationSource, /dns_identity_ready: boolean/);
  assert.match(operationSource, /typeof payload\.dns_identity_ready !== 'boolean'/);
  assert.match(serviceSource, /<MailProfileCards/);
  assert.match(serviceSource, /dnsIdentityReady=\{dnsIdentityReady\}/);
  assert.match(serviceSource, /!dnsIdentityReady && \(/);
  assert.match(serviceSource, /disabled=\{disabled \|\| !dnsIdentityReady \|\| !actionable\}/);
  assert.match(serviceSource, /navigate\('\/settings\?section=dns'\)/);
});

test('new confirmation, readiness, DNS and firewall copy stays in EN/TR parity', () => {
  const keys = [
    'err.HOST_MUTATION_BUSY',
    'err.HOST_MUTATION_UNAVAILABLE',
    'services.mutationReadiness.title',
    'services.mutationReadiness.checking',
    'services.mutationReadiness.panel_operation_active',
    'services.mutationReadiness.agent_mutation_active',
    'services.mutationReadiness.host_lock_busy',
    'services.mutationReadiness.package_manager_active',
    'services.mutationReadiness.state_unverified',
    'services.confirm.start.title',
    'services.confirm.start.description',
    'services.confirm.start.button',
    'services.confirm.stop.title',
    'services.confirm.stop.description',
    'services.confirm.stop.button',
    'services.confirm.restart.title',
    'services.confirm.restart.description',
    'services.confirm.restart.button',
    'services.confirm.repair.title',
    'services.confirm.repair.description',
    'services.confirm.repair.button',
    'services.confirm.install.title',
    'services.confirm.install.description',
    'services.confirm.install.button',
    'services.repo.confirm.enable.title',
    'services.repo.confirm.enable.description',
    'services.repo.confirm.enable.button',
    'services.repo.confirm.disable.title',
    'services.repo.confirm.disable.description',
    'services.repo.confirm.disable.button',
    'services.mailProfiles.configureDNS',
    'services.mailProfiles.dnsRequired',
    'services.mailProfiles.dnsRequiredShort',
    'firewall.changeFailed',
    'firewall.confirm.enable.title',
    'firewall.confirm.enable.description',
    'firewall.confirm.enable.button',
    'firewall.confirm.disable.title',
    'firewall.confirm.disable.description',
    'firewall.confirm.disable.button',
    'firewall.confirm.save.title',
    'firewall.confirm.save.description',
    'firewall.confirm.save.button',
  ];
  for (const key of keys) {
    assert.ok(enSource.includes(`'${key}':`), 'missing EN key ' + key);
    assert.ok(trSource.includes(`'${key}':`), 'missing TR key ' + key);
  }
});
