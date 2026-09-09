<?php
declare(strict_types=1);
$en=$lang==='en';
function h(mixed $v): string { return htmlspecialchars((string)$v,ENT_QUOTES|ENT_SUBSTITUTE,'UTF-8'); }
function t(string $tr,string $en): string { return $GLOBALS['en']?$en:$tr; }
function formStart(string $action): void {
    echo '<form method="post" action="/account/?action='.h($action).'&amp;lang='.h($GLOBALS['lang']).'">';
    echo '<input type="hidden" name="csrf" value="'.h($_SESSION['csrf']).'">';
}
function passwordField(string $autocomplete='current-password'): void {
    echo '<label>'.t('Parola','Password').'<input type="password" name="password" required minlength="12" maxlength="128" autocomplete="'.h($autocomplete).'"></label>';
}
$messages=[
 'invalid_email'=>['Geçerli bir e-posta adresi girin.','Enter a valid email address.'],
 'password_length'=>['Parolanız 12–128 bayt uzunluğunda olmalı.','Use a password between 12 and 128 bytes.'],
 'invalid_login'=>['E-posta veya parola doğru değil.','The email or password is incorrect.'],
 'verify_email'=>['Giriş yapmadan önce e-postanızı doğrulayın. Aşağıdan yeni doğrulama e-postası isteyebilirsiniz.','Verify your email before signing in. You can request another email below.'],
 'invalid_token'=>['Bağlantı kullanılmış veya süresi dolmuş. Yeni bir e-posta isteyin.','This link has been used or has expired. Request another email.'],
 'rate_limited'=>['Çok fazla deneme yapıldı. Lütfen daha sonra tekrar deneyin.','Too many attempts. Please try again later.'],
 'session_changed'=>['Oturumunuz yenilendi. Sayfayı yenileyip tekrar deneyin.','Your session changed. Reload this page and try again.'],
 'invalid_request'=>['İstek tamamlanamadı. Sayfayı yenileyip tekrar deneyin.','The request could not be completed. Reload and try again.'],
 'authentication_required'=>['Devam etmek için giriş yapın.','Sign in to continue.'],
 'accept_terms'=>['Devam etmek için kullanım koşullarını kabul edin.','Accept the terms to continue.'],
 'mail_unavailable'=>['E-posta şu anda gönderilemiyor. Daha sonra yeni bir doğrulama veya parola yenileme e-postası isteyin.','Email is temporarily unavailable. Request another verification or reset email later.'],
 'license_not_found'=>['Lisans bulunamadı.','License not found.'],
 'renewal_not_due'=>['Lisansınızı bitiş tarihinden önceki son 30 günde veya süresi dolduktan sonra yenileyebilirsiniz.','Renew during the last 30 days of the license or after it expires.'],
 'mail_sent'=>['Bu adres için işlem yapılabiliyorsa e-posta gönderildi. Gelen kutunuzu ve spam klasörünü kontrol edin.','If this address is eligible, an email has been sent. Check your inbox and spam folder.'],
 'email_verified'=>['E-postanız doğrulandı. Şimdi giriş yapabilirsiniz.','Your email is verified. You can now sign in.'],
 'password_reset'=>['Parolanız değiştirildi. Yeni parolanızla giriş yapın.','Your password has changed. Sign in with your new password.'],
 'renewed'=>['Lisans bir yıl uzatıldı. Sunucu sonraki lisans kontrolünde yeni tarihi alır.','The license was extended by one year. Your server receives the new date on its next license check.'],
];
function message(string $code): string { $m=$GLOBALS['messages'][$code]??$GLOBALS['messages']['invalid_request']; return $m[$GLOBALS['en']?1:0]; }
$titles=['terms'=>t('Kullanım koşulları ve veri açıklaması','Terms and data notice'),'login'=>t('Hesabınıza giriş yapın','Sign in to your account'),'register'=>t('CelikPanel hesabınızı oluşturun','Create your CelikPanel account'),
 'forgot'=>t('Parolanızı yenileyin','Reset your password'),'resend'=>t('Doğrulama e-postası isteyin','Request a verification email'),
 'verify'=>t('E-postanızı doğrulayın','Verify your email'),'reset'=>t('Yeni parolanızı belirleyin','Choose your new password'),
 'home'=>t('Lisanslarınız','Your licenses'),'release'=>t('Lisans anahtarını yenileyin','Replace your license key'),'renew'=>t('Lisansınızı uzatın','Renew your license')];
