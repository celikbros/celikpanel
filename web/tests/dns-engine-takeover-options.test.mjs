import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

const card = readFileSync(
  new URL('../src/components/DNSEngineCard.tsx', import.meta.url),
  'utf8',
);
const contract = readFileSync(
  new URL('../src/lib/dnsEngineContract.ts', import.meta.url),
  'utf8',
);
const copy = readFileSync(new URL('../src/i18n/dnsEngine.ts', import.meta.url), 'utf8');

async function loadContractRuntime() {
  const javascript = ts.transpileModule(contract, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const url = `data:text/javascript;base64,${Buffer.from(javascript).toString('base64')}`;
  return import(url);
}

const takeoverPreview = {
  preview_token: 'c'.repeat(32),
  source_engine: null,
  target_engine: 'bind',
  expected_revision: 0,
  action: 'adopt_unmanaged',
  topology: 'standalone',
  zone_count: 1,
  pending_zone_count: 0,
  dnssec_zone_count: 0,
  estimated_downtime_seconds: 0,
  requires_downtime_acknowledgement: false,
  requires_adoption_acknowledgement: true,
  blockers: [],
  impacts: ['replace_foreign_config', 'reload_target'],
  adopted_directives: [
    {
      directive: 'recursion',
      found: 'no',
      replacement: 'no',
      unchanged: true,
      file: '/etc/bind/named.conf.options',
      line: 4,
    },
    {
      directive: 'allow-transfer',
      found: '{ 203.0.113.7; }',
      replacement: '{ none; }',
      unchanged: false,
      file: '/etc/bind/named.conf.options',
      line: 5,
    },
  ],
};

// The difference the takeover makes has to survive the decoder, because the
// operator reads it before consenting to a change on a DNS server that is
// answering queries right now.
test('a takeover preview carries the value found and the value CelikPanel sets', async () => {
  const { decodeDNSEngineSwitchPreview } = await loadContractRuntime();
  const decoded = decodeDNSEngineSwitchPreview(takeoverPreview, null, 'bind', 0);
  assert.ok(decoded, 'the takeover preview did not decode');
  assert.equal(decoded.adopted_directives.length, 2);
  assert.deepEqual(decoded.adopted_directives[0], {
    directive: 'recursion',
    found: 'no',
    replacement: 'no',
    unchanged: true,
    file: '/etc/bind/named.conf.options',
    line: 4,
  });
  assert.deepEqual(decoded.adopted_directives[1], {
    directive: 'allow-transfer',
    found: '{ 203.0.113.7; }',
    replacement: '{ none; }',
    unchanged: false,
    file: '/etc/bind/named.conf.options',
    line: 5,
  });
});

// "recursion no;" on an authoritative server is already what CelikPanel sets.
// A preview that calls that a change is describing a loss that does not happen,
// and a preview that calls a real change unchanged is worse.
test('an unchanged value must be reported as unchanged, in both directions', async () => {
  const { decodeDNSEngineSwitchPreview } = await loadContractRuntime();
  const lying = {
    ...takeoverPreview,
    adopted_directives: [
      { ...takeoverPreview.adopted_directives[0], unchanged: false },
    ],
  };
  assert.equal(decodeDNSEngineSwitchPreview(lying, null, 'bind', 0), null);
  const alsoLying = {
    ...takeoverPreview,
    adopted_directives: [
      { ...takeoverPreview.adopted_directives[1], unchanged: true },
    ],
  };
  assert.equal(decodeDNSEngineSwitchPreview(alsoLying, null, 'bind', 0), null);
});

test('a difference list is decoded only for the action that makes it', async () => {
  const { decodeDNSEngineSwitchPreview } = await loadContractRuntime();
  const missing = { ...takeoverPreview };
  delete missing.adopted_directives;
  const decoded = decodeDNSEngineSwitchPreview(missing, null, 'bind', 0);
  assert.ok(decoded);
  assert.deepEqual(decoded.adopted_directives, []);

  const wrongAction = {
    ...takeoverPreview,
    action: 'install',
    requires_adoption_acknowledgement: false,
  };
  assert.equal(decodeDNSEngineSwitchPreview(wrongAction, null, 'bind', 0), null);
});

test('a directive the page cannot describe takes the whole preview to null', async () => {
  const { decodeDNSEngineSwitchPreview } = await loadContractRuntime();
  const cases = [
    { directive: 'listen-on' },
    { refusal: 'because_i_said_so' },
    { file: 'named.conf.options' },
    { line: 0 },
    { found: 'no\nrecursion yes;' },
    { replacement: 'x'.repeat(201) },
  ];
  for (const patch of cases) {
    const broken = {
      ...takeoverPreview,
      adopted_directives: [{ ...takeoverPreview.adopted_directives[0], ...patch }],
    };
    assert.equal(
      decodeDNSEngineSwitchPreview(broken, null, 'bind', 0),
      null,
      `${JSON.stringify(patch)} reached the screen`,
    );
  }
});

test('a refused directive carries its reason and no value it could not read', async () => {
  const { decodeDNSEngineSwitchPreview } = await loadContractRuntime();
  const refused = {
    ...takeoverPreview,
    preview_token: 'd'.repeat(32),
    blockers: [{ code: 'dns_options_unadoptable' }],
    adopted_directives: [
      {
        directive: 'recursion',
        found: '',
        replacement: 'no',
        unchanged: false,
        file: '/etc/bind/named.conf.options',
        line: 9,
        refusal: 'nested_scope',
      },
    ],
  };
  const decoded = decodeDNSEngineSwitchPreview(refused, null, 'bind', 0);
  assert.ok(decoded);
  assert.equal(decoded.adopted_directives[0].refusal, 'nested_scope');
  assert.equal(decoded.adopted_directives[0].found, '');

  const inventedValue = {
    ...refused,
    adopted_directives: [{ ...refused.adopted_directives[0], found: 'yes' }],
  };
  assert.equal(decodeDNSEngineSwitchPreview(inventedValue, null, 'bind', 0), null);
});

// The panel is the authority on what it can send. A directive name or a refusal
// code it can return that this page does not list would decode the whole
// preview to null, and the operator would be told only that the change could
// not be verified - the R-041 failure, in a new place.
test('every directive and refusal the agent can report is renderable here', () => {
  const goSource = readFileSync(
    new URL('../../internal/transport/dns_contracts.go', import.meta.url),
    'utf8',
  );
  const readGoList = (name) => {
    const match = goSource.match(
      new RegExp(`var ${name} = \\[\\]string\\{([\\s\\S]*?)\\}`),
    );
    assert.ok(match, `${name} is no longer a pinned Go list`);
    return [...match[1].matchAll(/"([a-z_-]+)"/g)].map((entry) => entry[1]);
  };
  const readContractList = (name) => {
    const match = contract.match(
      new RegExp(`export const ${name} = \\[([\\s\\S]*?)\\] as const;`),
    );
    assert.ok(match, `${name} is no longer a pinned UI list`);
    return [...match[1].matchAll(/'([a-z_-]+)'/g)].map((entry) => entry[1]);
  };

  const goDirectives = readGoList('DNSManagedBINDOptionDirectives');
  const uiDirectives = readContractList('DNS_MANAGED_BIND_OPTION_DIRECTIVES');
  assert.ok(goDirectives.length >= 4);
  assert.deepEqual(uiDirectives, goDirectives);

  const goRefusalConstants = new Map(
    [...goSource.matchAll(/DNSForeignOption([A-Za-z]+) = "([a-z_]+)"/g)]
      .map((match) => [`DNSForeignOption${match[1]}`, match[2]]),
  );
  const refusalListMatch = goSource.match(
    /var DNSForeignOptionRefusals = \[\]string\{([\s\S]*?)\}/,
  );
  assert.ok(refusalListMatch, 'the refusal list is no longer pinned in Go');
  const goRefusals = [...refusalListMatch[1].matchAll(/DNSForeignOption[A-Za-z]+/g)]
    .map((match) => {
      const value = goRefusalConstants.get(match[0]);
      assert.ok(value, `unresolved refusal constant ${match[0]}`);
      return value;
    });
  const uiRefusals = readContractList('DNS_FOREIGN_OPTION_REFUSALS');
  assert.ok(goRefusals.length >= 3);
  assert.deepEqual(uiRefusals, goRefusals);

  // Each one needs words a person can act on, in both locales.
  for (const refusal of uiRefusals) {
    const key = `'dnsEngine.adoption.refusal.${refusal}'`;
    assert.equal(
      copy.split(key).length - 1, 2,
      `${key} is missing from one of the two locales`,
    );
  }
  assert.equal(
    copy.split(`'dnsEngine.adoption.refusal.unknown'`).length - 1, 2,
    'the unknown-refusal fallback is missing from one of the two locales',
  );
});

test('the takeover dialogue renders the difference under its acknowledgement', () => {
  // The list belongs inside the takeover panel, above the one acknowledgement.
  const panel = card.slice(card.indexOf('dnsEngine.adoption.titleRunning'));
  const listAt = panel.indexOf('dnsEngine.adoption.settingsTitle');
  const acknowledgementAt = panel.indexOf('dnsEngine.adoption.acknowledgementRunning');
  assert.ok(listAt > 0, 'the takeover dialogue does not show what it replaces');
  assert.ok(
    listAt < acknowledgementAt,
    'the difference must be readable before the operator agrees',
  );

  // It is a list of directives, not a sentence about them.
  assert.match(card, /takenOverDirectives\.map\(/);
  assert.match(card, /<AdoptedDirectiveRow/);
  assert.match(card, /\{directive\.directive\}/);
  assert.match(card, /\{directive\.found\}/);
  assert.match(card, /\{directive\.replacement\}/);
  assert.match(card, /dnsEngine\.adoption\.settingsUnchanged/);
  assert.match(card, /dnsEngine\.adoption\.settingsLine/);

  // One acknowledgement, not two: the takeover asks for consent once.
  assert.equal(
    card.split('onAcknowledgeAdoption(event.target.checked)').length - 1, 1,
    'the takeover must have exactly one acknowledgement control',
  );
  assert.equal(
    card.split(`'dnsEngine.adoption.acknowledgement'`).length - 1, 1,
  );

  // A refusal is shown even when the preview is blocked, because the blocked
  // panel is exactly where the operator needs to read it.
  const refusalAt = card.indexOf('dnsEngine.adoption.refusalTitle');
  const takeoverPanelAt = card.indexOf(
    'preview.requires_adoption_acknowledgement && preview.blockers.length === 0',
  );
  assert.ok(refusalAt > 0 && takeoverPanelAt > 0);
  assert.ok(
    refusalAt < takeoverPanelAt,
    'a refusal must render outside the panel that a blocker hides',
  );
  assert.match(card, /dns_options_unadoptable: 'dnsEngine\.blocker\.optionsUnadoptable'/);
});

test('the takeover difference copy exists in both locales', () => {
  for (const key of [
    'dnsEngine.adoption.settingsTitle',
    'dnsEngine.adoption.settingsIntro',
    'dnsEngine.adoption.settingsNow',
    'dnsEngine.adoption.settingsAfter',
    'dnsEngine.adoption.settingsUnchanged',
    'dnsEngine.adoption.settingsLine',
    'dnsEngine.adoption.refusalTitle',
    'dnsEngine.blocker.optionsUnadoptable',
  ]) {
    assert.equal(
      copy.split(`'${key}'`).length - 1, 2,
      `${key} is missing from one of the two locales`,
    );
  }
  // The interpolations the rows depend on must be present in both languages,
  // or one locale renders a placeholder as prose.
  const intros = [...copy.matchAll(/'dnsEngine\.adoption\.settingsIntro': '([^']*)'/g)];
  assert.equal(intros.length, 2);
  for (const intro of intros) assert.match(intro[1], /\{file\}/);
  const lines = [...copy.matchAll(/'dnsEngine\.adoption\.settingsLine': '([^']*)'/g)];
  assert.equal(lines.length, 2);
  for (const line of lines) assert.match(line[1], /\{line\}/);
  const refusals = [...copy.matchAll(/'dnsEngine\.adoption\.refusal\.[a-z_]+': '([^']*)'/g)];
  assert.ok(refusals.length >= 8);
  for (const refusal of refusals) {
    assert.match(refusal[1], /\{directive\}/);
    assert.match(refusal[1], /\{file\}/);
    assert.match(refusal[1], /\{line\}/);
  }
});
