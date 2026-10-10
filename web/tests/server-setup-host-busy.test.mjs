import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { setupHostBusyGuidance, setupHostBusyKey } from '../src/lib/serverSetupGuidance.ts';
import { enScreens } from '../src/i18n/screens/en.ts';
import { trScreens } from '../src/i18n/screens/tr.ts';

// upd8 F1 follow-up: a setup step refused because the host was busy reaches the
// wizard as HOST_MUTATION_BUSY. Its headline names the reason, who acts, the
// next action and how setup resumes instead of the generic "A required check
// needs attention". Since 2026-10-08 the failed step carries the Panel's typed
// reason and the headline is selected from it; the reason sentence stays the
// fallback for a record written before the reason existed.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const httperr = read('../../cmd/panel/httperr.go');
const contracts = read('../../internal/transport/service_contracts.go');
const operations = read('../../cmd/panel/service_operations.go');
const setupOperations = read('../../cmd/panel/server_setup_operations.go');
const agentRPC = read('../../cmd/agent/service_mutation_rpc.go');
const setup = read('../src/components/ServerSetup.tsx');
const setupOperation = read('../src/lib/serverSetupOperation.ts');

// The stable reason code behind a transport constant name.
const reasonCode = (name) => {
  const match = contracts.match(new RegExp(String.raw`\b${name}\s*=\s*"([a-z_]+)"`));
  assert.ok(match, `service_contracts.go has no ${name}`);
  return match[1];
};

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

test('the typed reason selects the headline, whatever the sentence says', () => {
  const expected = {
    HostMutationReasonPackageManager: 'setup.blocker.packageBusy',
    HostMutationReasonAgentMutation: 'setup.blocker.changeBusy',
    HostMutationReasonPanelOperation: 'setup.blocker.changeBusy',
    HostMutationReasonHostLock: 'setup.blocker.hostHeld',
  };
  for (const [name, key] of Object.entries(expected)) {
    const reason = reasonCode(name);
    // The reason alone is enough: no sentence, the generic one, a reworded one.
    for (const message of [undefined, '', generic, 'A reworded sentence the browser has never seen.']) {
      assert.equal(setupHostBusyKey(message, reason), key, `${name} with ${String(message)}`);
    }
  }
  // The reason wins over a sentence that would select another headline.
  assert.equal(
    setupHostBusyKey(panelSentence('HostMutationReasonPackageManager'), reasonCode('HostMutationReasonAgentMutation')),
    'setup.blocker.changeBusy',
  );
  // A reason this browser does not know falls back to the sentence, then to the
  // text that covers every reason; an object key is never read as a reason.
  assert.equal(setupHostBusyKey(panelSentence('HostMutationReasonHostLock'), reasonCode('HostMutationReasonStateUnverified')), 'setup.blocker.hostHeld');
  for (const reason of ['', 'something_new', 'constructor', 'toString', reasonCode('HostMutationReasonStateUnverified')]) {
    assert.equal(setupHostBusyKey(generic, reason), 'setup.blocker.hostBusy', reason);
    assert.equal(setupHostBusyKey(undefined, reason), 'setup.blocker.hostBusy', reason);
  }
});

test('the Panel puts the typed reason on the failed setup step', () => {
  // The error the wizard reads has a reason field ...
  assert.match(operations, /Reason string `json:"reason,omitempty"`/);
  // ... filled for a child operation read back from its stored row ...
  assert.match(operations, /if errorCode\.String == errCodeHostMutationBusy \{\s*op\.Error\.Reason = hostMutationBusyReasonForMessage\(errorMessage\.String\)/);
  assert.match(setupOperations, /Detail: op\.Error\.Detail,\s*Reason: op\.Error\.Reason\}/);
  assert.match(setupOperations, /Detail: child\.Detail,\s*Reason: child\.Reason\}/);
  // ... and for a step the Agent refused directly.
  assert.match(setupOperations, /Message: classification\.Message,\s*Reason: classification\.Reason\}/);
  // Another Agent job owning the lease - the Panel's own short startup work
  // runs as one - is named by the Agent, so it reaches the wizard as
  // "another CelikPanel change is still running" and not as the generic text.
  assert.match(agentRPC, /\} else if errors\.Is\(err, errServiceMutationBusy\) \{[\s\S]*?response\.Reason = transport\.HostMutationReasonAgentMutation/);
  assert.equal(setupHostBusyKey(generic, reasonCode('HostMutationReasonAgentMutation')), 'setup.blocker.changeBusy');
});

test('the wizard reads the busy refusal with its reason and its sentence', () => {
  assert.match(setup, /const hostBusy = execution\?\.status === 'failed' && !reconnecting \? setupHostBusyGuidance\(execution\.error\) : null;/);
  assert.match(setup, /failureText\(execution\.error\.code, execution\.error\.message, execution\.error\.reason\)/);
  // Outside the lead block the reason still travels with its body.
  assert.match(setup, /const busy = setupHostBusyGuidance\(\{ code, message, reason \}\);\s*if \(busy\) return `\$\{t\(busy\.title\)\}\. \$\{t\(busy\.body\)\}`;/);
  // An unusable reason is dropped without hiding the durable execution.
  assert.match(setupOperation, /!optional\('reason', 64\)/);
});

