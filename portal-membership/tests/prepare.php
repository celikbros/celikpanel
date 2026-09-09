<?php
declare(strict_types=1);
if(PHP_SAPI!=='cli'||$argc!==2)exit(2);
umask(0077);$dir=$argv[1];
foreach([$dir,$dir.'/sessions',$dir.'/mail'] as $path){if(!is_dir($path))mkdir($path,0700,true);}
$pair=sodium_crypto_sign_keypair();
file_put_contents($dir.'/config.json',json_encode(['origin'=>'http://127.0.0.1:8379','sender'=>'noreply@celikpanel.net',
 'signing_key'=>base64_encode(sodium_crypto_sign_secretkey($pair)),'test_mail_dir'=>$dir.'/mail'],JSON_THROW_ON_ERROR));
echo "Local membership fixture prepared (no email delivery).\n";
