import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
const source = ts.transpileModule(readFileSync(new URL('../src/lib/recoveryShell.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 } }).outputText.replace('export function', 'function');
function fixture({ secure = true, support = true, existing, reject = false, hang = false } = {}) {
    const calls = [];
    const worker = { getRegistration: async () => existing, ready: hang ? new Promise(() => {}) : Promise.resolve(), register: (...args) => { calls.push(args); if (reject) throw Error('denied'); return Promise.resolve(); } };
    const context = vm.createContext({ isSecureContext: secure, navigator: support ? { serviceWorker: worker } : {}, URL,
        setTimeout: (fn, ms) => setTimeout(fn, Math.min(ms, 10)), clearTimeout });
    vm.runInContext(source, context);
    return { calls, prepare: () => vm.runInContext('prepareRecoveryShell()', context) };
}
test('unsupported and untrusted origins keep normal startup unchanged', async () => {
    for (const options of [{secure:false}, {support:false}]) { const f=fixture(options); await f.prepare(); assert.equal(f.calls.length,0); }
});
test('the static helper never replaces another root worker', async () => {
    const f=fixture({existing:{active:{scriptURL:'https://panel.test/owner-worker.js'}}}); await f.prepare(); assert.equal(f.calls.length,0);
});
test('repeated preparation is one registration, without cached script reuse', async () => {
    const f=fixture(); await Promise.all([f.prepare(), f.prepare()]); assert.equal(f.calls.length,1);
    assert.deepEqual(JSON.parse(JSON.stringify(f.calls[0])), ['/recovery-worker.js',{scope:'/',updateViaCache:'none'}]);
});
test('denial and hanging worker installation have bounded, nonfatal outcomes', async () => {
    for (const options of [{reject:true}, {hang:true}]) { const f=fixture(options); await f.prepare(); assert.equal(f.calls.length,1); }
});
