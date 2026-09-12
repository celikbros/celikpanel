import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';
import { setupCatalogFixture } from './fixtures/server-setup-components.mjs';

const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const dataURL = text => `data:text/javascript;base64,${Buffer.from(text).toString('base64')}`;
const compile = path => ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 },
}).outputText;
const setupURL = dataURL(compile('../src/lib/serverSetup.ts'));
const componentLibURL = dataURL(compile('../src/lib/serverSetupComponents.ts').replace("from './serverSetup'", `from '${setupURL}'`));
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
const componentUIURL = dataURL(`import React from '${reactURL}';\n` + compile('../src/components/ServerSetupComponents.tsx').replace(/from ['"]([^'"]+)['"]/g, (_, path) => `from '${path === 'react' ? reactURL : path.endsWith('/serverSetupComponents') ? componentLibURL : stub}'`));
const { ServerSetupComponents: ComponentPicker } = await import(componentUIURL);
async function loadComponent(name) {
    const source = compile(`../src/components/${name}.tsx`).replace(/from ['"]([^'"]+)['"]/g, (_, path) => {
        const url = path === 'react' ? reactURL : path.endsWith('/ServerSetupComponents') ? componentUIURL : path.endsWith('/serverSetupComponents') ? componentLibURL : path.endsWith('/ServerSetupChoice') ? choiceURL : path.endsWith('/serverSetupOperation') ? operationURL : path.endsWith('/serverSetup') ? setupURL : stub;
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
        if(url==='/api/v1/setup/components')return Response.json(setupCatalogFixture());
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
        assert.equal(tree.root.findAllByProps({name:'setup-purpose'}).length,5);
        assert.equal(state.guidance,'guided');assert.equal(state.status,'new');
    } finally {await cleanup();}
});

async function mountPicker(catalog, initial = []) {
    let last = initial;
    function PickerHarness() {
        const [selected, setSelected] = React.useState(initial);
        return React.createElement(ComponentPicker, { catalog, selected, disabled: false, onChange(next) { last = next; setSelected(next); } });
    }
    await act(async () => { tree = Renderer.create(React.createElement(PickerHarness)); });
    return () => last;
}
const component = id => tree.root.findByProps({ id: `setup-component-${id}` });
async function toggleComponent(id) { await act(async () => component(id).props.onChange()); }
async function choosePurpose(purpose) { await act(async () => tree.root.findByProps({ name: 'setup-purpose', value: purpose }).props.onChange()); }
const formNext = () => tree.root.findByProps({ type: 'submit' });

test('component picker can remove its paired mail choice and locks only dependencies of other choices', async () => {
    init();
    try {
        const selected = await mountPicker(setupCatalogFixture());
        assert.equal(tree.root.findAllByProps({ id: 'setup-component-dovecot' }).length, 0, 'paired services have one choice');
        await toggleComponent('postfix');
        assert.equal(component('postfix').props.checked, true);
        assert.equal(component('postfix').props.disabled, false, 'a reciprocal dependency must not trap its own choice');
        await toggleComponent('postfix');
        assert.deepEqual(selected(), []);
        await toggleComponent('roundcube');
        assert.equal(component('postfix').props.checked, true);
        assert.equal(component('postfix').props.disabled, true, 'webmail needs the mail pair');
        assert.equal(component('nginx').props.checked, true);
        assert.equal(component('nginx').props.disabled, true);
        await toggleComponent('roundcube');
        assert.equal(component('postfix').props.checked, false);
        assert.equal(component('postfix').props.disabled, false);
        assert.deepEqual(selected(), []);
        assert.equal(calls.length, 0, 'component selection performs no host operation');
    } finally { await cleanup(); }
});

