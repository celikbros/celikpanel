import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';

const require=createRequire(import.meta.url), reactURL=pathToFileURL(require.resolve('react')).href;
const dataURL=value=>`data:text/javascript;base64,${Buffer.from(value).toString('base64')}`;
const compile=path=>ts.transpileModule(readFileSync(new URL(path,import.meta.url),'utf8'),{compilerOptions:{jsx:ts.JsxEmit.React,module:ts.ModuleKind.ES2022,target:ts.ScriptTarget.ES2020}}).outputText;
const contractURL=dataURL(compile('../src/lib/remoteDNS.ts'));
const contract=await import(contractURL);
const stubURL=dataURL(`import React from '${reactURL}'; export const useAuth=()=>({role:globalThis.remoteFixture.role}); export const useI18n=()=>({t:(key,vars)=>key}); export const Button=props=>React.createElement('button',props); export const inputClass='';`);
const source=compile('../src/components/ServerSetupDNSConnections.tsx').replace(/from ['"]([^'"]+)['"]/g,(_,path)=>`from '${path==='react'?reactURL:path.endsWith('/remoteDNS')?contractURL:stubURL}'`);
const {ServerSetupDNSConnection:Connection,ServerSetupDNSAccess:Access}=await import(dataURL(`import React from '${reactURL}';\n${source}`));
const original={fetch:globalThis.fetch,window:globalThis.window,localStorage:globalThis.localStorage};
const connection=(extra={})=>({id:'a'.repeat(32),endpoint:'https://dns.example.com:2083',status:'ready',nameservers:['ns1.example.com','ns2.example.com'],created_at:'2026-09-11T00:00:00Z',...extra});
let fixture,tree,calls;
function init(role='admin'){
    fixture=globalThis.remoteFixture={role,list:[],clients:[],verified:true,validity:[],value:''};calls=[];tree=null;
    globalThis.window={setTimeout,clearTimeout};
    globalThis.localStorage={getItem(){throw new Error('remote credential storage read')},setItem(){throw new Error('remote credential storage write')},removeItem(){throw new Error('remote credential storage remove')}};
    globalThis.fetch=async(url,options)=>{
        calls.push({url,options});
        if(url.includes('check=1'))return Response.json({connection:fixture.list.find(item=>url.includes(item.id)),verified:fixture.verified});
        if(url==='/api/v1/dns/remote/connections'&&!options?.method)return Response.json({connections:fixture.list});
        if(url==='/api/v1/dns/remote/connections'&&options?.method==='POST'){fixture.list=[connection()];return Response.json({connection:fixture.list[0],verified:true});}
        if(url==='/api/v1/dns/remote/clients'&&!options?.method)return Response.json({clients:fixture.clients});
        if(url==='/api/v1/dns/remote/enrollments')return Response.json({enrollment_code:'secret-only-body',expires_at:'2099-09-11T00:10:00Z'});
        if(options?.method==='DELETE'){fixture.list=[];fixture.clients=fixture.clients.map(item=>({...item,revoked:true}));return new Response(null,{status:204});}
        throw new Error('unexpected request '+url);
    };
}
function Host(){const[value,setValue]=React.useState(fixture.value);return React.createElement(Connection,{value,onChange:next=>{fixture.value=next;setValue(next)},onValidityChange:React.useCallback(next=>fixture.validity.push(next),[])});}
async function mount(Component=Host){await act(async()=>{tree=Renderer.create(React.createElement(Component));});}
async function clean(){if(tree)await act(async()=>tree.unmount());for(const[key,value]of Object.entries(original)){if(value===undefined)delete globalThis[key];else globalThis[key]=value;}delete globalThis.remoteFixture;}
const button=label=>tree.root.findAllByType('button').find(item=>item.props.children===label);
const writes=()=>calls.filter(item=>item.options?.method&&item.options.method!=='GET');
async function fill(){await act(async()=>tree.root.findByProps({id:'setup-remote-endpoint'}).props.onChange({target:{value:'https://dns.example.com:2083'}}));await act(async()=>tree.root.findByProps({id:'setup-remote-code'}).props.onChange({target:{value:'one-time-secret'}}));}

