"""Exercise real HTTP sessions/CSRF/email links and activation without sending mail."""
import http.cookiejar, json, re, sys, urllib.request, urllib.parse, urllib.error
from pathlib import Path
base='http://127.0.0.1:8379'
private=Path(sys.argv[1])
jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
checks=0
def check(value,label):
 global checks
 assert value,label
 checks+=1
def request(action='home',data=None,origin=None):
 headers={}
 if origin:headers['Origin']=origin
 req=urllib.request.Request(base+'/account/?action='+action,data=None if data is None else urllib.parse.urlencode(data).encode(),headers=headers)
 try:
  with opener.open(req,timeout=20) as r:return r.status,r.read().decode(),r.headers
 except urllib.error.HTTPError as e:return e.code,e.read().decode(),e.headers
def csrf(body):return re.search(r'name="csrf" value="([a-f0-9]{64})"',body)[1]
def latest(purpose,email):
 messages=[json.loads(p.read_text()) for p in sorted((private/'mail').glob('*.json'),key=lambda p:p.stat().st_mtime_ns)]
 return [m for m in messages if m['purpose']==purpose and m['email']==email][-1]['token']
status,body,headers=request('register')
check(status==200 and '<form' in body,'register page')
check('no-store' in headers.get('Cache-Control',''),'no cache')
check("form-action 'self'" in headers.get('Content-Security-Policy',''),'forms allowed only same origin')
email='http-test@example.com';password='example-membership-password-123'
_,bad,_=request('register',{'email':email,'terms':'yes','csrf':'wrong'})
check('Oturumunuz yenilendi' in bad,'CSRF rejects')
check(not list((private/'mail').glob('*.json')),'CSRF does not send mail')
_,bad,_=request('register',{'email':email,'terms':'yes','csrf':csrf(bad)},'https://attacker.example')
check('İstek tamamlanamadı' in bad,'cross-origin rejected')
_,body,_=request('register',{'email':email,'terms':'yes','csrf':csrf(bad)})
check('e-posta gönderildi' in body,'registration feedback')
token=latest('verify',email)
_,body,_=request('verify')
_,body,_=request('verify',{'csrf':csrf(body),'token':token,'password':password})
check('E-postanız doğrulandı' in body,'verify owner and choose password')
_,body,_=request('login',{'csrf':csrf(body),'email':email,'password':password})
check('Lisanslarınız' in body,'signed in')
check(any(c.has_nonstandard_attr('HttpOnly') for c in jar),'httpOnly session')
_,body,_=request('issue',{'csrf':csrf(body)})
key=re.search(r'CPK-[a-f0-9]{64}',body)[0]
check('tekrar erişebilirsiniz' in body,'repeat key access notice')
license_id=re.search(r'action=reveal&amp;id=([a-f0-9]{32})',body)[1]
_,next_body,_=request()
check(key not in next_body,'key hidden until authenticated reveal')
_,body,_=request('reveal',{'csrf':csrf(next_body),'id':license_id,'password':'wrong-password-123'})
check(key not in body,'wrong password cannot reveal key')
_,body,headers=request('reveal',{'csrf':csrf(body),'id':license_id,'password':password})
check(key in body and 'no-store' in headers.get('Cache-Control',''),'owner can reveal the same key again')
_,body,_=request('reveal',{'csrf':csrf(body),'id':'f'*32,'password':password})
check(key not in body,'other license cannot be revealed')

def api(action,data):
 req=urllib.request.Request(base+'/account/?action='+action,data=json.dumps(data).encode(),headers={'Content-Type':'application/json'})
 try:
  with urllib.request.urlopen(req,timeout=20) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as e:return e.code,json.load(e)
code,e=api('activate',{'key':key,'server_id':'a'*64,'hostname':'fixture.example.com'})
check(code==200 and 'signature' in e,'API activation')
code,other=api('activate',{'key':key,'server_id':'b'*64,'hostname':'other.example.com'})
check(code==200 and 'signature' in other,'same IP reinstall reuses the key')
code,old=api('refresh',{'license_id':license_id,'server_id':'a'*64,'activation_token':e['activation_token']})
check(code==400 and old['error']=='invalid_activation','old installation refresh rejected')
code,spoof=api('activate',{'key':key,'server_id':'b'*64,'hostname':'other.example.com','server_ip':'9.9.9.9'})
check(code==400 and spoof['error']=='invalid_request','client cannot supply binding IP')
_,body,_=request('release',{'csrf':csrf(body),'id':license_id,'password':password})
check('Sunucu eşlemesi kaldırıldı' in body,'server release confirmed')
code,again=api('activate',{'key':key,'server_id':'a'*64,'hostname':'fixture.example.com'})
check(code==200,'same key works after transfer')
_,body,_=request('home&lang=en');check('Your licenses' in body and 'fixture.example.com' in body,'English account and binding')
_,body,_=request('terms&lang=en');check('Terms and data notice' in body and '7 days' in body,'terms reachable')
_,body,_=request('forgot');_,body,_=request('forgot',{'csrf':csrf(body),'email':email})
reset=latest('reset',email)
_,body,_=request('reset');_,body,_=request('reset',{'csrf':csrf(body),'token':reset,'password':'replacement-password-456'})
_,body,_=request('home');check('Sign in to your account' in body,'reset revokes existing session')
_,body,_=request('login',{'csrf':csrf(body),'email':email,'password':'replacement-password-456'})
check('Your licenses' in body,'new password works')
_,body,_=request('logout',{'csrf':csrf(body)})
_,body,_=request('home');check('Sign in to your account' in body,'logout invalidates session')
print(f'membership HTTP: {checks} checks passed')
