<?php
declare(strict_types=1);
require __DIR__.'/../src/Service.php';
use CelikPanel\Membership\Service;
use CelikPanel\Membership\Problem;

umask(0077);
$dir=sys_get_temp_dir().'/celikpanel-membership-test-'.bin2hex(random_bytes(8));mkdir($dir,0700);
$now=(new DateTimeImmutable('2028-02-29T10:00:00Z'))->getTimestamp();$mail=[];
$pair=sodium_crypto_sign_keypair();$secret=sodium_crypto_sign_secretkey($pair);$public=sodium_crypto_sign_publickey($pair);
$s=new Service($dir.'/db.sqlite',$secret,function ($email,$purpose,$token) use (&$mail) {$mail[]=compact('email','purpose','token');},function () use (&$now) {return $now;});
$count=0;
function check(bool $ok,string $name): void {global $count;if(!$ok)throw new RuntimeException($name);$count++;}
function rejects(callable $fn,string $code): void {try{$fn();}catch(Problem $e){check($e->getMessage()===$code,$code);return;}throw new RuntimeException('Expected '.$code);}
function claims(array $e,string $public): array {check(sodium_crypto_sign_verify_detached(base64_decode($e['signature']),base64_decode($e['payload']),$public),'signature');return json_decode(base64_decode($e['payload']),true,16,JSON_THROW_ON_ERROR);}

$password='test-password-never-production';
rejects(fn()=>$s->register("x@example.com\r\nBcc: other@example.com"),'invalid_email');
$s->register('Alice@example.com');check(count($mail)===1,'verification sent');
rejects(fn()=>$s->login('alice@example.com',$password),'invalid_login');
$firstToken=$mail[0]['token'];
$s->register('alice@example.com');
rejects(fn()=>$s->consumeToken($firstToken,'verify',$password),'invalid_token');
$s->consumeToken($mail[1]['token'],'verify',$password);
rejects(fn()=>$s->consumeToken($mail[1]['token'],'verify',$password),'invalid_token');
$member=$s->login('alice@example.com',$password);
rejects(fn()=>$s->login('alice@example.com','attacker-password-123'),'invalid_login');
$license=$s->issue($member['id']);$server=str_repeat('a',64);$other=str_repeat('b',64);
$e=$s->activate($license['key'],$server,'frankfurt.example.com');$c=claims($e,$public);
check($c['expires_at']===(new DateTimeImmutable('2029-02-28T10:00:00Z'))->getTimestamp(),'calendar year leap date');
$now+=3600;$retry=$s->activate($license['key'],$server,'frankfurt.example.com');$r=claims($retry,$public);
check($c['activated_at']===$r['activated_at']&&$c['expires_at']===$r['expires_at'],'retry preserves term');
check($retry['activation_token']===$e['activation_token'],'retry preserves credential');
rejects(fn()=>$s->activate($license['key'],$other,'boston.example.com'),'license_in_use');
$s->refresh($license['id'],$server,$e['activation_token']);
rejects(fn()=>$s->refresh($license['id'],$other,$e['activation_token']),'invalid_activation');
rejects(fn()=>$s->renew($member['id'],$license['id'],$password),'renewal_not_due');
rejects(fn()=>$s->release($member['id'],$license['id'],'wrong'),'invalid_login');
$replacement=$s->release($member['id'],$license['id'],$password);
rejects(fn()=>$s->activate($license['key'],$server,'frankfurt.example.com'),'invalid_license');
rejects(fn()=>$s->refresh($license['id'],$server,$e['activation_token']),'invalid_activation');
$moved=$s->activate($replacement['key'],$other,'boston.example.com');$mc=claims($moved,$public);
check($mc['expires_at']===$c['expires_at'],'transfer preserves expiry');
check($mc['server_id']===$other,'transfer binds other server');
$now=$c['expires_at'];
rejects(fn()=>$s->activate($replacement['key'],$other,'boston.example.com'),'license_expired');
$expired=claims($s->refresh($license['id'],$other,$moved['activation_token']),$public);
check($expired['expires_at']<=$now,'refresh reports expired term');
$s->renew($member['id'],$license['id'],$password);
$renewed=claims($s->refresh($license['id'],$other,$moved['activation_token']),$public);
check($renewed['expires_at']>$now,'renewal refreshed');
rejects(fn()=>$s->renew($member['id'],$license['id'],$password),'renewal_not_due');
$s->requestToken('alice@example.com','reset');$reset=end($mail)['token'];
rejects(fn()=>$s->consumeToken($reset,'verify',$password),'invalid_token');
$s->consumeToken($reset,'reset','a-new-password-12345');
check($s->member($member['id'],$member['epoch'])===null,'reset revokes all sessions');
rejects(fn()=>$s->login('alice@example.com',$password),'invalid_login');
$s->login('alice@example.com','a-new-password-12345');
rejects(fn()=>$s->consumeToken($reset,'reset','another-password-12345'),'invalid_token');
$s->requestToken('alice@example.com','reset');$now+=1801;
rejects(fn()=>$s->consumeToken(end($mail)['token'],'reset','another-password-12345'),'invalid_token');
check(!str_contains(file_get_contents($dir.'/db.sqlite'),$license['key']),'key not plaintext');
check(!str_contains(json_encode($s->licenses($member['id'])),'activation_token'),'listing does not expose credentials');
$s->limit('one',1,60);rejects(fn()=>$s->limit('one',1,60),'rate_limited');$now+=61;$s->limit('one',1,60);

// Two processes race different servers for the same unbound license.
if(function_exists('pcntl_fork')){
 $race=$s->issue($member['id']);$pids=[];
 for($i=0;$i<2;$i++){
  $pid=pcntl_fork();if($pid===0){$child=new Service($dir.'/db.sqlite',$secret,static function(){},fn()=>$now);
   try{$child->activate($race['key'],str_repeat((string)($i+1),64),'race.example.com');exit(0);}catch(Problem $e){exit($e->getMessage()==='license_in_use'?10:20);}}
  $pids[]=$pid;
 }
 $codes=[];foreach($pids as $pid){pcntl_waitpid($pid,$status);$codes[]=pcntl_wexitstatus($status);}sort($codes);check($codes===[0,10],'concurrent activation has one winner');
}
if (($interop=getenv('CELIKPANEL_LICENSE_INTEROP_FIXTURE')) !== false && $interop !== '') {
 file_put_contents($interop,json_encode(['public_key'=>bin2hex($public),'server_id'=>$other,'envelope'=>$s->refresh($license['id'],$other,$moved['activation_token'])],JSON_THROW_ON_ERROR));
}
echo "membership service: $count checks passed\n";
