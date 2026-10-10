import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';
import { remoteURL } from './fixtures/shared-layer.mjs';

const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const dataModule = source => 'data:text/javascript;base64,' + Buffer.from(source).toString('base64');
const compile = path => ts.transpileModule(readFileSync(new URL(path,import.meta.url),'utf8'), {compilerOptions:{jsx:ts.JsxEmit.React,module:ts.ModuleKind.ES2022,target:ts.ScriptTarget.ES2020}}).outputText;
const accessURL = dataModule(compile('../src/lib/accessObservation.ts'));
const recoveryURL = dataModule(compile('../src/lib/recoveryObservation.ts'));
const handoverURL = dataModule(compile('../src/lib/panelHandover.ts'));
const {parseAccessObservation} = await import(accessURL);
const {parseRecoveryObservation,savedRecoveryRequestId,reconcileRecoveryObservation,recoveryFailureGuidanceKey} = await import(recoveryURL);
const stub = dataModule(`import React from '${reactURL}';
 export const api = {me:signal=>globalThis.recoveryFixture.me(signal)};
 export const useI18n=()=>({t:key=>key,locale:'en'});
 export const BrandMark=()=>null,LanguageSwitcher=()=>null,Spinner=()=>null;
 export const Button=props=>React.createElement('button',props);
 export const AddressLink = props => React.createElement('a', { href: props.href }, props.address);
`);
// The shared reader and the first-read quiet time are the real ones (2026-10-10).
const quietURL = dataModule(`import React from '${reactURL}';\n`+compile('../src/lib/quietRead.ts').replace(/from ['"]react['"]/g,`from '${reactURL}'`));
function rewritten(path) { return dataModule(`import React from '${reactURL}';\n`+compile(path).replace(/from ['"]([^'"]+)['"]/g,(_,specifier)=>`from '${specifier==='react'?reactURL:specifier.endsWith('/recoveryObservation')?recoveryURL:specifier.endsWith('/panelHandover')?handoverURL:specifier.endsWith('/remote')?remoteURL:specifier.endsWith('/quietRead')?quietURL:stub}'`)); }
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

test('startup transitions to ready through read-only checks; a refused background request starts one read and stays unknown while that read fails',async()=>{
 let ready=false,reads=0,me=async()=>admin;
 setup(()=>{reads++;return me()},async()=>Response.json({schema:'celikpanel-panel-availability/v1',state:ready?'ready':'starting'}));
 try{
  await act(async()=>{tree=Renderer.create(React.createElement(Fixture))});assert.equal(session.state,'starting');
  ready=true;await act(async()=>session.retry());assert.equal(session.state,'ready');
  // The server cannot read the session: unknown, never signed out.
  me=async()=>{throw new Error('503 AUTH_STATUS_UNAVAILABLE')};
  await act(async()=>session.markUnavailable(true));
  assert.equal(session.state,'auth_unavailable');assert.equal(session.user,null);
  // Pages that stay mounted report the same refusal with every poll: no further read is started.
  const before=reads;
  await act(async()=>{session.markUnavailable(true);session.markUnavailable();session.markUnavailable(true)});
  assert.equal(reads,before);assert.equal(session.state,'auth_unavailable');
  // The read that follows decides: the session is ready again once the server answers.
  me=async()=>admin;await act(async()=>session.retry());assert.equal(session.state,'ready');assert.equal(session.user,admin);
  // PANEL_STARTING from a background request: one read reports what the Panel says now.
  ready=false;const starting=reads;
  await act(async()=>session.markUnavailable());
  assert.equal(session.state,'starting');assert.equal(session.user,admin);assert.equal(reads,starting+1);
  // A report that the server no longer stands by is corrected by that same read.
  ready=true;await act(async()=>session.retry());assert.equal(session.state,'ready');
  await act(async()=>session.markUnavailable());assert.equal(session.state,'ready');
  assert.ok(calls.every(([,options])=>!options.method));
 }finally{await clean()}
});

test('every unknown session state is read again by itself, also an unreadable session',async()=>{
 const previous=window.setInterval;const intervals=[];
 window.setInterval=(fn,ms)=>{intervals.push(ms);return 0};
 try{
  for(const [me,fetcher,expected] of [[async()=>{throw new Error('offline')},undefined,'auth_unavailable'],[async()=>admin,async()=>Response.json({}, {status:503}),'availability_unavailable'],[async()=>admin,async()=>Response.json({schema:'celikpanel-panel-availability/v1',state:'starting'}),'starting']]){
   intervals.length=0;setup(me,fetcher);
   try{await act(async()=>{tree=Renderer.create(React.createElement(Fixture))});assert.equal(session.state,expected);assert.deepEqual(intervals,[10000],expected);}finally{await clean()}
  }
 }finally{window.setInterval=previous}
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


test('optional waiting guidance keeps v1 semantics and ignores untrusted or inapplicable hints',()=>{
 const base={...known('running'),phase:'recovering',reason:'recovery_running',previous_failure:'update_failed'};
 for(const reason of ['initializing','starting','stopping']) {
  const value=parseRecoveryObservation({...base,waiting_for:reason},id);
  assert.equal(value.waiting_for,reason);assert.equal(value.previous_failure,'update_failed');assert.equal(value.terminal_proof,'none');
 }
 for(const waiting_for of [undefined,'unknown','private-diagnostic',{},null])assert.equal(parseRecoveryObservation({...base,waiting_for},id).waiting_for,undefined);
 for(const phase of ['failed','recovered','succeeded'])assert.equal(parseRecoveryObservation({...known(phase),waiting_for:'starting'},id).waiting_for,undefined);
 assert.equal(parseRecoveryObservation({...base,observation:'unavailable',waiting_for:'starting'},id).waiting_for,undefined);
 const waited=parseRecoveryObservation({...base,waiting_for:'starting'},id);
 assert.equal(reconcileRecoveryObservation(waited,parseRecoveryObservation(base,id)).record.waiting_for,undefined);
});

test('waiting recovery shows automatic continuation and known failure; checking is GET only',async()=>{
 const waited={...known('running'),phase:'recovering',reason:'recovery_running',waiting_for:'starting',previous_failure:'update_failed'};
 setup(async()=>admin,async()=>Response.json(waited));
 try {
  await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});
  const content=JSON.stringify(tree.toJSON());assert.ok(content.includes('recovery.wait.starting'));assert.ok(content.includes('recovery.wait.next'));assert.ok(content.includes('recovery.reason.update_failed'));
  assert.ok(!content.includes('recovery.next.recovering'));
  await act(async()=>{await tree.root.findByType('button').props.onClick()});
  assert.equal(calls.length,2);assert.ok(calls.every(([url,options])=>url.endsWith(id)&&(!options.method||options.method==='GET')));
  globalThis.fetch=async()=>{throw new Error('offline')};
  await act(async()=>{await tree.root.findByType('button').props.onClick()});
  assert.ok(JSON.stringify(tree.toJSON()).includes('recovery.wait.starting'));assert.ok(JSON.stringify(tree.toJSON()).includes('recovery.observationUnavailable'));
 }finally{await clean()}
});


test('automatic pause guidance is optional and applies only to a known recovery-required result',()=>{
 const base={...known('running'),phase:'recovery_required',reason:'recovery_incomplete',previous_failure:'update_failed'};
 const paused=parseRecoveryObservation({...base,automatic_recovery:'paused_retry_limit'},id);
 assert.equal(paused.automatic_recovery,'paused_retry_limit');assert.equal(paused.previous_failure,'update_failed');
 for(const automatic_recovery of [undefined,'running','private-diagnostic',{},null])assert.equal(parseRecoveryObservation({...base,automatic_recovery},id).automatic_recovery,undefined);
 for(const phase of ['failed','running','recovered','succeeded'])assert.equal(parseRecoveryObservation({...known(phase),automatic_recovery:'paused_retry_limit'},id).automatic_recovery,undefined);
 const unknown=parseRecoveryObservation({...base,observation:'unavailable',automatic_recovery:'paused_retry_limit'},id);
 assert.equal(unknown.automatic_recovery,undefined);assert.equal(reconcileRecoveryObservation(paused,unknown).record,paused);
 assert.equal(reconcileRecoveryObservation(paused,parseRecoveryObservation(base,id)).record.automatic_recovery,undefined);
 const terminal=parseRecoveryObservation(known('recovered'),id);
 assert.equal(reconcileRecoveryObservation(terminal,paused).record,terminal);
});

test('paused recovery explains owner action, keeps the failure and only reads on check/reload',async()=>{
 const paused={...known('running'),phase:'recovery_required',reason:'recovery_incomplete',automatic_recovery:'paused_retry_limit',previous_failure:'update_failed'};
 setup(async()=>admin,async()=>Response.json(paused));
 try {
  await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});
  const content=JSON.stringify(tree.toJSON());
  for(const text of ['recovery.automatic.pausedTitle','recovery.automatic.pausedHelp','recovery.automatic.resume','sudo journalctl -u celikpanel-release-recovery.service','recovery.reason.update_failed'])assert.ok(content.includes(text));
  assert.ok(!content.includes('recovery.next.recovering'));
  await act(async()=>{await tree.root.findByType('button').props.onClick()});
  await act(async()=>tree.unmount());tree=undefined;
  await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});
  assert.ok(JSON.stringify(tree.toJSON()).includes('recovery.automatic.pausedTitle'));
  assert.equal(calls.length,3);assert.ok(calls.every(([url,options])=>url.endsWith(id)&&(!options.method||options.method==='GET')));
  globalThis.fetch=async()=>{throw new Error('offline')};
  await act(async()=>{await tree.root.findByType('button').props.onClick()});
  const offline=JSON.stringify(tree.toJSON());assert.ok(offline.includes('recovery.automatic.pausedTitle'));assert.ok(offline.includes('recovery.observationUnavailable'));
 }finally{await clean()}
});

