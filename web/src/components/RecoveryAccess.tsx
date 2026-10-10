import { useCallback, useEffect, useRef, useState } from 'react';
import type { CurrentUser } from '../lib/api';
import { useI18n } from '../i18n';
import { BrandMark } from './BrandMark';
import { LanguageSwitcher } from './LanguageSwitcher';
import { Button, Spinner } from './ui';
import { AddressLink } from './AddressLink';
import { parseRecoveryObservation, reconcileRecoveryObservation, recoveryFailureGuidanceKey, retryingCauseKey, savedRecoveryFinished, savedRecoveryRequestId, UPDATE_MARKER_KEY, type RecoveryObservation } from '../lib/recoveryObservation';
import { handoverAddress, recoveryHandover, savedSetupHandoverHost, setupStartMarkerKey } from '../lib/panelHandover';
import { readRemote } from '../lib/remote';
import { useAccessWaitStage, useQuietRead } from '../lib/quietRead';
import type { AccessReadFailure } from '../auth/usePanelSession';

/** The interface still loading after this long: the reload is offered (as in AccessHold). */
const WAITING_PROLONGED_MS = 30000;

// The address the Panel serves its certificate for. Anything else is not the
// contract, and the handover then stays unnamed.
const decodeServedHost = (raw: unknown): unknown => {
    if (!raw || typeof raw !== 'object' || !('hostname' in raw)) throw new Error('access address');
    return (raw as { hostname: unknown }).hostname;
};

// Planned certificate handover during setup (2026-10-08). The page names it
// only when the server reports a managed certificate for the host that the
// setup started in this browser was securing. One public, read-only request
// per access check; a failed read leaves the ordinary unknown wording.
// Sunucu, bu tarayicida baslatilan kurulumun guvenceye aldigi ad icin yonetilen
// sertifika bildirdiginde planli devir anlatilir; okuma basarisizsa metin degismez.
export function usePanelHandover(username: string | undefined, enabled: boolean, checking: boolean) {
    const [handover, setHandover] = useState<{ host: string; elsewhere: boolean } | null>(null);
    useEffect(() => {
        if (!enabled || !username) { setHandover(null); return; }
        let saved: string | null = null;
        try { saved = savedSetupHandoverHost(localStorage.getItem(setupStartMarkerKey(username))); } catch { saved = null; }
        if (!saved) { setHandover(null); return; }
        const controller = new AbortController();
        const timeout = window.setTimeout(() => controller.abort(), 5000);
        // An address that could not be read names no handover: the ordinary wording stays.
        void readRemote('/api/v1/panel/access-address', decodeServedHost, undefined, { cache: 'no-store', signal: controller.signal })
            .then(result => { if (result.state === 'known' && !controller.signal.aborted) setHandover(recoveryHandover(saved, result.value, window.location.hostname)); })
            .finally(() => window.clearTimeout(timeout));
        return () => { controller.abort(); window.clearTimeout(timeout); };
    }, [username, enabled, checking]);
    return enabled ? handover : null;
}

