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
    // reason refines code. The server works out which of several things caused
    // a coded refusal and says so; a screen that has words for that reason uses
    // them, and one that does not falls back to the sentence for the code,
    // which is what every screen did before this existed.
    // reason, code'u inceltir. O gerekçe için sözü olan ekran onu kullanır;
    // olmayan, kodun cümlesine döner.
    reason?: string;
    // These flags are proof-bearing outcome fields, not synonyms. In
    // particular, partial_success alone must never be treated as proof that a
    // host mutation happened; only mutation_applied === true carries that
    // meaning.
    partialSuccess?: boolean;
    mutationApplied?: boolean;
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
                    reason: typeof d.reason === 'string' && d.reason ? d.reason : undefined,
                    partialSuccess: d.partial_success === true ? true : undefined,
                    mutationApplied: d.mutation_applied === true ? true : undefined,
                    details: Array.isArray(d.details)
                        ? d.details.filter((item: unknown): item is string => typeof item === 'string')
                        : undefined,
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
        // The refined sentence first: a refusal that named which of several
        // causes it was deserves the words for that cause, not the words that
        // cover all of them. R-068 is the case this was written for - "another
        // server change or package-manager task" was one sentence for three
        // situations, one of which does not end by waiting.
        // Önce inceltilmiş cümle: hangi sebep olduğunu adlandıran bir ret, o
        // sebebin sözlerini hak eder; hepsini kapsayanları değil.
        if (e.reason) {
            const refined = ('err.' + e.code + '.' + e.reason) as TranslationKey;
            const named = t(refined);
            if (named !== refined) return named;
        }
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
