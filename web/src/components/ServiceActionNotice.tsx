import { AlertTriangle, XCircle } from 'lucide-react';
import { Fragment, useEffect, useRef } from 'react';
import { useI18n } from '../i18n';
import { apiErrorText, type ApiError } from '../lib/apiError';
import { Button } from './ui';

// What a Start, Stop, Restart or Reload ended with when it is not "done"
// (10 Oct 2026; D-024, cmd/panel/service_action_outcome.go). The Panel answers
// with what was observed on the service, not with the service manager's exit
// status, and there are two answers that are not success:
//
//   - SERVICE_ACTION_FAILED: a verified failure, with the stage as `reason`.
//     Drawn on the failure surface.
//   - SERVICE_ACTION_UNKNOWN: the action was sent and what came of it could
//     not be established. It is not a failure and is not drawn as one: the
//     attention surface, like every other unknown result.
//
// Both carry the unit, the one command the server owner runs, the service's
// own line and, where another unit runs the service, that unit. These were
// toasts that left after five seconds; a sentence with a command to type stays
// until the person closes it or acts again.
//
// Bir Başlat, Durdur, Yeniden başlat ya da Yeniden yükle işleminin "yapıldı"
// olmayan sonucu. İki yanıt başarı değildir: doğrulanmış hata (hata yüzeyi) ve
// bilinmeyen sonuç (hata değildir; dikkat yüzeyi). İkisi de birimi, sunucu
// sahibinin çalıştıracağı tek komutu, hizmetin kendi satırını ve hizmeti başka
// bir birim çalıştırıyorsa o birimi taşır. Beş saniyede kaybolan bir bildirim
// değil: kişi kapatana ya da yeniden işlem yapana dek yerinde durur.
export function isServiceActionOutcome(error: ApiError): boolean {
    return error.code === 'SERVICE_ACTION_FAILED' || error.code === 'SERVICE_ACTION_UNKNOWN';
}

export function ServiceActionNotice({
    outcome,
    onClose,
    className,
}: {
    outcome: ApiError | null;
    onClose: () => void;
    className?: string;
}) {
    const { t } = useI18n();
    const box = useRef<HTMLDivElement>(null);
    useEffect(() => {
        if (outcome) box.current?.scrollIntoView?.({ block: 'nearest' });
    }, [outcome]);
    if (!outcome) return null;
    const unknown = outcome.code === 'SERVICE_ACTION_UNKNOWN';
    const vars = outcome.vars ?? {};
    // The service's line is set apart from the sentence around it, in the
    // face used for what a program printed.
    // Hizmetin satırı, çevresindeki cümleden ayrı ve program çıktısının
    // yazı tipiyle gösterilir.
    const [before, after] = t('services.action.said', { detail: '\u0000' }).split('\u0000');
    // The command is something to type on the server, so it is set apart from
    // the sentence that carries it. A sentence that names no command (the
    // server's own, for a code this page has no words for) gets it on a line
    // of its own.
    // Komut sunucuda yazılacak bir şeydir; onu taşıyan cümleden ayrı
    // gösterilir. Komutu adlandırmayan cümlede komut kendi satırında durur.
    const sentence = apiErrorText({ ...outcome, vars: { ...vars, command: '\u0001' } }, t, 'services.actionFailed').split('\u0001');
    const command = vars.command ? <code className="rounded bg-surface px-1 py-0.5 font-mono text-[0.9em] text-fg">{vars.command}</code> : null;
    return (
        <div
            ref={box}
            role="alert"
            data-service-action={unknown ? 'unknown' : 'failed'}
            className={`flex items-start gap-2 rounded-lg border p-3 text-sm leading-relaxed text-fg ${
                unknown ? 'border-warning-mark/50 bg-warning-mark/20' : 'border-danger/30 bg-danger/10'
            } ${className ?? ''}`}
        >
            {unknown
                ? <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
                : <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-danger" aria-hidden="true" />}
            <div className="min-w-0">
                <p className="max-w-[75ch] break-words">
                    {sentence.map((part, index) => <Fragment key={index}>{index > 0 && command}{part}</Fragment>)}
                </p>
                {sentence.length === 1 && command && <p className="mt-1.5 break-words">{command}</p>}
                {vars.detail && (
                    <p className="mt-1.5 max-w-[75ch] break-words text-fg-muted">
                        {before}<span className="font-mono text-fg">{vars.detail}</span>{after}
                    </p>
                )}
                {vars.owner_unit && vars.owner_unit !== vars.unit && (
                    <p className="mt-1.5 max-w-[75ch] break-words text-fg-muted">
                        {t('services.action.ownerUnit', { owner_unit: vars.owner_unit, unit: vars.unit ?? '' })}
                    </p>
                )}
                <div className="mt-2">
                    <Button type="button" onClick={onClose}>{t('common.close')}</Button>
                </div>
            </div>
        </div>
    );
}
