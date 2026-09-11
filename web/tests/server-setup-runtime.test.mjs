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
const dataURL = text => `data:text/javascript;base64,${Buffer.from(text).toString('base64')}`;
const compile = path => ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 },
}).outputText;
const setupURL = dataURL(compile('../src/lib/serverSetup.ts'));
const operationURL = dataURL(compile('../src/lib/serverSetupOperation.ts').replace("from './serverSetup'", `from '${setupURL}'`));
const setup = await import(setupURL);
const operations = await import(operationURL);
const stub = dataURL(`
import React from '${reactURL}';
export const useAuth = () => globalThis.setupFixture.auth;
export const useI18n = () => ({ t:(key, vars)=>globalThis.setupFixture.translations?.[key]??key, screensReady:true, screensFailed:false });
export const useLocation = () => ({ pathname:globalThis.setupFixture.path });
export const Navigate = props => React.createElement('redirect',props);
export const Link = props => React.createElement('a',{...props,href:props.to});
export const useServerSetup = () => globalThis.setupFixture.context;
export const ServerSetupShell = props => React.createElement('main',props);
export const BrandMark=()=>null, LanguageSwitcher=()=>null, ThemeSwitcher=()=>null, ChangePasswordModal=()=>null;
export const Button=props=>React.createElement('button',props);
export const Spinner=()=>React.createElement('span',null,'loading');
export const ArrowRight=()=>null, Check=()=>null, Circle=()=>null, Loader2=()=>null;
export const ServerSetupDNSConnection=props=>React.createElement('remote-connection',props);
export const inputClass='';
`);
const choiceURL = dataURL(`import React from '${reactURL}';\n` + compile('../src/components/ServerSetupChoice.tsx').replace(/from ['"]([^'"]+)['"]/g, (_, path) => `from '${path === 'react' ? reactURL : path.endsWith('/serverSetup') ? setupURL : stub}'`));
async function loadComponent(name) {
    const source = compile(`../src/components/${name}.tsx`).replace(/from ['"]([^'"]+)['"]/g, (_, path) => {
        const url = path === 'react' ? reactURL : path.endsWith('/ServerSetupChoice') ? choiceURL : path.endsWith('/serverSetupOperation') ? operationURL : path.endsWith('/serverSetup') ? setupURL : stub;
        return `from '${url}'`;
    });
    return (await import(dataURL(`import React from '${reactURL}';\n${source}`)))[name];
}
const Gate = await loadComponent('ServerSetupGate');
const Wizard = await loadComponent('ServerSetup');
const original = { fetch:globalThis.fetch, window:globalThis.window, localStorage:globalThis.localStorage };
const fresh = () => ({ version:1,revision:1,origin:'fresh',status:'new',required:true,
    draft:{purpose:'web',panel_domain:'',mail_hostname:'',dns_mode:'local',remote_dns_connection_id:'',dns_engine:'pdns',dns_role:'primary',ns1:'',ns2:'',local_ip:'',peer_ip:'',peer_ns:'',node_version:'',database:'mariadb'},checks:[] });
