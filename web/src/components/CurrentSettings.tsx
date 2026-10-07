import { AlertTriangle } from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { readApiError, type ApiError } from '../lib/apiError';
import { Button, Spinner } from './ui';

// What a settings screen knows about the server's current state. The rule the
// three screens that use this follow (8 Oct 2026; D-022, D-024):
//
//   - loading: the screen says it is reading. No form, no "off", no "none".
//   - known:   the server answered. Only now is anything shown as a setting,
//              and only now can it be saved — with the version it was read at.
//   - unknown: the read failed. The screen says so and offers Retry. It shows
//              no editable form and no empty list, because a default on screen
//              is one Save away from replacing what the owner really has.
//
// Kept small and local to those screens; the shape is the one an app-wide
// helper would take.
//
// Bir ayar ekranının sunucunun geçerli durumu hakkında bildiği şey: okunuyor
// (form yok, "kapalı" yok), biliniyor (ancak şimdi ayar diye gösterilir ve
// okunduğu sürümle kaydedilebilir), bilinmiyor (okuma başarısız; ekran bunu
// söyler ve Yeniden dene sunar, düzenlenebilir form ya da boş liste göstermez).
export type Current<T> =
    | { state: 'loading' }
    | { state: 'known'; value: T }
    | { state: 'unknown'; error: ApiError };

// readCurrent never throws: a refused, failed or unreadable answer is unknown.
// readCurrent hata fırlatmaz: reddedilen ya da okunamayan yanıt bilinmeyendir.
export async function readCurrent<T>(url: string): Promise<Current<T>> {
    try {
        const res = await fetch(url);
        if (!res.ok) return { state: 'unknown', error: await readApiError(res) };
        return { state: 'known', value: (await res.json()) as T };
    } catch {
        return { state: 'unknown', error: { message: '' } };
    }
}

// The server refused a write because the settings are no longer the ones this
// page loaded (or the page never loaded them). Nothing was saved.
// Sunucu yazıyı reddetti: ayarlar artık bu sayfanın yüklediği ayarlar değil.
export function isStaleWrite(error: ApiError): boolean {
    return error.code === 'SETTINGS_CHANGED' || error.code === 'SETTINGS_VERSION_REQUIRED';
}

function CurrentChecking() {
    const { t } = useI18n();
    const text = t('current.checking');
    return (
        <div className="flex items-center gap-2 py-1 text-sm text-fg-muted">
            <Spinner size="xs" label={text} />
            <span aria-hidden="true">{text}</span>
        </div>
    );
}

// CurrentGate is what a screen shows in place of its form or list until the
// state is known: the reading line, or "could not load" with Retry.
// CurrentGate, durum bilinene kadar formun ya da listenin yerinde gösterilendir.
export function CurrentGate({
    state,
    unknownKey,
    onRetry,
}: {
    state: Current<unknown>['state'];
    unknownKey: TranslationKey;
    onRetry: () => void;
}) {
    if (state === 'loading') return <CurrentChecking />;
    if (state === 'unknown') return <CurrentNotice textKey={unknownKey} actionKey="common.retry" onAction={onRetry} />;
    return null;
}

// StaleNotice sits above a form or list whose save the server refused because
// the settings changed after they were loaded. What was typed stays below it.
// StaleNotice, kaydı reddedilen formun ya da listenin üstünde durur; yazılan
// altında kalır.
export function StaleNotice({
    textKey,
    actionKey = 'current.reload',
    onReload,
    busy,
}: {
    textKey: TranslationKey;
    actionKey?: TranslationKey;
    onReload: () => void;
    busy?: boolean;
}) {
    return (
        <div className="mb-4">
            <CurrentNotice textKey={textKey} actionKey={actionKey} onAction={onReload} busy={busy} />
        </div>
    );
}

// One notice for both refusals: what happened, that nothing changed, and the
// one action that resumes work. The text is the screen's own sentence.
// İki ret için tek bildirim: ne oldu, hiçbir şeyin değişmediği ve işi sürdüren
// tek eylem.
function CurrentNotice({
    textKey,
    actionKey,
    onAction,
    busy,
}: {
    textKey: TranslationKey;
    actionKey: TranslationKey;
    onAction: () => void;
    busy?: boolean;
}) {
    const { t } = useI18n();
    return (
        <div
            role="alert"
            className="flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-3 text-sm leading-relaxed text-fg"
        >
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
            <div className="min-w-0">
                <p className="max-w-[75ch] break-words">{t(textKey)}</p>
                <Button type="button" className="mt-2" loading={busy} onClick={onAction}>
                    {t(actionKey)}
                </Button>
            </div>
        </div>
    );
}
