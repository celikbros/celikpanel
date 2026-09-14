import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const dataModule = source => 'data:text/javascript;base64,' + Buffer.from(source).toString('base64');
const compile = path => ts.transpileModule(readFileSync(new URL(path,import.meta.url),'utf8'), {compilerOptions:{jsx:ts.JsxEmit.React,module:ts.ModuleKind.ES2022,target:ts.ScriptTarget.ES2020}}).outputText;
const accessURL = dataModule(compile('../src/lib/accessObservation.ts'));
const recoveryURL = dataModule(compile('../src/lib/recoveryObservation.ts'));
const {parseAccessObservation} = await import(accessURL);
const {parseRecoveryObservation,savedRecoveryRequestId,reconcileRecoveryObservation} = await import(recoveryURL);
const stub = dataModule(`import React from '${reactURL}';
 export const api = {me:signal=>globalThis.recoveryFixture.me(signal)};
 export const useI18n=()=>({t:key=>key,locale:'en'});
 export const BrandMark=()=>null,LanguageSwitcher=()=>null,Spinner=()=>null;
 export const Button=props=>React.createElement('button',props);
`);
function rewritten(path) { return dataModule(`import React from '${reactURL}';\n`+compile(path).replace(/from ['"]([^'"]+)['"]/g,(_,specifier)=>`from '${specifier==='react'?reactURL:specifier.endsWith('/recoveryObservation')?recoveryURL:stub}'`)); }
const {usePanelSession}=await import(rewritten('../src/auth/usePanelSession.ts'));
const {RecoveryAccess,RecoveryStatus}=await import(rewritten('../src/components/RecoveryAccess.tsx'));
const originalFetch=globalThis.fetch;
const events=new EventTarget();
globalThis.window=Object.assign(events,{setTimeout,clearTimeout,setInterval,clearInterval,location:{reload(){}}});
globalThis.document={visibilityState:'visible'};
const id='a'.repeat(32),other='b'.repeat(32);
const admin={username:'admin',effective_role:'admin'};
let tree,session,calls=[];
function Fixture(){session=usePanelSession();return React.createElement('output',null,session.state);}
async function clean(){if(tree)await act(async()=>tree.unmount());tree=undefined;globalThis.fetch=originalFetch;delete globalThis.recoveryFixture;}
const known=(phase='failed')=>({schema:'celikpanel-recovery-status/v1',panel_state:'ready',request_id:id,observation:'known',phase,terminal_proof:phase==='recovered'?'rollback_verified':phase==='succeeded'?'update_verified':'none',reason:phase==='failed'?'update_failed':phase==='recovered'?'rollback_verified':phase==='succeeded'?'update_verified':'update_running',observed_at:'2026-09-14T08:00:00Z'});
function setup(me=async()=>admin,fetcher=async()=>Response.json({schema:'celikpanel-panel-availability/v1',state:'ready'})){
 calls=[];globalThis.recoveryFixture={me};globalThis.fetch=async(...args)=>{calls.push(args);return fetcher(...args)};
 globalThis.localStorage={getItem:()=>JSON.stringify({state_version:1,phase:'active',marker:{marker_version:1,request_id:id}})};
}

test('only known typed negative access requires activation; legacy positives keep their exact deadline',()=>{
 for(const state of ['missing','expired','invalid'])assert.equal(parseAccessObservation({state,observation:'known',can_use_panel:false,valid_until:0},100).allowed,false);
 for(const result of [{can_use_panel:false,valid_until:0},{state:'missing',can_use_panel:false,valid_until:0},{state:'verification_unavailable',observation:'unavailable',can_use_panel:false,valid_until:0},{state:'status_unavailable',observation:'unavailable',can_use_panel:false,valid_until:0}])assert.equal(parseAccessObservation(result,100).allowed,null);
 assert.deepEqual(parseAccessObservation({can_use_panel:true,valid_until:160},100),{allowed:true,until:160,state:'active'});
 for(const result of [{can_use_panel:true,valid_until:100},{can_use_panel:true,valid_until:160,state:'expired'},{can_use_panel:true,valid_until:160,observation:'unavailable'}])assert.throws(()=>parseAccessObservation(result,100));
});

test('saved operation is an exact ID hint; terminal messages cannot supply a server outcome',()=>{
 assert.equal(savedRecoveryRequestId(JSON.stringify({state_version:1,phase:'terminal',marker:{marker_version:1,request_id:id},message:'success',outcome:'succeeded'})),id);
 for(const value of [null,'{}','garbage',JSON.stringify({marker_version:1,request_id:'../../secret'}),JSON.stringify({marker_version:1,request_id:'A'.repeat(32)}),' '.repeat(8193)])assert.equal(savedRecoveryRequestId(value),null);
});