let calls, state, tree, store, fixture;
function init(role='admin', overrides={}) {
    calls=[]; state={...fresh(),...overrides}; tree=null; store=new Map();
    const context={snapshot:state,accept(next){state=next;context.snapshot=next;},async reload(){return state;}};
    fixture=globalThis.setupFixture={auth:{role,user:{username:role},logout(){}},path:'/',context};
    globalThis.localStorage={getItem:key=>store.get(key)??null,setItem:(key,value)=>store.set(key,value),removeItem:key=>store.delete(key)};
    globalThis.window={setTimeout,clearTimeout,setInterval,clearInterval,location:{hostname:'192.0.2.4',reload(){}}};
    globalThis.fetch=async(url,options)=>{
        calls.push({url,options});
        if(url.includes('/setup/operation'))return Response.json(null);
        if(url==='/api/v1/setup'&&options?.method==='PUT'){
            const body=JSON.parse(options.body);assert.equal(body.revision,state.revision);
            state={...state,draft:body.draft,revision:state.revision+1,status:'draft'};return Response.json(state);
        }
        if(url==='/api/v1/setup/plan')return Response.json(plan());
        return Response.json(state);
    };
}
function plan(extra={}){return{id:'a'.repeat(32),version:1,revision:state.revision,purpose:state.draft.purpose,steps:[{id:'verify',kind:'verify',target:'web'}],blockers:[],can_start:true,tcp_ports:[22,80,443,2083],udp_ports:[],preserve_ssh:true,persist_firewall:true,contact_email:'admin@example.com',...extra};}
function execution(marker,status='running'){return{id:'c'.repeat(32),plan_id:marker.plan_id,request_id:marker.request_id,status,phase:'queued',steps:[{id:'verify',kind:'verify',target:'web',status:'pending'}]};}
async function mount(Component=Wizard){await act(async()=>{tree=Renderer.create(React.createElement(Component,null,React.createElement('section',{'data-management':true})));});}
async function cleanup(){if(tree)await act(async()=>tree.unmount());for(const[key,value]of Object.entries(original)){if(value===undefined)delete globalThis[key];else globalThis[key]=value;}delete globalThis.setupFixture;}
const findButton=label=>tree.root.findAllByType('button').find(button=>button.props.children===label);
async function submit(){await act(async()=>tree.root.findByType('form').props.onSubmit({preventDefault(){}}));}
async function toReview(){await submit();await act(async()=>tree.root.findByProps({id:'setup-panel_domain'}).props.onChange({target:{value:'panel.example.com'}}));await act(async()=>tree.root.findByProps({name:'setup-dns',value:'external'}).props.onChange());await submit();}

test('setup state decoder rejects absent provenance and contradictory completion',()=>{
    assert.equal(setup.decodeServerSetup({...fresh(),origin:undefined}),null);
    assert.equal(setup.decodeServerSetup({...fresh(),required:undefined}),null);
    assert.equal(setup.decodeServerSetup({...fresh(),status:'ready',required:true}),null);
    assert.equal(setup.decodeServerSetup({...fresh(),status:'ready',required:false}).status,'ready');
    assert.equal(setup.shouldOpenServerSetup(fresh(),'/domains'),true);
    assert.equal(setup.shouldOpenServerSetup(fresh(),'/settings'),false);
    assert.equal(setup.shouldOpenServerSetup({...fresh(),origin:'legacy',status:'legacy',required:false},'/'),false);
});