test('typed update cause is optional, allowlisted and bound to the update failure; older-style unknown values stay generic',()=>{
 const failed={...known('failed'),previous_failure:'update_failed'};
 const typed=parseRecoveryObservation({...failed,failure_code:'candidate_panel_startup_check_failed'},id);
 assert.equal(typed.failure_code,'candidate_panel_startup_check_failed');assert.equal(typed.reason,'update_failed');
 for(const failure_code of [undefined,'future_code','update_failed',{},null])assert.equal(parseRecoveryObservation({...failed,failure_code},id).failure_code,undefined);
 // A later recovery failure hides the update cause; the record itself stays valid.
 const later=parseRecoveryObservation({...known('running'),phase:'recovery_required',reason:'recovery_failed',previous_failure:'recovery_failed',failure_code:'panel_start_unverified'},id);
 assert.equal(later.failure_code,undefined);assert.equal(later.observation,'known');
 // Reconcile keeps the cause with its carried failure and drops it for another failure.
 const recovering=parseRecoveryObservation({...known('running'),phase:'recovering',reason:'recovery_running'},id);
 assert.equal(reconcileRecoveryObservation(typed,recovering).record.failure_code,'candidate_panel_startup_check_failed');
 const guidance=(phase,code,extra={})=>recoveryFailureGuidanceKey(parseRecoveryObservation({...known(phase),previous_failure:'update_failed',failure_code:code,...extra},id));
 assert.equal(guidance('recovered','candidate_panel_startup_check_failed'),'recovery.failure.candidate_panel_startup_check_failed.recovered');
 assert.equal(guidance('failed','candidate_panel_startup_check_failed'),'recovery.failure.candidate_panel_startup_check_failed.returning');
 assert.equal(guidance('failed','panel_start_unverified'),'recovery.failure.panel_start_unverified.pending');
 assert.equal(guidance('succeeded','panel_start_unverified'),undefined);
 assert.equal(guidance('recovered','panel_start_unverified'),undefined);
 assert.equal(guidance('failed','future_code'),undefined);
 assert.equal(recoveryFailureGuidanceKey(parseRecoveryObservation({...known('running'),phase:'recovering',reason:'recovery_running',previous_failure:'update_failed',failure_code:'panel_start_unverified',waiting_for:'starting'},id)),undefined);
});

