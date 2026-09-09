<?php
declare(strict_types=1);
// Only used by PHP's local test server; never copied into membership releases.
$path=parse_url($_SERVER['REQUEST_URI'],PHP_URL_PATH);
if($path==='/account/'||$path==='/account/index.php') {require __DIR__.'/../http.php';return true;}
return false;