test('setup gate never fetches host setup for tenants; fresh administrators go to setup',async()=>{
    for(const role of ['reseller','customer','additional_user','admin']){
        init(role);try{await mount(Gate);assert.equal(calls.length,role==='admin'?1:0);assert.equal(tree.root.findAllByType('redirect').length,role==='admin'?1:0);if(role==='admin')assert.equal(tree.root.findByType('redirect').props.to,'/setup');}finally{await cleanup();}
    }
});
test('legacy and completed hosts keep dashboard access, including empty domain inventories',async()=>{
    for(const status of ['legacy','ready']){init('admin',{status,origin:'legacy',required:false});try{await mount(Gate);assert.equal(tree.root.findAllByProps({'data-management':true}).length,1);assert.equal(tree.root.findAllByType('redirect').length,0);}finally{await cleanup();}}
});
test('failed or malformed assessment does not guess fresh state or expose management',async()=>{
    for(const reply of [()=>Response.json({}, {status:503}),()=>Response.json({status:'new'}),()=>{throw new Error('offline')}]){
        init();globalThis.fetch=async()=>reply();try{await mount(Gate);assert.equal(tree.root.findAllByType('redirect').length,0);assert.equal(tree.root.findAllByProps({'data-management':true}).length,0);assert.ok(findButton('common.retry'));}finally{await cleanup();}
    }
});
test('opening the wizard performs only read requests; purpose and plan are persisted before installation',async()=>{
    init();try{await mount();assert.ok(calls.every(call=>!call.options?.method||call.options.method==='GET'));await toReview();assert.equal(state.draft.panel_domain,'panel.example.com');assert.equal(state.draft.dns_mode,'external');assert.equal(calls.filter(call=>call.options?.method==='PUT').length,2);assert.equal(calls.some(call=>call.url==='/api/v1/setup/start'),false);assert.equal(findButton('setup.start').props.disabled,true);}finally{await cleanup();}
});
test('a blocked plan cannot run, and changing material inputs requires a new review',async()=>{
    init();const read=fetch;globalThis.fetch=async(url,options)=>url==='/api/v1/setup/plan'?Response.json(plan({blockers:['server_setup_dns_mode_unsupported'],can_start:false})):read(url,options);
    try{await mount();await toReview();assert.equal(findButton('setup.start').props.disabled,true);assert.equal(tree.root.findAllByProps({type:'checkbox'}).length,0);await act(async()=>findButton('setup.back').props.onClick());assert.equal(findButton('setup.start'),undefined);assert.equal(tree.root.findByProps({id:'setup-panel_domain'}).props.value,'panel.example.com');}finally{await cleanup();}
});
test('a lost start response reconciles exact request identity and never starts twice',async()=>{
    init();let stored;const read=fetch;
    globalThis.fetch=async(url,options)=>{
        if(url==='/api/v1/setup/start'){calls.push({url,options});stored=JSON.parse(options.body);assert.ok(store.size>0,'recovery marker persisted before mutation');throw new Error('response lost');}
        if(url.includes('/setup/operation?request_id=')){calls.push({url,options});assert.ok(url.endsWith(stored.request_id));return Response.json(execution(stored));}
        return read(url,options);
    };
    try{await mount();await toReview();await act(async()=>tree.root.findByProps({type:'checkbox'}).props.onChange({target:{checked:true}}));const start=findButton('setup.start');await act(async()=>{void start.props.onClick();void start.props.onClick();});assert.equal(calls.filter(call=>call.url==='/api/v1/setup/start').length,1);assert.equal(tree.root.findAllByType('form').length,0);assert.ok(JSON.stringify(tree.toJSON()).includes('setup.installing'));}finally{await cleanup();}
});
test('operation success alone cannot show completion when the server readiness state is not ready',async()=>{
    init('admin',{status:'waiting'});const saved={plan_id:'a'.repeat(32),request_id:'b'.repeat(32),panel_domain:'panel.example.com'};store.set('celikpanel.setup.start.admin',JSON.stringify(saved));const read=fetch;
    globalThis.fetch=async(url,options)=>url.includes('/setup/operation')?Response.json(execution(saved,'succeeded')):read(url,options);
    try{await mount();assert.ok(JSON.stringify(tree.toJSON()).includes('setup.verificationFailed'));assert.ok(!JSON.stringify(tree.toJSON()).includes('setup.completeTitle'));}finally{await cleanup();}
});
test('execution decoder rejects an unrelated operation and unsafe panel links',()=>{
    const marker={request_id:'b'.repeat(32),plan_id:'a'.repeat(32),panel_domain:'panel.example.com'};
    assert.ok(operations.decodeSetupExecution(execution(marker),marker));
    assert.equal(operations.decodeSetupExecution({...execution(marker),plan_id:'f'.repeat(32)},marker),null);
    assert.equal(operations.safeSetupPanelURL('https://attacker.example/setup','panel.example.com'),null);
    assert.equal(operations.safeSetupPanelURL('javascript:alert(1)','panel.example.com'),null);
    assert.equal(operations.safeSetupPanelURL('https://panel.example.com:2083/','panel.example.com'),'https://panel.example.com:2083/');
});

