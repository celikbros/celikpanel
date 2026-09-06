import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// Two defects a browser found and the tests did not (register R-047). Both are
// layout, and layout is exactly what a source test can pin badly, so these
// assert the mechanism rather than the class string: a dialogue whose actions
// are outside the thing that scrolls, and a row that never end-aligns content
// it cannot fit.
const source = readFileSync(
  new URL('../src/components/ServiceList.tsx', import.meta.url),
  'utf8',
);

function mailProfileDialog() {
  const start = source.indexOf("id='mail-profile-confirm'");
  assert.ok(start > 0, 'the mail install dialogue is no longer identifiable');
  const end = source.indexOf('function InstallServiceDialog', start);
  assert.ok(end > start);
  return source.slice(start, end);
}

// The plan is long. When the whole dialogue scrolled, its confirm button sat
// below the fold at 1440x900 the moment it opened, and an operator who cannot
// see the action concludes the dialogue is broken.
//
// The shape that fixes it is no longer written here: it is the shared Dialog,
// where R-059 moved it after the same defect turned up in the DNS review
// dialogue. What this file still owns is that THIS dialogue uses it, and which
// of its parts go where — the plan in the body that scrolls, the
// acknowledgement beside the primary it gates. The shape itself is pinned in
// dialog-shape-contract.test.mjs.
//
// Bu kusuru gideren biçim artık burada yazılı değil: paylaşılan Dialog'dur.
// Burada kalan, bu diyaloğun onu kullandığı ve hangi parçasının nereye
// gittiğidir.
test('the mail install dialogue keeps its actions out of the part that scrolls', () => {
  const dialog = mailProfileDialog();

  // It is the shared dialogue, so it is a bounded column and not one scrolling
  // box — and it cannot quietly stop being one.
  const openTagEnd = dialog.indexOf('\n        >');
  assert.ok(openTagEnd > 0, 'the mail dialogue no longer opens a Dialog');
  assert.ok(
    source.lastIndexOf('<Dialog', source.indexOf("id='mail-profile-confirm'")) > 0,
    'the plan is not rendered by the shared Dialog',
  );
  assert.doesNotMatch(dialog, /fixed inset-0/, 'the dialogue must not build its own overlay again');
  assert.doesNotMatch(dialog, /max-h-\[90vh\]/, 'the bound belongs to the shared dialogue, not here');

  // The acknowledgement is in the footer slot, beside the primary it gates: a
  // disabled primary always has its reason on the same line of sight.
  const footerLeadAt = dialog.indexOf('footerLead={');
  const actionsAt = dialog.indexOf('actions={');
  const bodyAt = openTagEnd;
  assert.ok(footerLeadAt > 0 && actionsAt > footerLeadAt && bodyAt > actionsAt);
  const acknowledgementAt = dialog.indexOf('services.mailProfiles.plan.acknowledgement');
  const confirmAt = dialog.indexOf('services.mailProfiles.plan.confirm.');
  assert.ok(
    acknowledgementAt > footerLeadAt && acknowledgementAt < actionsAt,
    'the acknowledgement must be in the pinned footer',
  );
  assert.ok(confirmAt > actionsAt && confirmAt < bodyAt, 'the confirm must be in the action row');

  // The plan itself is still all there, in the body that scrolls: nothing was
  // shrunk to make room.
  for (const key of [
    'services.mailProfiles.plan.component',
    'services.mailProfiles.plan.serviceImpact',
    'services.mailProfiles.plan.firewallImpact',
    'services.mailProfiles.plan.tls',
    'services.mailProfiles.hostname.title',
  ]) {
    assert.ok(dialog.indexOf(key) > bodyAt, `${key} left the plan`);
  }
});

// justify-end on a row that cannot fit pushes the overflow off the LEFT edge,
// where nothing can reach it. At 390px that clipped the installed/catalogue
// buttons against the viewport instead of wrapping them.
test('the components toolbar never end-aligns a row it cannot fit', () => {
  const rowAt = source.indexOf('className="ml-auto flex w-full flex-wrap items-center');
  assert.ok(rowAt > 0, 'the components toolbar row is no longer identifiable');
  const row = source.slice(rowAt, source.indexOf('>', rowAt));
  assert.match(
    row,
    /justify-start/,
    'the toolbar must start at the left margin before it has room to end-align',
  );
  assert.match(row, /sm:justify-end/, 'it should still end-align once there is room');

  const clusterAt = source.indexOf('className="flex flex-wrap items-center gap-x-4', rowAt);
  assert.ok(
    clusterAt > rowAt && clusterAt < rowAt + 2000,
    'the view cluster must be allowed to wrap rather than overflow',
  );

  // Both view buttons are still one segmented control, not two loose buttons.
  const installedAt = source.indexOf('services.viewInstalled', clusterAt);
  const catalogAt = source.indexOf('services.viewCatalog', clusterAt);
  assert.ok(installedAt > 0 && catalogAt > installedAt);
  const control = source.slice(clusterAt, catalogAt);
  assert.match(control, /inline-flex overflow-hidden rounded-lg border border-border-strong/);
});