test('typed causes replace only the guidance text and the failure label; every key exists in EN and TR',async()=>{
 const locale=name=>readFileSync(new URL(`../src/i18n/${name}.ts`,import.meta.url),'utf8');
 for(const key of ['recovery.reason.candidate_panel_startup_check_failed','recovery.reason.panel_start_unverified','recovery.failure.candidate_panel_startup_check_failed.returning','recovery.failure.candidate_panel_startup_check_failed.recovered','recovery.failure.panel_start_unverified.pending'])
  for(const name of ['en','tr'])assert.ok(locale(name).includes(`'${key}':`),`${name} lacks ${key}`);
 assert.ok(locale('en').includes('sudo journalctl -u celikpanel-panel -n 50')&&locale('tr').includes('sudo journalctl -u celikpanel-panel -n 50'));
 for(const [record,next,reason] of [
  [{...known('recovered'),previous_failure:'update_failed',failure_code:'candidate_panel_startup_check_failed'},'recovery.failure.candidate_panel_startup_check_failed.recovered','recovery.reason.candidate_panel_startup_check_failed'],
  [{...known('failed'),previous_failure:'update_failed',failure_code:'panel_start_unverified'},'recovery.failure.panel_start_unverified.pending','recovery.reason.panel_start_unverified'],
 ]){
  setup(async()=>admin,async()=>Response.json(record));
  try {
   await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});
   const content=JSON.stringify(tree.toJSON());
   assert.ok(content.includes(next),content);assert.ok(content.includes(reason));assert.ok(content.includes(`recovery.phase.${record.phase}`));
   assert.ok(!content.includes(`recovery.next.${record.phase}`));
   assert.ok(calls.every(([,options])=>!options?.method||options.method==='GET'));
  }finally{await clean()}
 }
});