test('waiting plans can be revised only after server proof; rejection preserves the running context',async()=>{
    for(const permitted of [false,true]){
        init('admin',{status:'waiting'});
        state.draft.panel_domain='panel.example.com';
        const marker={plan_id:'a'.repeat(32),request_id:'b'.repeat(32),panel_domain:'panel.example.com'};
        store.set('celikpanel.setup.start.admin',JSON.stringify(marker));
        const running={...execution(marker,'waiting'),phase:'verification',steps:[{id:'verify',kind:'verify',target:'web',status:'succeeded'}]};
        const read=fetch;
        globalThis.fetch=async(url,options)=>{
            if(url.includes('/setup/operation'))return Response.json(running);
            if(url==='/api/v1/setup/revise'){
                calls.push({url,options});
                assert.deepEqual(JSON.parse(options.body),{revision:state.revision,execution_id:running.id});
                if(!permitted)return Response.json({code:'setup_not_ready'},{status:409});
                state={...state,revision:state.revision+1,status:'draft'};
                return Response.json(state);
            }
            return read(url,options);
        };
        try{
            await mount();await act(async()=>findButton('setup.editPlan').props.onClick());
            assert.equal(calls.filter(call=>call.url==='/api/v1/setup/revise').length,1);
            assert.equal(tree.root.findAllByType('form').length,permitted?1:0);
            assert.equal(store.size,permitted?0:1);
            if(permitted)assert.equal(tree.root.findByProps({id:'setup-panel_domain'}).props.value,'panel.example.com');
            else assert.ok(JSON.stringify(tree.toJSON()).includes('setup.reviseBlocked'));
        }finally{await cleanup();}
    }
});


test('application setup selects an exact official LTS version; unavailable lists offer retry without installation',async()=>{
    for (const available of [true,false]) {
        init();const base=globalThis.fetch;
        globalThis.fetch=async(url,options)=>{
            if(url==='/api/v1/runtimes/node/lts'){calls.push({url,options});return available?Response.json({releases:[{version:'24.18.0',name:'Fixture'}]}):Response.json({}, {status:502});}
            return base(url,options);
        };
        try{
            await mount();assert.equal(calls.some(call=>call.url==='/api/v1/runtimes/node/lts'),false);
            await act(async()=>tree.root.findByProps({name:'setup-purpose',value:'application'}).props.onChange());await submit();
            const selector=tree.root.findByProps({id:'setup-node_version'});assert.equal(selector.type,'select');assert.equal(selector.props.disabled,!available);
            if(available){await act(async()=>selector.props.onChange({target:{value:'24.18.0'}}));assert.equal(tree.root.findByProps({id:'setup-node_version'}).props.value,'24.18.0');}
            else assert.ok(findButton('common.retry'));
            assert.equal(calls.some(call=>call.url==='/api/v1/setup/start'),false);
        }finally{await cleanup();}
    }
});


test('license waiting cannot reopen an unfinished setup plan and mail certificate steps decode',async()=>{
    init();const current={...execution({plan_id:'a'.repeat(32),request_id:'c'.repeat(32)},'waiting'),phase:'license',error:{code:'license_required',message:'activate'},steps:[{id:'mail-tls',kind:'mail_certificate',target:'mail.example.com',status:'pending'}]};
    const base=globalThis.fetch;globalThis.fetch=async(url,options)=>url.includes('/setup/operation')?Response.json(current):base(url,options);
    try{await mount();assert.equal(findButton('setup.editPlan'),undefined);assert.equal(findButton('setup.verify'),undefined);assert.equal(tree.root.findAllByProps({to:'/settings?section=license'}).length,2);assert.ok(operations.decodeSetupExecution(current));}finally{await cleanup();}
});


test('unverified peer IPv6 is shown as a setup limitation without DNS changes',async()=>{
    init();const current={...execution({plan_id:'a'.repeat(32),request_id:'c'.repeat(32)},'waiting'),phase:'verification',checks:[{id:'dns',state:'action_required',code:'dns_peer_ipv6_unverified'}]};
    const base=globalThis.fetch;globalThis.fetch=async(url,options)=>url.includes('/setup/operation')?Response.json(current):base(url,options);
    try{await mount();const text=JSON.stringify(tree.toJSON());assert.ok(text.includes('setup.blocker.peerIPv6'));assert.equal(text.includes('setup.blocker.unknown'),false);assert.ok(calls.every(call=>!call.options?.method||call.options.method==='GET'));}finally{await cleanup();}
});


