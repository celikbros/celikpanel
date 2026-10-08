import type { ReactNode } from 'react';
import { AlertTriangle } from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { apiErrorText, type ApiError } from '../lib/apiError';
import type { ConfigFile, ConfigFileHandle } from '../lib/configFile';
import { Button, Checking, CouldNotCheck } from './ui';

// What every configuration editor shows around its own fields (9 Oct 2026;
// D-024): the three states of the read, and what a save came to.
//
//   checking    one quiet line. No editor.
//   unknown     "could not be read" with Retry. No editor, no Save: an editor
//               that opens on nothing is one Save from replacing the file.
//   known       the editor, built from the text the server sent.
//
// A save that the server refuses keeps what was typed. The notice says which
// kind of refusal it was: the file changed since it was read (reload), the
// service's own program does not accept it (its line, next to the field), the
// reload failed and the previous file is back (a verified failure), or the
// answer never arrived (unknown: read the file again before anything else).
//
// Her yapılandırma düzenleyicisinin kendi alanlarının çevresinde gösterdiği:
// okumanın üç durumu ve bir kaydın neyle sonuçlandığı. Sunucunun reddettiği
// kayıt, yazılanı ekranda tutar.

export function ConfigFileGate({
    handle,
    file,
    children,
}: {
    handle: ConfigFileHandle;
    /** The file's own name, as the owner knows it: "postgresql.conf". */
    file: string;
    children: (value: ConfigFile) => ReactNode;
}) {
    const { t } = useI18n();
    const { remote } = handle;
    if (remote.state === 'loading') return <Checking label={t('dbconf.checking', { file })} className="min-h-[2.75rem] py-2" />;
    if (remote.state === 'unknown') {
        return <CouldNotCheck text={t('dbconf.unknown', { file })} onRetry={() => void handle.reload()} busy={handle.reading} />;
    }
    return <>{children(remote.value)}</>;
}

// The card an editor of one configuration file stands in keeps one least
// height in every state: being read, could not be read, no such file, and the
// editor itself. Seen in a browser on 9 Oct 2026: the card was one line while
// the file was read and then the whole editor, and everything under it (the
// raw files, the overview, the log) was pushed down the page by several
// hundred pixels. The editor's own height depends on the file, so the card
// takes the rest of the window instead: what stands under it starts below the
// fold in every state, and nothing on screen changes place when the file
// arrives.
// Bir yapılandırma dosyasının düzenleyicisinin durduğu kart, her durumda tek
// bir en az yüksekliği korur: okunurken, okunamadığında, dosya yokken ve
// düzenleyicinin kendisinde. Kart pencerenin kalanını alır; altındaki her şey
// her durumda görünür alanın altında başlar ve dosya geldiğinde ekranda hiçbir
// şey yer değiştirmez.
export const editorCardHeight = 'min-h-[max(20rem,calc(100dvh-17rem))]';

/** The line the service's own program said, bounded by the server. */
export const refusalDetail = (refusal: ApiError | null) => refusal?.vars?.detail ?? '';

/** The line of the file a refusal is about (0-based), or -1. */
export const refusalLine = (refusal: ApiError | null) => {
    const line = Number(refusal?.vars?.line);
    return refusal?.code === 'CONFIG_INVALID' && Number.isInteger(line) && line > 0 ? line - 1 : -1;
};

/** The setting a refusal names, or ''. */
export const refusalName = (refusal: ApiError | null) =>
    (refusal?.code === 'CONFIG_INVALID' && refusal.reason === 'daemon' ? refusal.vars?.name ?? '' : '');

// FieldRefusal is the service's message next to the field it is about.
// FieldRefusal, hizmetin iletisini ilgili alanın yanında gösterir.
export function FieldRefusal({ id, service, refusal }: { id: string; service: string; refusal: ApiError }) {
    const { t } = useI18n();
    const detail = refusalDetail(refusal);
    return (
        // Tied to its field by aria-describedby; the notice above the form is
        // the one announcement of the refusal.
        // Alanına aria-describedby ile bağlıdır; reddin tek duyurusu formun
        // üstündeki bildirimdir.
        <p id={id} className="mt-1 break-words text-xs leading-relaxed text-danger">
            {refusal.reason === 'daemon' ? t('dbconf.says', { service }) : t('dbconf.notAccepted')}{' '}
            <span className="font-mono">{detail}</span>
        </p>
    );
}

function refusalSentence(refusal: ApiError, service: string, t: ReturnType<typeof useI18n>['t']): { text: string; failure: boolean; unknown: boolean } {
    const vars = { service, name: refusal.vars?.name ?? '' };
    if (refusal.code === 'CONFIG_INVALID') {
        const key = `dbconf.refused.${refusal.reason ?? ''}` as TranslationKey;
        const text = t(key, vars);
        return { text: text === key ? t('dbconf.refused.other', vars) : text, failure: false, unknown: false };
    }
    if (refusal.code === 'CONFIG_RELOAD_FAILED') {
        return {
            text: t(refusal.reason === 'restored' ? 'dbconf.reloadFailed.restored' : 'dbconf.reloadFailed.notRestored', vars),
            failure: true,
            unknown: false,
        };
    }
    // A coded refusal the server explained itself, else an answer that never
    // arrived: whether the file changed is then not known.
    // Sunucunun açıkladığı kodlu bir ret, yoksa hiç gelmeyen bir yanıt.
    if (refusal.code) return { text: apiErrorText(refusal, t), failure: false, unknown: false };
    return { text: t('dbconf.saveUnknown'), failure: false, unknown: true };
}