// An access or readiness check is not explained by an update that finished long
// ago (owner report, 2026-10-08). With unfinishedOnly the block is drawn only for
// a saved operation that is still running, failed or waiting for the owner, or
// whose result cannot be read. A verified update, or no saved operation, draws
// nothing and reads nothing. The saved browser record only decides whether to
// read; it is never shown as a server result.
// Erisim kontrolu, gunler once biten bir guncellemeyle aciklanmaz. unfinishedOnly
// ile blok yalnizca suren, basarisiz olan, sahibini bekleyen ya da sonucu
// okunamayan kayitli islem icin cizilir.
export function RecoveryStatus({ username, onUnauthorized, embedded = false, unfinishedOnly = false, disclosed = false, heading: Heading = 'h2' }: {
    username: string; onUnauthorized?: () => void; embedded?: boolean; unfinishedOnly?: boolean;
    /** Closed under its own title: available, but not presented as the reason for the page. */
    disclosed?: boolean; heading?: 'h2' | 'h4';
}) {
    const { t, locale } = useI18n();
    const [requestId, setRequestId] = useState<string | null>(() => { try { return savedRecoveryRequestId(localStorage.getItem(UPDATE_MARKER_KEY)); } catch { return null; } });
    const [savedFinished] = useState(() => { try { return unfinishedOnly && savedRecoveryFinished(localStorage.getItem(UPDATE_MARKER_KEY)); } catch { return false; } });
    const [last, setLast] = useState<RecoveryObservation | null>(null);
    const lastRef = useRef<RecoveryObservation | null>(null);
    const [unavailable, setUnavailable] = useState(false);
    const [busy, setBusy] = useState(false);
    const pending = useRef<AbortController | null>(null);
    const check = useCallback(async () => {
        if (!requestId || savedFinished || pending.current) return;
        const request = new AbortController(); pending.current = request; setBusy(true);
        const timeout = window.setTimeout(() => request.abort(), 15000);
        try {
            // A refused, dropped or unreadable answer is unknown: the last verified observation stays, marked as such.
            const result = await readRemote(`/api/v1/recovery/status?request_id=${requestId}`, raw => parseRecoveryObservation(raw, requestId),
                undefined, { signal: request.signal, cache: 'no-store' });
            if (pending.current !== request) return;
            if (result.state !== 'known') {
                if (result.status === 401) { lastRef.current = null; setLast(null); onUnauthorized?.(); }
                setUnavailable(true);
            } else if (!request.signal.aborted) {
                const merged = reconcileRecoveryObservation(lastRef.current, result.value);
                lastRef.current = merged.record; setLast(merged.record); setUnavailable(merged.unavailable);
            }
        } finally { window.clearTimeout(timeout); if (pending.current === request) { pending.current = null; setBusy(false); } }
    }, [requestId, savedFinished, username, onUnauthorized]);
    useEffect(() => {
        lastRef.current = null; setLast(null); setUnavailable(false); void check();
        const refresh = () => { if (document.visibilityState === 'visible') void check(); };
        const timer = window.setInterval(refresh, 5000); window.addEventListener('focus', refresh);
        return () => { pending.current?.abort(); pending.current = null; window.clearInterval(timer); window.removeEventListener('focus', refresh); };
    }, [check]);
    useEffect(() => {
        const changed = (event: StorageEvent) => { if (event.key === UPDATE_MARKER_KEY) setRequestId(current => current ?? savedRecoveryRequestId(event.newValue)); };
        window.addEventListener('storage', changed); return () => window.removeEventListener('storage', changed);
    }, []);
    if (unfinishedOnly && (!requestId || (last ? last.terminal_proof === 'update_verified' : savedFinished))) return null;
    const status = <section className={embedded || disclosed ? 'mt-2' : 'mt-8 border-t border-border pt-6'} aria-labelledby="recovery-operation-heading">
        <Heading id="recovery-operation-heading" className={embedded || disclosed ? 'sr-only' : 'text-lg font-semibold'}>{t('recovery.operationTitle')}</Heading>
        {!requestId ? <p className="mt-3 max-w-prose text-sm text-fg-muted">{t('recovery.noOperation')}</p> : <>
            <p className="mt-3 break-all text-sm text-fg-muted">{t('recovery.operationId')}: <span className="font-mono">{requestId}</span></p>
            <div className="mt-4 space-y-3 text-sm" role="status" aria-live="polite">
                {(unavailable || !last) && <p>{t(busy && !unavailable && !last ? 'recovery.checking' : 'recovery.observationUnavailable')}</p>}
                {last?.phase && (last.automatic_recovery === 'retry_scheduled' || last.automatic_recovery === 'pause_pending' ? <>
                    {/* Attempts remain, or the last one is finishing: the owner is not asked to act before the pause. */}
                    <p className="font-semibold">{t(last.automatic_recovery === 'pause_pending' ? 'recovery.automatic.pausingTitle' : 'recovery.automatic.retryTitle')}</p>
                    {retryingCauseKey(last.first_failure_code) && <p className="max-w-prose">{t(retryingCauseKey(last.first_failure_code)!)}</p>}
                    <p className="max-w-prose text-fg-muted">{t(last.automatic_recovery === 'pause_pending' ? 'recovery.automatic.pausingHelp' : 'recovery.automatic.retryHelp')}</p>
                </> : <><p className="font-semibold">{t(last.automatic_recovery ? 'recovery.automatic.pausedTitle' : last.waiting_for ? `recovery.wait.${last.waiting_for}` : `recovery.phase.${last.phase}`)}</p>{last.automatic_recovery && last.first_failure_code && <p className="max-w-prose">{t(`recovery.automatic.cause.${last.first_failure_code}`)}</p>}<p className="max-w-prose text-fg-muted">{t(last.automatic_recovery ? 'recovery.automatic.pausedHelp' : last.waiting_for ? 'recovery.wait.next' : recoveryFailureGuidanceKey(last) ?? `recovery.next.${last.phase}`)}</p>{last.automatic_recovery && <p className="max-w-prose text-fg-muted">{t(last.renewal_before_update === 'off' ? 'recovery.automatic.renewalOff' : 'recovery.automatic.renewal')}</p>}</>)}
                {last?.automatic_recovery === 'paused_retry_limit' && <div className="space-y-3">
                    <p className="max-w-prose">{t('recovery.automatic.inspect')}</p>
                    <pre className="whitespace-pre-wrap break-words rounded border border-border bg-surface px-3 py-3 text-sm"><code>sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50</code></pre>
                    <p className="max-w-prose text-fg-muted">{t('recovery.automatic.resume')}</p>
                </div>}
                {/* upd4 O9: a verified update shows no failure label for its earlier attempts. */}
                {last?.previous_failure && last.terminal_proof !== 'update_verified' && <p>{t('recovery.previousFailure')}: {t(`recovery.reason.${last.failure_code ?? last.previous_failure}`)}</p>}
                {last?.observed_at && <p className="text-fg-muted">{t('recovery.observedAt')}: <time dateTime={last.observed_at}>{new Date(last.observed_at).toLocaleString(locale === 'tr' ? 'tr-TR' : 'en-US')}</time></p>}
            </div>
            <Button className="mt-4" variant="secondary" disabled={busy} onClick={() => void check()}>{t(busy ? 'recovery.checking' : 'recovery.checkStatus')}</Button>
        </>}
    </section>;
    return disclosed ? <details className="mt-8 border-t border-border pt-6 text-sm"><summary className="cursor-pointer font-semibold text-primary">{t('recovery.operationTitle')}</summary>{status}</details> : status;
}

