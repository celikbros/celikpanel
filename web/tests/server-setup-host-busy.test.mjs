import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { setupHostBusyKey } from '../src/lib/serverSetupGuidance.ts';
import { enScreens } from '../src/i18n/screens/en.ts';
import { trScreens } from '../src/i18n/screens/tr.ts';

// upd8 F1 follow-up: a setup step refused because the host was busy reaches the
// wizard as HOST_MUTATION_BUSY with the Panel's reason sentence and no reason
// field. Its headline names the reason, who acts, the next action and how setup
// resumes instead of the generic "A required check needs attention".

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const httperr = read('../../cmd/panel/httperr.go');
const setup = read('../src/components/ServerSetup.tsx');

// The Panel's sentences, joined from their Go string concatenation.
const panelSentence = (reason) => {
  const match = httperr.match(new RegExp(String.raw`transport\.${reason}:\s*((?:"[^"]*"\s*\+?\s*)+),`));
  assert.ok(match, `httperr.go has no sentence for ${reason}`);
  return [...match[1].matchAll(/"([^"]*)"/g)].map((part) => part[1]).join('');
};
const generic = httperr.match(/const hostMutationBusyGenericMessage = "([^"]+)"/)[1];

test('each Panel reason sentence selects its own setup headline', () => {
  const expected = {
    HostMutationReasonPackageManager: 'setup.blocker.packageBusy',
    HostMutationReasonAgentMutation: 'setup.blocker.changeBusy',
    HostMutationReasonPanelOperation: 'setup.blocker.changeBusy',
    HostMutationReasonHostLock: 'setup.blocker.hostHeld',
  };
  for (const [reason, key] of Object.entries(expected)) {
    assert.equal(setupHostBusyKey(panelSentence(reason)), key, reason);
  }
  for (const message of [generic, '', undefined, 'something else']) {
    assert.equal(setupHostBusyKey(message), 'setup.blocker.hostBusy', String(message));
  }
});

test('the wizard reads the busy refusal with its sentence', () => {
  assert.match(setup, /if \(code === 'HOST_MUTATION_BUSY'\) return t\(setupHostBusyKey\(message\)\);/);
  assert.match(setup, /failureText\(execution\.error\.code, execution\.error\.message\)/);
});

test('busy headlines exist in both languages and say how setup resumes', () => {
  for (const key of ['setup.blocker.packageBusy', 'setup.blocker.changeBusy', 'setup.blocker.hostHeld', 'setup.blocker.hostBusy']) {
    assert.ok(enScreens[key] && trScreens[key], key);
    assert.match(enScreens[key], /^Setup stopped because .*choose(s)? Review a revised plan and start(s)? setup again.*[Ss]teps that already finished are kept/, key);
    assert.match(trScreens[key], /^Kurulum durdu, çünkü .*Düzeltilmiş planı incele’yi seçip kurulumu yeniden başlat.*[Tt]amamlanan adımlar korunur/, key);
  }
  assert.match(enScreens['setup.blocker.packageBusy'], /package task .* such as an automatic update\. Nothing is wrong\. Wait for that task to finish/);
  // The hold that waiting does not clear must not be told to wait.
  assert.match(enScreens['setup.blocker.hostHeld'], /waiting will not clear it\. The server administrator restarts the server/);
  assert.doesNotMatch(enScreens['setup.blocker.hostHeld'], /\bWait\b/);
  assert.doesNotMatch(trScreens['setup.blocker.hostHeld'], /bekleyin/);
});
