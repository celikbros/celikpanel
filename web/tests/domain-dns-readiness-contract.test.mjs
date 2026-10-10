import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';
import { capabilitiesURL, remoteURL } from './fixtures/shared-layer.mjs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const domains = read('../src/components/Domains.tsx');
const modal = read('../src/components/AddDomainModal.tsx');
const capabilities = read('../src/lib/hostingCapabilities.ts');
const remote = read('../src/lib/remote.ts');

// Until 9 Oct 2026 the page and the dialogue each read the server's
// capabilities and each decided on their own what an unfinished or failed read
// meant. The dialogue's answer was "DNS is missing": an owner opening Add
// domain on a server that had DNS saw "choose a DNS engine" and a disabled
// form for as long as the read took. The readiness rule now lives in one
// place, is computed from a known answer only, and both screens ask it.

test('there is one reader of the server capabilities', () => {
  const holders = [];
  const walk = (dir) => {
    for (const entry of readdirSync(new URL(dir, import.meta.url), { withFileTypes: true })) {
      if (entry.isDirectory()) walk(`${dir}${entry.name}/`);
      else if (/\.tsx?$/.test(entry.name) && read(dir + entry.name).includes('hosting/capabilities')) holders.push(dir + entry.name);
    }
  };
  walk('../src/');
  assert.deepEqual(holders, ['../src/lib/hostingCapabilities.ts'],
    'read the capabilities through useHostingCapabilities(); a second reader gives a failed read a second meaning');
  assert.match(capabilities, /options\.enabled === false \? null : HOSTING_CAPABILITIES_URL/);
  assert.match(remote, /if \(url === null\) return;/, 'a null address must read nothing');
});

test('an answer that is not the contract is unknown, never "nothing installed"', async () => {
  assert.match(capabilities, /typeof body\.dns_server !== 'string'/);
  assert.match(capabilities, /typeof body\.dns_identity_ready !== 'boolean'/);
  const { decodeHostingCapabilities } = await import(capabilitiesURL);
  const good = {
    web_server: 'nginx', php_versions: [], dns_server: 'bind', dns_identity_ready: true,
    dns_management_mode: 'local', dns_management_ready: true, mail_server: false, database_servers: [], db_tools: [],
  };
  assert.equal(decodeHostingCapabilities(good).dns_server, 'bind');
  for (const broken of [
    null, [], 'ok', {},
    { ...good, dns_server: undefined },
    { ...good, dns_identity_ready: 'true' },
    { ...good, php_versions: null },
    { ...good, database_servers: undefined },
    { ...good, db_tools: [1] },
  ]) {
    assert.throws(() => decodeHostingCapabilities(broken), `accepted ${JSON.stringify(broken)}`);
  }
});

test('domain creation requires explicit external or remote readiness, or a proven local DNS identity', async () => {
  assert.match(capabilities, /return caps\.dns_server !== '' && caps\.dns_identity_ready === true;/);
  assert.match(capabilities, /\(caps\.dns_management_mode === 'external' \|\| caps\.dns_management_mode === 'existing'\) &&\s*caps\.dns_management_ready === true/);
  assert.match(capabilities, /purpose === 'dnsonly' \? localDNSReady\(caps\) : hostingDNSReady\(caps\)/);
  assert.match(capabilities, /return caps\.dns_server === '' \? 'engine' : 'identity';/);

  const { dnsBlocker } = await import(capabilitiesURL);
  const caps = (overrides) => ({
    web_server: 'nginx', php_versions: [], dns_server: '', dns_identity_ready: false,
    dns_management_mode: 'local', dns_management_ready: false, mail_server: false, database_servers: [], db_tools: [],
    ...overrides,
  });
  // [capabilities, website, dnsonly]
  for (const [value, website, dnsonly] of [
    [caps({}), 'engine', 'engine'],
    [caps({ dns_server: 'bind' }), 'identity', 'identity'],
    [caps({ dns_server: 'bind', dns_identity_ready: true }), null, null],
    [caps({ dns_identity_ready: true }), 'engine', 'engine'],
    [caps({ dns_management_mode: 'external', dns_management_ready: true }), null, 'engine'],
    [caps({ dns_management_mode: 'existing', dns_management_ready: true }), null, 'engine'],
    [caps({ dns_management_mode: 'existing', dns_management_ready: false }), 'engine', 'engine'],
    [caps({ dns_management_mode: 'local', dns_management_ready: true }), 'engine', 'engine'],
    [caps({ dns_server: 'pdns', dns_management_mode: 'external', dns_management_ready: true }), null, 'identity'],
  ]) {
    assert.equal(dnsBlocker(value, 'website'), website, `website on ${JSON.stringify(value)}`);
    assert.equal(dnsBlocker(value, 'dnsonly'), dnsonly, `dnsonly on ${JSON.stringify(value)}`);
  }
});

