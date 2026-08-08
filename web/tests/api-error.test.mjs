import assert from 'node:assert/strict';
import test from 'node:test';

import { readApiError } from '../src/lib/apiError.ts';

test('readApiError keeps only string detail lines from JSON errors', async () => {
  const response = new Response(JSON.stringify({
    error: 'installation failed',
    code: 'service_install_failed',
    details: ['postgresql@18-main is inactive', null, 18, {}, '+2'],
  }), {
    status: 500,
    headers: { 'content-type': 'application/json' },
  });

  const error = await readApiError(response);

  assert.equal(error.message, 'installation failed');
  assert.equal(error.code, 'service_install_failed');
  assert.deepEqual(error.details, ['postgresql@18-main is inactive', '+2']);
});

test('readApiError ignores a non-array details value', async () => {
  const response = new Response(JSON.stringify({
    error: 'installation failed',
    details: 'not an array',
  }), { status: 500 });

  const error = await readApiError(response);

  assert.equal(error.details, undefined);
});
