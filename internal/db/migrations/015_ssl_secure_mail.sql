-- Whether a domain's certificate also secures its mail (SMTP/IMAP/POP via
-- SNI) — the panel-side switch behind "secure mail with this certificate".
-- Bir domain'in sertifikasının postasını da (SNI ile SMTP/IMAP/POP) koruyup
-- korumadığı — "maili bu sertifikayla koru" anahtarının panel tarafı.
ALTER TABLE ssl_certificates ADD COLUMN secure_mail INTEGER DEFAULT 0;
