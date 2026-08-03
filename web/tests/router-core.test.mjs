import assert from 'node:assert/strict';
import test from 'node:test';

import { matchRoute } from '../src/router-core.ts';

test('matches the root route and an optional trailing slash', () => {
  assert.deepEqual(matchRoute('/', '/'), {});
  assert.deepEqual(matchRoute('/settings', '/settings/'), {});
});

test('extracts and decodes dynamic route parameters', () => {
  assert.deepEqual(
    matchRoute('/domains/:domainName', '/domains/biovision.health'),
    { domainName: 'biovision.health' },
  );
  assert.deepEqual(
    matchRoute('/services/:serviceId', '/services/php%2Dfpm'),
    { serviceId: 'php-fpm' },
  );
});

test('rejects different or partially matching paths', () => {
  assert.equal(matchRoute('/domains', '/domain'), null);
  assert.equal(matchRoute('/domains', '/domains/biovision.health'), null);
  assert.equal(matchRoute('/domains/:domainName', '/domains'), null);
});

test('rejects malformed encoded parameters without crashing', () => {
  assert.equal(matchRoute('/domains/:domainName', '/domains/%E0%A4%A'), null);
});

test('supports an explicit wildcard fallback', () => {
  assert.deepEqual(matchRoute('*', '/anything/here'), {});
});