test('a requirement is shown as unmet only for a known answer', async () => {
  const { gateOn, LOADING } = await import(remoteURL);
  const blocked = () => 'engine';
  assert.deepEqual(gateOn(LOADING, blocked), { state: 'checking' });
  assert.deepEqual(gateOn({ state: 'unknown', reason: { message: '' } }, blocked), { state: 'unknown' });
  // An earlier answer does not decide a gate: a failed read is not "blocked".
  assert.deepEqual(
    gateOn({ state: 'unknown', reason: { message: '' }, previous: { value: 1, observedAt: 1 } }, blocked),
    { state: 'unknown' },
  );
  assert.deepEqual(gateOn({ state: 'known', value: 1, observedAt: 1 }, blocked), { state: 'blocked', reason: 'engine' });
  assert.deepEqual(gateOn({ state: 'known', value: 1, observedAt: 1 }, () => null), { state: 'open' });
});

test('the Domains page disables Add, and names the fix, only when DNS is known to be missing', () => {
  assert.match(domains, /const capabilities = useHostingCapabilities\(\{ enabled: !isTeamMember \}\);/);
  assert.match(domains, /const dns = gateOn\(capabilities\.remote, \(value\) => dnsBlocker\(value, 'website'\)\);/);
  assert.match(domains, /const dnsBlocked = !isTeamMember && dns\.state === 'blocked' \? dns\.reason : null;/);
  assert.match(domains, /disabled=\{dnsBlocked !== null\}/);
  assert.equal(domains.split('disabled={dnsBlocked !== null}').length - 1, 1, 'one Add button definition carries the gate');
  assert.match(domains, /dnsBlocked === 'engine' \? t\('domains\.add\.needsDns'\) : t\('err\.DNS_SETTINGS_REQUIRED'\)/);
  assert.match(domains, /\/settings\?section=dns/);
  assert.match(domains, /err\.DNS_SETTINGS_REQUIRED\.action/);
  // "No domains yet" is a claim about the server: it needs the answer as proof.
  assert.match(domains, /<KnownEmpty\s+of=\{shown\}\s+icon=\{Globe\}\s+title=\{t\('domains\.empty'\)\}/);
  assert.doesNotMatch(domains, /<EmptyState/);
  assert.doesNotMatch(domains, /dnsMissing|dnsReadinessKnown/);
});

test('the Add domain dialogue shows the blocker only for a known answer and never submits without one', () => {
  assert.match(modal, /const capabilities = useHostingCapabilities\(\);/);
  assert.match(modal, /const dns = gateOn\(capabilities\.remote, \(value\) => dnsBlocker\(value, purpose\)\);/);
  assert.match(modal, /if \(dns\.state !== 'open'\) return;/);
  assert.match(modal, /disabled=\{loading \|\| dns\.state !== 'open'\}/);
  assert.match(modal, /\{dns\.state === 'blocked' && \(/);
  assert.match(modal, /\{dns\.state === 'unknown' && \(\s*<CouldNotCheck\s+text=\{t\('domains\.add\.dnsUnknown'\)\}\s+onRetry=\{\(\) => void capabilities\.retry\(\)\}/);
  assert.match(modal, /\{dns\.state === 'checking' && <Checking label=\{t\('dns\.checkingServer'\)\} \/>\}/);
  // The blocker names the half that is missing, from the known answer.
  assert.match(modal, /dns\.reason === 'identity' \? t\('err\.DNS_SETTINGS_REQUIRED'\) : t\('domains\.add\.needsDns'\)/);
  assert.match(modal, /err\.DNS_SETTINGS_REQUIRED\.action/);
  // The two text keys of a known-missing DNS appear only inside the blocker.
  const blocker = modal.slice(modal.indexOf("{dns.state === 'blocked' && ("), modal.indexOf('{t(\'domains.add.domainName\')}'));
  for (const key of ["'domains.add.needsDns'", "'err.DNS_SERVER_REQUIRED.action'", "'err.DNS_SETTINGS_REQUIRED'"]) {
    assert.ok(blocker.includes(key), `${key} is missing from the blocker`);
    assert.equal(modal.split(key).length - 1, blocker.split(key).length - 1, `${key} is drawn outside the known-negative blocker`);
  }
  // An option is unavailable only once the answer is known.
  assert.match(modal, /const websiteAvailable = caps \? hostingDNSReady\(caps\) && caps\.web_server !== '' : true;/);
  assert.match(modal, /const dnsOnlyAvailable = caps \? localDNSReady\(caps\) : true;/);
  assert.doesNotMatch(modal, /dnsMissing|fetch\(`\$\{API_BASE\}\/hosting/);
});