test('remote DNS parsers reject invalid identities, secret-bearing endpoints and malformed lists',()=>{
    assert.equal(contract.decodeDNSConnections({connections:[connection({id:'other'})]}),null);
    assert.equal(contract.remoteDNSEndpoint('https://user:secret@dns.example.com'),null);
    assert.equal(contract.remoteDNSEndpoint('https://dns.example.com/?code=secret'),null);
    assert.equal(contract.remoteDNSEndpoint('http://dns.example.com'),null);
    assert.equal(contract.remoteDNSEndpoint('https://127.0.0.1'),null);
    assert.equal(contract.remoteDNSEndpoint('https://dns.example.com:2083/'),'https://dns.example.com:2083');
});
test('remote administrative views never fetch for tenants and mounting cannot pair or generate',async()=>{
    for(const role of ['customer','reseller','additional_user','admin'])for(const Component of [Host,Access]){
        init(role);try{await mount(Component);assert.equal(writes().length,0);assert.equal(calls.length,role==='admin'?1:0);}finally{await clean();}
    }
});
test('saved ready connection is not current proof; selected identity must verify afresh',async()=>{
    for(const verified of [true,false]){
        init();fixture.list=[connection()];fixture.value=connection().id;fixture.verified=verified;
        const base=globalThis.fetch;globalThis.fetch=async(url,options)=>{if(url.includes('check=1')){calls.push({url,options});return Response.json({connection:connection({nameservers:['fresh1.example.com','fresh2.example.com']}),verified});}return base(url,options);};
        try{await mount();assert.equal(fixture.validity.at(-1),verified);assert.ok(calls.some(item=>item.url.endsWith(`id=${connection().id}&check=1`)));assert.equal(writes().length,0);assert.ok(JSON.stringify(tree.toJSON()).includes(verified?'setup.remote.proof.verified':'setup.remote.proof.failed'));assert.equal(JSON.stringify(tree.toJSON()).includes('fresh1.example.com'),verified);assert.equal(JSON.stringify(tree.toJSON()).includes('ns1.example.com'),false);}finally{await clean();}
    }
});
test('pairing requires explicit scoped confirmation and duplicate clicks send one credential-bearing body',async()=>{
    init();try{
        await mount();await fill();assert.equal(button('setup.remote.authorize').props.disabled,true);assert.equal(writes().length,0);
        await act(async()=>tree.root.findByProps({type:'checkbox'}).props.onChange({target:{checked:true}}));const action=button('setup.remote.authorize');
        await act(async()=>{void action.props.onClick();void action.props.onClick();});
        assert.equal(writes().length,1);assert.deepEqual(JSON.parse(writes()[0].options.body),{endpoint:'https://dns.example.com:2083',enrollment_code:'one-time-secret'});
        assert.ok(calls.every(item=>!item.url.includes('one-time-secret')));assert.equal(tree.root.findByProps({id:'setup-remote-code'}).props.value,'');assert.equal(fixture.value,connection().id);assert.equal(fixture.validity.at(-1),true);
    }finally{await clean();}
});
test('lost pairing response reconciles pending identity and resumes without resending the code',async()=>{
    init();const base=globalThis.fetch;let first=true;
    globalThis.fetch=async(url,options)=>{
        if(url==='/api/v1/dns/remote/connections'&&options?.method==='POST'&&first){first=false;calls.push({url,options});fixture.list=[connection({status:'pending'})];throw new Error('lost reply');}
        return base(url,options);
    };
    try{
        await mount();await fill();await act(async()=>tree.root.findByProps({type:'checkbox'}).props.onChange({target:{checked:true}}));await act(async()=>button('setup.remote.authorize').props.onClick());
        assert.equal(tree.root.findByProps({id:'setup-remote-code'}).props.value,'');assert.equal(button('setup.remote.authorize').props.disabled,true);
        await act(async()=>button('setup.remote.resume').props.onClick());assert.equal(writes().length,2);assert.deepEqual(JSON.parse(writes()[1].options.body),{id:connection().id});assert.equal(fixture.value,connection().id);
    }finally{await clean();}
});
test('receiver enrollment is explicit, keeps secret in memory and revocation needs its own confirmation',async()=>{
    init();fixture.clients=[{id:'b'.repeat(32),label:'Hosting server',created_at:'2026-09-11T00:00:00Z',revoked:false}];
    try{
        await mount(Access);assert.equal(button('setup.remote.generate').props.disabled,true);assert.equal(writes().length,0);
        await act(async()=>tree.root.findByProps({type:'checkbox'}).props.onChange({target:{checked:true}}));await act(async()=>button('setup.remote.generate').props.onClick());
        assert.equal(writes().length,1);assert.equal(tree.root.findByProps({id:'dns-enrollment-code'}).props.type,'password');assert.ok(calls.every(item=>!item.url.includes('secret-only-body')));
        await act(async()=>button('setup.remote.revoke').props.onClick());assert.equal(writes().length,1);
        const confirm=tree.root.findAllByType('button').find(item=>item.props.variant==='danger');await act(async()=>confirm.props.onClick());assert.equal(writes().length,2);assert.equal(writes()[1].options.method,'DELETE');assert.ok(JSON.stringify(tree.toJSON()).includes('setup.remote.revoked'));
    }finally{await clean();}
});
test('disconnecting a selected connection requires confirmation and clears selection only after success',async()=>{
    init();fixture.list=[connection()];fixture.value=connection().id;
    try{await mount();await act(async()=>button('setup.remote.disconnect').props.onClick());assert.equal(writes().length,0);const confirm=tree.root.findAllByType('button').find(item=>item.props.variant==='danger');await act(async()=>confirm.props.onClick());assert.equal(writes().length,1);assert.equal(fixture.value,'');assert.equal(fixture.validity.at(-1),false);}finally{await clean();}
});


