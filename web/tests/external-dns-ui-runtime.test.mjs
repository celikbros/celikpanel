import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';

const require=createRequire(import.meta.url);
const reactURL=pathToFileURL(require.resolve('react')).href;
const url=text=>'data:text/javascript;base64,'+Buffer.from(text).toString('base64');
const stub=url(`
import React from '${reactURL}';
const t=key=>key;
export const useI18n=()=>({t,locale:'en'});
export const useNavigate=()=>()=>true;
export const showToast=(tone,message)=>globalThis.remoteMailToast?.(tone,message);
export const Button=props=>React.createElement('button',props);
export const EmptyState=props=>React.createElement('section',null,props.title,props.hint,props.action);
export const Spinner=()=>React.createElement('span',null,'loading');
export const Dialog=props=>React.createElement('section',null,props.title,props.children,props.footer);
export const ErrorBanner=()=>null;
export const StatusDot=()=>null, HelpButton=()=>null;
export const Globe=()=>null, Plus=()=>null, Trash2=()=>null, ShieldCheck=()=>null, Copy=()=>null, AlertTriangle=()=>null, RefreshCw=()=>null, Check=()=>null, KeyRound=()=>null, FileCheck2=()=>null, Info=()=>null,Lock=()=>null,Server=()=>null,Network=()=>null;
export const inputClass='';
export const readApiError=async r=>({message:'failed'}),apiErrorText=error=>error.message;
`);
async function component(name){const code=ts.transpileModule(readFileSync(new URL('../src/components/'+name+'.tsx',import.meta.url),'utf8'),{compilerOptions:{jsx:ts.JsxEmit.React,module:ts.ModuleKind.ES2022,target:ts.ScriptTarget.ES2020}}).outputText.replace(/from ['"]([^'"]+)['"]/g,(_,path)=>`from '${path==='react'?reactURL:stub}'`);return(await import(url(`import React from '${reactURL}';\n${code}`)))[name];}
const DNS=await component('DomainDNSManager');
const Connection=await component('DomainConnection');
const MailAuth=await component('MailAuthPanel');
const AddDomain=await component('AddDomainModal');
const originalFetch=globalThis.fetch;
let tree,calls;
const record={id:1,name:'example.com',type:'A',content:'192.0.2.4',ttl:3600,disabled:false};
const authRecord={name:'example.com',recommended:'v=spf1 -all',zone_value:'',dns_value:'',resolved:true,status:'missing'};
function init(){tree=null;calls=[];globalThis.fetch=async(path,options)=>{
    calls.push({path,options});
    if(path.endsWith('/dns/zone'))return Response.json({type:'EXTERNAL',management:'external',managed:false});
    if(path.endsWith('/dns/records'))return Response.json({records:[record],management:'external',published:false});
    if(path.endsWith('/connection'))return Response.json({domain:'example.com',server_ip:'192.0.2.4',nameservers:[],live_nameservers:['ns.provider.example'],live_ips:[],status:'elsewhere',ssl_ready:false,glue_needed:false,nameservers_usable:false,checked_at:'2026-09-10T00:00:00Z',dns_management_mode:'external',required_records:[record]});
    if(path.endsWith('/mail/auth'))return Response.json({domain:'example.com',zone_exists:false,dns_management_mode:'external',spf:authRecord,dkim:authRecord,dmarc:authRecord,dkim_selector:'default',signing_installed:true,required_records:[{...record,name:'mail.example.com'},{...record,name:'example.com',type:'MX',content:'mail.example.com',prio:10}]});
    return Response.json({web_server:'nginx',php_versions:[],dns_server:'',dns_identity_ready:false,dns_management_mode:'external',dns_management_ready:true,mail_server:false});
};}
async function mount(Component,props={}){await act(async()=>{tree=Renderer.create(React.createElement(Component,{domainId:1,domainName:'example.com',...props}));});}
async function cleanup(){if(tree)await act(async()=>tree.unmount());globalThis.fetch=originalFetch;}
const text=()=>JSON.stringify(tree.toJSON());

test('external DNS records are provider instructions without local mutation or engine warnings',async()=>{
    init();try{await mount(DNS);assert.ok(text().includes('dns.externalTitle'));assert.ok(text().includes('192.0.2.4'));for(const key of ['dns.notServed','dns.addRecord','dns.republish','dnssec.sign'])assert.ok(!text().includes(key),key);assert.ok(calls.every(call=>!call.options?.method));}finally{await cleanup();}
});
test('external domain connection never asks for local nameservers or delegation',async()=>{
    init();try{await mount(Connection);assert.ok(text().includes('dns.externalTitle'));assert.ok(text().includes('192.0.2.4'));for(const key of ['conn.nsBroken.title','conn.routeA.title','conn.routeA.step2'])assert.ok(!text().includes(key),key);}finally{await cleanup();}
});
test('external mail exposes delivery and authentication records but cannot apply local DNS',async()=>{
    init();try{await mount(MailAuth);assert.ok(text().includes('mailauth.externalHelp'));assert.ok(text().includes('mail.example.com'));assert.ok(text().includes('v=spf1 -all'));assert.ok(!text().includes('mailauth.apply'));assert.ok(!text().includes('mailauth.dmarcPolicy'));assert.ok(calls.every(call=>!call.options?.method));}finally{await cleanup();}
});
test('external websites are available without a local DNS engine, while DNS-only creation stays unavailable',async()=>{
    init();try{await mount(AddDomain,{onClose(){},onSuccess(){}});const choices=tree.root.findAllByProps({name:'purpose'});assert.equal(choices.length,2);assert.equal(choices[0].props.disabled,false);assert.equal(choices[1].props.disabled,true);assert.equal(choices[0].props.checked,true);}finally{await cleanup();}
});


test('remote mail previews additional routing and reports preserved-record conflicts',async()=>{
    for(const code of ['REMOTE_DNS_MAIL_RECORD_CONFLICT','REMOTE_DNS_MAIL_ADDRESS_REQUIRED']){
        init();const base=globalThis.fetch;const messages=[];globalThis.remoteMailToast=(tone,message)=>messages.push({tone,message});
        globalThis.fetch=async(path,options)=>{
            if(path.endsWith('/mail/auth'))return Response.json({domain:'example.com',zone_exists:true,dns_management_mode:'existing',spf:authRecord,dkim:authRecord,dmarc:authRecord,dkim_selector:'default',signing_installed:true,required_records:[{...record,name:'mail.example.com'},{...record,type:'MX',content:'mail.example.com',prio:10}]});
            if(path.endsWith('/mail/auth/apply')){calls.push({path,options});return Response.json({code,message:'untrusted remote detail'}, {status:409});}
            return base(path,options);
        };
        try{
            await mount(MailAuth);assert.ok(text().includes('mailauth.remoteHelp'));assert.ok(text().includes('mail.example.com'));assert.equal(calls.some(call=>call.options?.method==='POST'),false);
            const apply=tree.root.findAllByType('button').find(button=>React.Children.toArray(button.props.children).includes('mailauth.apply'));await act(async()=>apply.props.onClick());
            assert.equal(calls.filter(call=>call.options?.method==='POST').length,1);assert.equal(messages.at(-1).message,code==='REMOTE_DNS_MAIL_RECORD_CONFLICT'?'mailauth.remoteConflict':'mailauth.remoteAddressRequired');assert.equal(messages.some(item=>item.message.includes('untrusted')),false);
        }finally{delete globalThis.remoteMailToast;await cleanup();}
    }
});