test('recovery success requires exact identity, schema, timestamp and the matching terminal proof',()=>{
 for(const phase of ['recovered','succeeded'])assert.equal(parseRecoveryObservation(known(phase),id).phase,phase);
 for(const value of [{...known(),request_id:other},{...known(),schema:'old'},{...known(),observed_at:'yesterday'},{...known('recovered'),terminal_proof:'none'},{...known('failed'),terminal_proof:'rollback_verified'}])assert.throws(()=>parseRecoveryObservation(value,id));
 const unknown=parseRecoveryObservation({...known('succeeded'),observation:'unavailable'},id);
 assert.equal(unknown.phase,undefined);assert.equal(unknown.terminal_proof,'none');
 assert.equal(reconcileRecoveryObservation(parseRecoveryObservation(known(),id),parseRecoveryObservation(known('running'),id)).record.previous_failure,'update_failed');
});

test('initial auth failure stays unknown; only confirmed no session asks for sign-in',async()=>{
 for(const me of [async()=>{throw new Error('503 AUTH_STATUS_UNAVAILABLE')},async()=>{throw new TypeError('offline')},async()=>null]){
  setup(me);try{await act(async()=>{tree=Renderer.create(React.createElement(Fixture))});assert.equal(session.state,await me().then(()=>true).catch(()=>false)?'unauthenticated':'auth_unavailable');assert.equal(calls.length,0);assert.equal(session.user,null);}finally{await clean()}
 }
});

test('availability must be explicitly ready; startup and legacy 404 never open management',async()=>{
 for(const [response,expected] of [[()=>Response.json({schema:'celikpanel-panel-availability/v1',state:'starting'}),'starting'],[()=>Response.json({}, {status:404}),'availability_unavailable'],[()=>Response.json({}, {status:503}),'availability_unavailable'],[()=>Response.json({schema:'celikpanel-panel-availability/v1',state:'ready'}),'ready']]){
  setup(undefined,async()=>response());try{await act(async()=>{tree=Renderer.create(React.createElement(Fixture))});assert.equal(session.state,expected);assert.equal(session.user,admin);assert.equal(calls.length,1);assert.equal(calls[0][0],'/api/v1/panel/availability');}finally{await clean()}
 }
});

test('late auth or availability responses cannot restore an old identity after logout',async()=>{
 let resolve;setup(()=>new Promise(done=>{resolve=done}));
 try{await act(async()=>{tree=Renderer.create(React.createElement(Fixture))});await act(async()=>session.transitionAuthentication(null));await act(async()=>resolve(admin));assert.equal(session.state,'unauthenticated');assert.equal(session.user,null);assert.equal(calls.length,0);}finally{await clean()}
 setup(undefined,()=>new Promise(done=>{resolve=done}));
 try{await act(async()=>{tree=Renderer.create(React.createElement(Fixture))});await act(async()=>session.transitionAuthentication(null));await act(async()=>resolve(Response.json({schema:'celikpanel-panel-availability/v1',state:'ready'})));assert.equal(session.state,'unauthenticated');assert.equal(session.user,null);}finally{await clean()}
});

test('startup transitions to ready through read-only checks; a failed authenticated read stays unavailable',async()=>{
 let ready=false;setup(undefined,async()=>Response.json({schema:'celikpanel-panel-availability/v1',state:ready?'ready':'starting'}));
 try{await act(async()=>{tree=Renderer.create(React.createElement(Fixture))});assert.equal(session.state,'starting');ready=true;await act(async()=>session.retry());assert.equal(session.state,'ready');await act(async()=>session.markUnavailable(true));assert.equal(session.state,'auth_unavailable');assert.equal(session.user,null);assert.ok(calls.every(([,options])=>!options.method));}finally{await clean()}
});

test('recovery polling keeps verified failure when observation is unavailable and never starts a mutation',async()=>{
 let reply=known();setup(undefined,async()=>Response.json(reply));
 try{
  await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});
  assert.ok(JSON.stringify(tree.toJSON()).includes('recovery.phase.failed'));
  reply={...known(),observation:'unavailable'};
  await act(async()=>tree.root.findByType('button').props.onClick());
  assert.ok(JSON.stringify(tree.toJSON()).includes('recovery.phase.failed'));
  assert.ok(JSON.stringify(tree.toJSON()).includes('recovery.observationUnavailable'));
  reply=known('running');await act(async()=>tree.root.findByType('button').props.onClick());
  assert.ok(JSON.stringify(tree.toJSON()).includes('recovery.reason.update_failed'));
  assert.ok(calls.every(([url,options])=>url===`/api/v1/recovery/status?request_id=${id}`&&!options.method));
 }finally{await clean()}
});

