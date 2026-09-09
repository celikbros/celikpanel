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
check((int)$s->licenses($member['id'])[0]['expires']===(new DateTimeImmutable('2029-02-28T10:00:00Z'))->getTimestamp(),'term starts on issue');
check($s->reveal($member['id'],$license['id'],$password)['key']===$license['key'],'same key can be revealed');
rejects(fn()=>$s->reveal($member['id'],$license['id'],'wrong'),'invalid_login');
rejects(fn()=>$s->reveal($member['id'],str_repeat('f',32),$password),'license_not_found');
$s->db->prepare('UPDATE licenses SET key_encrypted=NULL WHERE id=?')->execute([$license['id']]);
rejects(fn()=>$s->reveal($member['id'],$license['id'],$password),'key_not_saved');
rejects(fn()=>$s->remember($member['id'],$license['id'],$password,'CPK-'.str_repeat('0',64)),'invalid_license');
check($s->remember($member['id'],$license['id'],$password,$license['key'])['key']===$license['key'],'legacy key saved without rotation');
check($s->reveal($member['id'],$license['id'],$password)['key']===$license['key'],'legacy key can be revealed');
$encrypted=$s->db->query('SELECT key_encrypted FROM licenses')->fetchColumn();
check(!str_contains($encrypted,$license['key']),'stored key is encrypted');
$s->db->prepare('UPDATE licenses SET key_encrypted=? WHERE id=?')->execute(['broken',$license['id']]);
rejects(fn()=>$s->reveal($member['id'],$license['id'],$password),'key_not_saved');
$s->db->prepare('UPDATE licenses SET key_encrypted=? WHERE id=?')->execute([$encrypted,$license['id']]);
$now+=86400*10;
$e=$s->activate($license['key'],$server,'frankfurt.example.com','8.8.8.8');$c=claims($e,$public);
check($c['expires_at']===(new DateTimeImmutable('2029-02-28T10:00:00Z'))->getTimestamp(),'calendar year leap date');
$now+=3600;$retry=$s->activate($license['key'],$server,'frankfurt.example.com','8.8.8.8');$r=claims($retry,$public);
check($c['activated_at']===$r['activated_at']&&$c['expires_at']===$r['expires_at'],'retry preserves term');
check($retry['activation_token']===$e['activation_token'],'retry preserves credential');
rejects(fn()=>$s->activate($license['key'],$other,'boston.example.com','9.9.9.9'),'license_in_use');
$s->refresh($license['id'],$server,$e['activation_token'],'8.8.8.8');
rejects(fn()=>$s->refresh($license['id'],$other,$e['activation_token'],'9.9.9.9'),'invalid_activation');
rejects(fn()=>$s->renew($member['id'],$license['id'],$password),'renewal_not_due');
rejects(fn()=>$s->release($member['id'],$license['id'],'wrong'),'invalid_login');
$s->release($member['id'],$license['id'],$password);
$replacement=$license;
check($s->reveal($member['id'],$license['id'],$password)['key']===$license['key'],'transfer preserves the key');
rejects(fn()=>$s->refresh($license['id'],$server,$e['activation_token'],'8.8.8.8'),'invalid_activation');
$moved=$s->activate($replacement['key'],$other,'boston.example.com','9.9.9.9');$mc=claims($moved,$public);
check($mc['expires_at']===$c['expires_at'],'transfer preserves expiry');
check($mc['server_id']===$other,'transfer binds other server');
// The key survives reinstall at the same verified IP, but not a change of IP.
$ipLicense=$s->issue($member['id']);
$ipEnvelope=$s->activate($ipLicense['key'],$server,'ip.example.com','8.8.4.4');
$ipClaims=claims($ipEnvelope,$public);
rejects(fn()=>$s->activate($ipLicense['key'],$server,'ip.example.com','9.9.9.9'),'license_in_use');
rejects(fn()=>$s->refresh($ipLicense['id'],$server,$ipEnvelope['activation_token'],'9.9.9.9'),'license_in_use');
rejects(fn()=>$s->activate($ipLicense['key'],$server,'ip.example.com','127.0.0.1'),'invalid_server_ip');
rejects(fn()=>$s->activate($ipLicense['key'],$server,'ip.example.com','10.1.1.1'),'invalid_server_ip');
$reinstalled=$s->activate($ipLicense['key'],$other,'ip.example.com','::ffff:8.8.4.4');
$reinstalledClaims=claims($reinstalled,$public);
check($reinstalledClaims['expires_at']===$ipClaims['expires_at'],'reinstall preserves expiry');
check($reinstalledClaims['server_id']===$other,'reinstall updates receipt identity');
rejects(fn()=>$s->refresh($ipLicense['id'],$server,$ipEnvelope['activation_token'],'8.8.4.4'),'invalid_activation');
$s->refresh($ipLicense['id'],$other,$reinstalled['activation_token'],'8.8.4.4');
// A legacy server proves its original machine/credential once before IP binding.
$s->db->prepare('UPDATE licenses SET server_ip=NULL,key_encrypted=NULL WHERE id=?')->execute([$ipLicense['id']]);
rejects(fn()=>$s->activate($ipLicense['key'],$server,'ip.example.com','8.8.4.4'),'license_in_use');
$s->refresh($ipLicense['id'],$other,$reinstalled['activation_token'],'8.8.4.4');
$s->activate($ipLicense['key'],$server,'ip.example.com','8.8.4.4');
check($s->reveal($member['id'],$ipLicense['id'],$password)['key']===$ipLicense['key'],'activation makes legacy key available');
$rotated=$s->rotate($member['id'],$ipLicense['id'],$password);
rejects(fn()=>$s->activate($ipLicense['key'],$server,'ip.example.com','8.8.4.4'),'invalid_license');
rejects(fn()=>$s->activate($rotated['key'],$server,'ip.example.com','9.9.9.9'),'license_in_use');
$rotatedClaims=claims($s->activate($rotated['key'],$server,'ip.example.com','8.8.4.4'),$public);
check($rotatedClaims['expires_at']===$ipClaims['expires_at'],'rotation preserves expiry and IP');
check($s->reveal($member['id'],$rotated['id'],$password)['key']===$rotated['key'],'replacement key can be revealed');
// Copying encrypted bytes to another license cannot expose or swap the key.
$s->db->prepare('UPDATE licenses SET key_encrypted=(SELECT key_encrypted FROM licenses WHERE id=?) WHERE id=?')->execute([$license['id'],$ipLicense['id']]);
rejects(fn()=>$s->reveal($member['id'],$ipLicense['id'],$password),'key_not_saved');

