import assert from 'node:assert/strict';
import test from 'node:test';
import { setupExecutionGuidance } from '../src/lib/serverSetupGuidance.ts';
import { enScreens as baseEnScreens } from '../src/i18n/screens/en.ts';
import { trScreens as baseTrScreens } from '../src/i18n/screens/tr.ts';

import { enSetupDNS } from '../src/i18n/setupDNS/en.ts';
import { trSetupDNS } from '../src/i18n/setupDNS/tr.ts';
const enScreens = {...baseEnScreens,...enSetupDNS};
const trScreens = {...baseTrScreens,...trSetupDNS};

const context = (extra={}) => ({dns_mode:'local',dns_role:'primary',dns_engine:'bind',local_nameserver:'ns1.example.com',local_ip:'192.0.2.10',peer_nameserver:'ns2.example.com',peer_ip:'192.0.2.20',panel_domain:'panel.example.com',mail_hostname:'mail.example.com',dns_hosting_management:'',...extra});
const execution = (extra={}) => ({status:'running',phase:'01-dns',steps:[{id:'01-dns',kind:'dns',target:'local',status:'running'}],context:context(),...extra});
const keys = guidance => [...guidance.messages,...guidance.details].map(item=>item.key);

test('both initial DNS roles identify the reviewed peer without claiming it is offline',()=>{
    for(const role of ['primary','secondary']){
        const guide=setupExecutionGuidance(execution({context:context({dns_role:role})}));
        assert.ok(keys(guide).includes(role==='primary'?'setup.guide.startSecondary':'setup.guide.startPrimary'));
        assert.deepEqual(guide.messages[0].values,{local:'ns1.example.com',localIP:'192.0.2.10',peer:'ns2.example.com',peerIP:'192.0.2.20'});
        assert.ok(!keys(guide).includes('setup.guide.pairUnverified'),'initial work is not evidence of a failed pair check');
    }
});
test('DNS-only and manual secondary operation do not ask for panel authorization',()=>{
    for(const management of ['','manual','panel']){
        const guide=setupExecutionGuidance(execution({context:context({dns_role:'secondary',dns_hosting_management:management}),status:'waiting',phase:'dns_readiness'}));
        assert.ok(keys(guide).includes('setup.guide.pairUnverified'));
        assert.equal(keys(guide).includes('setup.guide.automaticRecords'),management==='panel');
        assert.equal(keys(guide).includes('setup.guide.manualRecords'),management==='manual');
        assert.ok(keys(guide).includes('setup.guide.nativeDNS'));
    }
});
test('external and existing DNS do not inherit paired-server installation instructions',()=>{
    for(const mode of ['external','existing']){
        const guide=setupExecutionGuidance(execution({context:context({dns_mode:mode,dns_role:''})}));
        assert.ok(!keys(guide).some(key=>['setup.guide.startPrimary','setup.guide.startSecondary','setup.guide.pairChecks'].includes(key)));
        assert.ok(keys(guide).includes(mode==='external'?'setup.guide.certificateDNS':'setup.guide.existingDNS'));
    }
});
test('a stopped secondary and an uncertain DNS result do not promise an install retry',()=>{
    const failed=setupExecutionGuidance(execution({status:'failed',steps:[{id:'01-dns',kind:'dns',target:'local',status:'failed'}],context:context({dns_role:'secondary'})}));
    assert.equal(failed.title,'setup.guide.failedTitle');assert.ok(keys(failed).includes('setup.guide.failed'));
    assert.ok(!keys(failed).includes('setup.guide.monitoring'));
    const uncertain=setupExecutionGuidance(execution({error:{code:'server_setup_reconciling',message:'pending receipt'}}));
    assert.equal(uncertain.title,'setup.guide.confirmTitle');assert.equal(uncertain.messages[0].key,'setup.guide.confirm');
    assert.ok(keys(uncertain).includes('setup.guide.startSecondary'));
});
test('certificate guidance uses the affected reviewed domain, never the mail domain for panel access',()=>{
    for(const kind of ['panel_certificate','mail_certificate']){
        const guide=setupExecutionGuidance(execution({steps:[{id:'cert',kind,target:'ignored editable value',status:'running'}]}));
        assert.equal(guide.messages[0].values.domain,kind==='mail_certificate'?'mail.example.com':'panel.example.com');
    }
});
test('license and missing observations stay distinct and preserve recovery meaning',()=>{
    const license=setupExecutionGuidance(execution({status:'waiting',phase:'license'}));
    assert.equal(license.title,'setup.licenseWaiting');assert.deepEqual(keys(license),['setup.guide.license']);
    const unknown=setupExecutionGuidance(execution({context:undefined,steps:[],phase:'new_backend_phase'}));
    assert.ok(keys(unknown).includes('setup.guide.unknown'));
    assert.equal(setupExecutionGuidance(execution({status:'succeeded'})),null);
});
test('verification separates DNS, mail delivery, identity, and unknown checks without false peer advice',()=>{
    const guide=setupExecutionGuidance(execution({status:'waiting',phase:'verification',context:context({dns_mode:'external',dns_role:''}),checks:[
        {id:'dns',state:'unknown',code:'dns_unavailable'},
        {id:'mail_delivery',state:'action_required',code:'mail_delivery_required'},
        {id:'mail_identity',state:'action_required',code:'mail_identity_required'},
        {id:'future_check',state:'unknown',code:'future_check_unavailable'},
        {id:'firewall',state:'ready',code:''},
    ]}));
    for(const key of ['externalChecks','mailDelivery','mailIdentity','checkUnknown'])assert.ok(keys(guide).includes('setup.guide.'+key));
    assert.ok(!keys(guide).includes('setup.guide.pairChecks'));
    assert.ok(!keys(guide).includes('setup.guide.firewall'));
});
test('all guidance states have complete matching English and Turkish placeholders',()=>{
    const enKeys=Object.keys(enScreens).filter(key=>key.startsWith('setup.guide.'));
    assert.deepEqual(Object.keys(trScreens).filter(key=>key.startsWith('setup.guide.')).sort(),enKeys.sort());
    for(const key of enKeys){
        assert.equal(typeof trScreens[key],'string');
        assert.deepEqual((enScreens[key].match(/\{[^}]+\}/g)||[]).sort(),(trScreens[key].match(/\{[^}]+\}/g)||[]).sort(),key);
        assert.ok(!trScreens[key].includes('\ufffd'));
    }
});


