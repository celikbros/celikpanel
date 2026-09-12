import assert from 'node:assert/strict';
import test from 'node:test';
import { setupExecutionGuidance } from '../src/lib/serverSetupGuidance.ts';
import { enScreens } from '../src/i18n/screens/en.ts';
import { trScreens } from '../src/i18n/screens/tr.ts';

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
