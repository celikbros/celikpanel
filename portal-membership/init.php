<?php
declare(strict_types=1);
// Run only via CLI, with the permanent private directory outside httpdocs.
if (PHP_SAPI !== 'cli' || $argc !== 2) { exit(2); }
umask(0077);
$dir=$argv[1];
if (!str_starts_with($dir,'/') || str_contains($dir,'/../') || is_link($dir)) { throw new RuntimeException('Unsafe private path'); }
if (!is_dir($dir) && !mkdir($dir,0700,true)) { throw new RuntimeException('Cannot create private directory'); }
if (!is_dir($dir.'/sessions')) { mkdir($dir.'/sessions',0700); }
$path=$dir.'/config.json';
if (!file_exists($path)) {
    $pair=sodium_crypto_sign_keypair();
    $config=['origin'=>'https://celikpanel.net','sender'=>'noreply@celikpanel.net',
        'signing_key'=>base64_encode(sodium_crypto_sign_secretkey($pair))];
    $f=fopen($path,'x'); if (!$f) { throw new RuntimeException('Cannot create config'); }
    fwrite($f,json_encode($config,JSON_THROW_ON_ERROR|JSON_PRETTY_PRINT)); fflush($f); fsync($f); fclose($f);
}
$config=json_decode(file_get_contents($path),true,8,JSON_THROW_ON_ERROR);
require __DIR__.'/src/Service.php';
new \CelikPanel\Membership\Service($dir.'/members.sqlite',base64_decode($config['signing_key'],true),static function (): void {});
echo 'license_public_key='.bin2hex(sodium_crypto_sign_publickey_from_secretkey(base64_decode($config['signing_key'],true)))."\n";