test('unverified sessions and non-admin users never request or display operation identity',async()=>{
 for(const user of [null,{username:'tenant',effective_role:'customer'}]){
  setup();try{await act(async()=>{tree=Renderer.create(React.createElement(RecoveryAccess,{user,cause:'auth',onRetry(){}}))});assert.equal(calls.length,0);assert.ok(!JSON.stringify(tree.toJSON()).includes(id));assert.equal(tree.root.findAllByType('input').length,0);}finally{await clean()}
 }
});

test('missing browser ID does not trigger latest-operation discovery',async()=>{
 setup();globalThis.localStorage={getItem:()=>null};
 try{await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});assert.equal(calls.length,0);assert.ok(JSON.stringify(tree.toJSON()).includes('recovery.noOperation'));}finally{await clean()}
});


test('phase and reason must describe the same observation',()=>{
 for(const value of [{...known('succeeded'),reason:'update_running'},{...known('recovered'),reason:'update_verified'},{...known(),reason:'recovery_running'},{...known('running'),reason:'operation_accepted'},{...known(),observed_at:'2026-02-30T08:00:00Z'}])assert.throws(()=>parseRecoveryObservation(value,id));
});

test('older observations and later worker errors cannot erase exact verified terminal proof',()=>{
 const terminal=parseRecoveryObservation(known('recovered'),id);
 for(const candidate of [{...known('running'),observed_at:'2026-09-14T07:59:59Z'},{...known(),observed_at:'2026-09-14T08:00:01Z'},{...known('succeeded'),observed_at:'2026-09-14T08:00:02Z'}]){
  const result=reconcileRecoveryObservation(terminal,parseRecoveryObservation(candidate,id));assert.equal(result.record,terminal);assert.equal(result.unavailable,true);
 }
 const failure=parseRecoveryObservation(known(),id);
 const stale=reconcileRecoveryObservation(failure,parseRecoveryObservation({...known('running'),observed_at:'2026-09-14T07:59:59Z'},id));
 assert.equal(stale.record,failure);assert.equal(stale.unavailable,true);
 const missing=reconcileRecoveryObservation(terminal,parseRecoveryObservation({...known(),observation:'unavailable'},id));assert.equal(missing.record,terminal);assert.equal(missing.unavailable,true);
});

test('a different tab cannot replace an already tracked operation ID',async()=>{
 setup(undefined,async()=>Response.json(known()));
 try{
  await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});
  const event=new Event('storage');event.key='celikpanel.system-update-operation.v1';event.newValue=JSON.stringify({marker_version:1,request_id:other});
  await act(async()=>window.dispatchEvent(event));await act(async()=>tree.root.findByType('button').props.onClick());
  assert.ok(calls.every(([url])=>url.endsWith(id)));assert.ok(JSON.stringify(tree.toJSON()).includes(id));assert.ok(!JSON.stringify(tree.toJSON()).includes(other));
 }finally{await clean()}
});


test('malformed exact request identity cannot enter the recovery parser',()=>{
 assert.throws(()=>parseRecoveryObservation({...known(),request_id:'../latest'},'../latest'));
});

test('a rejected lazy provider reaches the eager recovery boundary',async()=>{
 const app=readFileSync(new URL('../src/App.tsx',import.meta.url),'utf8');
 const source=app.slice(app.indexOf('class RecoveryBoundary'),app.indexOf('function App()'));
 const compiled=ts.transpileModule(source+'\nexport { RecoveryBoundary };',{compilerOptions:{jsx:ts.JsxEmit.React,module:ts.ModuleKind.ES2022,target:ts.ScriptTarget.ES2020}}).outputText;
 const {RecoveryBoundary}=await import(dataModule(`import React from '${reactURL}'; const Component=React.Component; const StandaloneRecovery=()=>React.createElement('aside',null,'eager-recovery');\n`+compiled));
 const Broken=React.lazy(()=>Promise.reject(new Error('optional updater chunk unavailable')));
 const previousError=console.error;console.error=()=>{};
 try{await act(async()=>{tree=Renderer.create(React.createElement(RecoveryBoundary,null,React.createElement(React.Suspense,{fallback:null},React.createElement(Broken))))});assert.equal(tree.root.findByType('aside').props.children,'eager-recovery');}
 finally{console.error=previousError;await clean()}
 const auth=app.slice(app.indexOf('function AuthGate()'),app.indexOf('function StandaloneRecovery()'));
 assert.ok(!auth.includes('<RouteLoadBoundary>'),'initial lazy shells reach the eager root boundary');
 assert.ok(app.slice(app.indexOf('function App()')).indexOf('<RecoveryBoundary>')<app.slice(app.indexOf('function App()')).indexOf('<SystemUpdateOperationProvider>'));
});
