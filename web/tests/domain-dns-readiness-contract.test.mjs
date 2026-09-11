import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const domains = readFileSync(new URL('../src/components/Domains.tsx', import.meta.url), 'utf8');
const modal = readFileSync(new URL('../src/components/AddDomainModal.tsx', import.meta.url), 'utf8');

test('domain creation requires explicit external or remote readiness, or a proven local DNS identity', () => {
  assert.match(domains, /typeof c\.dns_identity_ready !== 'boolean'/);
  assert.match(domains, /dnsIdentityReady !== true/);
  assert.match(domains, /disabled=\{dnsMissing\}/);
  assert.match(domains, /\/settings\?section=dns/);
  assert.match(modal, /dns_identity_ready: boolean/);
  assert.match(domains, /\(c\.dns_management_mode === 'external' \|\| c\.dns_management_mode === 'existing'\) && c\.dns_management_ready === true/);
  assert.match(modal, /caps\.dns_identity_ready === true/);
  assert.match(modal, /caps\?\.dns_management_mode === 'external' && caps\.dns_management_ready === true/);
  assert.match(modal, /purpose === 'dnsonly' \? !localDNSReady : !hostingDNSReady/);
  assert.match(modal, /if \(dnsMissing\) return/);
  assert.match(modal, /disabled=\{loading \|\| dnsMissing\}/);
  assert.match(modal, /err\.DNS_SETTINGS_REQUIRED\.action/);
});
