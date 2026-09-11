import subprocess,concurrent.futures,json,sys
from pathlib import Path
ROOT=Path(sys.argv[1] if len(sys.argv)>1 else '/var/tmp/cp-setup-profiles-20260911-v3')
assert str(ROOT) in ('/var/tmp/cp-setup-profiles-20260911-v2','/var/tmp/cp-setup-profiles-20260911-v3')
nodes=json.loads((ROOT/'plan.json').read_text())
options=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={ROOT}/ssh-known-hosts']
guest=r'''
import os,json,ssl,socket,smtplib,sqlite3,hashlib,time,subprocess,urllib.request,urllib.error,sys
from pathlib import Path
profile=sys.argv[1]
assert profile in ('web','application','webmail')
folder=Path('/var/lib/celikpanel-profile-vm')
assert (folder/'fixture').read_text()=='fresh-setup-profile-acceptance-v1\n'
assert not Path('/opt/celikpanel/bin/panel').exists()
conn=sqlite3.connect('/var/lib/celikpanel/celikpanel.db')
assert conn.execute('select status from server_setup_state where id=1').fetchone()[0]=='ready'
panel='panel.'+profile+'.setup.test'
mail='mail.'+profile+'.setup.test'
context=ssl.create_default_context()
def run(*args):
    result=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=360)
    if result.returncode:raise RuntimeError(' '.join(args)+': '+result.stdout[-5000:])
    return result.stdout.strip()
def tlsleaf(domain,port,starttls=False):
    if starttls:
        client=smtplib.SMTP(domain,port,timeout=6);client.ehlo();client.starttls(context=context)
        cert=client.sock.getpeercert(binary_form=True);client.quit()
    else:
        with socket.create_connection((domain,port),timeout=6) as raw:
            with context.wrap_socket(raw,server_hostname=domain) as secure:cert=secure.getpeercert(binary_form=True)
    return hashlib.sha256(cert).hexdigest()
def state():
    return {'setup':conn.execute('select status,revision,completed_at from server_setup_state where id=1').fetchone(),'children':conn.execute('select id,status,kind,request_id from service_operations order by id').fetchall()}
before={'panel_leaf':tlsleaf(panel,2083),'state':state(),'panel_pid':run('systemctl','show','-p','MainPID','--value','celikpanel-panel.service'),'timer_enabled':run('systemctl','is-enabled','certbot.timer'),'timer_active':run('systemctl','is-active','certbot.timer')}
if profile=='webmail':
    before['mail_submission_leaf']=tlsleaf(mail,587,True);before['mail_imaps_leaf']=tlsleaf(mail,993)
    assert before['mail_submission_leaf']==before['mail_imaps_leaf']
(folder/'renewal-before.json').write_text(json.dumps(before,indent=2))
if profile=='web':
    drop=Path('/etc/systemd/system/celikpanel-panel.service.d');drop.mkdir(exist_ok=True)
    (drop/'90-fixture-expired-license.conf').write_text('[Service]\nEnvironment=CELIKPANEL_SETUP_PROFILE_LICENSE=expired\n')
    run('systemctl','daemon-reload');run('systemctl','restart','celikpanel-panel.service')
    for attempt in range(60):
        try:
            req=urllib.request.Request('https://'+panel+':2083/api/v1/setup')
            req.add_header('Cookie','celikpanel_session='+(folder/'session').read_text())
            urllib.request.urlopen(req,context=context,timeout=8)
            raise AssertionError('expired license admitted management API')
        except urllib.error.HTTPError as e:
            raw=e.read().decode()
            if e.code==403 and 'license_required' in raw:break
        except OSError:pass
        time.sleep(1)
    else:raise RuntimeError('license-expired API gate did not settle')
    before['expired_license_api']='403 license_required'
    before['panel_pid_before_expiry']=before['panel_pid']
    before['panel_pid']=run('systemctl','show','-p','MainPID','--value','celikpanel-panel.service')
    before['workloads_after_expiry']={unit:run('systemctl','is-active',unit) for unit in ('nginx','php8.4-fpm','mariadb')}
    (folder/'renewal-before.json').write_text(json.dumps(before,indent=2))
# Exercise the installed Certbot service and an actual systemd timer firing.
# The overrides exist only inside the disposable guest. They force renewal now
# without changing its saved ACME endpoint, challenge plugin or deploy hooks.
timer=Path('/etc/systemd/system/certbot.timer.d');timer.mkdir(exist_ok=True)
service=Path('/etc/systemd/system/certbot.service.d');service.mkdir(exist_ok=True)
(timer/'90-fixture-acceptance.conf').write_text('[Timer]\nOnCalendar=\nOnBootSec=\nOnUnitActiveSec=\nOnActiveSec=5s\nAccuracySec=1s\nRandomizedDelaySec=0\nPersistent=false\n')
(service/'90-fixture-acceptance.conf').write_text('[Service]\nExecStart=\nExecStart=/usr/bin/certbot -q renew --force-renewal --no-random-sleep-on-renew\n')
before_trigger=run('systemctl','show','-p','LastTriggerUSec','--value','certbot.timer')
try:
    run('systemctl','daemon-reload');run('systemctl','restart','certbot.timer')
    deadline=time.monotonic()+240
    after={}
    while time.monotonic()<deadline:
        try:
            after={'panel_leaf':tlsleaf(panel,2083),'timer_trigger':run('systemctl','show','-p','LastTriggerUSec','--value','certbot.timer'),'service_result':run('systemctl','show','-p','Result','--value','certbot.service')}
            if profile=='webmail':after['mail_submission_leaf']=tlsleaf(mail,587,True);after['mail_imaps_leaf']=tlsleaf(mail,993)
            service_state=run('systemctl','show','-p','ActiveState','--value','certbot.service')
            changed=after['panel_leaf']!=before['panel_leaf']
            if profile=='webmail':changed=changed and after['mail_submission_leaf']==after['mail_imaps_leaf'] and after['mail_submission_leaf']!=before['mail_submission_leaf']
            if changed and after['timer_trigger']!=before_trigger and after['service_result']=='success' and service_state=='inactive':break
        except (OSError,RuntimeError,ssl.SSLError,smtplib.SMTPException):pass
        time.sleep(3)
    else:raise RuntimeError('actual timer/hook renewal did not converge: '+json.dumps(after))
    after['state']=state();assert after['state']==before['state'],'renewal changed setup or child operation identity'
    after['panel_pid']=run('systemctl','show','-p','MainPID','--value','celikpanel-panel.service')
    assert after['panel_pid']!=before['panel_pid']
    after['renewal_configs']={p.name:p.read_text() for p in Path('/etc/letsencrypt/renewal').glob('*.conf')}
    assert after['renewal_configs'] and all('https://acme.setup.test:14000/dir' in c for c in after['renewal_configs'].values())
    after['active_workloads']={unit:run('systemctl','is-active',unit) for unit in (('nginx','postfix','dovecot','mariadb','rspamd') if profile=='webmail' else (('nginx','php8.4-fpm','mariadb') if profile=='web' else ('nginx',)))}
    if profile=='web':
        req=urllib.request.Request('https://'+panel+':2083/api/v1/setup')
        req.add_header('Cookie','celikpanel_session='+(folder/'session').read_text())
        try:
            urllib.request.urlopen(req,context=context,timeout=8)
            raise AssertionError('renewal restart admitted expired-license management API')
        except urllib.error.HTTPError as e:
            assert e.code==403 and 'license_required' in e.read().decode()
            after['expired_license_api']='403 license_required'
    (folder/'renewal-after.json').write_text(json.dumps(after,indent=2))
    print('REAL_CERTBOT_TIMER_RENEWAL_PASSED '+profile+' '+json.dumps(after),flush=True)
finally:
    (timer/'90-fixture-acceptance.conf').unlink(missing_ok=True)
    (service/'90-fixture-acceptance.conf').unlink(missing_ok=True)
    run('systemctl','daemon-reload');run('systemctl','restart','certbot.timer')
    print('restored installed timer '+run('systemctl','is-active','certbot.timer'),flush=True)
'''
def renew(name):
    assert name in nodes
    row=nodes[name];port=str(row['port'])
    pid=int((ROOT/name/'qemu.pid').read_text())
    assert b'cp-profile65-'+name.encode() in Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
    with (ROOT/name/'renewal-driver.log').open('w') as log:
        result=subprocess.run(['ssh',*options,'-p',port,'celik@127.0.0.1','sudo python3 - '+name],input=guest,text=True,stdout=log,stderr=subprocess.STDOUT,timeout=600)
    journal=subprocess.run(['ssh',*options,'-p',port,'celik@127.0.0.1','sudo journalctl -u certbot.service -u celikpanel-panel.service -u celikpanel-agent.service -u celikpanel-fixture-acme.service --no-pager'],capture_output=True,timeout=45)
    (ROOT/name/'renewal-journal.log').write_bytes(journal.stdout+journal.stderr)
    print(name+' renewal exit '+str(result.returncode),flush=True)
    if result.returncode:print((ROOT/name/'renewal-driver.log').read_text()[-7000:],flush=True)
    return result.returncode
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:codes=list(pool.map(renew,sys.argv[2:] or ['web','application','webmail']))
if any(codes):raise SystemExit(1)
