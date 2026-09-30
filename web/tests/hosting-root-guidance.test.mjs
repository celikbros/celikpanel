import assert from 'node:assert/strict';
import test from 'node:test';

import { apiErrorText, readApiError } from '../src/lib/apiError.ts';
import { setupHostingRootBlockerValues } from '../src/lib/serverSetupGuidance.ts';
import { en as enShell } from '../src/i18n/en.ts';
import { tr as trShell } from '../src/i18n/tr.ts';
import { enScreens } from '../src/i18n/screens/en.ts';
import { trScreens } from '../src/i18n/screens/tr.ts';

// Native finding P3 (1 Oct 2026): an owner's directory above the hosting root
// that the web server or the site users cannot pass. Both the domain-create
// refusal and the setup review blocker name the directory, its mode and
// owner, the server owner, the command and how work resumes, in EN and TR.

const translate = (catalog) => (key, vars) => {
  let text = catalog[key] ?? key;
  for (const [name, value] of Object.entries(vars ?? {})) text = text.replaceAll(`{${name}}`, String(value));
  return text;
};
const placeholders = (text) => [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort();

test('hosting root texts exist in both languages with the same placeholders', () => {
  for (const [key, enText, trText] of [
    ['err.HOSTING_ROOT_NOT_TRAVERSABLE', enShell['err.HOSTING_ROOT_NOT_TRAVERSABLE'], trShell['err.HOSTING_ROOT_NOT_TRAVERSABLE']],
    ['setup.blocker.hostingRoot', enScreens['setup.blocker.hostingRoot'], trScreens['setup.blocker.hostingRoot']],
  ]) {
    assert.ok(enText && trText, `${key} missing`);
    assert.deepEqual(placeholders(enText), ['command', 'directory', 'mode', 'owner'], key);
    assert.deepEqual(placeholders(trText), placeholders(enText), key);
  }
});

test('the domain-create refusal names directory, mode, owner, actor, command and resume', async () => {
  const response = new Response(JSON.stringify({
    error: 'server text',
    code: 'HOSTING_ROOT_NOT_TRAVERSABLE',
    details: ['/var/www', '0750', 'root:celikpanel'],
    vars: { directory: '/var/www', mode: '0750', owner: 'root:celikpanel', command: 'sudo chmod 755 /var/www', ignored: 7 },
  }), { status: 409 });
  const error = await readApiError(response);
  assert.deepEqual(error.vars, { directory: '/var/www', mode: '0750', owner: 'root:celikpanel', command: 'sudo chmod 755 /var/www' });
  const enText = apiErrorText(error, translate({ ...enShell }));
  const trText = apiErrorText(error, translate({ ...trShell }));
  for (const text of [enText, trText]) {
    for (const fact of ['/var/www', '0750', 'root:celikpanel', 'sudo chmod 755 /var/www']) assert.ok(text.includes(fact), `${fact} in ${text}`);
    assert.ok(!/\{\w+\}/.test(text), text);
  }
  assert.match(enText, /^The site was not created, and nothing was changed\./);
  assert.match(enText, /server owner/);
  assert.match(enText, /create the site again/);
  assert.match(trText, /^Site oluşturulmadı ve hiçbir şey değiştirilmedi\./);
  assert.match(trText, /sunucu sahibinindir/);
  assert.match(trText, /siteyi yeniden oluşturun/);
});

test('the setup review blocker is read whole and malformed codes are refused', () => {
  const values = setupHostingRootBlockerValues('server_setup_hosting_root_not_traversable:0750:root:celikpanel:/var/www');
  assert.deepEqual(values, { directory: '/var/www', mode: '0750', owner: 'root:celikpanel', command: 'sudo chmod 755 /var/www' });
  const enText = translate(enScreens)('setup.blocker.hostingRoot', values);
  const trText = translate(trScreens)('setup.blocker.hostingRoot', values);
  assert.match(enText, /server owner/);
  assert.match(enText, /Review setup plan/);
  assert.ok(trText.includes(trScreens['setup.review']), 'TR names the review button as it is labelled');
  for (const code of [
    'server_setup_hosting_root_not_traversable',
    'server_setup_hosting_root_not_traversable:750:root:root:/var/www',
    'server_setup_hosting_root_not_traversable:0750:root:root:var/www',
    'server_setup_service_unsupported:dovecot',
  ]) assert.equal(setupHostingRootBlockerValues(code), null, code);
});