$now=$c['expires_at'];
rejects(fn()=>$s->activate($replacement['key'],$other,'boston.example.com','9.9.9.9'),'license_expired');
$expired=claims($s->refresh($license['id'],$other,$moved['activation_token'],'9.9.9.9'),$public);
check($expired['expires_at']<=$now,'refresh reports expired term');
$s->renew($member['id'],$license['id'],$password);
$renewed=claims($s->refresh($license['id'],$other,$moved['activation_token'],'9.9.9.9'),$public);
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

// Migration fills only unstarted legacy terms and preserves granted expiries and key hashes.
$legacy=$s->issue($member['id']);
$beforeMigration=$s->db->query('SELECT id,key_hash,expires FROM licenses ORDER BY id')->fetchAll(PDO::FETCH_ASSOC);
$s->db->prepare('UPDATE licenses SET expires=NULL,key_encrypted=NULL WHERE id=?')->execute([$legacy['id']]);
$migrated=new Service($dir.'/db.sqlite',$secret,static function(){},fn()=>$now);
$afterMigration=$migrated->db->query('SELECT id,key_hash,expires FROM licenses ORDER BY id')->fetchAll(PDO::FETCH_ASSOC);
check($beforeMigration===$afterMigration,'migration preserves keys and granted terms');
check((int)$migrated->db->query('SELECT COUNT(*) FROM licenses WHERE key_encrypted IS NULL')->fetchColumn()===1,'legacy key is not silently replaced');

// Two processes race different servers for the same unbound license.
if(function_exists('pcntl_fork')){
 $race=$s->issue($member['id']);$pids=[];
 for($i=0;$i<2;$i++){
  $pid=pcntl_fork();if($pid===0){$child=new Service($dir.'/db.sqlite',$secret,static function(){},fn()=>$now);
   try{$child->activate($race['key'],str_repeat((string)($i+1),64),'race.example.com',$i===0?'8.8.8.8':'9.9.9.9');exit(0);}catch(Problem $e){exit($e->getMessage()==='license_in_use'?10:20);}}
  $pids[]=$pid;
 }
 $codes=[];foreach($pids as $pid){pcntl_waitpid($pid,$status);$codes[]=pcntl_wexitstatus($status);}sort($codes);check($codes===[0,10],'concurrent activation has one winner');
}
if (($interop=getenv('CELIKPANEL_LICENSE_INTEROP_FIXTURE')) !== false && $interop !== '') {
 file_put_contents($interop,json_encode(['public_key'=>bin2hex($public),'server_id'=>$other,'envelope'=>$s->refresh($license['id'],$other,$moved['activation_token'],'9.9.9.9')],JSON_THROW_ON_ERROR));
}
echo "membership service: $count checks passed\n";
