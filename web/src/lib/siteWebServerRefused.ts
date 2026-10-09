import type { TranslationKey } from '../i18n/en';
import type { ApiError } from './apiError';

// A site that was not created because the web server refused the configuration
// CelikPanel generated for it (12 Oct 2026; D-024,
// cmd/panel/domain_web_server_refused.go). The Panel answers
// SITE_WEB_SERVER_REFUSED with what was verified as `reason`:
//
//   - `removed` / `import_removed`: what had been created for the site was
//     removed again and that removal was confirmed;
//   - `cleanup_unconfirmed` / `import_cleanup_unconfirmed`: it was not
//     confirmed, and the sentence says where to look.
//
// The import's two also say that nothing of the archive was imported. The
// sentences live with the screens' copy, not in the boot copy: they are read
// on the two screens that create a site. A reason this build has no words for
// keeps the server's own sentence. nginx's own line travels in `details`, for
// an administrator, and is drawn under the sentence by ErrorBanner.
//
// Web sunucusu CelikPanel'in ürettiği yapılandırmayı reddettiği için
// oluşturulmayan site. Panel, neyin doğrulandığını `reason` ile söyler. Bu
// derlemenin sözü olmayan bir gerekçe, sunucunun kendi cümlesini korur.
const sentences: Record<string, TranslationKey> = {
    removed: 'domains.add.webServerRefused.removed',
    cleanup_unconfirmed: 'domains.add.webServerRefused.unconfirmed',
    import_removed: 'import.webServerRefused.removed',
    import_cleanup_unconfirmed: 'import.webServerRefused.unconfirmed',
};

type Say = (key: TranslationKey, vars?: Record<string, string | number>) => string;

export function isSiteWebServerRefused(error: ApiError | null): boolean {
    return error?.code === 'SITE_WEB_SERVER_REFUSED';
}

// The refusal with its sentence in the page's language. The code, the reason
// and the details are kept, so the one renderer of refusals draws it.
export function siteWebServerRefusedIn(error: ApiError, t: Say): ApiError {
    const key = sentences[error.reason ?? ''];
    if (!isSiteWebServerRefused(error) || !key) return error;
    return { ...error, message: t(key, { domain: error.vars?.domain ?? '', command: error.vars?.command ?? 'sudo nginx -t' }) };
}
