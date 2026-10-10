// What a domain's mail screens read about the server's mail service
// (`GET /api/v1/domains/{id}/mail/setup`), and its one decoder (see
// lib/remote.ts: every reader of an address shares the decoder and the
// request). The client settings card and the webmail card both read it.
//
// A read that failed is unknown. It is not "webmail is not available on this
// server", which is what the card said before 9 Oct 2026.
//
// Bir alan adının posta ekranlarının sunucunun posta hizmeti hakkında okuduğu
// şey ve onun tek çözücüsü. Başarısız okuma bilinmeyendir; "bu sunucuda webmail
// yok" değildir.

export interface MailProtocol {
    host: string;
    port: number;
    security: string;
    auth_required?: boolean;
}

export interface MailSetup {
    mail_host: string;
    imap: MailProtocol;
    pop3: MailProtocol;
    smtp: MailProtocol;
    username_is_full_email: boolean;
    // Only the panel's fixed public proxy may become a link. Any unavailable,
    // malformed or external value fails closed to null: no link.
    // Yalnız panelin sabit genel vekili bağlantı olabilir.
    webmail: '/webmail/' | null;
}

export const mailSetupURL = (domainId: number) => `/api/v1/domains/${domainId}/mail/setup`;

function protocol(raw: unknown): MailProtocol {
    const value = raw as Partial<MailProtocol> | null;
    if (!value || typeof value.host !== 'string' || typeof value.port !== 'number' || typeof value.security !== 'string') {
        throw new Error('mail setup');
    }
    return value as MailProtocol;
}

export function decodeMailSetup(raw: unknown): MailSetup {
    const setup = raw as Record<string, unknown> | null;
    if (!setup || typeof setup !== 'object' || typeof setup.webmail_available !== 'boolean') throw new Error('mail setup');
    return {
        mail_host: typeof setup.mail_host === 'string' ? setup.mail_host : '',
        imap: protocol(setup.imap),
        pop3: protocol(setup.pop3),
        smtp: protocol(setup.smtp),
        username_is_full_email: setup.username_is_full_email === true,
        webmail: setup.webmail_available === true && setup.webmail_url === '/webmail/' ? setup.webmail_url : null,
    };
}