if (!isset($titles[$action])) { $action=$member?'home':'login'; }
header('Content-Type: text/html; charset=UTF-8');
?>
<!doctype html><html lang="<?=h($lang)?>"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title><?=h($titles[$action])?> · CelikPanel</title><link rel="icon" href="/assets/favicon-v2.svg"><link rel="stylesheet" href="/assets/site.css"><link rel="stylesheet" href="/account/member.css"><script src="/account/member.js" defer></script></head>
<body class="member-page"><a class="member-skip" href="#main"><?=t('İçeriğe geç','Skip to content')?></a>
<header class="member-header"><a href="/" class="member-brand"><img src="/assets/favicon-v2.svg" alt="" width="28" height="28">CelikPanel</a><nav aria-label="<?=t('Hesap menüsü','Account navigation')?>"><a href="?action=<?=h($action)?>&amp;lang=<?= $en?'tr':'en' ?>" lang="<?=$en?'tr':'en'?>"><?=$en?'Türkçe':'English'?></a><?php if ($member): formStart('logout'); ?><button class="member-link" type="submit"><?=t('Çıkış yap','Sign out')?></button></form><?php endif ?></nav></header>
<main id="main" class="member-main <?= $action==='home'?'member-wide':'' ?>">
<h1><?=h($titles[$action])?></h1>
<?php if ($error): ?><p class="member-notice member-error" role="alert"><?=h(message($error))?></p><?php endif ?>
<?php if ($notice): ?><p class="member-notice" role="status"><?=h(message($notice))?></p><?php endif ?>
<?php if ($action==='terms'): require __DIR__.'/terms.php'; ?>
<?php elseif ($action==='home' && $member): ?>
<p class="member-intro"><?=h($member['email'])?> · <?=t('Her lisans bir aktif sunucu içindir.','Each license is for one active server.')?></p>
<?php if ($issued): ?><section class="member-key" aria-labelledby="new-key"><h2 id="new-key"><?=t('Kurulum anahtarınız','Your installation key')?></h2><p><?=t('Bu anahtar yalnızca şimdi gösterilir. Güvenli bir yere kaydedin; kurulum istediğinde girin.','This key is shown only now. Save it securely and enter it when the installer asks.')?></p><code id="license-key"><?=h($issued['key'])?></code><button type="button" data-copy="license-key"><?=t('Anahtarı kopyala','Copy key')?></button><span id="copy-status" role="status"></span></section><?php endif ?>
<section class="member-summary"><div><h2>CelikPanel</h2><p><?=t('Şu an ücretsiz. İlk aktivasyondan itibaren bir yıl geçerli.','Currently free. Valid for one year from first activation.')?></p></div><?php formStart('issue'); ?><button type="submit"><?=t('Ücretsiz lisans oluştur','Create a free license')?></button></form></section>
<?php if (!$licenses): ?><p class="member-empty"><?=t('Henüz lisansınız yok. İlk lisansınızı oluşturup sunucunuza CelikPanel kurabilirsiniz.','You have no licenses yet. Create your first license to install CelikPanel on your server.')?></p><?php endif ?>
<div class="member-licenses"><?php foreach ($licenses as $l): $expired=$l['expires'] && (int)$l['expires']<=$service->now(); ?>
<article class="member-license"><div><h2><?=h($l['hostname']?:t('Aktivasyon bekliyor','Awaiting activation'))?></h2><p><span class="member-state"><?= $expired?t('Süresi doldu','Expired'):($l['server_id']?t('Aktif','Active'):t('Sunucuya bağlanmadı','Not bound to a server')) ?></span> <code>…<?=h($l['key_suffix'])?></code></p></div><dl><dt><?=t('Bitiş tarihi (UTC)','Expires (UTC)')?></dt><dd><?=$l['expires']?h(gmdate('Y-m-d H:i',(int)$l['expires'])):t('İlk aktivasyonda başlar','Starts on first activation')?></dd></dl><div class="member-actions"><a href="?action=release&amp;id=<?=h($l['id'])?>"><?=t('Anahtar / sunucu değiştir','Replace key / change server')?></a><?php if ($l['expires'] && (int)$l['expires']<=$service->now()+30*86400): ?><a href="?action=renew&amp;id=<?=h($l['id'])?>"><?=t('Ücretsiz yenile','Renew for free')?></a><?php endif ?></div></article><?php endforeach ?></div>
<section class="member-install"><h2><?=t('Sunucunuza kurun','Install on your server')?></h2><p><?=t('Desteklenen temiz bir Linux sunucusunda çalıştırın. Yönetici hesabınızı sunucuda oluşturacaksınız; bu hesabın parolası websitesiyle paylaşılmaz.','Run on a supported clean Linux server. You will create the administrator on your server; its password is not shared with this website.')?></p><pre><code id="install-command">curl -fL --proto '=https' -o celikpanel-install.sh https://celikpanel.net/get.sh &amp;&amp;
sh celikpanel-install.sh</code></pre><button type="button" data-copy="install-command"><?=t('Komutu kopyala','Copy command')?></button><p><?=t('Yarım kalan kurulumda aynı komutu yeniden çalıştırın. Lisans süresi yeniden başlamaz.','If installation is interrupted, run the same command again. The license term does not restart.')?></p></section>
<?php elseif (in_array($action,['release','renew'],true) && $member): ?>
<p><?= $action==='release'?t('Bu işlem eski anahtarı iptal eder ve sunucu bağlantısını kaldırır. Bitiş tarihi değişmez. Eski sunucudaki doğrulanmış lisans en fazla 7 gün daha yeni kaynak oluşturmaya izin verebilir. Çalışan siteler ve e-posta durdurulmaz.','This replaces the old key and releases the server binding. The expiry date stays the same. The old server’s cached license may allow new provisioning for up to 7 more days. Running sites and mail are not stopped.'):t('Şu an yenileme ücretsizdir. Lisans bitişi bir yıl uzatılır.','Renewal is currently free. The license expiry is extended by one year.') ?></p>
<?php formStart($action); ?><input type="hidden" name="id" value="<?=h($_POST['id']??$_GET['id']??'')?>"><?php passwordField(); ?><button type="submit"><?= $action==='release'?t('Yeni anahtar oluştur','Create replacement key'):t('Bir yıl ücretsiz uzat','Extend free for one year') ?></button></form><a href="/account/"><?=t('Lisanslarıma dön','Back to licenses')?></a>
<?php else: ?>
<?php if ($action==='register'): ?><p class="member-intro"><?=t('Bir hesapla sunucularınızın lisanslarını yönetin. Şu an ücretsiz; ödeme bilgisi gerekmez.','Manage your server licenses from one account. Currently free; no payment details required.')?></p><?php endif ?>
<?php formStart($action); ?>
<?php if (!in_array($action,['verify','reset'],true)): ?><label><?=t('E-posta adresi','Email address')?><input name="email" type="email" required maxlength="254" autocomplete="email" value="<?=h($_POST['email']??'')?>"></label><?php endif ?>
<?php if (in_array($action,['login','verify','reset'],true)) { passwordField($action==='login'?'current-password':'new-password'); } ?>
<?php if (in_array($action,['verify','reset'],true)): ?><label><?=t('E-postanızdaki doğrulama kodu','Code from your email')?><input name="token" id="email-token" required pattern="[a-f0-9]{64}" maxlength="64" autocomplete="off" value="<?=h($_POST['token']??'')?>"></label><p><?=t('E-postadaki bağlantıyla geldiğinizde bu alan otomatik doldurulur.','This field is filled automatically when you follow the email link.')?></p><?php endif ?>
<?php if ($action==='register'): ?><p><?=t('E-postanızı doğrularken bu hesaba özel bir parola belirleyeceksiniz.','You will choose a unique password when verifying your email.')?></p><label class="member-check"><input type="checkbox" name="terms" value="yes" required> <span><a href="/account/?action=terms" target="_blank" rel="noopener"><?=t('Kullanım koşulları ve gizlilik açıklamasını','Terms and privacy notice')?></a> <?=t('okudum ve kabul ediyorum.','— I have read and accept.')?></span></label><?php endif ?>
<button type="submit"><?= match($action) {'register'=>t('Hesap oluştur','Create account'),'verify'=>t('E-postamı doğrula','Verify my email'),'reset'=>t('Parolayı değiştir','Change password'),'forgot','resend'=>t('E-posta gönder','Send email'),default=>t('Giriş yap','Sign in')} ?></button></form>
<nav class="member-secondary" aria-label="<?=t('Hesap yardımı','Account help')?>"><a href="?action=<?=$action==='register'?'login':'register'?>"><?=$action==='register'?t('Zaten hesabım var','I already have an account'):t('Hesap oluştur','Create an account')?></a><a href="?action=forgot"><?=t('Parolamı unuttum','Forgot password')?></a><a href="?action=resend"><?=t('Doğrulama e-postasını yeniden gönder','Resend verification email')?></a></nav>
<?php endif ?>
</main><footer class="member-footer">CelikPanel · <a href="/"><?=t('Websitesine dön','Back to website')?></a></footer></body></html>
