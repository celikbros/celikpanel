import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const source = readFileSync(
  new URL('../src/components/ServiceShell.tsx', import.meta.url),
  'utf8',
);

test('an absent-service install first click opens confirmation without starting installation', () => {
  const requestStart = source.indexOf('const requestInstall = () => {');
  const requestEnd = source.indexOf('    };', requestStart);
  assert.ok(requestStart >= 0, 'requestInstall handler is missing');
  assert.ok(requestEnd > requestStart, 'requestInstall handler is incomplete');
  const requestBody = source.slice(requestStart, requestEnd);
  assert.ok(requestBody.includes('setInstallConfirmationOpen(true)'));
  assert.ok(!requestBody.includes('startInstall'));
  assert.ok(source.includes('onClick={requestInstall}'));
  assert.ok(source.includes('<ServiceInstallConfirmationDialog'));

  const installStart = source.indexOf('const install = async () => {');
  const installEnd = source.indexOf('    };', installStart);
  assert.ok(installStart >= 0, 'confirmed install handler is missing');
  assert.ok(installEnd > installStart, 'confirmed install handler is incomplete');
  const installBody = source.slice(installStart, installEnd);
  const readinessGuard = installBody.indexOf('installReadiness?.ready !== true');
  const mutation = installBody.indexOf('startInstall');
  assert.ok(readinessGuard >= 0, 'exact readiness guard is missing');
  assert.ok(mutation > readinessGuard, 'exact readiness guard must run before startInstall');
});

test('install confirmation readiness is fetched without cache and fails closed', () => {
  assert.ok(source.includes("fetch('/api/v1/host-mutation-readiness'"));
  assert.ok(source.includes("cache: 'no-store'"));
  assert.ok(source.includes("typeof payload.ready !== 'boolean'"));
  assert.ok(source.includes('payload.code === undefined && payload.reason === undefined'));
  assert.ok(source.includes('HOST_MUTATION_BUSY'));
  assert.ok(source.includes('HOST_MUTATION_UNAVAILABLE'));
  assert.ok(source.includes('package_manager_active'));
  assert.ok(source.includes('state_unverified'));
  assert.ok(source.includes('unverifiedHostMutationReadiness()'));
  assert.ok(source.includes('installReadiness?.ready !== true'));
});

test('install confirmation is accessible, cancel-focused and uses shared localized copy', () => {
  // Role, modality, the title and description ids and Escape are the shared
  // Dialog's since R-059, and are pinned in dialog-shape-contract.test.mjs.
  // Naming the dialogue 'service-install-confirm' is what gives it those ids.
  //
  // Rol, kiplik, baslik/aciklama kimlikleri ve Escape R-059'dan beri paylasilan
  // Dialog'a aittir; diyalogu adlandirmak ona o kimlikleri verir.
  assert.ok(source.includes('<Dialog'));
  assert.ok(source.includes('id="service-install-confirm"'));
  assert.ok(source.includes('onDismiss={onCancel}'));
  assert.ok(source.includes('autoFocus'));
  assert.ok(source.includes('onClick={onCancel}'));
  assert.ok(source.includes('disabled={confirmDisabled}'));
  assert.ok(source.includes('services.confirm.install.title'));
  assert.ok(source.includes('services.confirm.install.description'));
  assert.ok(source.includes('services.confirm.install.button'));
  assert.ok(source.includes('services.mutationReadiness.checking'));
  assert.ok(source.includes('services.mutationReadiness.title'));
});