test('a paused recovery names the first typed cause before the pause guidance, and only when paused',async()=>{
 const paused={...known('running'),phase:'recovery_required',reason:'recovery_incomplete',automatic_recovery:'paused_retry_limit',previous_failure:'recovery_failed'};
 for(const [first,expected] of [['panel_start_unverified','recovery.automatic.cause.panel_start_unverified'],['candidate_panel_startup_check_failed','recovery.automatic.cause.candidate_panel_startup_check_failed'],[undefined,null],['future_code',null]]){
  const record=first===undefined?paused:{...paused,first_failure_code:first};
  const parsed=parseRecoveryObservation(record,id);
  assert.equal(parsed.first_failure_code,expected?first:undefined);
  setup(async()=>admin,async()=>Response.json(record));
  try {
   await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});
   const content=JSON.stringify(tree.toJSON());
   assert.ok(content.includes('recovery.automatic.pausedHelp')&&content.includes('recovery.automatic.resume')&&content.includes('sudo journalctl -u celikpanel-release-recovery.service'));
   if(expected){assert.ok(content.indexOf(expected)>0&&content.indexOf(expected)<content.indexOf('recovery.automatic.pausedHelp'),content);}
   else assert.ok(!content.includes('recovery.automatic.cause.'),content);
  }finally{await clean()}
 }
 // Not paused: the field is ignored even if present.
 assert.equal(parseRecoveryObservation({...paused,automatic_recovery:undefined,first_failure_code:'panel_start_unverified'},id).first_failure_code,undefined);
 const locale=name=>readFileSync(new URL(`../src/i18n/${name}.ts`,import.meta.url),'utf8');
 for(const key of ['recovery.automatic.cause.panel_start_unverified','recovery.automatic.cause.candidate_panel_startup_check_failed'])
  for(const name of ['en','tr'])assert.ok(locale(name).includes(`'${key}':`),`${name} lacks ${key}`);
});

// upd3 F1/F2/F3 on the recovery screen: a scheduled retry asks nothing of the
// owner and keeps the first cause; a preflight stop is final; the pause names
// the renewal state. Reads only.
test('scheduled retry, preflight stop and paused renewal guidance on the recovery screen',async()=>{
 const retry={...known('running'),phase:'recovery_required',reason:'recovery_failed',automatic_recovery:'retry_scheduled',previous_failure:'recovery_failed',first_failure_code:'panel_start_unverified'};
 const stop={...known('failed'),previous_failure:'update_failed',failure_code:'recovery_runtime_preflight_failed'};
 const paused={...known('running'),phase:'recovery_required',reason:'recovery_incomplete',automatic_recovery:'paused_retry_limit',previous_failure:'recovery_failed'};
 for(const [record,present,absent] of [
  [retry,['recovery.automatic.retryTitle','recovery.failure.panel_start_unverified.pending','recovery.automatic.retryHelp','recovery.reason.recovery_failed'],['recovery.automatic.pausedTitle','recovery.automatic.pausedHelp','recovery.automatic.resume','recovery.automatic.renewal','celikpanel-release-recovery.service','recovery.next.recovery_required']],
  [stop,['recovery.phase.failed','recovery.failure.recovery_runtime_preflight_failed.stopped','recovery.reason.recovery_runtime_preflight_failed'],['recovery.next.failed','recovery.automatic.']],
  [paused,['recovery.automatic.pausedTitle','recovery.automatic.pausedHelp','recovery.automatic.renewal','recovery.automatic.resume'],['recovery.automatic.retryTitle']],
 ]){
  setup(async()=>admin,async()=>Response.json(record));
  try {
   await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});
   const content=JSON.stringify(tree.toJSON());
   for(const text of present)assert.ok(content.includes(text),`${text} missing: ${content}`);
   for(const text of absent)assert.ok(!content.includes(text),`${text} shown: ${content}`);
   assert.ok(calls.every(([,options])=>!options?.method||options.method==='GET'));
  }finally{await clean()}
 }
 const locale=name=>readFileSync(new URL(`../src/i18n/${name}.ts`,import.meta.url),'utf8');
 for(const key of ['recovery.automatic.retryTitle','recovery.automatic.retryHelp','recovery.automatic.renewal','recovery.reason.recovery_runtime_preflight_failed','recovery.failure.recovery_runtime_preflight_failed.stopped','recovery.automatic.cause.recovery_runtime_preflight_failed'])
  for(const name of ['en','tr'])assert.ok(locale(name).includes(`'${key}':`),`${name} lacks ${key}`);
});

