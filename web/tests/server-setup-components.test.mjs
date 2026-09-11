import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';
import { setupCatalogFixture } from './fixtures/server-setup-components.mjs';

const dataURL = text => `data:text/javascript;base64,${Buffer.from(text).toString('base64')}`;
const compile = path => ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 },
}).outputText;
const setupURL = dataURL(compile('../src/lib/serverSetup.ts'));
const helpers = await import(dataURL(compile('../src/lib/serverSetupComponents.ts').replace(/from ['"]\.\/serverSetup['"]/g, `from '${setupURL}'`)));
const setup = await import(setupURL);

test('catalogue accepts reciprocal dependencies and external conflict providers', () => {
    const catalog = setupCatalogFixture();
    catalog.components.find(row => row.id === 'nginx').conflicts.push('apache');
    assert.ok(helpers.decodeSetupComponentCatalog(catalog));
});

test('catalogue rejects contradictory inventory, duplicate choices and unresolved dependencies', () => {
    const corruptions = [
        value => { value.version = 2; },
        value => { value.inventory_state = 'guess'; },
        value => { value.components.push({ ...value.components[0] }); },
        value => { value.components[0].installed = 'yes'; },
        value => { value.components[0].supported = null; },
        value => { value.components[0].dependencies = ['missing-provider']; },
        value => { value.presets.web = ['missing-provider']; },
        value => { value.required_components = ['missing-provider']; },
    ];
    for (const corrupt of corruptions) {
        const catalog = setupCatalogFixture();
        corrupt(catalog);
        assert.equal(helpers.decodeSetupComponentCatalog(catalog), null);
    }
});

test('dependency closure terminates the mail cycle and resolves webmail requirements once', () => {
    const catalog = setupCatalogFixture();
    const pair = helpers.setupComponentClosure(['postfix'], catalog);
    assert.ok(pair instanceof Set);
    assert.deepEqual([...pair].sort(), ['dovecot', 'postfix']);
    const webmail = helpers.setupComponentClosure(['roundcube', 'roundcube'], catalog);
    assert.deepEqual([...webmail].sort(), ['dovecot', 'nginx', 'php-fpm', 'postfix', 'roundcube']);
    assert.deepEqual([...helpers.setupComponentClosure([], catalog)], []);
});

test('reciprocal mail lifecycle is one selectable group; one-way dependencies remain separate', () => {
    const groups = helpers.setupComponentGroups(setupCatalogFixture());
    const ids = groups.map(group => group.map(row => row.id).sort());
    assert.deepEqual(ids.find(group => group.includes('postfix')), ['dovecot', 'postfix']);
    assert.deepEqual(ids.find(group => group.includes('roundcube')), ['roundcube']);
    assert.equal(new Set(ids.flat()).size, setupCatalogFixture().components.length);
    assert.equal(ids.flat().length, setupCatalogFixture().components.length);
});

test('purpose changes clear the previous component selection without mutating the previous draft', () => {
    const draft = { purpose: 'web', dns_mode: 'external', database: 'mariadb', customization: { components: ['redis'] } };
    const changed = setup.chooseSetupPurpose(draft, 'custom');
    assert.equal(changed.purpose, 'custom');
    assert.deepEqual(changed.customization, { components: [] });
    assert.equal(setup.chooseSetupPurpose(draft, 'application').customization, undefined);
    assert.deepEqual(draft.customization, { components: ['redis'] });
});


test('completion destinations use the prepared services and preserve untouched profile routing', () => {
    assert.equal(setup.setupNextPath('web', new Set(['fail2ban'])), '/services');
    assert.equal(setup.setupNextPath('custom', new Set()), '/settings?section=dns');
    assert.equal(setup.setupNextPath('custom', new Set(['nginx', 'postgresql'])), '/domains');
    assert.equal(setup.setupNextPath('custom', new Set(['node'])), '/domains');
    for (const id of ['phpmyadmin', 'phppgadmin', 'roundcube']) {
        assert.equal(setup.setupNextPath('custom', new Set([id])), '/domains', `${id} implies web access even without a loaded catalogue`);
    }
    assert.equal(setup.setupNextPath('web', new Set(['postfix', 'dovecot'])), '/services');
    assert.equal(setup.setupNextPath('web'), '/domains');
    assert.equal(setup.setupNextPath('application'), '/domains');
    assert.equal(setup.setupNextPath('dns'), '/settings?section=dns');
});
