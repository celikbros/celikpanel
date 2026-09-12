import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';
import { componentOperationGuidance, componentOperationPhaseKey } from '../src/lib/componentOperationGuidance.ts';

const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const dataURL = source => `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
const compile = source => ts.transpileModule(source, { compilerOptions: {
    jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020,
}}).outputText;
const read = path => readFileSync(new URL(path, import.meta.url), 'utf8');
const providerSource = read('../src/components/ComponentOperation.tsx');
const decoderSource = providerSource.slice(providerSource.indexOf('export function operationError('), providerSource.indexOf('function restoredOperation('));
const decoderURL = dataURL(compile(decoderSource));
const { operationError } = await import(decoderURL);
const guidanceURL = dataURL(compile(read('../src/lib/componentOperationGuidance.ts')));
const stubURL = dataURL(`import React from '${reactURL}';
export const useI18n = () => ({ t: (key, vars={}) => {
    let text=globalThis.operationGuidanceCatalog[key]??key;
    for (const [name,value] of Object.entries(vars)) text=text.replaceAll('{'+name+'}',String(value));
    return text;
}});
export const ErrorBanner = props => React.createElement('error-banner', props);
export const AlertTriangle=()=>React.createElement('warning-icon');
export const Loader2=()=>React.createElement('spinner-icon');
export const WifiOff=()=>React.createElement('offline-icon');
export const X=()=>null;
`);
const overlaySource = compile(read('../src/components/OperationOverlay.tsx')).replace(/from ['"]([^'"]+)['"]/g, (_, name) => {
    const url = name === 'react' ? reactURL : name.endsWith('/ComponentOperation') ? decoderURL : name.endsWith('/componentOperationGuidance') ? guidanceURL : stubURL;
    return `from '${url}'`;
});
const Overlay = (await import(dataURL(`import React from '${reactURL}';\n${overlaySource}`))).default;
const base = { view:null, operation:null, label:'Nginx', submitting:false, recovering:false, refreshing:false, interrupted:false };
const operation = (status='running', extra={}) => ({id:'op-123',phase:'installing',status,...extra});

async function withOverlay(props, inspect) {
    let tree;
    await act(async () => { tree=Renderer.create(React.createElement(Overlay,{...base,...props})); });
    try { await inspect(tree); } finally { await act(async()=>tree.unmount()); }
}

for (const [locale,catalog] of Object.entries({en,tr})) {
    test(`${locale}: terminal failure stays visible during result scan and lost connection`, async()=>{
        globalThis.operationGuidanceCatalog=catalog;
        for (const interrupted of [false,true]) {
            await withOverlay({operation:operation('failed',{error:{code:'service_install_failed',message:'safe error',reason:'inactive_service',action:'/components',details:['nginx is inactive']}}),refreshing:true,interrupted},tree=>{
                const text=JSON.stringify(tree.toJSON());
                assert.ok(text.includes(catalog['services.operation.failedChecking']));
                assert.ok(!text.includes(catalog['services.operation.refreshing']));
                assert.ok(!text.includes(catalog['services.operation.backgroundHint']));
                assert.equal(tree.root.findAllByType('spinner-icon').length,0);
                assert.equal(tree.root.findAllByType('button').length,0,'result verification must not unlock navigation or offer an install retry');
                const error=tree.root.findByType('error-banner').props.error;
                assert.equal(error.code,'service_install_failed');
                assert.equal(error.reason,'inactive_service');
                assert.equal(error.action,undefined,'a failure action must wait until verified terminal release');
                assert.deepEqual(error.details,['nginx is inactive']);
            });
        }
    });
    test(`${locale}: lost response cannot claim the server job is still running`,async()=>{
        globalThis.operationGuidanceCatalog=catalog;
        await withOverlay({operation:operation(),interrupted:true},tree=>{
            const text=JSON.stringify(tree.toJSON());
            assert.ok(text.includes(catalog['services.operation.uncertainHint']));
            assert.ok(!text.includes(catalog['services.operation.backgroundHint']));
            assert.equal(tree.root.findAllByType('button').length,0);
        });
    });
    test(`${locale}: released errors keep recovery actions and explicitly require a new user action`,async()=>{
        globalThis.operationGuidanceCatalog=catalog;
        let dismissed=0;
        await withOverlay({failure:{code:'license_required',message:'license missing',action:'/activate'},dismissLabel:'Dismiss',onDismiss:()=>dismissed++},tree=>{
            assert.equal(tree.root.findByType('error-banner').props.error.action,'/activate');
            assert.ok(JSON.stringify(tree.toJSON()).includes(catalog['services.operation.failureNextStep']));
            tree.root.findByType('button').props.onClick();
            assert.equal(dismissed,1);
        });
    });
}

test('uncertain, terminal-success and running observations stay distinct',()=>{
    assert.equal(componentOperationGuidance(null,false,true,false).hintKey,'services.operation.uncertainHint');
    assert.equal(componentOperationGuidance(operation('succeeded'),false,false,false).hintKey,'services.operation.verifyingHint');
    assert.equal(componentOperationGuidance(operation(),false,false,false).hintKey,'services.operation.backgroundHint');
    assert.equal(componentOperationGuidance(operation('failed'),true,true,true).failed,true);
});

test('unknown backend phase remains an honest generic stage without leaking internal identifiers',async()=>{
    globalThis.operationGuidanceCatalog=en;
    await withOverlay({operation:operation('running',{phase:'native-phase|private-identifier'})},tree=>{
        const text=JSON.stringify(tree.toJSON());
        assert.ok(text.includes(en['services.operation.running']));
        assert.ok(!text.includes('private-identifier'));
    });
});

test('an external operation owns its distinct guidance and error severity',async()=>{
    globalThis.operationGuidanceCatalog=en;
    await withOverlay({operation:operation('failed'),view:{title:'DNS verification',status:'Peer unavailable',hint:'Check the peer',busy:false,severity:'error'}},tree=>{
        const text=JSON.stringify(tree.toJSON());
        assert.ok(text.includes('Peer unavailable'));
        assert.ok(text.includes('Check the peer'));
        assert.equal(tree.root.findAllByType('error-banner').length,0);
    });
});

test('structured outcome flags stay separate and codes cannot be inferred from free text',()=>{
    const result=operationError({code:'HOST_MUTATION_BUSY',reason:'package_manager_active',message:'busy',partial_success:true,mutation_applied:false,details:['evidence',null]},'failed');
    assert.equal(result.reason,'package_manager_active');
    assert.equal(result.partialSuccess,true);
    assert.equal(result.mutationApplied,undefined);
    assert.deepEqual(result.details,['evidence']);
    assert.equal(operationError('vendor license invalid','failed').code,undefined);
});

test('real mail profile and service scan stages are readable, unknown phases never become instructions',()=>{
    assert.equal(componentOperationPhaseKey('profile/webmail/postfix/installing'),'services.operation.phase.installing');
    assert.equal(componentOperationPhaseKey('profile/core-mail/scanning'),'services.operation.phase.scanning');
    assert.equal(componentOperationPhaseKey('profile/protected-mail/preflight'),'services.operation.phase.preparing');
    assert.equal(componentOperationPhaseKey('scanning'),'services.operation.phase.scanning');
    assert.equal(componentOperationPhaseKey('constructor'),'services.operation.running');
    assert.equal(componentOperationPhaseKey('profile/untrusted/service/installing'),'services.operation.running');
});