// upd4 F6/O8/O9/F4 on the recovery screen: while the last attempt is finishing
// the owner is not asked to act and the first cause stays; the pause's renewal
// line follows the recorded scheduler state; a verified update shows no
// failure label; a refused update check is final. Reads only.
test('finishing the last attempt, renewal already off, verified after a failure and a refused check on the recovery screen',async()=>{
 const pending={...known('running'),phase:'recovery_required',reason:'recovery_failed',automatic_recovery:'pause_pending',previous_failure:'recovery_failed',first_failure_code:'panel_start_unverified'};
 const pausedOff={...known('running'),phase:'recovery_required',reason:'recovery_incomplete',automatic_recovery:'paused_retry_limit',previous_failure:'recovery_failed',renewal_before_update:'off'};
 const verified={...known('succeeded'),previous_failure:'recovery_failed'};
 const refused={...known('failed'),previous_failure:'update_failed',failure_code:'update_preflight_refused'};
 for(const [record,present,absent] of [
  [pending,['recovery.automatic.pausingTitle','recovery.failure.panel_start_unverified.pending','recovery.automatic.pausingHelp'],['recovery.automatic.pausedTitle','recovery.automatic.pausedHelp','recovery.automatic.retryTitle','recovery.automatic.resume','recovery.automatic.renewal','recovery.next.recovery_required','celikpanel-release-recovery.service']],
  [pausedOff,['recovery.automatic.pausedTitle','recovery.automatic.renewalOff','recovery.automatic.resume'],['"recovery.automatic.renewal"']],
  [verified,['recovery.phase.succeeded','recovery.next.succeeded'],['recovery.previousFailure','recovery.reason.recovery_failed']],
  [refused,['recovery.phase.failed','recovery.failure.recovery_runtime_preflight_failed.stopped','recovery.reason.update_preflight_refused'],['recovery.next.failed','recovery.automatic.']],
 ]){
  setup(async()=>admin,async()=>Response.json(record));
  try {
   await act(async()=>{tree=Renderer.create(React.createElement(RecoveryStatus,{username:'admin'}))});
   const content=JSON.stringify(tree.toJSON());
   for(const text of present)assert.ok(content.includes(text),`${text} missing: ${content}`);
   for(const text of absent)assert.ok(!content.includes(text),`${text} shown: ${content}`);
   assert.ok(calls.every(([,options])=>!options?.method||options.method==='GET'));
  }finally{await clean()}
 }
 // The hints bind to their records only; renewal is read only at the pause.
 assert.equal(parseRecoveryObservation({...pending,reason:'recovery_incomplete'},id).automatic_recovery,undefined);
 assert.equal(parseRecoveryObservation({...pending,renewal_before_update:'off'},id).renewal_before_update,undefined);
 assert.equal(parseRecoveryObservation({...pausedOff,renewal_before_update:'maybe'},id).renewal_before_update,undefined);
 assert.equal(parseRecoveryObservation({...pending,first_failure_code:'update_preflight_refused'},id).first_failure_code,undefined);
 const locale=name=>readFileSync(new URL(`../src/i18n/${name}.ts`,import.meta.url),'utf8');
 for(const key of ['recovery.automatic.pausingTitle','recovery.automatic.pausingHelp','recovery.automatic.renewalOff','recovery.reason.update_preflight_refused'])
  for(const name of ['en','tr'])assert.ok(locale(name).includes(`'${key}':`),`${name} lacks ${key}`);
});

// 2026-10-08: during setup the Panel restarts once to serve its new certificate.
// The page names that only when the server reports a managed certificate for the
// host the setup started in this browser was securing; the saved update result
// stays available but is not presented as the reason.
test('planned certificate handover is named on server evidence only, with reads only',async()=>{
 const host='boston.example.com';
 const marker=JSON.stringify({request_id:'c'.repeat(32),plan_id:'d'.repeat(32),panel_domain:host,handover:true});
 const update=JSON.stringify({state_version:1,phase:'active',marker:{marker_version:1,request_id:id}});
 const storage=setupMarker=>({getItem:key=>key==='celikpanel.setup.start.admin'?setupMarker:update});
 let operation='running';
 const server=served=>async url=>String(url).includes('/panel/access-address')?Response.json({hostname:served}):Response.json(known(operation));
 const render=async cause=>{await act(async()=>{tree=Renderer.create(React.createElement(RecoveryAccess,{user:admin,cause,onRetry(){}}))});await act(async()=>{});return JSON.stringify(tree.toJSON());};
 for(const cause of ['availability','starting']){
  setup(async()=>admin,server(host));globalThis.localStorage=storage(marker);
  try{
   const content=await render(cause);
   assert.ok(content.includes('recovery.handoverTitle')&&content.includes('recovery.handoverHelp'),content);
   assert.ok(!content.includes(`recovery.${cause}Title`)&&!content.includes(`recovery.${cause}Help`),content);
   const link=tree.root.findAllByType('a').find(node=>node.props.href===`https://${host}/setup`);
   assert.ok(link,'the secure address is a real link');
   // An update that is still running is one step away, inside a closed disclosure.
   const details=tree.root.findByType(RecoveryStatus).findByType('details');
   assert.equal(details.props.open,undefined);
   assert.equal(details.findByType('summary').props.children,'recovery.operationTitle');
   assert.ok(calls.every(([,options])=>!options?.method||options.method==='GET'));
  }finally{await clean()}
 }
 // A verified update is not part of the restart at all.
 operation='succeeded';setup(async()=>admin,server(host));globalThis.localStorage=storage(marker);
 try{
  const content=await render('starting');
  assert.ok(content.includes('recovery.handoverTitle'),content);
  assert.equal(tree.root.findAllByType('details').length,0);
  assert.ok(!content.includes('recovery.operationTitle')&&!content.includes('recovery.phase.succeeded'),content);
 }finally{operation='running';await clean()}
 // No server report, another host, a setup without the step, or another cause: the page cannot know.
 for(const [served,saved,cause] of [['',marker,'availability'],['other.example.com',marker,'availability'],[host,JSON.stringify({request_id:'c'.repeat(32),plan_id:'d'.repeat(32),panel_domain:host}),'availability'],[host,marker,'license']]){
  setup(async()=>admin,server(served));globalThis.localStorage=storage(saved);
  try{
   const content=await render(cause);
   assert.ok(content.includes(`recovery.${cause}Title`)&&!content.includes('recovery.handover'),content);
   assert.equal(tree.root.findAllByType('details').length,0);
  }finally{await clean()}
 }
 // The address read failing leaves the ordinary wording.
 setup(async()=>admin,async url=>{if(String(url).includes('/panel/access-address'))throw new Error('offline');return Response.json(known('succeeded'));});globalThis.localStorage=storage(marker);
 try{assert.ok((await render('availability')).includes('recovery.availabilityTitle'));}finally{await clean()}
});

