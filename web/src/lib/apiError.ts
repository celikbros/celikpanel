import type { TranslationKey } from '../i18n/en';

// The API error contract (B1, Jul 18): every error body is one JSON shape —
// {error, code?, action?}. `code` is a stable machine constant for a
// deliberate refusal; the UI prefers its localized text (`err.<CODE>`) over
// the server's English message. `action` is an in-panel path that fixes the
// refusal; its button label is `err.<CODE>.action`.
//
// API hata sözleşmesi (B1, 18 Tem): her hata gövdesi tek JSON biçimidir —
// {error, code?, action?}. `code` bilinçli bir reddin sabit makine
// sabitidir; UI, sunucunun İngilizce mesajı yerine yerelleştirilmiş metnini
// (`err.<CODE>`) tercih eder. `action` reti düzelten panel-içi yoldur;
// düğme etiketi `err.<CODE>.action` anahtarıdır.

export interface ApiError {
    message: string;
    code?: string;
    action?: string;
    // details: the refusal's evidence, one display line per item — e.g. the
    // sites that block removing a runtime version (B3d). Additive: absent on
    // older responses, safely ignored by older screens.
    // details: retin kanıtı, kalem başına bir görüntü satırı — örn. bir
    // runtime sürümünün kaldırılmasını engelleyen siteler (B3d). Eklemeli:
    // eski cevaplarda yok, eski ekranlar güvenle yok sayar.
    details?: string[];
}

// readApiError tolerates all three generations of error bodies: the coded
// JSON envelope, legacy plain text, and an empty body. This is THE one way
// to read an error response — do not hand-roll res.text()/res.json().
// readApiError üç kuşak hata gövdesini de tolere eder: kodlu JSON zarf,
// eski düz metin ve boş gövde. Hata cevabı okumanın TEK yolu budur —
// elle res.text()/res.json() yazmayın.
export async function readApiError(res: Response): Promise<ApiError> {
    try {
        const text = (await res.text()).trim();
        if (!text) return { message: '' };
        try {
            const d = JSON.parse(text);
            if (d && typeof d === 'object' && ('error' in d || 'code' in d)) {
                return {
                    message: d.error || '',
                    code: d.code,
                    action: d.action,
                    details: Array.isArray(d.details) ? d.details : undefined,
                };
            }
        } catch {
            /* legacy plain text / eski düz metin */
        }
        return { message: text };
    } catch {
        return { message: '' };
    }
}

type T = (key: TranslationKey, vars?: Record<string, string | number>) => string;

// apiErrorText prefers the localized text of a coded refusal and falls back
// to the server message, then to a generic error.
// apiErrorText, kodlu reddin yerelleştirilmiş metnini tercih eder; sunucu
// mesajına, o da yoksa genel hataya düşer.
export function apiErrorText(e: ApiError, t: T, fallbackKey: TranslationKey = 'common.error'): string {
    if (e.code) {
        const key = ('err.' + e.code) as TranslationKey;
        const s = t(key);
        if (s !== key) return s;
    }
    return e.message || t(fallbackKey);
}

// apiErrorActionLabel: the fix-it button's label for a coded refusal that
// carries an action path; '' when there is nothing to render.
// apiErrorActionLabel: action yolu taşıyan kodlu ret için düğme etiketi;
// çizilecek bir şey yoksa ''.
export function apiErrorActionLabel(e: ApiError, t: T): string {
    if (!e.code || !e.action) return '';
    const key = ('err.' + e.code + '.action') as TranslationKey;
    const s = t(key);
    return s !== key ? s : t('common.goFix');
}