test('component picker blocks conflicting and unsupported choices and preserves installed software', async () => {
    init();const catalog = setupCatalogFixture();
    catalog.components.find(row => row.id === 'nginx').installed = true;
    try {
        const selected = await mountPicker(catalog, ['nginx']);
        assert.ok(JSON.stringify(tree.toJSON()).includes('setup.components.installed'));
        assert.equal(tree.root.findAllByProps({ id: 'setup-component-nftables' }).length, 0);
        assert.equal(tree.root.findAllByProps({ id: 'setup-component-certbot' }).length, 0);
        assert.equal(component('clamav').props.disabled, true);
        await toggleComponent('redis');
        assert.equal(component('valkey').props.disabled, true);
        assert.equal(component('redis').props.disabled, false);
        await toggleComponent('redis');
        assert.equal(component('valkey').props.disabled, false);
        await toggleComponent('nginx');
        assert.deepEqual(selected(), []);
        assert.equal(catalog.components.find(row => row.id === 'nginx').installed, true);
        assert.ok(JSON.stringify(tree.toJSON()).includes('setup.components.preserve'));
        assert.equal(calls.length, 0, 'deselecting installed software cannot call removal or installation APIs');
    } finally { await cleanup(); }
});

test('unknown inventory disables new choices without inventing installed state', async () => {
    init();const catalog = setupCatalogFixture();catalog.inventory_state = 'unknown';
    try {
        await mountPicker(catalog);
        assert.ok(tree.root.findAllByProps({ type: 'checkbox' }).every(input => input.props.disabled));
        assert.ok(JSON.stringify(tree.toJSON()).includes('setup.components.inventoryUnknown'));
        assert.equal(JSON.stringify(tree.toJSON()).includes('setup.components.installed'), false);
        assert.equal(calls.length, 0);
    } finally { await cleanup(); }
});

test('default profiles keep four steps and customization is a separate explicit choice', async () => {
    init();
    try {
        await mount();
        assert.equal(tree.root.findByType('ol').findAllByType('li').length, 4);
        assert.equal(tree.root.findAllByProps({ name: 'setup-purpose' }).length, 5);
        assert.ok(findButton('setup.components.customize'));
        await submit();
        assert.equal(state.draft.customization, undefined);
        assert.ok(tree.root.findByProps({ id: 'setup-panel_domain' }));
        assert.equal(calls.some(call => call.url === '/api/v1/setup/start'), false);
    } finally { await cleanup(); }
});

test('profile customization starts from its defaults and saves only explicit choices', async () => {
    init();
    try {
        await mount();await act(async () => findButton('setup.components.customize').props.onClick());
        assert.equal(tree.root.findByType('ol').findAllByType('li').length, 5);
        for (const id of ['nginx', 'php-fpm', 'mariadb']) assert.equal(component(id).props.checked, true);
        await toggleComponent('mariadb');await toggleComponent('postgresql');await submit();
        assert.deepEqual([...state.draft.customization.components].sort(), ['nginx', 'php-fpm', 'postgresql']);
        assert.ok(tree.root.findByProps({ id: 'setup-panel_domain' }));
        assert.equal(tree.root.findAllByProps({ id: 'setup-node_version' }).length, 0);
        assert.equal(tree.root.findAllByProps({ id: 'setup-mail_hostname' }).length, 0);
        assert.equal(calls.some(call => call.url === '/api/v1/setup/start'), false);
    } finally { await cleanup(); }
});

test('custom setup persists an explicit empty selection and restores it after reopening', async () => {
    init();
    try {
        await mount();await choosePurpose('custom');await submit();
        assert.ok(tree.root.findAllByProps({ type: 'checkbox' }).every(input => !input.props.checked));
        await submit();
        assert.deepEqual(state.draft.customization, { components: [] });
        assert.equal(state.draft.purpose, 'custom');
        await act(async () => tree.unmount());tree = null;
        fixture.context.accept(state);await mount();
        assert.ok(component('nginx'));
        assert.ok(tree.root.findAllByProps({ type: 'checkbox' }).every(input => !input.props.checked));
        assert.equal(calls.some(call => call.url === '/api/v1/setup/start'), false);
    } finally { await cleanup(); }
});