// Owner report, 2026-10-08: an access check showed "Update and recovery status:
// Update verified" for an update that had finished days earlier. A gate draws
// the block only for an operation that is still running, failed or waiting for
// the owner, or whose result cannot be read; it never starts anything.
test('a finished update is not drawn as part of an access or readiness gate',async()=>{
 const saved=(phase,outcome)=>JSON.stringify({state_version:1,phase,marker:{marker_version:1,request_id:id},...(outcome?{outcome}:{})});
 const page=async(cause,record,reply)=>{
  setup(async()=>admin,async()=>reply());globalThis.localStorage={getItem:()=>record};
  await act(async()=>{tree=Renderer.create(React.createElement(RecoveryAccess,{user:admin,cause,onRetry(){}}))});await act(async()=>{});
  return JSON.stringify(tree.toJSON());
 };
 try{
  for(const cause of ['license','availability','starting']){
   // This browser saw the update verified: nothing is drawn and nothing is read.
   let content=await page(cause,saved('terminal','succeeded'),()=>Response.json(known('succeeded')));
   assert.ok(!content.includes('recovery.operationTitle')&&!content.includes(id),content);assert.equal(calls.length,0);await clean();
   // The server reports it verified: read once, then not drawn.
   content=await page(cause,saved('active'),()=>Response.json(known('succeeded')));
   assert.ok(!content.includes('recovery.operationTitle')&&!content.includes('recovery.phase.succeeded'),content);await clean();
   // No saved operation: no block and no "no operation" text.
   content=await page(cause,null,()=>Response.json(known('succeeded')));
   assert.ok(!content.includes('recovery.operationTitle')&&!content.includes('recovery.noOperation'),content);assert.equal(calls.length,0);await clean();
   // Still running, failed, rolled back after a failure, or unreadable: drawn, because it may be the reason or needs the owner.
   for(const [record,reply,text] of [[saved('active'),()=>Response.json(known('running')),'recovery.phase.running'],[saved('active'),()=>Response.json(known('failed')),'recovery.phase.failed'],[saved('terminal','failed'),()=>Response.json(known('recovered')),'recovery.phase.recovered'],[saved('active'),()=>Response.json({}, {status:503}),'recovery.observationUnavailable']]){
    content=await page(cause,record,reply);
    assert.ok(content.includes('recovery.operationTitle')&&content.includes(text)&&content.includes(id),`${cause}: ${text}: ${content}`);
    assert.ok(calls.every(([,options])=>!options?.method||options.method==='GET'));await clean();
   }
  }
  // A page that could not load keeps the full reader, as before.
  const content=await page('bundle',saved('terminal','succeeded'),()=>Response.json(known('succeeded')));
  assert.ok(content.includes('recovery.operationTitle')&&content.includes('recovery.phase.succeeded'),content);
 }finally{await clean()}
});

