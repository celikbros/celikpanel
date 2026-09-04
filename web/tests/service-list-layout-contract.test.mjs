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
  const start = source.indexOf("aria-labelledby='mail-profile-confirm-title'");
  assert.ok(start > 0, 'the mail install dialogue is no longer identifiable');
  const end = source.indexOf('function InstallServiceDialog', start);
  assert.ok(end > start);
  return source.slice(start, end);
}

// The plan is long. When the whole dialogue scrolled, its confirm button sat
// below the fold at 1440x900 the moment it opened, and an operator who cannot
// see the action concludes the dialogue is broken.
test('the mail install dialogue keeps its actions out of the part that scrolls', () => {
  const dialog = mailProfileDialog();

  // The dialogue is a bounded column, not one scrolling box.
  assert.match(dialog, /className='flex max-h-\[90vh\] w-full max-w-2xl flex-col/);
  assert.doesNotMatch(
    dialog.slice(0, dialog.indexOf('>')),
    /overflow-y-auto/,
    'the dialogue itself must not be the scroller',
  );

  const scrollerAt = dialog.indexOf("className='min-h-0 flex-1 overflow-y-auto");
  assert.ok(scrollerAt > 0, 'the plan is not in a scrollable body of its own');
  const footerAt = dialog.indexOf("className='shrink-0 border-t border-border");
  assert.ok(footerAt > scrollerAt, 'the actions are not in a footer of their own');

  // The confirm button, and the acknowledgement that gates it, are after the
  // scroller closes: a disabled primary always has its reason beside it.
  const confirmAt = dialog.indexOf('services.mailProfiles.plan.confirm.');
  const acknowledgementAt = dialog.indexOf('services.mailProfiles.plan.acknowledgement');
  assert.ok(
    acknowledgementAt > footerAt && confirmAt > footerAt,
    'the acknowledgement and the confirm must live in the fixed footer',
  );

  // The plan itself is still all there: nothing was shrunk to make room.
  for (const key of [
    'services.mailProfiles.plan.component',
    'services.mailProfiles.plan.serviceImpact',
    'services.mailProfiles.plan.firewallImpact',
    'services.mailProfiles.plan.tls',
    'services.mailProfiles.hostname.title',
  ]) {
    const at = dialog.indexOf(key);
    assert.ok(at > scrollerAt && at < footerAt, `${key} left the plan`);
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