test('a new build requires plan review without misdiagnosing DNS or component failure',()=>{
    const guide=setupExecutionGuidance(execution({status:'failed',error:{code:'server_setup_build_changed',message:'review'},steps:[{id:'service',kind:'service',target:'nginx',status:'failed'}]}));
    assert.equal(guide.title,'setup.guide.buildChangedTitle');
    assert.deepEqual(keys(guide),['setup.guide.buildChanged']);
});


test('public DNS prerequisite uses the exact certificate step for panel and mail and distinguishes wrong from unknown answers',()=>{
    for(const domain of ['panel.example.com','mail.example.com'])for(const code of ['server_setup_access_dns_required','server_setup_access_dns_mismatch']){
        const guide=setupExecutionGuidance(execution({status:'waiting',phase:'access_dns',error:{code,message:'result'},context:context({access_dns_ip:'192.0.2.10'}),steps:[{id:'access',kind:'access_dns',target:domain,qualifier:'192.0.2.10',status:'running'}]}));
        assert.deepEqual(guide.messages[0].values,{domain,ip:'192.0.2.10'});
        assert.ok(keys(guide).includes(code.endsWith('mismatch')?'setup.guide.accessDNSMismatch':'setup.guide.accessDNSUnknown'));
        assert.ok(keys(guide).includes('setup.guide.accessDNSResume'));
    }
});

test('public DNS ownership determines who must act without requiring a website or remote panel',()=>{
    for(const mode of ['local','external','existing'])for(const role of ['primary','secondary']){
        const infrastructure_dns=mode==='local'&&role==='primary'?{zone:'example.com'}:undefined;
        const guide=setupExecutionGuidance(execution({status:'waiting',phase:'access_dns',context:context({dns_mode:mode,dns_role:role,infrastructure_dns}),steps:[{id:'access',kind:'access_dns',target:'panel.example.com',qualifier:'192.0.2.10',status:'running'}]}));
        const expected=mode==='local'?(role==='primary'?'setup.guide.accessDNSPrepared':'setup.guide.accessDNSSecondary'):'setup.guide.accessDNSProvider';
        assert.ok(keys(guide).includes(expected));
        assert.ok(!keys(guide).includes('setup.guide.automaticRecords'));
    }
});

test('stopped DNS checks never promise automatic continuation while active secondary waits identify the primary',()=>{
    const failed=setupExecutionGuidance(execution({status:'failed',phase:'access_dns',steps:[{id:'access',kind:'access_dns',target:'panel.example.com',qualifier:'192.0.2.10',status:'failed'}]}));
    assert.ok(keys(failed).includes('setup.guide.failed'));
    assert.ok(!keys(failed).includes('setup.guide.accessDNSResume'));
    const waiting=setupExecutionGuidance(execution({status:'waiting',phase:'primary_dns',context:context({dns_role:'secondary'})}));
    assert.ok(keys(waiting).includes('setup.guide.primaryDNSWaiting'));
    assert.deepEqual(waiting.messages[0].values,{primary:'ns2.example.com',primaryIP:'192.0.2.20'});
    assert.ok(keys(waiting).includes('setup.guide.nativeDNS'));
});

test('unknown infrastructure publication preserves uncertainty instead of directing duplicate creation',()=>{
    const guide=setupExecutionGuidance(execution({phase:'infrastructure_dns',error:{code:'server_setup_infrastructure_dns_unknown',message:'pending outcome'},context:context({infrastructure_dns:{zone:'example.com'}})}));
    assert.ok(keys(guide).includes('setup.infrastructure.unknown'));
    assert.ok(!keys(guide).includes('setup.guide.infrastructureDNSPeer'));
    assert.ok(!keys(guide).includes('setup.guide.accessDNSResume'));
});

test('infrastructure record copy has complete bilingual placeholders and intact Turkish characters',()=>{
    const enKeys=Object.keys(enScreens).filter(key=>key.startsWith('setup.infrastructure.'));
    assert.deepEqual(Object.keys(trScreens).filter(key=>key.startsWith('setup.infrastructure.')).sort(),enKeys.sort());
    for(const key of enKeys){
        assert.deepEqual((enScreens[key].match(/\{[^}]+\}/g)||[]).sort(),(trScreens[key].match(/\{[^}]+\}/g)||[]).sort(),key);
        assert.ok(!trScreens[key].includes('?'),key);
    }
    assert.ok(trScreens['setup.infrastructure.title'].includes('kayıtları'));
});