// The first read being in flight is not a failure, and before the quiet time it
// is not a page either (seventh native record, cell 5, 2026-10-10: the full-page
// "Checking panel access" was painted before any session read had answered, on
// 18 of 18 cold loads). Only the page background is drawn until the quiet time
// has passed or a read has answered; after it, the wait is explained; a known
// negative replaces the screen at once. "Could not be checked" needs a read that failed.
// Ilk okuma yanit vermeden sessiz sure icinde sayfa cizilmez; bilinen olumsuz sonuc hemen gosterilir.
const QUIET_MS=1500,PROLONGED_MS=30000;
function handTimers(){
 const previous=window.setTimeout,previousClear=window.clearTimeout,timers=[];
 window.setTimeout=(fn,ms)=>{if(ms===QUIET_MS||ms===PROLONGED_MS){const timer={fn,ms,live:true};timers.push(timer);return timer;}return previous(fn,ms);};
 window.clearTimeout=timer=>{if(timer&&typeof timer==='object'&&'live' in timer)timer.live=false;else previousClear(timer);};
 return {
  fire:async ms=>{const due=timers.filter(timer=>timer.live&&timer.ms===ms);for(const timer of due)timer.live=false;await act(async()=>{for(const timer of due)timer.fn()});return due.length;},
  restore(){window.setTimeout=previous;window.clearTimeout=previousClear;},
 };
}
const quietSurface=()=>tree.root.findAll(node=>typeof node.type==='string'&&node.props['data-access-quiet']!==undefined).length===1;
const labels=()=>tree.root.findAllByType('button').map(node=>[node.props.children].flat(2).filter(item=>typeof item==='string').join(''));

test('a cold load draws nothing before the quiet time, then the waiting state; a known negative replaces it at once',async()=>{
 const clock=handTimers();
 const render=async props=>{await act(async()=>{tree=Renderer.create(React.createElement(RecoveryAccess,{onRetry(){},...props}))});await act(async()=>{});return JSON.stringify(tree.toJSON());};
 const text=()=>JSON.stringify(tree.toJSON());
 try{
  setup();
  // The session read in flight: only the page background. No sentence, no button, no reload.
  let content=await render({user:null,cause:'checking',checking:true});
  assert.ok(quietSurface(),content);assert.ok(!content.includes('recovery.'),content);assert.deepEqual(labels(),[]);
  assert.equal(tree.root.findAllByType('h1').length,0);assert.equal(calls.length,0,'nothing is read for the page itself');
  // Quiet time passed, still no answer: the wait, that nobody acts, a busy check; the reload only after half a minute.
  assert.equal(await clock.fire(QUIET_MS),1);
  content=text();
  assert.ok(content.includes('recovery.checkingTitle')&&content.includes('recovery.waitingHelp'),content);
  for(const absent of ['recovery.checkingHelp','recovery.authTitle','recovery.availabilityTitle','recovery.licenseTitle','app.reload','recovery.waitingProlonged','recovery.operationTitle'])assert.ok(!content.includes(absent),`${absent}: ${content}`);
  assert.deepEqual(labels(),['recovery.checking']);assert.equal(tree.root.findByType('button').props.disabled,true);
  assert.equal(await clock.fire(PROLONGED_MS),1);
  assert.ok(text().includes('recovery.waitingProlonged'));assert.deepEqual(labels(),['recovery.checking','app.reload']);
  // The next gate of the same load (the interface arrived; its own session read is in flight) continues the
  // explained wait: it does not go blank again (browser run, 2026-10-10).
  await act(async()=>tree.update(React.createElement(RecoveryAccess,{key:'next',user:null,cause:'checking',checking:true,onRetry(){}})));
  assert.ok(!quietSurface()&&text().includes('recovery.checkingTitle')&&text().includes('recovery.waitingHelp'),text());
  await clean();setup();
  // Once nothing explained is on screen, a new wait starts quiet again.
  await render({user:null,cause:'checking',checking:true});
  assert.ok(quietSurface());
  await clean();setup();
  // After sign-in: the identity is known and readiness is in flight. The same quiet; then the read fails and the
  // failure replaces the background at once, with both actions, without waiting for the quiet time.
  content=await render({user:admin,cause:'checking',checking:true});
  assert.ok(quietSurface()&&!content.includes('admin'),content);
  await act(async()=>tree.update(React.createElement(RecoveryAccess,{user:admin,cause:'availability',onRetry(){}})));
  content=text();
  assert.ok(!quietSurface()&&content.includes('recovery.availabilityTitle')&&content.includes('recovery.availabilityHelp'),content);
  assert.deepEqual(labels().slice(0,2),['recovery.retry','app.reload']);
  await clean();setup();
  // A known negative on its own (the Panel says it is starting; the session could not be read): drawn at once.
  for(const [cause,user] of [['starting',admin],['auth',null],['availability',admin],['bundle',admin]]){
   content=await render({user,cause});
   assert.ok(!quietSurface()&&content.includes(`recovery.${cause}Title`),`${cause}: ${content}`);
   assert.ok(labels().includes('app.reload'),cause);
   await clean();setup();
  }
  // A check the owner asked for from a known negative is not hidden again behind the quiet time.
  content=await render({user:null,cause:'auth',checking:true});
  assert.ok(!quietSurface()&&content.includes('recovery.checkingTitle'),content);
  await clean();setup();
  // Session and readiness confirmed, the interface still loading: the same quiet, then what is awaited.
  content=await render({user:admin,cause:'loading'});
  assert.ok(quietSurface(),content);
  assert.equal(await clock.fire(QUIET_MS),1);
  content=text();
  assert.ok(content.includes('recovery.loadingTitle')&&content.includes('recovery.loadingHelp')&&!content.includes('recovery.checkingTitle'),content);
  assert.deepEqual(labels(),[],'there is nothing to check: the interface is on its way');
  assert.equal(await clock.fire(PROLONGED_MS),1);assert.deepEqual(labels(),['app.reload']);
  assert.equal(calls.length,0,'no operation is read while nothing is known');
 }finally{clock.restore();await clean();}
 const app=readFileSync(new URL('../src/App.tsx',import.meta.url),'utf8');
 const gate=app.slice(app.indexOf('function AuthGate()'),app.indexOf('function StandaloneRecovery('));
 assert.match(gate,/const cause = state === 'checking' \? 'checking' : state === 'auth_unavailable' \|\| !user \? 'auth' : state === 'starting' \? 'starting' : 'availability';/);
 // The interface still being fetched is a wait as well.
 assert.match(app,/<Suspense fallback=\{<StandaloneRecovery loading \/>\}>/);
 const lock=readFileSync(new URL('../src/components/LicenseLockScreen.tsx',import.meta.url),'utf8');
 // A license decision is not explained by an update: no "no update operation ID" text on the activation page.
 assert.match(lock,/role === 'admin' && !checking && <RecoveryStatus username=\{user\.username\} unfinishedOnly \/>/);
 // The quiet time is the hold layer's.
 assert.match(readFileSync(new URL('../src/lib/quietRead.ts',import.meta.url),'utf8'),/export const QUIET_READ_MS = 1500;/);
 assert.match(readFileSync(new URL('../src/components/AccessHold.tsx',import.meta.url),'utf8'),/export const ACCESS_HOLD_QUIET_MS = 1500;/);
});