test('custom mail and runtime selections determine required access inputs', async () => {
    init();const base = fetch;
    globalThis.fetch = async (url, options) => url === '/api/v1/runtimes/node/lts'
        ? Response.json({ releases: [{ version: '24.18.0', name: 'Fixture' }] }) : base(url, options);
    try {
        await mount();await choosePurpose('custom');await submit();
        await toggleComponent('roundcube');await toggleComponent('node');await submit();
        assert.ok(tree.root.findByProps({ id: 'setup-node_version' }));
        assert.ok(tree.root.findByProps({ id: 'setup-mail_hostname' }));
        assert.deepEqual([...state.draft.customization.components].sort(), ['node', 'roundcube']);
        assert.equal(calls.some(call => call.url === '/api/v1/setup/start'), false);
    } finally { await cleanup(); }
});

test('catalogue failure or unknown inventory prevents advancing custom setup and offers retry', async () => {
    for (const reply of [() => Response.json({}, { status: 503 }), () => Response.json({}), () => Response.json({ ...setupCatalogFixture(), inventory_state: 'unknown' })]) {
        init();const base = fetch;
        globalThis.fetch = async (url, options) => url === '/api/v1/setup/components' ? reply() : base(url, options);
        try {
            await mount();await choosePurpose('custom');await submit();
            assert.equal(formNext().props.disabled, true);
            const writes = calls.filter(call => call.options?.method === 'PUT').length;
            await submit();
            assert.equal(calls.filter(call => call.options?.method === 'PUT').length, writes);
            assert.equal(tree.root.findAllByProps({ id: 'setup-panel_domain' }).length, 0);
            assert.equal(calls.some(call => call.url === '/api/v1/setup/plan'), false);
            assert.ok(findButton('common.retry'));
        } finally { await cleanup(); }
    }
});

test('editing a reviewed component selection invalidates confirmation and requires a new review', async () => {
    init();
    try {
        await mount();await choosePurpose('custom');await submit();await toggleComponent('postgresql');await submit();
        await act(async () => tree.root.findByProps({ id: 'setup-panel_domain' }).props.onChange({ target: { value: 'panel.example.com' } }));
        await act(async () => tree.root.findByProps({ name: 'setup-dns', value: 'external' }).props.onChange());
        await submit();
        await act(async () => tree.root.findByProps({ type: 'checkbox' }).props.onChange({ target: { checked: true } }));
        assert.equal(findButton('setup.start').props.disabled, false);
        const reviewedRevision = state.revision;
        await act(async () => findButton('setup.back').props.onClick());
        await act(async () => findButton('setup.back').props.onClick());
        assert.equal(findButton('setup.start'), undefined);
        await toggleComponent('redis');await submit();await submit();
        assert.ok(state.revision > reviewedRevision);
        assert.equal(calls.filter(call => call.url === '/api/v1/setup/plan').length, 2);
        assert.equal(findButton('setup.start').props.disabled, true, 'a prior acknowledgement never authorizes changed components');
        assert.equal(tree.root.findByProps({ type: 'checkbox' }).props.checked, false);
        assert.equal(calls.some(call => call.url === '/api/v1/setup/start'), false);
    } finally { await cleanup(); }
});

test('draft decoding distinguishes an untouched preset from explicit empty customization', () => {
    const preset = fresh();
    assert.equal(setup.decodeServerSetup(preset).draft.customization, undefined);
    const custom = { ...preset, draft: { ...preset.draft, purpose: 'custom', customization: { components: [] } } };
    assert.deepEqual(setup.decodeServerSetup(custom).draft.customization, { components: [] });
    for (const customization of [null, {}, { components: null }, { components: [1] }, { components: ['redis', 'redis'] }]) {
        assert.equal(setup.decodeServerSetup({ ...custom, draft: { ...custom.draft, customization } }), null);
    }
});

