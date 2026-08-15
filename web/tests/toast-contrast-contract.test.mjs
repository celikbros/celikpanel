import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const toastSource = readFileSync(
  new URL('../src/components/Toast.tsx', import.meta.url),
  'utf8',
);

test('toast variants use the foreground paired with their semantic background', () => {
  for (const expected of [
    /case 'success': return 'bg-success\/90 border-success text-success-fg'/,
    /case 'error': return 'bg-danger\/90 border-danger text-danger-fg'/,
    /case 'warning': return 'bg-warning\/90 border-warning text-warning-fg'/,
    /case 'info': return 'bg-primary\/90 border-primary text-primary-fg'/,
  ]) {
    assert.match(toastSource, expected);
  }

  assert.doesNotMatch(toastSource, /bg-success\/90 border-success text-success(?:['\\s])/);
  assert.doesNotMatch(toastSource, /bg-danger\/90 border-danger text-danger(?:['\\s])/);
  assert.doesNotMatch(toastSource, /bg-warning\/90 border-warning text-warning(?:['\\s])/);
  assert.doesNotMatch(toastSource, /bg-primary\/90 border-primary text-primary(?:['\\s])/);
});

test('toast close control has an explicit accessible name', () => {
  assert.match(
    toastSource,
    /type=\{'button'\}\s+aria-label=\{'Dismiss notification'\}/,
  );
  assert.match(toastSource, /text-inherit/);
  assert.match(toastSource, /focus-visible:ring-current/);
});