test('unavailable clipboard reports a recoverable error without losing the generated code',async()=>{
    init();const descriptor=Object.getOwnPropertyDescriptor(globalThis,'navigator');Object.defineProperty(globalThis,'navigator',{configurable:true,value:{}});
    try{
        await mount(Access);await act(async()=>tree.root.findByProps({type:'checkbox'}).props.onChange({target:{checked:true}}));await act(async()=>button('setup.remote.generate').props.onClick());
        await act(async()=>button('conn.copy').props.onClick());assert.ok(JSON.stringify(tree.toJSON()).includes('setup.remote.copyFailed'));assert.equal(tree.root.findByProps({id:'dns-enrollment-code'}).props.value,'secret-only-body');assert.equal(writes().length,1);
    }finally{if(descriptor)Object.defineProperty(globalThis,'navigator',descriptor);else delete globalThis.navigator;await clean();}
});


test('an unresolved old enrollment cannot block a separately confirmed new code',async()=>{
    init();const old=connection({id:'b'.repeat(32),status:'pending'});fixture.list=[old];const base=globalThis.fetch;
    globalThis.fetch=async(url,options)=>{if(url==='/api/v1/dns/remote/connections'&&options?.method==='POST'){calls.push({url,options});fixture.list=[old,connection()];return Response.json({connection:connection(),verified:true});}return base(url,options);};
    try{
        await mount();assert.ok(button('setup.remote.resume'));await fill();assert.equal(button('setup.remote.authorize').props.disabled,true);
        await act(async()=>tree.root.findByProps({type:'checkbox'}).props.onChange({target:{checked:true}}));assert.equal(button('setup.remote.authorize').props.disabled,false);
        await act(async()=>button('setup.remote.authorize').props.onClick());assert.equal(writes().length,1);assert.equal(JSON.parse(writes()[0].options.body).enrollment_code,'one-time-secret');assert.equal(fixture.value,connection().id);assert.ok(button('setup.remote.resume'),'old durable identity is preserved for recovery');
    }finally{await clean();}
});


test('setup publisher pins the reviewed endpoint and cannot select another ready authority',async()=>{
    init(); fixture.list=[connection(),connection({id:'b'.repeat(32),endpoint:'https://other.example.com:2083'})];
    fixture.value='b'.repeat(32);
    function Scoped(){const[value,setValue]=React.useState(fixture.value);return React.createElement(Connection,{value,requiredEndpoint:'https://dns.example.com:2083',onChange:setValue,onValidityChange:React.useCallback(next=>fixture.validity.push(next),[])});}
    try{
        await mount(Scoped);
        assert.equal(tree.root.findByProps({id:'setup-remote-endpoint'}).props.readOnly,true);
        assert.equal(tree.root.findByProps({id:'setup-remote-endpoint'}).props.value,'https://dns.example.com:2083');
        assert.equal(fixture.validity.at(-1),false);
        const options=tree.root.findByProps({id:'setup-remote-connection'}).findAllByType('option');
        assert.equal(options.some(item=>item.props.value==='b'.repeat(32)&&!item.props.disabled),false);
        assert.equal(writes().length,0);
        await act(async()=>tree.root.findByProps({id:'setup-remote-connection'}).props.onChange({target:{value:'a'.repeat(32)}}));
        assert.equal(fixture.validity.at(-1),true);
    }finally{await clean();}
});