test('customization review distinguishes installed software and dependencies without starting work', async () => {
    init();const base = fetch;
    const components = [
        { id: 'postgresql', selected: true, required: false, installed: false },
        { id: 'nftables', selected: false, required: true, installed: true },
        { id: 'certbot', selected: false, required: true, installed: false },
    ];
    globalThis.fetch = async (url, options) => url === '/api/v1/setup/plan' ? Response.json(plan({ components })) : base(url, options);
    try {
        await mount();await choosePurpose('custom');await submit();await toggleComponent('postgresql');await submit();
        await act(async () => tree.root.findByProps({ id: 'setup-panel_domain' }).props.onChange({ target: { value: 'panel.example.com' } }));
        await act(async () => tree.root.findByProps({ name: 'setup-dns', value: 'external' }).props.onChange());
        await submit();
        const text = JSON.stringify(tree.toJSON());
        for (const label of ['PostgreSQL', 'Firewall', 'Certbot', 'setup.components.toInstall', 'setup.components.keep', 'setup.components.dependency', 'setup.components.preserve']) assert.ok(text.includes(label), label);
        assert.equal(findButton('setup.start').props.disabled, true);
        assert.equal(calls.some(call => call.url === '/api/v1/setup/start'), false);
        for (const invalid of [null, [{ ...components[0], installed: 'yes' }], [{ ...components[0], required: undefined }]]) {
            assert.equal(operations.decodeSetupPlan(plan({ components: invalid }), state.revision), null);
        }
    } finally { await cleanup(); }
});

test('a saved customized profile reopens with its exact choices instead of preset defaults', async () => {
    init('admin', { status: 'draft', draft: { ...fresh().draft, customization: { components: ['postgresql', 'redis'] } } });
    try {
        await mount();
        assert.equal(component('postgresql').props.checked, true);
        assert.equal(component('redis').props.checked, true);
        assert.equal(component('mariadb').props.checked, false);
        assert.equal(component('nginx').props.checked, false);
        assert.ok(calls.every(call => !call.options?.method || call.options.method === 'GET'));
    } finally { await cleanup(); }
});

test('catalogue retry obtains fresh inventory before customization can continue', async () => {
    init();const base = fetch;let healthy = false;let requests = 0;
    globalThis.fetch = async (url, options) => {
        if (url === '/api/v1/setup/components') { requests++;return healthy ? Response.json(setupCatalogFixture()) : Response.json({}, { status: 503 }); }
        return base(url, options);
    };
    try {
        await mount();await choosePurpose('custom');await submit();
        assert.equal(formNext().props.disabled, true);
        healthy = true;await act(async () => findButton('common.retry').props.onClick());
        assert.equal(requests, 2);
        assert.equal(formNext().props.disabled, false);
        await toggleComponent('postgresql');await submit();
        assert.deepEqual(state.draft.customization, { components: ['postgresql'] });
        assert.ok(tree.root.findByProps({ id: 'setup-panel_domain' }));
        assert.equal(calls.some(call => call.url === '/api/v1/setup/start'), false);
    } finally { await cleanup(); }
});

test('completion links and labels follow customized services instead of the original profile', async () => {
    const scenarios = [
        ['web', ['fail2ban'], '/services', 'setup.nextComponents'],
        ['custom', [], '/settings?section=dns', 'setup.nextDNS'],
        ['custom', ['nginx'], '/domains', 'setup.nextWebsite'],
        ['web', ['node'], '/domains', 'setup.nextApplication'],
        ['web', undefined, '/domains', 'setup.nextWebsite'],
        ['dns', undefined, '/settings?section=dns', 'setup.nextDNS'],
    ];
    for (const [purpose, selected, expectedPath, label] of scenarios) {
        const draft = { ...fresh().draft, purpose, ...(selected === undefined ? {} : { customization: { components: selected } }) };
        init('admin', { status: 'ready', required: false, draft });
        try {
            await mount();
            assert.equal(tree.root.findAllByType('a').filter(link => link.props.href === expectedPath).length, 1, `${purpose}/${selected} next action`);
            assert.ok(JSON.stringify(tree.toJSON()).includes(label), `${purpose}/${selected} label`);
            assert.equal(tree.root.findAllByType('form').length, 0);
            assert.ok(calls.every(call => !call.options?.method || call.options.method === 'GET'));
        } finally { await cleanup(); }
    }
});