// Browser inspection, 2026-10-08: the reason was the third thing on the screen,
// under two near-identical headings and two generic paragraphs, in the failure
// colour while it said "Nothing is wrong", and the button it named was below the
// step list. The reason is now the one heading; its body says who acts, the next
// action and how setup resumes; the action sits beside it.
test('the busy reason is the heading, and its body says how setup resumes', () => {
  for (const key of ['setup.blocker.packageBusy', 'setup.blocker.changeBusy', 'setup.blocker.hostHeld', 'setup.blocker.hostBusy']) {
    assert.ok(enScreens[key] && trScreens[key], key);
    assert.match(enScreens[`${key}Title`], /^Setup stopped: [a-z].*[^.]$/, key);
    assert.match(trScreens[`${key}Title`], /^Kurulum durdu: [a-zçğıöşü].*[^.]$/, key);
    assert.match(enScreens[key], /choose(s)? Review a revised plan and start(s)? setup again.*[Ss]teps that already finished are kept/, key);
    assert.match(trScreens[key], /Düzeltilmiş planı incele’yi seçip kurulumu yeniden başlat.*[Tt]amamlanan adımlar korunur/, key);
    // The heading has said it; the body does not say it again.
    assert.doesNotMatch(enScreens[key], /Setup stopped/, key);
    assert.doesNotMatch(trScreens[key], /Kurulum durdu/, key);
  }
  assert.match(enScreens['setup.blocker.packageBusyTitle'], /another package task is running on this server$/);
  assert.match(enScreens['setup.blocker.packageBusy'], /^The other task may be an automatic update\. Nothing is wrong\. Wait for it to finish/);
  assert.match(trScreens['setup.blocker.packageBusy'], /^Diğer işlem otomatik bir güncelleme olabilir\. Bu bir arıza değil\. Bitmesini bekleyin/);
  // The hold that waiting does not clear must not be told to wait.
  assert.match(enScreens['setup.blocker.hostHeld'], /^Waiting will not clear it\. The server administrator restarts the server/);
  assert.doesNotMatch(enScreens['setup.blocker.hostHeld'], /\bWait\b/);
  assert.doesNotMatch(trScreens['setup.blocker.hostHeld'], /bekleyin/);
});

test('a busy server leads the screen as a wait, never in the failure colour, with its action beside it', () => {
  assert.equal(setupHostBusyGuidance(null), null);
  assert.equal(setupHostBusyGuidance({ code: 'service_install_failed', message: 'x' }), null);
  const reasons = { HostMutationReasonPackageManager: 'packageBusy', HostMutationReasonAgentMutation: 'changeBusy', HostMutationReasonPanelOperation: 'changeBusy', HostMutationReasonHostLock: 'hostHeld' };
  for (const [name, key] of Object.entries(reasons)) {
    assert.deepEqual(setupHostBusyGuidance({ code: 'HOST_MUTATION_BUSY', message: generic, reason: reasonCode(name) }),
      { title: `setup.blocker.${key}Title`, body: `setup.blocker.${key}`, caution: key === 'hostHeld' }, name);
  }
  assert.deepEqual(setupHostBusyGuidance({ code: 'HOST_MUTATION_BUSY', message: generic }), { title: 'setup.blocker.hostBusyTitle', body: 'setup.blocker.hostBusy', caution: false });
  // One heading: the reason, in the place of the generic one.
  assert.match(setup, /const stateTitle = hostBusy \? hostBusy\.title : guidance && \(execution\?\.status !== 'running' \|\| confirmingPrevious\) \? guidance\.title : null;/);
  assert.match(setup, /reconnecting \? 'setup\.reconnecting' : stateTitle \?\? \(/);
  // The lead block: body and action, on the neutral surface, or the caution tone for a held server.
  const block = setup.slice(setup.indexOf('{hostBusy ? <div role="alert"'), setup.indexOf('</div> : guidance && <aside'));
  assert.ok(block.length > 0, 'the busy lead block is gone');
  assert.match(block, /hostBusy\.caution \? 'border-warning-mark\/40 bg-warning-mark\/10' : 'border-border bg-surface'/);
  assert.match(block, /<p className="[^"]*text-fg">\{t\(hostBusy\.body\)\}<\/p>\s*<Button variant="primary" className="mt-4" onClick=\{editPlan\}>\{t\('setup\.revise'\)\}<\/Button>/);
  assert.doesNotMatch(block, /danger|guidance\.messages/);
  // The generic guidance and the failure-coloured line are not drawn beside it; the technical details stay.
  assert.match(setup, /\{!hostBusy && \(!\(confirmingPrevious \|\| observingMailEnrollment \|\| planReopened\) \|\| !guidance\) && <p className=\{execution\.status === 'failed' \? 'text-danger' : confirmingPrevious \|\| waitingDNSPrerequisite \? 'text-fg-muted' : 'text-fg'\}>/);
  // The lead area comes before the step list.
  assert.ok(setup.indexOf('{hostBusy ? <div role="alert"') < setup.indexOf('execution?.steps.map('));
});
