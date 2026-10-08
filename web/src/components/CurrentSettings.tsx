import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import type { ApiError } from '../lib/apiError';
import { readRemote } from '../lib/remote';
import { Checking, CouldNotCheck } from './ui';

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
// Since 9 Oct 2026 this is the settings-form face of the app-wide rule in
// lib/remote.ts: the read is readRemote, the reading line is Checking and the
// notice is CouldNotCheck. What stays here is what only a form needs: the
// refusal of a stale write, and the notice that keeps what was typed.
//
// Bir ayar ekranının sunucunun geçerli durumu hakkında bildiği şey: okunuyor
// (form yok, "kapalı" yok), biliniyor (ancak şimdi ayar diye gösterilir ve
// okunduğu sürümle kaydedilebilir), bilinmiyor (okuma başarısız; ekran bunu
// söyler ve Yeniden dene sunar, düzenlenebilir form ya da boş liste göstermez).
// 9 Eki 2026'dan beri bu, lib/remote.ts'teki uygulama geneli kuralın ayar
// formu yüzüdür.
export type Current<T> =
    | { state: 'loading' }
    | { state: 'known'; value: T }
    | { state: 'unknown'; error: ApiError };

// readCurrent never throws: a refused, failed or unreadable answer is unknown.
// readCurrent hata fırlatmaz: reddedilen ya da okunamayan yanıt bilinmeyendir.
export async function readCurrent<T>(url: string): Promise<Current<T>> {
    const next = await readRemote(url, (raw) => raw as T);
    return next.state === 'known' ? { state: 'known', value: next.value } : { state: 'unknown', error: next.reason };
}

// The server refused a write because the settings are no longer the ones this
// page loaded (or the page never loaded them). Nothing was saved.
// Sunucu yazıyı reddetti: ayarlar artık bu sayfanın yüklediği ayarlar değil.
export function isStaleWrite(error: ApiError): boolean {
    return error.code === 'SETTINGS_CHANGED' || error.code === 'SETTINGS_VERSION_REQUIRED';
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
    const { t } = useI18n();
    if (state === 'loading') return <Checking label={t('current.checking')} className="py-1" />;
    if (state === 'unknown') return <CouldNotCheck text={t(unknownKey)} onRetry={onRetry} />;
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
    const { t } = useI18n();
    return <CouldNotCheck className="mb-4" text={t(textKey)} actionLabel={t(actionKey)} onRetry={onReload} busy={busy} />;
}