test('saved external DNS stays visible until the administrator changes an empty custom setup to local DNS', async () => {
    init('admin', { status: 'draft', draft: { ...fresh().draft, purpose: 'custom', dns_mode: 'external', customization: { components: [] } } });
    try {
        await mount();await submit();
        const external = tree.root.findByProps({ name: 'setup-dns', value: 'external' });
        assert.equal(external.props.checked, true);
        assert.equal(external.props.disabled, false, 'an incompatible saved value is still a visible, enabled current selection');
        assert.equal(formNext().props.disabled, true);
        assert.ok(tree.root.findAllByProps({ role: 'alert' }).length > 0, 'the user can discover how to correct the DNS mode');
        const writes = calls.filter(call => call.options?.method === 'PUT').length;
        await submit();
        assert.equal(calls.filter(call => call.options?.method === 'PUT').length, writes, 'incompatible DNS cannot produce a saved reviewed plan');
        assert.equal(calls.some(call => call.url === '/api/v1/setup/plan'), false);
        assert.equal(state.draft.dns_mode, 'external', 'choosing components never silently rewrites the saved DNS mode');
        await act(async () => tree.root.findByProps({ name: 'setup-dns', value: 'local' }).props.onChange());
        assert.equal(tree.root.findByProps({ name: 'setup-dns', value: 'local' }).props.checked, true);
        assert.equal(formNext().props.disabled, false);
        assert.equal(state.draft.dns_mode, 'external', 'changing the form does not persist before Continue');
        assert.equal(calls.some(call => call.url === '/api/v1/setup/start'), false);
    } finally { await cleanup(); }
});

test('saved local secondary DNS requires an explicit primary selection before reviewing custom hosting', async () => {
    init('admin', { status: 'draft', draft: { ...fresh().draft, purpose: 'custom', dns_mode: 'local', dns_role: 'secondary', customization: { components: ['nginx'] } } });
    try {
        await mount();await submit();
        const selector = tree.root.findByProps({ id: 'setup-dns_role' });
        assert.equal(selector.props.value, 'secondary');
        assert.equal(selector.findAllByType('option').find(option => option.props.value === 'secondary').props.disabled, false, 'keep the existing role selectable while explaining the conflict');
        assert.equal(formNext().props.disabled, true);
        assert.ok(tree.root.findAllByProps({ role: 'alert' }).length > 0);
        const writes = calls.filter(call => call.options?.method === 'PUT').length;
        await submit();
        assert.equal(calls.filter(call => call.options?.method === 'PUT').length, writes);
        assert.equal(calls.some(call => call.url === '/api/v1/setup/plan'), false);
        assert.equal(state.draft.dns_role, 'secondary');
        await act(async () => selector.props.onChange({ target: { value: 'primary' } }));
        assert.equal(tree.root.findByProps({ id: 'setup-dns_role' }).props.value, 'primary');
        assert.equal(formNext().props.disabled, false);
        assert.equal(state.draft.dns_role, 'secondary', 'role changes must be explicitly saved');
        assert.equal(calls.some(call => call.url === '/api/v1/setup/start'), false);
    } finally { await cleanup(); }
});


test('DNS role changes preserve physical server names, addresses and unrelated draft fields', () => {
    const draft = {...fresh().draft, ns1:'ns2.example.com', ns2:'ns1.example.com', peer_ns:'ns1.example.com', local_ip:'72.62.38.15', peer_ip:'2.25.80.4'};
    const secondary = setup.changeSetupDNSRole(draft, 'secondary');
    assert.equal(secondary.ns2, draft.ns1);
    assert.equal(secondary.ns1, draft.ns2);
    assert.equal(secondary.local_ip, draft.local_ip);
    assert.equal(secondary.peer_ip, draft.peer_ip);
    assert.equal(secondary.peer_ns, draft.peer_ns);
    assert.equal(setup.setupDNSNames(secondary).mismatch, false);
    assert.deepEqual(setup.changeSetupDNSRole(secondary, 'primary'), draft);
    assert.equal(setup.changeSetupDNSRole(draft, 'primary'), draft);
    assert.equal(setup.setupDNSNames({...draft, peer_ns:' NS1.Example.COM. '}).mismatch, false);
});

