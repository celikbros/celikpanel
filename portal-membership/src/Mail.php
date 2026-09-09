<?php
declare(strict_types=1);

namespace CelikPanel\Membership;

require_once __DIR__.'/Service.php';
require_once __DIR__.'/../vendor/phpmailer/Exception.php';
require_once __DIR__.'/../vendor/phpmailer/SMTP.php';
require_once __DIR__.'/../vendor/phpmailer/PHPMailer.php';

use PHPMailer\PHPMailer\PHPMailer;

final class Mail
{
    public static function send(array $config, string $recipient, string $subject, string $body): void
    {
        try {
            $sender = $config['sender'] ?? '';
            foreach ([$sender, $recipient] as $address) {
                if (!is_string($address) || !filter_var($address, FILTER_VALIDATE_EMAIL) || preg_match('/[\r\n]/', $address)) {
                    throw new \RuntimeException('Invalid address');
                }
            }
            // Existing self-hosted deployments may keep their local mail transport.
            // A configured SMTP transport never falls back to unauthenticated mail.
            if (!array_key_exists('smtp', $config)) {
                if (!mail($recipient, $subject, $body, ['From'=>'CelikPanel <'.$sender.'>', 'Content-Type'=>'text/plain; charset=UTF-8'])) {
                    throw new \RuntimeException('Local transport failed');
                }
                return;
            }
            $smtp = $config['smtp'];
            if (!is_array($smtp) || !is_string($smtp['host'] ?? null) || strlen($smtp['host']) > 253 ||
                !preg_match('/\A[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?\z/D', $smtp['host']) ||
                !is_int($smtp['port'] ?? null) || $smtp['port'] < 1 || $smtp['port'] > 65535 ||
                !is_string($smtp['username'] ?? null) || !filter_var($smtp['username'], FILTER_VALIDATE_EMAIL) ||
                !is_string($smtp['password'] ?? null) || $smtp['password'] === '' || strlen($smtp['password']) > 1024) {
                throw new \RuntimeException('Invalid SMTP configuration');
            }
            $mail = new PHPMailer(true);
            $mail->isSMTP();
            $mail->Host = $smtp['host'];
            $mail->Port = $smtp['port'];
            $mail->SMTPAuth = true;
            $mail->AuthType = 'LOGIN';
            $mail->Username = $smtp['username'];
            $mail->Password = $smtp['password'];
            $mail->SMTPSecure = PHPMailer::ENCRYPTION_SMTPS;
            $mail->SMTPAutoTLS = false;
            $mail->SMTPOptions = ['ssl'=>['verify_peer'=>true, 'verify_peer_name'=>true, 'allow_self_signed'=>false]];
            $mail->SMTPDebug = 0;
            $mail->Timeout = 10;
            $mail->getSMTPInstance()->Timelimit = 15;
            $mail->CharSet = PHPMailer::CHARSET_UTF8;
            $mail->Encoding = PHPMailer::ENCODING_QUOTED_PRINTABLE;
            $mail->XMailer = '';
            $mail->setFrom($sender, 'CelikPanel');
            $mail->addAddress($recipient);
            $mail->Subject = $subject;
            $mail->Body = $body;
            $mail->send();
        } catch (\Throwable $error) {
            // Neither SMTP diagnostics nor credentials enter responses or logs.
            throw new Problem('mail_unavailable');
        }
    }
}