// The recovery route as App.tsx mounts it, with the real session reads: while the
// interface is fetched, or after it failed to load. A Panel that is down or
// starting is a known answer and is shown at once; it is never hidden behind the
// quiet time. Kurtarma yolu: kapali ya da baslayan Panel hemen gosterilir.
test('the standalone recovery route shows a Panel that is down or starting at once, and hides only a read in flight',async()=>{
 const app=readFileSync(new URL('../src/App.tsx',import.meta.url),'utf8');
 const slice=app.slice(app.indexOf('function StandaloneRecovery('),app.indexOf('class RecoveryBoundary'));
 const compiled=ts.transpileModule(slice+'\nexport { StandaloneRecovery };',{compilerOptions:{jsx:ts.JsxEmit.React,module:ts.ModuleKind.ES2022,target:ts.ScriptTarget.ES2020}}).outputText;
 const {StandaloneRecovery}=await import(dataModule(`import React, { useCallback } from '${reactURL}';
  import { usePanelSession } from '${rewritten('../src/auth/usePanelSession.ts')}';
  import { RecoveryAccess } from '${rewritten('../src/components/RecoveryAccess.tsx')}';
  const Login = () => React.createElement('form', null, 'sign-in');
  ${compiled}`));
 const ready=state=>async()=>Response.json({schema:'celikpanel-panel-availability/v1',state});
 const render=async props=>{await act(async()=>{tree=Renderer.create(React.createElement(StandaloneRecovery,props))});await act(async()=>{});return JSON.stringify(tree.toJSON());};
 const cases=[
  // Nothing answers: the session read fails, which is known, so it is said at once.
  [async()=>{throw new TypeError('Failed to fetch')},undefined,{loading:true},'recovery.authTitle'],
  [async()=>admin,ready('starting'),{loading:true},'recovery.startingTitle'],
  [async()=>admin,async()=>Response.json({}, {status:503}),{loading:true},'recovery.availabilityTitle'],
  // The interface failed to load: the full reader, at once.
  [async()=>admin,ready('ready'),{},'recovery.bundleTitle'],
 ];
 for(const [me,fetcher,props,title] of cases){
  setup(me,fetcher);globalThis.localStorage={getItem:()=>null};
  try{const content=await render(props);assert.ok(!quietSurface()&&content.includes(title),`${title}: ${content}`);}finally{await clean();}
 }
 // Reads in flight, or only the interface still on its way: the background alone.
 for(const [me,fetcher] of [[()=>new Promise(()=>{}),undefined],[async()=>admin,()=>new Promise(()=>{})],[async()=>admin,ready('ready')]]){
  setup(me,fetcher);globalThis.localStorage={getItem:()=>null};
  try{const content=await render({loading:true});assert.ok(quietSurface()&&!content.includes('recovery.'),content);}finally{await clean();}
 }
 // No session: the sign-in form, as before.
 setup(async()=>null);
 try{await render({loading:true});assert.equal(tree.root.findAllByType('form').length,1);}finally{await clean();}
});