export function ConfigSaveNotices({
    handle,
    file,
    service,
    placed,
}: {
    handle: ConfigFileHandle;
    file: string;
    /** The service's name as the owner knows it: "PostgreSQL". */
    service: string;
    /** true when the refusal's line is already shown next to a field. */
    placed: boolean;
}) {
    const { t } = useI18n();
    const { stale, refusal, saved } = handle;
    if (stale) {
        return (
            <CouldNotCheck
                className="mb-4"
                text={t('dbconf.stale', { file })}
                actionLabel={t('dbconf.reload')}
                onRetry={() => void handle.reload()}
                busy={handle.reading}
            />
        );
    }
    if (refusal) {
        const { text, failure, unknown } = refusalSentence(refusal, service, t);
        const detail = refusalDetail(refusal);
        const line = refusalLine(refusal);
        return (
            <div
                role="alert"
                className={`mb-4 flex items-start gap-2 rounded-lg border p-3 text-sm leading-relaxed ${
                    failure ? 'border-danger/30 bg-danger/10 text-fg' : 'border-warning-mark/50 bg-warning-mark/20 text-fg'
                }`}
            >
                <AlertTriangle className={`mt-0.5 h-4 w-4 shrink-0 ${failure ? 'text-danger' : 'text-warning'}`} aria-hidden="true" />
                <div className="min-w-0">
                    <p className="max-w-[75ch] break-words">{text}</p>
                    {detail && (!placed || failure) && (
                        <p className="mt-1.5 max-w-[75ch] break-words text-fg-muted">
                            {failure || refusal.reason === 'daemon' ? t('dbconf.says', { service }) : t('dbconf.notAccepted')}{' '}
                            {line >= 0 && !failure && <span>{t('dbconf.atLine', { line: line + 1 })} </span>}
                            <span className="font-mono text-fg">{detail}</span>
                        </p>
                    )}
                    {refusal.code === 'CONFIG_RELOAD_FAILED' && refusal.reason !== 'restored' && refusal.vars?.name && (
                        <p className="mt-1.5 break-all font-mono text-xs text-fg">{refusal.vars.name}</p>
                    )}
                    {unknown && (
                        <Button type="button" className="mt-2" loading={handle.reading} onClick={() => void handle.reload()}>
                            {t('dbconf.reload')}
                        </Button>
                    )}
                </div>
            </div>
        );
    }
    if (saved) {
        const lines: string[] = [];
        if (saved.unchanged) lines.push(t('dbconf.saved.unchanged'));
        else if (saved.applied === 'reloaded') lines.push(t('dbconf.saved.reloaded', { service }));
        else if (saved.applied === 'restart_required') lines.push(t('dbconf.saved.restartRequired', { service }));
        else if (saved.applied === 'not_running') lines.push(t('dbconf.saved.notRunning', { service }));
        else lines.push(t('dbconf.saved.written'));
        if (saved.restartRequired.length > 0) lines.push(t('dbconf.saved.waitsForRestart', { service, names: saved.restartRequired.join(', ') }));
        if (!saved.unchanged && saved.applied === 'reloaded' && saved.daemonCheck === 'not_checked') lines.push(t('dbconf.saved.notChecked', { service }));
        return (
            <div role="status" className="mb-4 rounded-lg border border-border bg-surface-2/50 p-3 text-sm leading-relaxed text-fg">
                {lines.map((line, index) => (
                    <p key={line} className={`max-w-[75ch] break-words ${index > 0 ? 'mt-1.5 text-fg-muted' : 'font-medium'}`}>{line}</p>
                ))}
                {saved.backup && (
                    <p className="mt-1.5 text-fg-muted">
                        {t('dbconf.saved.backup')} <span className="break-all font-mono text-xs text-fg">{saved.backup}</span>
                    </p>
                )}
            </div>
        );
    }
    return null;
}

// The row under an editor: what will be saved, and the one primary action.
// Save is enabled only for a file that is known, changed and not stale.
// Düzenleyicinin altındaki satır: neyin kaydedileceği ve tek birincil eylem.
export function ConfigSaveBar({
    handle,
    changes,
    blockedBy,
    onSave,
    onDiscard,
}: {
    handle: ConfigFileHandle;
    changes: number;
    /** Why Save is off although something changed, shown beside it. */
    blockedBy?: string;
    onSave: () => void;
    onDiscard: () => void;
}) {
    const { t } = useI18n();
    const ready = handle.remote.state === 'known' && changes > 0 && !handle.stale && !blockedBy;
    return (
        <div className="sticky bottom-0 -mx-5 mt-5 flex flex-wrap items-center justify-end gap-x-3 gap-y-2 border-t border-border bg-surface px-5 py-3">
            <p className="min-w-0 basis-full text-xs text-fg-muted sm:mr-auto sm:basis-auto" aria-live="polite">
                {blockedBy || (changes > 0 ? t('dbconf.changes', { n: changes }) : t('dbconf.noChanges'))}
            </p>
            <Button type="button" onClick={onDiscard} disabled={changes === 0 || handle.saving}>
                {t('dbconf.discard')}
            </Button>
            <Button type="button" variant="primary" onClick={onSave} disabled={!ready} loading={handle.saving}>
                {handle.saving ? t('dbconf.saving') : t('dbconf.save')}
            </Button>
        </div>
    );
}
