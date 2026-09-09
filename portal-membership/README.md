# Central membership application

The public account entry selects a content-addressed application staged outside
`httpdocs`. Member data, sessions, the license signing key and mail credentials
belong in the private directory. They must never enter source control or a portal
archive. Keep the directory mode 0700 and `config.json` mode 0600.

## Mail delivery

For hosts that prohibit PHP's local `mail()` transport, add an `smtp` object to
the private configuration alongside `sender`:

```json
{
  "sender": "accounts@example.com",
  "smtp": {
    "host": "smtp.example.com",
    "port": 465,
    "username": "accounts@example.com",
    "password": "<mailbox password; private configuration only>"
  }
}
```

SMTP always uses implicit TLS with certificate and hostname verification and
authenticated LOGIN. There is no plaintext or local-mail fallback after an SMTP
failure. Debug logging is disabled and callers receive only `mail_unavailable`.
Omitting `smtp` retains the local mail transport for hosts that support it.

PHPMailer 7.1.1 is vendored from the exact upstream commit recorded in
`vendor/phpmailer/UPSTREAM.json`, with its original license and file digests.
The installer needs no Composer or network dependency resolution. When updating
the vendor, retain the license and refresh the upstream manifest deliberately.

`deploy/test-membership.sh` includes isolated TLS SMTP tests, member/service
tests, PHP-to-Go signed-license interoperability and the HTTP account flow.
SMTP tests reject untrusted certificates, incorrect certificate hostnames and
incorrect authentication without exposing credentials. Production acceptance
also requires a real verification message to an authorized recipient: local
`mail()` success alone does not prove delivery.

After staging the new immutable application, publish the account selector using
the generic portal publisher. A same-version site-content update may change only
the existing selector in its exact immutable-release format; signed release
files, installer and other protected portal paths remain byte-identical.
