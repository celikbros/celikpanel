<?php
declare(strict_types=1);

use CelikPanel\Membership\Service;
use CelikPanel\Membership\Problem;

require_once __DIR__.'/src/Service.php';
require_once __DIR__.'/src/Mail.php';
umask(0077);
ini_set('display_errors','0');
header('Cache-Control: no-store');
header('X-Content-Type-Options: nosniff');
header('Referrer-Policy: same-origin');
header("Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'");
$action=$_GET['action'] ?? 'home';
$api=in_array($action,['activate','refresh'],true);
$private=getenv('CELIKPANEL_MEMBERSHIP_PRIVATE') ?: dirname(__DIR__,3).'/membership-private';
try {
    $config=json_decode(file_get_contents($private.'/config.json'),true,8,JSON_THROW_ON_ERROR);
    $origin=$config['origin'];
    $send=static function (string $email,string $purpose,string $token) use ($config): void {
        $url=$config['origin'].'/account/?action='.$purpose.'#token='.$token;
        $subject=$purpose==='verify'?'CelikPanel email verification / E-posta dogrulama':'CelikPanel password reset / Parola yenileme';
        $body="CelikPanel\n\n".($purpose==='verify'?'Confirm your email / E-postanizi dogrulayin:':'Reset your password / Parolanizi yenileyin:')."\n".$url.
            "\n\nValid for 30 minutes. If you did not request this, ignore this email.\n30 dakika gecerlidir. Istegi siz yapmadiysaniz bu e-postayi yok sayin.\n";
        // Test delivery is available only in the explicit loopback development config.
        if (($config['test_mail_dir'] ?? '') !== '' && str_starts_with($config['origin'],'http://127.0.0.1:')) {
            file_put_contents($config['test_mail_dir'].'/'.bin2hex(random_bytes(12)).'.json',json_encode(compact('email','purpose','token'),JSON_THROW_ON_ERROR)); return;
        }
        \CelikPanel\Membership\Mail::send($config,$email,$subject,$body);
    };
    $service=new Service($private.'/members.sqlite',base64_decode($config['signing_key'],true),$send);
    $ip=$_SERVER['REMOTE_ADDR'] ?? 'unknown';
    if ((int)($_SERVER['CONTENT_LENGTH'] ?? 0)>8192) { http_response_code(413); exit; }
    if ($api) {
        header('Content-Type: application/json');
        if ($_SERVER['REQUEST_METHOD']!=='POST') { http_response_code(405); header('Allow: POST'); echo '{"error":"method_not_allowed"}'; exit; }
        $service->limit('api:'.$ip,60,60);
        if (strtolower(trim(explode(';',$_SERVER['CONTENT_TYPE'] ?? '')[0]))!=='application/json') { throw new Problem('invalid_request'); }
        $raw=file_get_contents('php://input',false,null,0,8193);
        if (strlen($raw)>8192) { throw new Problem('invalid_request'); }
        try { $in=json_decode($raw,true,8,JSON_THROW_ON_ERROR); } catch (\JsonException $e) { throw new Problem('invalid_request'); }
        if (!is_array($in)) { throw new Problem('invalid_request'); }
        foreach ($in as $v) { if (!is_string($v)) { throw new Problem('invalid_request'); } }
        $result=$action==='activate'?$service->activate($in['key']??'',$in['server_id']??'',$in['hostname']??''):
            $service->refresh($in['license_id']??'',$in['server_id']??'',$in['activation_token']??'');
        echo json_encode($result,JSON_THROW_ON_ERROR); exit;
    }
    $service->limit('page:'.$ip,120,60);
    $secure=str_starts_with($origin,'https://');
    session_name($secure?'__Host-celikpanel_member':'celikpanel_member_dev');
    session_save_path($private.'/sessions');
    ini_set('session.use_strict_mode','1'); ini_set('session.use_only_cookies','1'); ini_set('session.gc_maxlifetime','43200');
    session_set_cookie_params(['lifetime'=>0,'path'=>'/','secure'=>$secure,'httponly'=>true,'samesite'=>'Lax']);
    session_start();
    if (!isset($_SESSION['created']) || $_SESSION['created'] < time()-43200) { $_SESSION=[]; session_regenerate_id(true); $_SESSION['created']=time(); }
    $_SESSION['csrf'] ??= bin2hex(random_bytes(32));
    $lang=($_GET['lang'] ?? $_SESSION['lang'] ?? 'tr')==='en'?'en':'tr'; $_SESSION['lang']=$lang;
    $member=$service->member((int)($_SESSION['member']??0),(int)($_SESSION['epoch']??0));
    if (!$member) { unset($_SESSION['member'],$_SESSION['epoch']); }
    $error=null; $notice=$_SESSION['notice']??null; unset($_SESSION['notice']);
    $issued=$_SESSION['issued']??null; unset($_SESSION['issued']);
    if ($_SERVER['REQUEST_METHOD']==='POST') {
        try {
            if (isset($_SERVER['HTTP_ORIGIN']) && $_SERVER['HTTP_ORIGIN']!==$origin) { throw new Problem('invalid_request'); }
            if (!is_string($_POST['csrf']??null) || !hash_equals($_SESSION['csrf'],$_POST['csrf'])) { throw new Problem('session_changed'); }
            foreach ($_POST as $v) { if (!is_string($v)) { throw new Problem('invalid_request'); } }
            $service->limit('form:'.$ip,30,900);
            $email=$_POST['email']??''; $password=$_POST['password']??'';
            $go='login';
            switch ($action) {
                case 'register':
                    if (($_POST['terms']??'')!=='yes') { throw new Problem('accept_terms'); }
                    $service->register($email); $_SESSION['notice']='mail_sent'; break;
                case 'resend': $service->requestToken($email,'verify'); $_SESSION['notice']='mail_sent'; break;
                case 'forgot': $service->requestToken($email,'reset'); $_SESSION['notice']='mail_sent'; break;
                case 'verify': $service->consumeToken($_POST['token']??'','verify',$password); $_SESSION['notice']='email_verified'; break;
                case 'reset': $service->consumeToken($_POST['token']??'','reset',$password); $_SESSION['notice']='password_reset'; break;
                case 'login':
                    $m=$service->login($email,$password); session_regenerate_id(true);
                    $_SESSION=['member'=>$m['id'],'epoch'=>$m['epoch'],'lang'=>$lang,'created'=>time(),'csrf'=>bin2hex(random_bytes(32))]; $go='home'; break;
                case 'logout': $_SESSION=[]; session_regenerate_id(true); break;
                case 'issue': case 'release': case 'renew':
                    if (!$member) { throw new Problem('authentication_required'); }
                    if ($action==='issue') { $_SESSION['issued']=$service->issue((int)$member['id']); }
                    elseif ($action==='release') { $_SESSION['issued']=$service->release((int)$member['id'],$_POST['id']??'',$password); }
                    else { $service->renew((int)$member['id'],$_POST['id']??'',$password); $_SESSION['notice']='renewed'; }
                    $go='home'; break;
                default: throw new Problem('invalid_request');
            }
            header('Location: /account/?action='.$go.'&lang='.$lang,true,303); exit;
        } catch (Problem $e) { $error=$e->getMessage(); }
    } elseif ($_SERVER['REQUEST_METHOD']!=='GET') { http_response_code(405); exit; }
    if ($action==='home' && !$member) { $action='login'; }
    $licenses=$member?$service->licenses((int)$member['id']):[];
    require __DIR__.'/view.php';
} catch (Throwable $e) {
    http_response_code($e instanceof Problem?400:503);
    if ($api) { header('Content-Type: application/json'); echo json_encode(['error'=>$e instanceof Problem?$e->getMessage():'service_unavailable']); }
    else { header('Content-Type: text/plain; charset=UTF-8'); echo "Üyelik hizmetine şu anda erişilemiyor. Lütfen daha sonra tekrar deneyin.\nMembership is temporarily unavailable. Please try again later."; }
    // Do not log exception messages containing request data, credentials or SQL values.
    error_log('CelikPanel membership request failed: '.get_class($e));
}