test('remote DNS setup requires fresh proof and reviews only the selected saved connection',async()=>{
    init();const id='d'.repeat(32);const base=globalThis.fetch;
    globalThis.fetch=async(url,options)=>url==='/api/v1/setup/plan'?Response.json(plan({remote_dns_connection:{id,endpoint:'https://dns.example.com:2083',nameservers:['ns1.example.com','ns2.example.com']}})):base(url,options);
    try{
        await mount();await submit();
        await act(async()=>tree.root.findByProps({id:'setup-panel_domain'}).props.onChange({target:{value:'panel.example.com'}}));
        await act(async()=>tree.root.findByProps({name:'setup-dns',value:'existing'}).props.onChange());
        await act(async()=>tree.root.findByType('remote-connection').props.onChange(id));
        const before=calls.length;await submit();assert.equal(calls.length,before,'unverified selection cannot persist or review setup');
        assert.ok(JSON.stringify(tree.toJSON()).includes('setup.remote.proof.failed'));
        await act(async()=>tree.root.findByType('remote-connection').props.onValidityChange(true));
        await submit();
        assert.equal(state.draft.remote_dns_connection_id,id);
        assert.ok(JSON.stringify(tree.toJSON()).includes('https://dns.example.com:2083'));
        assert.equal(findButton('setup.start').props.disabled,true,'review still requires explicit installation confirmation');
        assert.equal(calls.some(call=>call.url==='/api/v1/setup/start'),false);
        assert.equal(store.size,0,'review does not persist credentials or an execution marker');
    }finally{await cleanup();}
});

test('remote DNS review rejects a plan bound to a different saved connection',async()=>{
    init();const base=globalThis.fetch;
    globalThis.fetch=async(url,options)=>url==='/api/v1/setup/plan'?Response.json(plan({remote_dns_connection:{id:'e'.repeat(32),endpoint:'https://other.example.com:2083',nameservers:['ns1.example.com']}})):base(url,options);
    try{
        await mount();await submit();
        await act(async()=>tree.root.findByProps({id:'setup-panel_domain'}).props.onChange({target:{value:'panel.example.com'}}));
        await act(async()=>tree.root.findByProps({name:'setup-dns',value:'existing'}).props.onChange());
        await act(async()=>tree.root.findByType('remote-connection').props.onChange('d'.repeat(32)));
        await act(async()=>tree.root.findByType('remote-connection').props.onValidityChange(true));
        await submit();
        assert.equal(findButton('setup.start'),undefined);
        assert.ok(JSON.stringify(tree.toJSON()).includes('setup.planFailed'));
        assert.equal(calls.some(call=>call.url==='/api/v1/setup/start'),false);
    }finally{await cleanup();}
});

test('paired DNS guidance keeps both roles out of the final-verification dependency loop',async()=>{
    for(const lang of ['en','tr']){
        const catalogue=(await import(dataURL(compile(`../src/i18n/screens/${lang}.ts`))))[`${lang}Screens`];
        init();fixture.translations=catalogue;
        try{
            await mount();
            await act(async()=>tree.root.findByProps({name:'setup-purpose',value:'dns'}).props.onChange());
            await submit();
            const guidance=catalogue['setup.dnsPairOrder'];
            assert.ok(guidance && guidance!=='setup.dnsPairOrder');
            for(const role of ['primary','secondary']){
                await act(async()=>tree.root.findByProps({id:'setup-dns_role'}).props.onChange({target:{value:role}}));
                assert.ok(JSON.stringify(tree.toJSON()).includes(guidance),`${lang} ${role} must explain primary DNS before secondary, not primary completion`);
            }
            assert.equal(calls.some(call=>call.url==='/api/v1/setup/start'),false);
            await act(async()=>findButton(catalogue['setup.back']).props.onClick());
            await act(async()=>tree.root.findByProps({name:'setup-purpose',value:'web'}).props.onChange());
            await submit();
            await act(async()=>tree.root.findByProps({name:'setup-dns',value:'external'}).props.onChange());
            assert.ok(!JSON.stringify(tree.toJSON()).includes(guidance),'external DNS has no local pair setup dependency');
        }finally{await cleanup();}
    }
});

