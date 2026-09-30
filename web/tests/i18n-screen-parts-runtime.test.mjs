import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import TestRenderer, { act } from 'react-test-renderer';
import ts from 'typescript';

// The screen copy is stored in two files per locale (screens/ and
// screens/server/) because one file outgrew the per-chunk bundle budget. The
// split must not open a window in which a screen renders with only one of them:
// the provider's screen flag may turn true only once every part has arrived,
// and a part that fails must fail the screen copy as a whole. This mounts the
// real provider over the real catalogue files, holds the server part back, and
// records every render.
//
// Ekran metni dil başına iki dosyadadır. Bölünme, bir ekranın yalnız bir parça
// ile çizildiği bir an açmamalıdır: ekran bayrağı ancak her parça geldiğinde
// doğru olur; bir parçanın hatası ekran metninin tamamını başarısız sayar.
const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const jsxRuntimeURL = pathToFileURL(require.resolve('react/jsx-runtime')).href;
const moduleURL = (source) => 'data:text/javascript;base64,' + Buffer.from(source).toString('base64');
const compile = (path) => ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.ReactJSX },
}).outputText;

const catalogueURL = {
    './en': moduleURL(compile('../src/i18n/en.ts')),
    './tr': moduleURL(compile('../src/i18n/tr.ts')),
    './screens/en': moduleURL(compile('../src/i18n/screens/en.ts')),
    './screens/tr': moduleURL(compile('../src/i18n/screens/tr.ts')),
    './screens/server/en': moduleURL(compile('../src/i18n/screens/server/en.ts')),
    './screens/server/tr': moduleURL(compile('../src/i18n/screens/server/tr.ts')),
};

let scenario = 0;
async function loadProvider(heldPart) {
    // A fresh module graph per scenario: the held part waits on a gate the test
    // opens or breaks.
    const id = ++scenario;
    const imports = { ...catalogueURL };
    imports[heldPart] = moduleURL(`// scenario ${id}\nawait globalThis.i18nPartGate;\nexport * from '${catalogueURL[heldPart]}';`);
    const source = compile('../src/i18n/index.tsx')
        .replace(/from ['"]([^'"]+)['"]/g, (_m, path) => {
            const target = path === 'react' ? reactURL : path === 'react/jsx-runtime' ? jsxRuntimeURL : null;
            assert.ok(target, `unmapped static import ${path}`);
            return `from '${target}'`;
        })
        .replace(/import\(['"]([^'"]+)['"]\)/g, (_m, path) => {
            assert.ok(imports[path], `unmapped dynamic import ${path}`);
            return `import('${imports[path]}')`;
        });
    return import(moduleURL(`// provider ${id}\n${source}`));
}

function gate() {
    let open, fail;
    globalThis.i18nPartGate = new Promise((resolve, reject) => { open = resolve; fail = reject; });
    globalThis.i18nPartGate.catch(() => {});
    return { open, fail };
}

const probeKeys = ['app.name', 'setup.guide.more', 'svc.notInstalled'];

async function mount(i18n, locale) {
    globalThis.window = { localStorage: { getItem: () => locale, setItem: () => {} } };
    const renders = [];
    function Probe() {
        const { t, screensReady, screensFailed } = i18n.useI18n();
        // What a screen behind the gate would draw: nothing until the copy is ready.
        const texts = screensReady ? probeKeys.map((key) => t(key)) : [];
        renders.push({ screensReady, screensFailed, texts });
        return React.createElement('p', null, screensReady ? texts.join(' | ') : screensFailed ? 'failed' : 'waiting');
    }
    let renderer;
    await act(async () => {
        renderer = TestRenderer.create(React.createElement(i18n.I18nProvider, null, React.createElement(Probe)));
    });
    const settle = async () => {
        for (let i = 0; i < 20; i++) await act(async () => { await new Promise((resolve) => setTimeout(resolve, 5)); });
    };
    return { renders, settle, text: () => renderer.root.findByType('p').props.children, unmount: () => act(async () => renderer.unmount()) };
}

for (const [locale, expected] of [
    ['en', ['CelikPanel', 'Checks and how to continue', 'Not installed']],
    ['tr', ['CelikPanel', 'Kontroller ve devam etme yolu', 'Kurulu değil']],
]) {
    for (const held of [`./screens/${locale}`, `./screens/server/${locale}`]) {
        test(`${locale}: no screen copy is ready while ${held} is still loading`, async () => {
            const { open } = gate();
            const i18n = await loadProvider(held);
            const view = await mount(i18n, locale);
            await view.settle();
            assert.equal(view.text(), 'waiting', 'the screen copy was ready before every part arrived');
            assert.ok(view.renders.length > 0 && view.renders.every((render) => !render.screensReady));

            open();
            await view.settle();
            assert.equal(view.text(), expected.join(' | '));
            for (const render of view.renders.filter((item) => item.screensReady)) {
                assert.deepEqual(render.texts, expected, 'a render saw the screen flag before its copy');
            }
            await view.unmount();
        });

        test(`${locale}: a failing ${held} fails the screen copy as a whole`, async () => {
            const { fail } = gate();
            const i18n = await loadProvider(held);
            const view = await mount(i18n, locale);
            await view.settle();
            const logged = console.error;
            console.error = () => {};
            try {
                fail(new Error('chunk unavailable'));
                await view.settle();
            } finally {
                console.error = logged;
            }
            assert.equal(view.text(), 'failed');
            assert.ok(view.renders.every((render) => !render.screensReady));
            await view.unmount();
        });
    }
}