/** Eager shell: no lazy screen catalogue, router, update provider, or mutation API. */
export function RecoveryAccess({ user, cause, checking = false, failure = null, onRetry, onUnauthorized }: {
    /**
     * checking: the first session or readiness read is still in flight. loading: the session and readiness are
     * confirmed and the interface itself is still being fetched. Nothing has failed in either, so nothing is
     * reported as failed, and nothing at all is drawn before the quiet time (lib/quietRead.ts).
     */
    user?: CurrentUser | null; cause: 'checking' | 'loading' | 'auth' | 'starting' | 'availability' | 'license' | 'bundle';
    checking?: boolean;
    /** What the last access read that answered with a failure said; named under a known negative. */
    failure?: AccessReadFailure | null;
    onRetry: () => void; onUnauthorized?: () => void;
}) {
    const { t } = useI18n();
    // The first wait of this page: no read has answered yet, or only the interface is still on its way.
    const firstWait = cause === 'checking' || cause === 'loading';
    const waiting = firstWait || (checking && !user);
    const quiet = useQuietRead(firstWait);
    const [loadingProlonged, setLoadingProlonged] = useState(false);
    useEffect(() => {
        if (cause !== 'loading' || quiet) { setLoadingProlonged(false); return; }
        const timer = window.setTimeout(() => setLoadingProlonged(true), WAITING_PROLONGED_MS);
        return () => window.clearTimeout(timer);
    }, [cause, quiet]);
    // An access read that has not answered: explained after the quiet time, "Check now" after 15 s, the reload
    // beside it after 30 s. Counted from the page's first unanswered read; a re-read does not restart it.
    const stage = useAccessWaitStage(cause === 'checking' && !quiet);
    // The automatic re-read repeats by itself; only a check the owner asked for is drawn as busy (as in AccessHold).
    const [asked, setAsked] = useState(false);
    useEffect(() => { if (!checking) setAsked(false); }, [checking]);
    const handover = usePanelHandover(user?.username, cause === 'availability' || cause === 'starting', checking);
    const address = handover?.elsewhere ? handoverAddress(handover.host, window.location.port) : '';
    // Before the quiet time only the page background: no sentence, no button, no spinner. A read that answers
    // in time leaves nothing behind; one that answers without confirming access replaces this at once.
    if (quiet) return <div className="min-h-screen bg-bg" aria-busy="true" data-access-quiet="" />;
    const header = <header className="border-b border-border bg-surface px-4 py-5 sm:px-8"><div className="mx-auto flex max-w-3xl items-center justify-between gap-4"><div className="flex items-center gap-3 font-semibold"><BrandMark className="h-7 w-7 text-primary" />CelikPanel</div><LanguageSwitcher /></div></header>;
    if (firstWait) {
        // The wait outlasted the quiet time: what is awaited, that nobody needs to act, and how it ends. As in the
        // hold layer over a mounted page, the reload is offered only once the wait has lasted half a minute.
        const opening = cause === 'loading';
        const prolonged = opening ? loadingProlonged : stage === 'prolonged';
        const long = !opening && stage !== 'waiting';
        const busy = checking && (!long || asked);
        return <div className="min-h-screen bg-bg text-fg">
            {header}
            <main className="mx-auto max-w-3xl px-4 py-10 sm:px-8 sm:py-16">
                {user && <p className="mb-5 break-words text-sm text-fg-muted">{user.username}</p>}
                <h1 className="text-2xl font-semibold">{t(opening ? 'recovery.loadingTitle' : 'recovery.checkingTitle')}</h1>
                <p className="mt-4 max-w-prose break-words text-sm leading-relaxed text-fg-muted" role="status">{t(opening ? 'recovery.loadingHelp' : long ? 'recovery.waitingLong' : 'recovery.waitingHelp')}</p>
                {prolonged && <p className="mt-4 max-w-prose text-sm leading-relaxed text-fg-muted">{t(opening ? 'recovery.waitingProlongedLoading' : 'recovery.waitingProlonged')}</p>}
                {(!opening || prolonged) && <div className="mt-6 flex flex-wrap items-center gap-3">
                    {!opening && <Button disabled={busy} onClick={() => { setAsked(true); onRetry(); }}>{busy && <Spinner />}{t(busy ? 'recovery.checking' : long ? 'recovery.checkNow' : 'recovery.retry')}</Button>}
                    {prolonged && <Button variant="secondary" onClick={() => window.location.reload()}>{t('app.reload')}</Button>}
                </div>}
            </main>
        </div>;
    }
    return <div className="min-h-screen bg-bg text-fg">
        {header}
        <main className="mx-auto max-w-3xl px-4 py-10 sm:px-8 sm:py-16">
            {user && <p className="mb-5 break-words text-sm text-fg-muted">{user.username}</p>}
            <h1 className="text-2xl font-semibold">{t(waiting ? 'recovery.checkingTitle' : handover ? 'recovery.handoverTitle' : `recovery.${cause}Title`)}</h1>
            <p className="mt-4 max-w-prose break-words text-sm leading-relaxed text-fg-muted" role="status">{waiting ? t('recovery.checkingHelp') : handover ? t('recovery.handoverHelp', { host: handover.host }) : t(`recovery.${cause}Help`)}</p>
            {/* An answered failure names what was read; a check in flight or the planned handover does not. */}
            {!waiting && !handover && failure && <p className="mt-4 max-w-prose break-words text-sm leading-relaxed text-fg-muted">{failure.kind === 'status' ? t('recovery.failure.status', { status: String(failure.status) }) : t(`recovery.failure.${failure.kind}`)}</p>}
            {address && <p className="mt-4 max-w-prose text-sm leading-relaxed text-fg-muted">{t('recovery.handoverAddress')} <AddressLink href={`${address}/setup`} address={address} /></p>}
            <div className="mt-6 flex flex-wrap items-center gap-3"><Button disabled={checking} onClick={onRetry}>{checking && <Spinner />}{t(checking ? 'recovery.checking' : 'recovery.retry')}</Button><Button variant="secondary" onClick={() => window.location.reload()}>{t('app.reload')}</Button></div>
            {/* A finished update is not the reason for an access check, so only an unfinished operation is drawn here.
                During the planned handover even that stays one step away. A page that failed to load keeps the full reader. */}
            {user?.effective_role === 'admin' && <RecoveryStatus key={user.username} username={user.username}
                onUnauthorized={onUnauthorized} unfinishedOnly={cause !== 'bundle'} disclosed={!!handover} />}
        </main>
    </div>;
}
