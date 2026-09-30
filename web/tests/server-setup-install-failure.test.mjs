import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';
import { setupExecutionGuidance } from '../src/lib/serverSetupGuidance.ts';
import { enScreens } from '../src/i18n/screens/en.ts';
import { trScreens } from '../src/i18n/screens/tr.ts';

// upd1 finding P2: a failed component install must name the component, what
// stopped, the host's own line, who acts and how setup continues (D-024).
const dataURL = text => `data:text/javascript;base64,${Buffer.from(text).toString('base64')}`;
const compile = path => ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 },
}).outputText;
const setupURL = dataURL(compile('../src/lib/serverSetup.ts'));
const operations = await import(dataURL(compile('../src/lib/serverSetupOperation.ts').replace("from './serverSetup'", `from '${setupURL}'`)));

const id = c => c.repeat(32);
const steps = [
    { id: '04-service', kind: 'service', target: 'mariadb', status: 'succeeded' },
    { id: '05-mail_profile', kind: 'mail_profile', target: 'webmail', status: 'failed' },
    { id: '06-service', kind: 'service', target: 'certbot', status: 'pending' },
];
const failed = error => ({ id: id('a'), request_id: id('b'), plan_id: id('c'), status: 'failed', phase: '05-mail_profile', steps,
    error: { code: 'service_install_failed', message: 'The service could not be installed and verified.', ...error } });
const keys = guidance => guidance.messages.map(item => item.key);
const render = (catalog, item) => Object.entries(item.values || {}).reduce((text, [name, value]) => text.replaceAll(`{${name}}`, value), catalog[item.key]);

test('each install step names the component, the host line, the actor, the resume path and the mail fallback', () => {
    for (const [step, action] of [['package_install', 'package'], ['unit_start', 'service'], ['configure', 'other'], ['verify', 'other'], ['preflight', 'other']]) {
        const guide = setupExecutionGuidance(failed({ component: 'dovecot', step, detail: 'error: target not found: dovecot' }));
        assert.equal(guide.title, 'setup.guide.failedTitle');
        assert.deepEqual(keys(guide), [`setup.guide.installFailed.${step}`, 'setup.guide.installFailedDetail',
            `setup.guide.installFailedAction.${action}`, 'setup.guide.installFailedResume', 'setup.guide.installFailedWithoutMail']);
        assert.equal(guide.messages[0].values.component, 'Dovecot');
        assert.equal(guide.messages[1].values.detail, 'error: target not found: dovecot');
        assert.ok(!keys(guide).includes('setup.guide.failed') && !keys(guide).includes('setup.guide.component'), 'the generic texts are replaced');
        for (const catalog of [enScreens, trScreens]) {
            for (const item of guide.messages) {
                const text = render(catalog, item);
                assert.ok(text && !/\{[a-z]+\}/.test(text), `${item.key} renders completely`);
            }
        }
    }
});

test('a non-mail component gets no mail fallback, and a missing host line is not invented', () => {
    const guide = setupExecutionGuidance({ ...failed({ component: 'mariadb', step: 'unit_start' }), phase: '04-service',
        steps: [{ id: '04-service', kind: 'service', target: 'mariadb', status: 'failed' }] });
    assert.deepEqual(keys(guide), ['setup.guide.installFailed.unit_start', 'setup.guide.installFailedAction.service', 'setup.guide.installFailedResume']);
});

test('the screen names the component in its own language, as the step list does', () => {
    const turkish = id => ({ cron: trScreens['setup.component.cron'], 'core-mail': trScreens['dashboard.audit.profile.coreMail'] }[id] || id);
    const cron = setupExecutionGuidance({ ...failed({ component: 'cron', step: 'package_install' }), phase: '04-service',
        steps: [{ id: '04-service', kind: 'service', target: 'cron', status: 'failed' }] }, turkish);
    assert.equal(cron.messages[0].values.component, 'Zamanlanmış görevler (cron)');
    const mail = setupExecutionGuidance(failed({ component: 'core-mail', step: 'configure' }), turkish);
    assert.equal(mail.messages[0].values.component, 'Temel Posta');
});

test('an older failure without guidance keeps the previous generic texts', () => {
    const guide = setupExecutionGuidance(failed({}));
    assert.deepEqual(keys(guide), ['setup.guide.failed', 'setup.guide.component']);
    const unknownStep = setupExecutionGuidance(failed({ component: 'dovecot', step: 'reboot' }));
    assert.deepEqual(keys(unknownStep), ['setup.guide.failed', 'setup.guide.component']);
});

test('the decoder keeps valid guidance and drops malformed optional fields without hiding the execution', () => {
    const good = operations.decodeSetupExecution(failed({ component: 'dovecot', step: 'unit_start', detail: 'Job for dovecot.service failed' }));
    assert.equal(good.error.component, 'dovecot');
    assert.equal(good.error.detail, 'Job for dovecot.service failed');
    const bad = operations.decodeSetupExecution(failed({ component: 7, step: 'unit_start', detail: 'x'.repeat(5000) }));
    assert.ok(bad, 'malformed optional guidance must not hide the durable execution');
    assert.equal(bad.error.code, 'service_install_failed');
    assert.equal(bad.error.component, undefined);
    assert.equal(bad.error.detail, undefined);
});

test('English and Turkish name the actor and the next action for every step', () => {
    for (const catalog of [enScreens, trScreens]) {
        for (const step of ['preflight', 'package_install', 'configure', 'unit_start', 'verify']) {
            assert.ok(catalog[`setup.guide.installFailed.${step}`].includes('{component}'));
        }
        for (const action of ['package', 'service', 'other']) assert.ok(catalog[`setup.guide.installFailedAction.${action}`].length > 20);
        assert.ok(catalog['setup.guide.installFailedDetail'].includes('{detail}'));
        assert.ok(catalog['setup.failure.install']);
    }
    assert.match(enScreens['setup.guide.installFailedResume'], /Review a revised plan/);
    assert.match(trScreens['setup.guide.installFailedResume'], /Düzeltilmiş planı incele/);
    assert.match(enScreens['setup.guide.installFailedWithoutMail'], /Web hosting/);
    assert.match(trScreens['setup.guide.installFailedWithoutMail'], /Web barındırma/);
});