test('pending receipt reconciliation is neutral progress and preserves the operation admission lock',async()=>{
    for(const status of ['running','failed']){
        init('admin',{status});
        const marker={plan_id:'a'.repeat(32),request_id:'b'.repeat(32),panel_domain:'panel.example.com'};
        store.set('celikpanel.setup.start.admin',JSON.stringify(marker));
        const current={...execution(marker,status),phase:'dns',error:{code:'server_setup_reconciling',message:'Pending exact receipt'}};
        const read=fetch;
        globalThis.fetch=async(url,options)=>url.includes('/setup/operation')?Response.json(current):read(url,options);
        try{
            await mount();
            const notice=tree.root.findAllByProps({role:status==='running'?'status':'alert'}).find(node=>node.findAllByType('p').some(p=>p.props.children==='setup.confirmingPrevious'));
            assert.ok(notice,'pending running receipt is status; terminal failure remains an alert');
            assert.equal(notice.findAllByType('p')[0].props.className,status==='running'?'text-fg-muted':'text-danger');
            assert.equal(tree.root.findAllByType('form').length,0);
            assert.equal(findButton('setup.editPlan'),undefined);
            assert.equal(findButton('setup.start'),undefined);
            assert.equal(findButton('setup.reconnect'),undefined);
            assert.equal(!!findButton('setup.revise'),status==='failed');
            assert.equal(store.get('celikpanel.setup.start.admin'),JSON.stringify(marker));
            assert.ok(calls.every(call=>!call.options?.method||call.options.method==='GET'),'reading reconciliation cannot launch or revise any work');
        }finally{await cleanup();}
    }
});


test('an undecided upgraded host gets a choice, while manual and completed hosts keep access',async()=>{
    for(const [status,guidance,redirect] of [['legacy','undecided',true],['new','manual',false],['legacy','manual',false],['ready','undecided',false],['running','manual',true]]){
        init('admin',{status,guidance,origin:'legacy',required:!['legacy','ready'].includes(status)});
        try {await mount(Gate);assert.equal(tree.root.findAllByType('redirect').length,redirect?1:0);assert.ok(calls.every(c=>!c.options?.method));}
        finally {await cleanup();}
    }
    assert.equal(setup.decodeServerSetup({...fresh(),guidance:'skip_checks'}),null);
});

test('manual choice is persisted once before leaving and does not complete or install anything',async()=>{
    init('admin',{status:'legacy',origin:'legacy',required:false,guidance:'undecided'});
    const read=fetch;
    globalThis.fetch=async(url,options)=>{
        if(url==='/api/v1/setup/guidance'){
            calls.push({url,options});const choice=JSON.parse(options.body);
            assert.deepEqual(choice,{revision:1,guidance:'manual'});
            state={...state,guidance:'manual',revision:2};return Response.json(state);
        }
        return read(url,options);
    };
    try {
        await mount();assert.ok(findButton('setup.useWizard'));assert.ok(findButton('setup.manual'));assert.equal(calls.length,0);
        const button=findButton('setup.manual');await act(async()=>{void button.props.onClick();void button.props.onClick();});
        assert.equal(calls.length,1);assert.equal(tree.root.findByType('redirect').props.to,'/');
        assert.equal(state.status,'legacy');assert.equal(state.guidance,'manual');assert.equal(store.size,0);
    } finally {await cleanup();}
});

test('guidance failure stays visible and a guided choice can reopen a manually configured server',async()=>{
    init('admin',{guidance:'manual'});
    const read=fetch;let fail=true;
    globalThis.fetch=async(url,options)=>{
        if(url==='/api/v1/setup/guidance'){
            if(fail)return Response.json({error:'conflict'},{status:409});
            state={...state,guidance:'guided',revision:state.revision+1};return Response.json(state);
        }
        return read(url,options);
    };
    try {
        await mount();await act(async()=>findButton('setup.useWizard').props.onClick());
        assert.equal(tree.root.findAllByType('redirect').length,0);assert.ok(JSON.stringify(tree.toJSON()).includes('setup.choiceFailed'));
        fail=false;await act(async()=>findButton('setup.useWizard').props.onClick());
        assert.equal(tree.root.findAllByProps({name:'setup-purpose'}).length,4);
        assert.equal(state.guidance,'guided');assert.equal(state.status,'new');
    } finally {await cleanup();}
});
