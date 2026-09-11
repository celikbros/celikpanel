const fs = require('node:fs/promises');
const path = require('node:path');
const http = require('node:http');
const assert = require('node:assert/strict');
const puppeteer = require('C:/Users/alice/AppData/Local/npm-cache/_npx/1a4eb60c8f6b0f89/node_modules/puppeteer');
const root = path.resolve('web/dist');
const output = path.resolve('.tmp-portal-review/remote-dns');
const server = http.createServer(async (req, res) => {
  const pathname = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
  let file = path.resolve(root, '.' + pathname);
  if (!file.startsWith(root + path.sep)) file = path.join(root, 'index.html');
  try { if (!(await fs.stat(file)).isFile()) throw Error(); }
  catch { file = path.join(root, 'index.html'); }
  const data = await fs.readFile(file);
  res.writeHead(200, {'Content-Type': {'.html':'text/html', '.js':'text/javascript', '.css':'text/css', '.svg':'image/svg+xml', '.woff2':'font/woff2'}[path.extname(file)] || 'application/octet-stream'});
  res.end(data);
});
(async()=>{
 await fs.mkdir(output,{recursive:true});
 await new Promise(resolve=>server.listen(8998,'127.0.0.1',resolve));
 const browser=await puppeteer.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
 const results=[];
 try{
  const cases=['tr','en'].flatMap(lang=>[1440,390].map(width=>({lang,width,scenario:'new'})));
  for(const {lang,width,scenario} of cases){
   console.log('scenario',scenario,lang,width);
   const role=scenario==='customer'?'customer':'admin';
   const context=await browser.createBrowserContext(),page=await context.newPage();
   await page.setViewport({width,height:900});
   let state={version:1,revision:0,origin:scenario==='legacy'?'legacy':'fresh',status:['legacy','ready','waiting'].includes(scenario)?scenario:'new',required:!['legacy','ready'].includes(scenario),server_ip:'198.51.100.42',checks:[],draft:{purpose:'web',panel_domain:'',mail_hostname:'',dns_mode:'local',dns_engine:'pdns',dns_role:'primary',ns1:'',ns2:'',local_ip:'',peer_ip:'',peer_ns:'',node_version:'',database:'mariadb',remote_dns_connection_id:''}};
   let plan={id:'a'.repeat(32),version:1,revision:0,purpose:'web',steps:[{id:'01-dns',kind:'dns',target:'external'},{id:'02-service',kind:'service',target:'nginx'},{id:'03-service',kind:'service',target:'php-fpm'},{id:'04-service',kind:'service',target:'mariadb'},{id:'05-firewall',kind:'firewall',target:'nftables'},{id:'06-panel_certificate',kind:'panel_certificate',target:'panel.example.com'},{id:'07-verify',kind:'verify',target:'web'}],blockers:[],can_start:true,tcp_ports:[22,80,443,2083],udp_ports:[],preserve_ssh:true,persist_firewall:true,contact_email:'admin@example.com'};
   let operation=scenario==='waiting'?{id:'b'.repeat(32),request_id:'b'.repeat(32),plan_id:plan.id,status:'waiting',phase:'verification',steps:plan.steps.map(s=>({...s,status:'succeeded'})),checks:[{id:'panel_renewal',state:'action_required',code:'panel_renewal_required'}]}:null;
   let setupReads=0,startCalls=0,pairCalls=0; let connections=[]; const remoteID='c'.repeat(32); const remoteEndpoint='https://dns-panel.example.com:2083'; const remoteConnection={id:remoteID,endpoint:remoteEndpoint,status:'ready',nameservers:['ns1.example.com','ns2.example.com'],created_at:'2026-09-11T00:00:00Z'};const writes=[],errors=[];
   page.on('pageerror',e=>errors.push(e.message)); page.on('console',async m=>{if(m.type()==='error'){console.log('CONSOLE',m.text());for(const arg of m.args())try{console.log(await arg.evaluate(v=>v instanceof Error?v.stack:''))}catch{}}});
   await page.evaluateOnNewDocument(lang=>localStorage.setItem('celikpanel.lang',lang),lang);
   await page.setRequestInterception(true);
   page.on('request',req=>{
    if(!req.url().includes('/api/'))return req.continue();
    const url=new URL(req.url()),p=url.pathname;let status=200,data={};
    if(req.method()!=='GET')writes.push(p);
    if(p==='/api/v1/auth/me')data={id:1,username:role,role,effective_role:role,account_type:'account',features:{team_members:false},impersonating:false};
    else if(p==='/api/v1/license/access')data={can_use_panel:scenario!=='locked',valid_until:Math.floor(Date.now()/1000)+3600};
    else if(p==='/api/v1/panel/license')data={state:'missing',can_provision:false};
    else if(p==='/api/v1/setup'){
     setupReads++;
     if(scenario==='unavailable'){status=503;data={error:'Fixture unavailable'};}
     else {if(req.method()==='PUT'){const saved=JSON.parse(req.postData());assert.equal(saved.revision,state.revision);state={...state,status:'draft',revision:state.revision+1,draft:saved.draft};}data=state;}
    }
    else if(p==='/api/v1/setup/operation')data=operation;
    else if(p==='/api/v1/setup/plan'){plan={...plan,revision:state.revision,remote_dns_connection:{id:remoteID,endpoint:remoteEndpoint,nameservers:remoteConnection.nameservers}};data=plan;}
    else if(p==='/api/v1/dns/remote/connections'){if(req.method()==='POST'){pairCalls++;const body=JSON.parse(req.postData());assert.equal(body.endpoint,remoteEndpoint);assert.equal(body.enrollment_code,'f'.repeat(64));connections=[remoteConnection];data={connection:remoteConnection};}else if(url.searchParams.get('check'))data={connection:remoteConnection,verified:true};else data={connections};}
    else if(p==='/api/v1/setup/start'){startCalls++;const saved=JSON.parse(req.postData());operation={id:saved.request_id,request_id:saved.request_id,plan_id:saved.plan_id,status:'succeeded',phase:'complete',steps:plan.steps.map(s=>({...s,status:'succeeded'}))};state={...state,status:'ready',required:false};data=operation;status=202;}
    else if(p==='/api/v1/system/stats'){status=503;data={error:'Fixture stats unavailable'};}
    else if(p==='/api/v1/dashboard')data={databases:0,mail_accounts:0,expiring_certs:[]};
    else if(p==='/api/v1/audit-logs')data={entries:[]};
    else if(p==='/api/v1/users')data={users:[]};
    else if(p==='/api/v1/domains')data=[];
    else if(p==='/api/v1/hosting/capabilities')data={dns_server:'',dns_identity_ready:false,dns_management_mode:'external',dns_management_ready:true};
    else if(p==='/api/v1/panel/version')data={version:'setup-development',commit:'a'.repeat(40)};
    req.respond({status,contentType:'application/json',body:JSON.stringify(data)});
   });
   await page.goto('http://127.0.0.1:8998/',{waitUntil:'networkidle0'});
   await page.waitForSelector('h1',{timeout:12000}).catch(async error=>{console.log('ERRORS',errors);console.log(await page.evaluate(()=>document.body.innerText));await page.screenshot({path:path.join(output,'debug-'+scenario+'.png'),fullPage:true});throw error;});
   assert.deepEqual(writes,[],'mount must be read-only');
   if(scenario==='new'){
    assert.equal(new URL(page.url()).pathname,'/setup');
    await page.waitForSelector('input[name="setup-purpose"]');
    assert.equal(await page.$$eval('input[name="setup-purpose"]',els=>els.length),4);
    await page.screenshot({path:path.join(output,`${lang}-${width}-purpose.png`),fullPage:true});
    await page.click('button[type="submit"]');
    await page.waitForSelector('#setup-panel_domain');
    await page.type('#setup-panel_domain','panel.example.com');
    await page.click('input[name="setup-dns"][value="existing"]');
    await page.waitForSelector('#setup-remote-endpoint');
    await page.type('#setup-remote-endpoint',remoteEndpoint);
    await page.type('#setup-remote-code','f'.repeat(64));
    await page.click('fieldset details input[type="checkbox"]');
    await page.$eval('fieldset details button',button=>button.click());
    await page.waitForFunction(id=>document.querySelector('#setup-remote-connection')?.value===id,{},remoteID);
    await page.waitForFunction(()=>!document.querySelector('button[type="submit"]').disabled);
    assert.equal(pairCalls,1);
    assert.equal(await page.$eval('#setup-remote-code',el=>el.value),'');
    assert.equal(await page.evaluate(()=>JSON.stringify(localStorage).includes('f'.repeat(64))),false);
    await page.waitForFunction(()=>!document.querySelector('#setup-ns1'));
    assert.equal(await page.$('#setup-database'),null,'fixed web stack must not offer ignored database choice');
    await page.screenshot({path:path.join(output,`${lang}-${width}-access.png`),fullPage:true});
    await page.click('button[type="submit"]');
    await page.waitForSelector('input[type="checkbox"]');
    await page.waitForFunction(endpoint=>document.body.innerText.includes(endpoint),{},remoteEndpoint);
    await page.screenshot({path:path.join(output,`${lang}-${width}-review.png`),fullPage:true});
    await page.click('input[type="checkbox"]');
    const startText=lang==='tr'?'Kurulumu':'Start reviewed setup';
    await page.evaluate(startText=>{const buttons=[...document.querySelectorAll('form button:not([disabled])')];const b=buttons.at(-1);if(!b)throw Error('start button missing');b.click();},startText);
    await page.waitForSelector('#setup-ready-title');
    assert.equal(startCalls,1);
   } else if(scenario==='waiting') { assert.equal(new URL(page.url()).pathname,'/setup');await page.waitForSelector('#setup-progress-title'); }
   else if(scenario==='locked'){assert.equal(new URL(page.url()).pathname,'/activate');assert.equal(setupReads,0);}
   else if(scenario==='unavailable'){assert.ok((await page.$eval('h1',el=>el.textContent)).includes('could not be checked'));}
   else {assert.equal(new URL(page.url()).pathname,'/');if(role==='customer')assert.equal(setupReads,0);}
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,`${lang}/${width}/${scenario} overflow`);
   assert.deepEqual(errors,[]);
   await page.screenshot({path:path.join(output,`${lang}-${width}-${scenario}-final.png`),fullPage:true});
   results.push({lang,width,scenario,setupReads,startCalls,pairCalls,writes,errors});
   await context.close();
  }
  await fs.writeFile(path.join(output,'browser-results.json'),JSON.stringify(results,null,2));console.log(JSON.stringify(results));
 }finally{await browser.close();server.close()}
})().catch(e=>{console.error(e);process.exitCode=1;server.close()});
