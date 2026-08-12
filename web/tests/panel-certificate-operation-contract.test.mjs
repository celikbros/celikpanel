import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const settingsSource = readFileSync(
  new URL('../src/components/Settings.tsx', import.meta.url),
  'utf8',
);

function panelCertificatePanelSource() {
  const start = settingsSource.indexOf('function PanelCertificatePanel()');
  const end = settingsSource.indexOf('function TwoFactorPanel()', start);
  assert.ok(start >= 0 && end > start);
  return settingsSource.slice(start, end);
}

test('panel certificate POST persists an exact recovery identity before submission', () => {
  const source = panelCertificatePanelSource();
  assert.match(settingsSource, /crypto\.getRandomValues\(bytes\)/);
  assert.match(settingsSource, /\^\[a-f0-9\]\{32\}\$/);
  assert.match(source, /issueInFlightRef\.current \|\| pendingOperation !== null \|\| restarting \|\| !domain/);
  assert.match(source, /disabled=\{busy \|\| pendingOperation !== null \|\| restarting \|\| !domain\}/);
  const store = source.indexOf('storePanelCertificateMarker(marker)');
  const submit = source.indexOf("fetch('/api/v1/panel/certificate'", store);
  assert.ok(store >= 0 && submit > store, 'marker must be durable before POST');
  assert.match(source, /body: JSON\.stringify\(\{ domain, request_id: marker\.request_id \}\)/);
});

test('ambiguous certificate POST responses retain the marker for exact polling', () => {
  const source = panelCertificatePanelSource();
  const failureStart = source.indexOf('if (!res.ok)');
  const failureEnd = source.indexOf('const operation = decodePanelCertificateOperation', failureStart);
  assert.ok(failureStart >= 0 && failureEnd > failureStart);
  const failure = source.slice(failureStart, failureEnd);
  assert.match(failure, /res\.status !== 401/);
  assert.match(failure, /res\.status !== 408/);
  assert.match(failure, /res\.status !== 429/);
  assert.match(failure, /if \(rejectionIsDefinitive\) \{[\s\S]*clearPanelCertificateMarker\(\)/);
  assert.doesNotMatch(failure, /if \(!res\.ok\) \{\s*clearPanelCertificateMarker\(\)/);
  assert.match(source, /A lost POST response is not proof that the durable row was not/);
});

test('certificate polling accepts only the exact durable operation identity', () => {
  assert.match(settingsSource, /value\.request_id !== marker\.request_id/);
  assert.match(settingsSource, /value\.kind !== 'panel_certificate_issue'/);
  assert.match(settingsSource, /value\.service_id !== marker\.domain/);
  const source = panelCertificatePanelSource();
  assert.match(source, /service\/operation\?request_id=\$\{encodeURIComponent\(exactMarker\.request_id\)\}/);
  assert.match(source, /if \(operation === null\) \{[\s\S]*schedule\(\)/);
  assert.match(source, /if \(operation\.status === 'failed'\)/);
  assert.match(source, /if \(operation\.status !== 'succeeded'\)/);
});
