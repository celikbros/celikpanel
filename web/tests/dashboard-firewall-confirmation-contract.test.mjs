import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const dashboardSource = readFileSync(
  new URL('../src/components/Dashboard.tsx', import.meta.url),
  'utf8',
);

test('dashboard firewall enable CTA requests confirmation and persistence stays explicit', () => {
  const requestStart = dashboardSource.indexOf('const requestTurnOnFirewall = () => {');
  const requestEnd = dashboardSource.indexOf('\n    };', requestStart);
  assert.ok(requestStart >= 0 && requestEnd > requestStart);
  const requestBody = dashboardSource.slice(requestStart, requestEnd);

  assert.match(requestBody, /setFirewallConfirmationOpen\(true\)/);
  assert.doesNotMatch(requestBody, /fetch\('\/api\/v1\/firewall'/);
  assert.equal(
    dashboardSource.match(/onAct: requestTurnOnFirewall/g)?.length,
    1,
    'the attention CTA must request confirmation instead of mutating directly',
  );
  assert.match(dashboardSource, /done: fw\?\.enabled === true && fw\.persistence_state === 'ready'/);
  assert.match(dashboardSource, /cta: fw\?\.enabled \? 'dashboard\.saveFirewall' : 'firewall\.turnOn'/);
  assert.match(dashboardSource, /onAct: fw\?\.enabled \? undefined : requestTurnOnFirewall/);
});

test('dashboard firewall confirmation fails closed on host mutation readiness', () => {
  assert.match(dashboardSource, /fetch\('\/api\/v1\/host-mutation-readiness', \{/);
  assert.match(dashboardSource, /method: 'GET'/);
  assert.match(dashboardSource, /cache: 'no-store'/);
  assert.match(dashboardSource, /HOST_MUTATION_UNAVAILABLE/);
  assert.match(dashboardSource, /reason: 'state_unverified'/);
  assert.match(dashboardSource, /if \(hostMutationReadiness\?\.ready !== true \|\| fwBusy\) return/);
  assert.match(
    dashboardSource,
    /disabled=\{busy \|\| readiness\?\.ready !== true \|\| \(noSSHService && !noSSHAcknowledged\)\}/,
  );
});

test('dashboard performs the authoritative POST only from an accessible confirmation dialog', () => {
  assert.match(dashboardSource, /<DashboardFirewallConfirmationDialog/);
  assert.match(dashboardSource, /onConfirm=\{\(\) => void turnOnFirewall\(\)\}/);
  assert.match(dashboardSource, /role="dialog"/);
  assert.match(dashboardSource, /aria-modal="true"/);
  assert.match(dashboardSource, /aria-labelledby="dashboard-firewall-confirm-title"/);
  assert.match(dashboardSource, /aria-describedby="dashboard-firewall-confirm-description"/);
  assert.match(dashboardSource, /autoFocus/);
  assert.match(dashboardSource, /if \(event\.key === 'Escape' && !busy\) onCancel\(\)/);
  assert.match(dashboardSource, /t\('firewall\.confirm\.enable\.title'\)/);
  assert.match(dashboardSource, /t\('firewall\.confirm\.enable\.description'\)/);
  assert.match(dashboardSource, /t\('services\.mutationReadiness\.title'\)/);

  const postStart = dashboardSource.indexOf('const turnOnFirewall = async () => {');
  const postEnd = dashboardSource.indexOf('\n    };', postStart);
  assert.ok(postStart >= 0 && postEnd > postStart);
  const postBody = dashboardSource.slice(postStart, postEnd);
  assert.match(postBody, /fetch\('\/api\/v1\/firewall'/);
  assert.match(postBody, /method: 'POST'/);
  assert.match(postBody, /readApiError\(r\)/);
  assert.match(postBody, /apiErrorText\(/);
  assert.doesNotMatch(postBody, /d\.error/);
});

// R-035. A server proven to have no SSH service has no door for the firewall to
// lock, so the dashboard offers the way forward under its own acknowledgement -
// never folded into the confirmation the operator already gave, and never
// offered for a probe that could not run.
test('dashboard firewall enable asks for its own no-SSH acknowledgement', () => {
  assert.match(dashboardSource, /readFirewallSSHReason\(fw\?\.ssh_discovery_reason\)/);
  assert.match(dashboardSource, /firewallNeedsNoSSHConsent = firewallSSHReason === 'no_ssh_service'/);
  assert.match(dashboardSource, /<FirewallNoSSHAcknowledgement/);
  assert.match(dashboardSource, /noSSHService=\{firewallNeedsNoSSHConsent\}/);
  assert.match(dashboardSource, /if \(firewallNeedsNoSSHConsent && !noSSHAcknowledged\) return/);
  assert.match(
    dashboardSource,
    /noSSHAcknowledged\s+\? \{ enabled: true, no_ssh_acknowledged: true \}\s+: \{ enabled: true \}/,
  );
  assert.match(dashboardSource, /firewall\.ssh\.no_ssh_service\.confirm/);
  assert.match(dashboardSource, /setNoSSHAcknowledged\(false\)/);
});