test('only a usable server-reported IPv4 is offered for prefilling', () => {
    for (const value of [undefined, '', '10.0.0.1', '127.0.0.1', '169.254.2.1', '172.16.0.2', '192.168.1.3', '100.64.1.1', '224.0.0.1', '0.1.2.3', '256.1.1.1', '1.2.3', '01.2.3.4', '2001:db8::1']) assert.equal(setup.setupDetectedIPv4(value), '');
    assert.equal(setup.setupDetectedIPv4('72.62.38.15'), '72.62.38.15');
});

test('detected IP fills the field but never replaces saved or manually cleared input', async () => {
    for (const saved of ['', '2.25.80.4']) {
        init('admin', {status:'draft', server_ip:'72.62.38.15', draft:{...fresh().draft, local_ip:saved}});
        try {
            await mount();
            assert.equal(tree.root.findByProps({id:'setup-local_ip'}).props.value, saved || '72.62.38.15');
            assert.equal(calls.some(c => c.options?.method === 'PUT'), false);
            await act(async () => tree.root.findByProps({id:'setup-local_ip'}).props.onChange({target:{value:''}}));
            await act(async () => tree.root.findByProps({name:'setup-dns', value:'external'}).props.onChange());
            await act(async () => tree.root.findByProps({name:'setup-dns', value:'local'}).props.onChange());
            assert.equal(tree.root.findByProps({id:'setup-local_ip'}).props.value, '');
        } finally { await cleanup(); }
    }
});

test('local DNS edits derive the peer once and submit the same physical pairing after role change', async () => {
    init('admin', {status:'draft', draft:{...fresh().draft, purpose:'dns', panel_domain:'panel.example.com', ns1:'ns2.example.com', ns2:'ns1.example.com', peer_ns:'ns1.example.com', local_ip:'72.62.38.15', peer_ip:'2.25.80.4'}});
    try {
        await mount();
        assert.equal(tree.root.findAllByProps({id:'setup-peer_ns'}).length, 0);
        await act(async () => tree.root.findByProps({id:'setup-dns_role'}).props.onChange({target:{value:'secondary'}}));
        assert.equal(tree.root.findByProps({id:'setup-ns2'}).props.value, 'ns2.example.com');
        await act(async () => tree.root.findByProps({id:'setup-ns1'}).props.onChange({target:{value:'primary.example.com'}}));
        await submit();
        assert.equal(state.draft.peer_ns, 'primary.example.com');
        assert.equal(state.draft.ns1, 'primary.example.com');
        assert.equal(state.draft.ns2, 'ns2.example.com');
        assert.equal(state.draft.local_ip, '72.62.38.15');
        assert.equal(state.draft.peer_ip, '2.25.80.4');
        assert.equal(calls.some(c => c.url === '/api/v1/setup/start'), false);
    } finally { await cleanup(); }
});

test('a conflicting saved peer remains visible and blocks review until explicitly corrected', async () => {
    init('admin', {status:'draft', draft:{...fresh().draft, panel_domain:'panel.example.com', ns1:'primary.example.com', ns2:'secondary.example.com', peer_ns:'old.example.com'}});
    try {
        await mount();
        assert.ok(JSON.stringify(tree.toJSON()).includes('old.example.com'));
        await submit();
        assert.equal(calls.some(c => c.url === '/api/v1/setup/plan'), false);
        assert.equal(state.draft.peer_ns, 'old.example.com');
        await act(async () => findButton('setup.useDisplayedPeer').props.onClick());
        await submit();
        assert.equal(state.draft.peer_ns, 'secondary.example.com');
        assert.equal(calls.filter(c => c.url === '/api/v1/setup/plan').length, 1);
    } finally { await cleanup(); }
});
